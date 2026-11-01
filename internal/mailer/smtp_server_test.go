package mailer

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime/quotedprintable"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"secret-santa/internal/config"
)

type receivedMail struct {
	from, to string
	data     string // dot-unstuffed message, CRLF normalised to LF
	viaTLS   bool
	helo     string
}

// fakeSMTP is a small scriptable SMTP server for exercising SMTPMailer.
type fakeSMTP struct {
	t  *testing.T
	ln net.Listener

	tlsCfg      *tls.Config // enables STARTTLS (or implicit TLS when implicitTLS)
	implicitTLS bool
	user, pass  string // when set, AUTH PLAIN must match

	rejectRcpt     map[string]string // lower-case address -> full reply line
	dropAfter      int               // close the connection after N delivered messages (0 = never)
	dataReplyDelay time.Duration

	mu       sync.Mutex
	conns    int
	authCmds int
	mails    []receivedMail
}

func newFakeSMTP(t *testing.T, configure func(*fakeSMTP)) *fakeSMTP {
	t.Helper()
	f := &fakeSMTP{t: t, rejectRcpt: map[string]string{}}
	if configure != nil {
		configure(f)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if f.tlsCfg != nil && f.implicitTLS {
		ln = tls.NewListener(ln, f.tlsCfg)
	}
	f.ln = ln
	t.Cleanup(func() { _ = ln.Close() })
	go f.serve()
	return f
}

func (f *fakeSMTP) hostPort() (string, int) {
	h, p, _ := net.SplitHostPort(f.ln.Addr().String())
	port, _ := strconv.Atoi(p)
	return h, port
}

func (f *fakeSMTP) received() []receivedMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]receivedMail(nil), f.mails...)
}

func (f *fakeSMTP) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns++
		f.mu.Unlock()
		go f.handle(c)
	}
}

func (f *fakeSMTP) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	reply := func(conn net.Conn, s string) { _, _ = fmt.Fprintf(conn, "%s\r\n", s) }

	_, isTLS := c.(*tls.Conn)
	if f.tlsCfg != nil && f.implicitTLS {
		isTLS = true
	}
	r := bufio.NewReader(c)
	reply(c, "220 fake ESMTP")

	var from, to, helo string
	delivered := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			helo = strings.TrimSpace(line[len("EHLO"):])
			caps := "250-fake\r\n250-8BITMIME"
			if f.tlsCfg != nil && !isTLS {
				caps += "\r\n250-STARTTLS"
			}
			caps += "\r\n250 AUTH PLAIN"
			reply(c, caps)
		case upper == "STARTTLS":
			if f.tlsCfg == nil {
				reply(c, "502 5.5.1 STARTTLS not supported")
				continue
			}
			reply(c, "220 ready")
			tc := tls.Server(c, f.tlsCfg)
			if err := tc.Handshake(); err != nil {
				return
			}
			c, isTLS = tc, true
			r = bufio.NewReader(tc)
		case strings.HasPrefix(upper, "AUTH PLAIN "):
			f.mu.Lock()
			f.authCmds++
			f.mu.Unlock()
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN "):]))
			if string(raw) == "\x00"+f.user+"\x00"+f.pass {
				reply(c, "235 2.7.0 ok")
			} else {
				reply(c, "535 5.7.8 bad credentials")
			}
		case strings.HasPrefix(upper, "MAIL FROM:"):
			from = angleAddr(line[len("MAIL FROM:"):])
			reply(c, "250 ok")
		case strings.HasPrefix(upper, "RCPT TO:"):
			to = angleAddr(line[len("RCPT TO:"):])
			if rej, ok := f.rejectRcpt[strings.ToLower(to)]; ok {
				reply(c, rej)
			} else {
				reply(c, "250 ok")
			}
		case upper == "DATA":
			reply(c, "354 go")
			var sb strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				l = strings.TrimRight(l, "\r\n")
				if l == "." {
					break
				}
				l = strings.TrimPrefix(l, ".") // undo dot-stuffing
				sb.WriteString(l + "\n")
			}
			if f.dataReplyDelay > 0 {
				time.Sleep(f.dataReplyDelay)
			}
			f.mu.Lock()
			f.mails = append(f.mails, receivedMail{from: from, to: to, data: sb.String(), viaTLS: isTLS, helo: helo})
			f.mu.Unlock()
			delivered++
			if f.dropAfter > 0 && delivered >= f.dropAfter {
				return // connection dropped without a reply
			}
			reply(c, "250 queued")
		case upper == "RSET":
			reply(c, "250 reset")
		case upper == "QUIT":
			reply(c, "221 bye")
			return
		default:
			reply(c, "250 ok")
		}
	}
}

// angleAddr extracts the address from "<addr> PARAM=..." command arguments.
func angleAddr(arg string) string {
	arg = strings.TrimSpace(arg)
	if i := strings.Index(arg, "<"); i >= 0 {
		if j := strings.Index(arg[i:], ">"); j > 0 {
			return arg[i+1 : i+j]
		}
	}
	return arg
}

func selfSignedTLS(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fake smtp"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}, pool
}

func mailerFor(f *fakeSMTP, mode, user, pass string) *SMTPMailer {
	host, port := f.hostPort()
	return NewSMTPMailer(&config.Config{
		AppEnv: "production", SMTPHost: host, SMTPPort: port,
		SMTPUser: user, SMTPPass: pass,
		SMTPFrom: "Secret Santa <santa@example.com>", SMTPTLSMode: mode,
		AppBaseURL: "https://santa.example.org",
	})
}

func TestSMTPSendFormatsMessageCorrectly(t *testing.T) {
	f := newFakeSMTP(t, nil)
	m := mailerFor(f, "none", "", "")
	err := m.Send(Message{To: "bob@example.com", Subject: "Noël — Événement", Body: "Bonjour\n.line starting with a dot\nbye"})
	if err != nil {
		t.Fatal(err)
	}
	got := f.received()
	if len(got) != 1 {
		t.Fatalf("expected 1 mail, got %d", len(got))
	}
	d := got[0].data
	if got[0].from != "santa@example.com" || got[0].to != "bob@example.com" {
		t.Fatalf("bad envelope: %+v", got[0])
	}
	for _, want := range []string{
		"From: \"Secret Santa\" <santa@example.com>\n",
		"To: bob@example.com\n",
		"MIME-Version: 1.0\n",
		"Content-Type: text/plain; charset=UTF-8\n",
		"Date: ",
		"Auto-Submitted: auto-generated\n",
		"Content-Transfer-Encoding: quoted-printable\n",
		"\n.line starting with a dot\n", // survived dot-stuffing
	} {
		if !strings.Contains(d, want) {
			t.Fatalf("message lacks %q:\n%s", want, d)
		}
	}
	if !strings.Contains(d, "Subject: =?utf-8?q?No=C3=ABl") {
		t.Fatalf("non-ASCII subject must be MIME-encoded:\n%s", d)
	}
	if f.authCmds != 0 {
		t.Fatal("no AUTH must be attempted without credentials")
	}
}

func TestSMTPMessageIDHeloAndBodyEncoding(t *testing.T) {
	f := newFakeSMTP(t, nil)
	m := mailerFor(f, "none", "", "")
	long := strings.Repeat("é", 600) // > 998 octets on one line
	body := "Bonjour Zoé,\nCadeau pour Noël — 5 €\n" + long + "\n"
	if err := m.Send(Message{To: "bob@example.com", Subject: "s", Body: body, ReplyTo: "Zoé <zoe@example.org>"}); err != nil {
		t.Fatal(err)
	}
	got := f.received()[0]
	if got.helo != "santa.example.org" {
		t.Fatalf("EHLO must use the app host, got %q", got.helo)
	}
	parts := strings.SplitN(got.data, "\n\n", 2)
	headers := parts[0]
	if !regexp.MustCompile(`(?m)^Message-ID: <[0-9a-f]{32}@example\.com>$`).MatchString(headers) {
		t.Fatalf("bad Message-ID:\n%s", headers)
	}
	if !strings.Contains(headers, "Reply-To: ") || !strings.Contains(headers, "<zoe@example.org>") {
		t.Fatalf("missing Reply-To:\n%s", headers)
	}
	for _, l := range strings.Split(parts[1], "\n") {
		if len(l) > 76 {
			t.Fatalf("line longer than 76 octets: %d", len(l))
		}
		for i := 0; i < len(l); i++ {
			if l[i] >= 0x80 {
				t.Fatal("body must be 7-bit")
			}
		}
	}
	dec, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(parts[1])))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimRight(string(dec), "\r\n") != strings.TrimRight(body, "\n") {
		t.Fatalf("body does not decode back to the original:\n%q", dec)
	}
}

func TestSMTPFromEncodesAccentedDisplayName(t *testing.T) {
	f := newFakeSMTP(t, nil)
	host, port := f.hostPort()
	m := NewSMTPMailer(&config.Config{SMTPHost: host, SMTPPort: port, SMTPTLSMode: "none",
		SMTPFrom: "Père Noël <santa@example.com>", AppBaseURL: "https://santa.example.org"})
	if err := m.Send(Message{To: "bob@example.com", Subject: "s", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	headers := strings.SplitN(f.received()[0].data, "\n\n", 2)[0]
	if !strings.Contains(headers, "From: =?utf-8?q?P=C3=A8re_No=C3=ABl?= <santa@example.com>") {
		t.Fatalf("display name must be encoded:\n%s", headers)
	}
}

func TestSMTPHeaderInjectionIsNeutralised(t *testing.T) {
	f := newFakeSMTP(t, nil)
	m := mailerFor(f, "none", "", "")

	// CR/LF in the subject must not create a new header
	if err := m.Send(Message{To: "bob@example.com", Subject: "Hi\r\nBcc: evil@example.com", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	headers := strings.SplitN(f.received()[0].data, "\n\n", 2)[0]
	for _, line := range strings.Split(headers, "\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Fatalf("injected header present:\n%s", headers)
		}
	}

	// CR/LF in the recipient is refused before anything is sent
	before := len(f.received())
	err := m.Send(Message{To: "bob@example.com\r\nBcc: evil@example.com", Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("recipient containing CRLF must be rejected")
	}
	if len(f.received()) != before {
		t.Fatal("nothing must be delivered for an invalid recipient")
	}
}

func TestSMTPAuthenticationSuccessAndFailure(t *testing.T) {
	f := newFakeSMTP(t, func(f *fakeSMTP) { f.user, f.pass = "santa", "s3cret" })

	if err := mailerFor(f, "none", "santa", "s3cret").Send(Message{To: "a@example.com", Subject: "s", Body: "b"}); err != nil {
		t.Fatalf("valid credentials must work: %v", err)
	}

	bad := mailerFor(f, "none", "santa", "wrong")
	errs := bad.SendBatch([]Message{
		{To: "a@example.com", Subject: "1", Body: "b"},
		{To: "b@example.com", Subject: "2", Body: "b"},
	})
	for i, err := range errs {
		if err == nil || !strings.Contains(err.Error(), "smtp auth") {
			t.Fatalf("message %d: expected an auth error, got %v", i, err)
		}
	}
	if n := len(f.received()); n != 1 {
		t.Fatalf("a failed login must deliver nothing, got %d total mails", n)
	}
}

func TestSMTPSTARTTLS(t *testing.T) {
	cfg, pool := selfSignedTLS(t)
	f := newFakeSMTP(t, func(f *fakeSMTP) { f.tlsCfg = cfg; f.user, f.pass = "santa", "pw" })

	m := mailerFor(f, "starttls", "santa", "pw")
	m.rootCAs = pool
	if err := m.Send(Message{To: "a@example.com", Subject: "s", Body: "b"}); err != nil {
		t.Fatalf("STARTTLS with a trusted certificate must work: %v", err)
	}
	if got := f.received(); len(got) != 1 || !got[0].viaTLS {
		t.Fatalf("message must have been delivered over TLS: %+v", got)
	}

	// untrusted certificate: refused, credentials never sent in clear
	untrusted := mailerFor(f, "starttls", "santa", "pw")
	err := untrusted.Send(Message{To: "b@example.com", Subject: "s", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "starttls") {
		t.Fatalf("expected a starttls verification error, got %v", err)
	}
	if len(f.received()) != 1 {
		t.Fatal("nothing may be delivered after a failed TLS verification")
	}
}

func TestSMTPSTARTTLSRequiredButUnsupportedFails(t *testing.T) {
	f := newFakeSMTP(t, nil) // does not advertise STARTTLS
	err := mailerFor(f, "starttls", "", "").Send(Message{To: "a@example.com", Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("starttls mode must not silently fall back to plaintext")
	}
	if len(f.received()) != 0 {
		t.Fatal("nothing may be delivered in clear text")
	}
}

func TestSMTPImplicitTLS(t *testing.T) {
	cfg, pool := selfSignedTLS(t)
	f := newFakeSMTP(t, func(f *fakeSMTP) { f.tlsCfg = cfg; f.implicitTLS = true })

	m := mailerFor(f, "tls", "", "")
	m.rootCAs = pool
	if err := m.Send(Message{To: "a@example.com", Subject: "s", Body: "b"}); err != nil {
		t.Fatalf("implicit TLS must work: %v", err)
	}
	if got := f.received(); len(got) != 1 || !got[0].viaTLS {
		t.Fatalf("expected one TLS delivery, got %+v", got)
	}

	err := mailerFor(f, "tls", "", "").Send(Message{To: "a@example.com", Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("an untrusted certificate must be refused")
	}
}

func TestSMTPPartialFailuresAreReportedPerMessage(t *testing.T) {
	f := newFakeSMTP(t, func(f *fakeSMTP) {
		f.rejectRcpt["nobody@example.com"] = "550 5.1.1 no such user"
		f.rejectRcpt["full@example.com"] = "452 4.2.2 mailbox full"
	})
	errs := mailerFor(f, "none", "", "").SendBatch([]Message{
		{To: "a@example.com", Subject: "1", Body: "b"},
		{To: "nobody@example.com", Subject: "2", Body: "b"},
		{To: "not an address", Subject: "3", Body: "b"},
		{To: "full@example.com", Subject: "4", Body: "b"},
		{To: "z@example.com", Subject: "5", Body: "b"},
	})
	if errs[0] != nil || errs[4] != nil {
		t.Fatalf("valid messages must be delivered despite neighbours failing: %v", errs)
	}
	if errs[1] == nil || !strings.Contains(errs[1].Error(), "550") {
		t.Fatalf("expected 550, got %v", errs[1])
	}
	if errs[2] == nil || !strings.Contains(errs[2].Error(), "invalid email address") {
		t.Fatalf("expected an invalid address error, got %v", errs[2])
	}
	if errs[3] == nil || !strings.Contains(errs[3].Error(), "452") {
		t.Fatalf("expected 452, got %v", errs[3])
	}
	if n := len(f.received()); n != 2 {
		t.Fatalf("expected 2 deliveries, got %d", n)
	}
	if f.conns != 1 {
		t.Fatalf("expected one pooled connection, got %d", f.conns)
	}
}

func TestSMTPConnectionDroppedMidBatch(t *testing.T) {
	f := newFakeSMTP(t, func(f *fakeSMTP) { f.dropAfter = 1 })
	errs := mailerFor(f, "none", "", "").SendBatch([]Message{
		{To: "a@example.com", Subject: "1", Body: "b"},
		{To: "b@example.com", Subject: "2", Body: "b"},
		{To: "c@example.com", Subject: "3", Body: "b"},
	})
	// The first message is accepted by the server but its acknowledgement was lost:
	// it must be reported as failed (the safe choice: it can be resent) and so must the rest.
	for i, err := range errs {
		if err == nil {
			t.Fatalf("message %d: expected an error after the connection dropped", i)
		}
	}
}

func TestSMTPServerUnreachable(t *testing.T) {
	f := newFakeSMTP(t, nil)
	m := mailerFor(f, "none", "", "")
	_ = f.ln.Close()
	errs := m.SendBatch([]Message{{To: "a@example.com", Subject: "1", Body: "b"}, {To: "b@example.com", Subject: "2", Body: "b"}})
	for i, err := range errs {
		if err == nil || !strings.Contains(err.Error(), "smtp dial") {
			t.Fatalf("message %d: expected a dial error, got %v", i, err)
		}
	}
}

func TestSMTPInvalidFromAddressFailsEverything(t *testing.T) {
	f := newFakeSMTP(t, nil)
	m := mailerFor(f, "none", "", "")
	m.from = "definitely not an address"
	errs := m.SendBatch([]Message{{To: "a@example.com", Subject: "1", Body: "b"}})
	if errs[0] == nil || len(f.received()) != 0 || f.conns != 0 {
		t.Fatalf("an invalid From must fail before connecting: err=%v conns=%d", errs[0], f.conns)
	}
}

func TestSMTPBatchBudgetStopsSlowBatches(t *testing.T) {
	f := newFakeSMTP(t, func(f *fakeSMTP) { f.dataReplyDelay = 120 * time.Millisecond })
	m := mailerFor(f, "none", "", "")
	m.batchBudget = 150 * time.Millisecond

	msgs := make([]Message, 6)
	for i := range msgs {
		msgs[i] = Message{To: fmt.Sprintf("u%d@example.com", i), Subject: "s", Body: "b"}
	}
	start := time.Now()
	errs := m.SendBatch(msgs)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("batch must stop near its budget, took %v", elapsed)
	}
	if errs[0] != nil {
		t.Fatalf("the first message should be delivered, got %v", errs[0])
	}
	if !errors.Is(errs[len(errs)-1], ErrBatchBudgetExceeded) {
		t.Fatalf("late messages must report ErrBatchBudgetExceeded, got %v", errs[len(errs)-1])
	}
	delivered := len(f.received())
	failed := 0
	for _, err := range errs {
		if err != nil {
			failed++
		}
	}
	if delivered+failed != len(msgs) {
		t.Fatalf("every message must be either delivered or reported failed: delivered=%d failed=%d", delivered, failed)
	}
}

func TestSMTPEmptyBatch(t *testing.T) {
	m := NewSMTPMailer(&config.Config{SMTPHost: "127.0.0.1", SMTPPort: 1, SMTPFrom: "a@example.com", SMTPTLSMode: "none"})
	if errs := m.SendBatch(nil); len(errs) != 0 {
		t.Fatalf("expected no results, got %v", errs)
	}
}

func TestMockMailerFailForAndReset(t *testing.T) {
	m := NewMockMailer()
	m.SetFailFor("bad@example.com", errors.New("boom"))
	errs := m.SendBatch([]Message{{To: "ok@example.com"}, {To: "bad@example.com"}})
	if errs[0] != nil || errs[1] == nil {
		t.Fatalf("unexpected results: %v", errs)
	}
	m.SetFailFor("bad@example.com", nil)
	if err := m.Send(Message{To: "bad@example.com"}); err != nil {
		t.Fatalf("failure must be clearable: %v", err)
	}
	if len(m.SentMessages()) != 2 {
		t.Fatalf("expected 2 recorded messages, got %d", len(m.SentMessages()))
	}
	m.Reset()
	if len(m.SentMessages()) != 0 || !strings.Contains(m.String(), "0 messages") {
		t.Fatal("Reset must clear recorded messages")
	}
}
