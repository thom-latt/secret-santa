package mailer

import (
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
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
}

func NewSMTPMailer(cfg *config.Config) *SMTPMailer {
	return &SMTPMailer{
		host:    cfg.SMTPHost,
		port:    cfg.SMTPPort,
		user:    cfg.SMTPUser,
		pass:    cfg.SMTPPass,
		from:    cfg.SMTPFrom,
		tlsMode: cfg.SMTPTLSMode,
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

	for i, msg := range msgs {
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
	tlsCfg := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}

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

	cleanSubject := strings.ReplaceAll(strings.ReplaceAll(msg.Subject, "\r", ""), "\n", " ")
	encodedSubject := mime.QEncoding.Encode("utf-8", cleanSubject)
	dateHeader := time.Now().UTC().Format(time.RFC1123Z)
	payload := fmt.Sprintf(
		"Date: %s\r\nFrom: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n",
		dateHeader, fromHeader, toAddr, encodedSubject, msg.Body,
	)

	if _, err := w.Write([]byte(payload)); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return nil
}

func parseEnvelopeAddress(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid email address %q: %w", raw, err)
	}
	return addr.Address, nil
}
