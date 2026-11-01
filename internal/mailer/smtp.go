package mailer

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"secret-santa/internal/config"
)

type SMTPMailer struct {
	host    string
	port    int
	user    string
	pass    string
	from    string
	tlsMode string
	// helloHost is announced in EHLO (host of APP_BASE_URL).
	helloHost string

	// batchBudget caps the total time SendBatch may spend; messages not sent
	// within it fail (and can be resent) so a slow server cannot stall a
	// request for minutes. Zero means defaultBatchBudget.
	batchBudget time.Duration
	// rootCAs overrides the system roots when verifying the server (tests).
	rootCAs *x509.CertPool
}

const defaultBatchBudget = 90 * time.Second

// ErrBatchBudgetExceeded is returned for messages not attempted because the
// batch time budget ran out.
var ErrBatchBudgetExceeded = errors.New("smtp batch time budget exceeded, resend later")

func NewSMTPMailer(cfg *config.Config) *SMTPMailer {
	helloHost := "localhost"
	if u, err := url.Parse(cfg.AppBaseURL); err == nil && u.Hostname() != "" {
		helloHost = u.Hostname()
	}
	return &SMTPMailer{
		helloHost: helloHost,
		host:      cfg.SMTPHost,
		port:      cfg.SMTPPort,
		user:      cfg.SMTPUser,
		pass:      cfg.SMTPPass,
		from:      cfg.SMTPFrom,
		tlsMode:   cfg.SMTPTLSMode,
	}
}

func (s *SMTPMailer) Send(msg Message) error {
	errs := s.SendBatch([]Message{msg})
	return errs[0]
}

func (s *SMTPMailer) SendBatch(msgs []Message) []error {
	errs := make([]error, len(msgs))
	if len(msgs) == 0 {
		return errs
	}

	fromAddr, err := parseEnvelopeAddress(s.from)
	if err != nil {
		for i := range errs {
			errs[i] = err
		}
		return errs
	}

	client, conn, err := s.dialClient()
	if err != nil {
		for i := range errs {
			errs[i] = err
		}
		return errs
	}
	defer func() {
		_ = client.Quit()
		_ = conn.Close()
	}()

	budget := s.batchBudget
	if budget <= 0 {
		budget = defaultBatchBudget
	}
	budgetEnd := time.Now().Add(budget)

	for i, msg := range msgs {
		if time.Now().After(budgetEnd) {
			errs[i] = ErrBatchBudgetExceeded
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		toAddr, parseErr := parseEnvelopeAddress(msg.To)
		if parseErr != nil {
			errs[i] = parseErr
			continue
		}
		if err := sendOnClient(client, s.from, fromAddr, toAddr, msg); err != nil {
			errs[i] = err
			_ = client.Reset()
		}
	}

	return errs
}

func (s *SMTPMailer) dialClient() (*smtp.Client, net.Conn, error) {
	addr := net.JoinHostPort(s.host, strconv.Itoa(s.port))
	dialer := &net.Dialer{Timeout: 5 * time.Second}

	var conn net.Conn
	var err error
	tlsCfg := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12, RootCAs: s.rootCAs}

	if s.tlsMode == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("smtp dial %s: %w", addr, err)
	}

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("smtp client init: %w", err)
	}

	if err := client.Hello(s.helloHost); err != nil {
		_ = client.Close()
		_ = conn.Close()
		return nil, nil, fmt.Errorf("smtp hello: %w", err)
	}

	if s.tlsMode == "starttls" {
		if err := client.StartTLS(tlsCfg); err != nil {
			_ = client.Close()
			_ = conn.Close()
			return nil, nil, fmt.Errorf("smtp starttls: %w", err)
		}
	}

	if s.user != "" {
		auth := smtp.PlainAuth("", s.user, s.pass, s.host)
		if err := client.Auth(auth); err != nil {
			_ = client.Close()
			_ = conn.Close()
			return nil, nil, fmt.Errorf("smtp auth: %w", err)
		}
	}

	return client, conn, nil
}

func sendOnClient(client *smtp.Client, fromHeader, fromAddr, toAddr string, msg Message) error {
	if err := client.Mail(fromAddr); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := client.Rcpt(toAddr); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}

	headers, err := buildHeaders(fromHeader, toAddr, msg)
	if err != nil {
		_ = w.Close()
		return err
	}
	if _, err := io.WriteString(w, headers); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write body: %w", err)
	}
	qp := quotedprintable.NewWriter(w)
	if _, err := io.WriteString(qp, msg.Body+"\n"); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := qp.Close(); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return nil
}

func stripCRLF(s string, repl string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", repl)
}

// buildHeaders returns the RFC 5322 header block, ending with the blank line.
func buildHeaders(fromHeader, toAddr string, msg Message) (string, error) {
	from, err := mail.ParseAddress(stripCRLF(fromHeader, " "))
	if err != nil {
		return "", fmt.Errorf("invalid from address %q: %w", fromHeader, err)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("message id: %w", err)
	}
	domain := from.Address[strings.LastIndex(from.Address, "@")+1:]
	var sb strings.Builder
	h := func(k, v string) { sb.WriteString(k + ": " + v + "\r\n") }
	h("Date", time.Now().UTC().Format(time.RFC1123Z))
	h("Message-ID", "<"+hex.EncodeToString(id[:])+"@"+domain+">")
	h("From", from.String())
	h("To", stripCRLF(toAddr, ""))
	if msg.ReplyTo != "" {
		replyTo, err := mail.ParseAddress(stripCRLF(msg.ReplyTo, " "))
		if err != nil {
			return "", fmt.Errorf("invalid reply-to %q: %w", msg.ReplyTo, err)
		}
		h("Reply-To", replyTo.String())
	}
	h("Subject", mime.QEncoding.Encode("utf-8", stripCRLF(msg.Subject, " ")))
	h("MIME-Version", "1.0")
	h("Auto-Submitted", "auto-generated")
	h("Content-Type", "text/plain; charset=UTF-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	sb.WriteString("\r\n")
	return sb.String(), nil
}

func parseEnvelopeAddress(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid email address %q: %w", raw, err)
	}
	return addr.Address, nil
}
