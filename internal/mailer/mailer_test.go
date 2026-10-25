package mailer

import (
	"strings"
	"testing"

	"secret-santa/internal/config"
)

func TestNewMailerAlwaysMockInDevelopment(t *testing.T) {
	cfg := &config.Config{
		AppEnv:      "development",
		SMTPHost:    "smtp.invalid-nonexistent.local",
		SMTPPort:    587,
		SMTPTLSMode: "starttls",
	}

	m := NewMailer(cfg)
	mock, ok := m.(*MockMailer)
	if !ok {
		t.Fatalf("expected *MockMailer in development, got %T", m)
	}

	err := m.Send(Message{
		To:      "alice@example.com",
		Subject: "Test",
		Body:    "Hello",
	})
	if err != nil {
		t.Fatalf("expected MockMailer.Send to succeed without network, got %v", err)
	}
	if len(mock.SentMessages()) != 1 {
		t.Fatalf("expected 1 recorded message, got %d", len(mock.SentMessages()))
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
}
