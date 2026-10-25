package mailer

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"secret-santa/internal/config"
)

func TestNewMailerAlwaysMockInDevelopment(t *testing.T) {
	cfg := &config.Config{
		AppEnv:      "development",
		AppBaseURL:  "http://localhost:8080",
		SMTPHost:    "127.0.0.1",
		SMTPPort:    1, // closed port — any network dial would immediately fail
		SMTPUser:    "santa@example.com",
		SMTPPass:    "super-secret-smtp-pass",
		SMTPFrom:    "Secret Santa <santa@example.com>",
		SMTPTLSMode: "starttls",
	}

	var logBuf bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOut)

	m := NewMailer(cfg)
	mock, ok := m.(*ConsoleMockMailer)
	if !ok {
		t.Fatalf("expected *ConsoleMockMailer in development, got %T", m)
	}

	msg := BuildAdminMagicLinkEmail(cfg.AppBaseURL, "fr", "alice@example.com", AdminMagicLinkData{
		OrganizerName: "Alice",
		EventTitle:    "Noël Test",
		AdminToken:    "tok_dev_123",
	})
	if err := m.Send(msg); err != nil {
		t.Fatalf("expected ConsoleMockMailer.Send to succeed without network, got %v", err)
	}

	errs := m.SendBatch([]Message{
		BuildParticipantDrawEmail(cfg.AppBaseURL, "en", "bob@example.com", ParticipantDrawData{
			ParticipantName: "Bob",
			EventTitle:      "Noël Test",
			Token:           "tok_part_456",
		}),
	})
	if len(errs) != 1 || errs[0] != nil {
		t.Fatalf("expected SendBatch to succeed without network, got %v", errs)
	}

	if len(mock.SentMessages()) != 2 {
		t.Fatalf("expected 2 recorded messages, got %d", len(mock.SentMessages()))
	}

	logged := logBuf.String()
	for _, want := range []string{
		"[MOCK EMAIL]",
		"http://localhost:8080/admin/events/tok_dev_123",
		"http://localhost:8080/p/tok_part_456",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("expected terminal log output to contain clickable link %q, got:\n%s", want, logged)
		}
	}
}

func TestNewMailerReturnsSMTPInProduction(t *testing.T) {
	cfg := &config.Config{
		AppEnv:      "production",
		SMTPHost:    "smtp.example.com",
		SMTPPort:    587,
		SMTPFrom:    "Secret Santa <santa@example.com>",
		SMTPTLSMode: "starttls",
	}
	m := NewMailer(cfg)
	if _, ok := m.(*SMTPMailer); !ok {
		t.Fatalf("expected *SMTPMailer in production, got %T", m)
	}
}

func TestEmailTemplatesFRAndEN(t *testing.T) {
	baseURL := "https://santa.mydomain.com"

	adminFR := BuildAdminMagicLinkEmail(baseURL, "fr", "org@example.com", AdminMagicLinkData{
		OrganizerName: "Jean Dupont",
		EventTitle:    "Noël Famille",
		AdminToken:    "tok_admin_123",
	})
	if !strings.Contains(adminFR.Subject, "Noël Famille") || !strings.Contains(adminFR.Body, "https://santa.mydomain.com/admin/events/tok_admin_123") {
		t.Fatalf("unexpected FR admin email: %+v", adminFR)
	}

	adminEN := BuildAdminMagicLinkEmail(baseURL, "en", "org@example.com", AdminMagicLinkData{
		OrganizerName:  "John Doe",
		EventTitle:     "Family Xmas",
		AdminToken:     "tok_admin_456",
		InvitedByAdmin: true,
	})
	if !strings.Contains(adminEN.Subject, "Family Xmas") || !strings.Contains(adminEN.Body, "https://santa.mydomain.com/admin/events/tok_admin_456") || !strings.Contains(adminEN.Body, "invited you") {
		t.Fatalf("unexpected EN admin email: %+v", adminEN)
	}

	partFR := BuildParticipantDrawEmail(baseURL, "fr", "p@example.com", ParticipantDrawData{
		ParticipantName: "Marie",
		EventTitle:      "Noël Famille",
		EventDate:       "2026-12-24",
		Budget:          "20 €",
		Description:     "Rendez-vous à 20h",
		Token:           "tok_part_789",
	})
	for _, want := range []string{"https://santa.mydomain.com/p/tok_part_789", "2026-12-24", "20 €", "Rendez-vous à 20h"} {
		if !strings.Contains(partFR.Body, want) {
			t.Errorf("FR participant email missing %q: %s", want, partFR.Body)
		}
	}

	partEN := BuildParticipantDrawEmail(baseURL, "en", "p@example.com", ParticipantDrawData{
		ParticipantName: "Mary",
		EventTitle:      "Family Xmas",
		EventDate:       "2026-12-24",
		Budget:          "$25",
		Description:     "See you at 8pm",
		Token:           "tok_part_999",
	})
	for _, want := range []string{"https://santa.mydomain.com/p/tok_part_999", "2026-12-24", "$25", "See you at 8pm"} {
		if !strings.Contains(partEN.Body, want) {
			t.Errorf("EN participant email missing %q: %s", want, partEN.Body)
		}
	}

	recFR := BuildRecoveryEmail(baseURL, "fr", "both@example.com",
		[]RecoveryItem{{Title: "Noël FR", URL: baseURL + "/admin/events/adm1"}},
		[]RecoveryItem{{Title: "Noël FR", URL: baseURL + "/p/part1"}},
	)
	if !strings.Contains(recFR.Subject, "Vos liens magiques actifs") || !strings.Contains(recFR.Body, "/admin/events/adm1") || !strings.Contains(recFR.Body, "/p/part1") {
		t.Fatalf("unexpected FR recovery email: %+v", recFR)
	}

	recEN := BuildRecoveryEmail(baseURL, "en", "both@example.com",
		[]RecoveryItem{{Title: "Xmas EN", URL: baseURL + "/admin/events/adm2"}},
		[]RecoveryItem{{Title: "Xmas EN", URL: baseURL + "/p/part2"}},
	)
	if !strings.Contains(recEN.Subject, "Your active magic links") || !strings.Contains(recEN.Body, "/admin/events/adm2") || !strings.Contains(recEN.Body, "/p/part2") {
		t.Fatalf("unexpected EN recovery email: %+v", recEN)
	}
}

func TestSMTPMailerPooledBatchAndPartialFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	var connCount atomic.Int32
	var deliveredCount atomic.Int32

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			connCount.Add(1)
			go func(c net.Conn) {
				defer c.Close()
				_, _ = fmt.Fprintf(c, "220 localhost ESMTP\r\n")
				scanner := bufio.NewScanner(c)
				inData := false
				for scanner.Scan() {
					line := scanner.Text()
					if inData {
						if line == "." {
							inData = false
							deliveredCount.Add(1)
							_, _ = fmt.Fprintf(c, "250 2.0.0 OK\r\n")
						}
						continue
					}
					upper := strings.ToUpper(line)
					switch {
					case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
						_, _ = fmt.Fprintf(c, "250-localhost\r\n250 AUTH PLAIN\r\n")
					case strings.HasPrefix(upper, "AUTH "):
						_, _ = fmt.Fprintf(c, "235 2.7.0 Authentication successful\r\n")
					case strings.HasPrefix(upper, "MAIL FROM:"):
						_, _ = fmt.Fprintf(c, "250 2.1.0 OK\r\n")
					case strings.HasPrefix(upper, "RCPT TO:"):
						if strings.Contains(strings.ToLower(line), "reject@example.com") {
							_, _ = fmt.Fprintf(c, "550 5.1.1 User unknown\r\n")
						} else {
							_, _ = fmt.Fprintf(c, "250 2.1.5 OK\r\n")
						}
					case upper == "DATA":
						inData = true
						_, _ = fmt.Fprintf(c, "354 End data with <CR><LF>.<CR><LF>\r\n")
					case upper == "RSET":
						_, _ = fmt.Fprintf(c, "250 2.0.0 Reset OK\r\n")
					case upper == "QUIT":
						_, _ = fmt.Fprintf(c, "221 2.0.0 Bye\r\n")
						return
					default:
						_, _ = fmt.Fprintf(c, "250 OK\r\n")
					}
				}
			}(conn)
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	smtpMailer := NewSMTPMailer(&config.Config{
		AppEnv:      "production",
		SMTPHost:    host,
		SMTPPort:    port,
		SMTPUser:    "santa",
		SMTPPass:    "secret",
		SMTPFrom:    "Secret Santa <santa@example.com>",
		SMTPTLSMode: "none",
	})

	errs := smtpMailer.SendBatch([]Message{
		{To: "first@example.com", Subject: "Noël 1 — Événement", Body: "Link 1"},
		{To: "reject@example.com", Subject: "Noël 2", Body: "Link 2"},
		{To: "third@example.com", Subject: "Noël 3", Body: "Link 3"},
	})

	if len(errs) != 3 {
		t.Fatalf("expected 3 error slots, got %d", len(errs))
	}
	if errs[0] != nil {
		t.Fatalf("expected message 1 to succeed, got %v", errs[0])
	}
	if errs[1] == nil || !strings.Contains(errs[1].Error(), "550") {
		t.Fatalf("expected message 2 to fail with 550, got %v", errs[1])
	}
	if errs[2] != nil {
		t.Fatalf("expected message 3 to succeed after RSET on same connection, got %v", errs[2])
	}
	if connCount.Load() != 1 {
		t.Fatalf("expected single pooled TCP connection for batch, got %d connections", connCount.Load())
	}
	if deliveredCount.Load() != 2 {
		t.Fatalf("expected 2 delivered messages, got %d", deliveredCount.Load())
	}
}

