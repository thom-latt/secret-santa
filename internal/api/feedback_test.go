package api

import (
	"errors"
	"net/http"
	"secret-santa/internal/config"
	"strings"
	"testing"
)

func feedbackBody(msg string) string { return `{"kind":"bug","message":"` + msg + `"}` }

func TestParticipantFeedbackIsEmailedToOperator(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, parts := seedDrawnEvent(t, env, 3)
	p := parts[1]
	rec := doRaw(env.handler, http.MethodPost, "/api/p/"+p.ParticipantToken+"/feedback", feedbackBody("it broke"), map[string]string{"User-Agent": "UA-test"}, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d %s", rec.Code, rec.Body.String())
	}
	msgs := env.mailer.SentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.To != env.cfg.OperatorEmail || m.ReplyTo != p.Email {
		t.Fatalf("bad addressing: to=%q replyto=%q", m.To, m.ReplyTo)
	}
	for _, want := range []string{"it broke", ev.ID, ev.Title, "participant", "UA-test"} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	for _, secret := range []string{p.ParticipantToken, ev.AdminToken} {
		if strings.Contains(m.Body, secret) || strings.Contains(m.Subject, secret) {
			t.Fatalf("token leaked in email")
		}
	}
}

func TestAdminFeedbackIsEmailedToOperator(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, _ := seedDrawnEvent(t, env, 3)
	rec := doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev.AdminToken+"/feedback", `{"kind":"idea","message":"  nice  "}`, nil, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d %s", rec.Code, rec.Body.String())
	}
	msgs := env.mailer.SentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.To != env.cfg.OperatorEmail || m.ReplyTo != ev.OrganizerEmail {
		t.Fatalf("bad addressing: to=%q replyto=%q", m.To, m.ReplyTo)
	}
	if m.Subject != "[Secret Santa] Idea: nice" {
		t.Fatalf("subject = %q (message must be trimmed)", m.Subject)
	}
	for _, want := range []string{"organizer", ev.ID, ev.Title} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if strings.Contains(m.Body, ev.AdminToken) || strings.Contains(m.Subject, ev.AdminToken) {
		t.Fatalf("admin token leaked in email")
	}
}

func TestFeedbackValidation(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, _ := seedDrawnEvent(t, env, 3)
	path := "/api/admin/events/" + ev.AdminToken + "/feedback"
	for name, body := range map[string]string{
		"kind":      `{"kind":"rant","message":"hi"}`,
		"empty":     feedbackBody("   "),
		"too long":  feedbackBody(strings.Repeat("é", 2001)),
		"malformed": `nope`,
	} {
		if rec := doRaw(env.handler, http.MethodPost, path, body, nil, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", name, rec.Code)
		}
	}
	if rec := doRaw(env.handler, http.MethodPost, path, feedbackBody(strings.Repeat("é", 2000)), nil, ""); rec.Code != http.StatusNoContent {
		t.Errorf("2000 runes should pass, got %d", rec.Code)
	}
	if len(env.mailer.SentMessages()) != 1 {
		t.Fatalf("invalid requests must not send mail")
	}
}

func TestFeedbackUnknownToken(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	for _, path := range []string{"/api/p/nope/feedback", "/api/admin/events/nope/feedback"} {
		if rec := doRaw(env.handler, http.MethodPost, path, feedbackBody("hi"), nil, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, rec.Code)
		}
	}
}

func TestFeedbackRateLimits(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, _ := seedDrawnEvent(t, env, 3)
	path := "/api/admin/events/" + ev.AdminToken + "/feedback"
	for i := 0; i < feedbackIPLimit; i++ {
		if rec := doRaw(env.handler, http.MethodPost, path, feedbackBody("hi"), nil, "192.0.2.9:1"); rec.Code != http.StatusNoContent {
			t.Fatalf("request %d: expected 204, got %d", i, rec.Code)
		}
	}
	if rec := doRaw(env.handler, http.MethodPost, path, feedbackBody("hi"), nil, "192.0.2.9:1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 per IP, got %d", rec.Code)
	}

	env2 := newTestEnv(t)
	defer env2.cleanup()
	ev2, _ := seedDrawnEvent(t, env2, 3)
	path2 := "/api/admin/events/" + ev2.AdminToken + "/feedback"
	for i := 0; i < feedbackEventLimit; i++ {
		if rec := doRaw(env2.handler, http.MethodPost, path2, feedbackBody("hi"), nil, uniqueRemote(i)); rec.Code != http.StatusNoContent {
			t.Fatalf("request %d: expected 204, got %d", i, rec.Code)
		}
	}
	if rec := doRaw(env2.handler, http.MethodPost, path2, feedbackBody("hi"), nil, uniqueRemote(feedbackEventLimit)); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 per event, got %d", rec.Code)
	}
}

func TestFeedbackMailFailureIs502(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, _ := seedDrawnEvent(t, env, 3)
	env.mailer.SetFailFor(env.cfg.OperatorEmail, errors.New("smtp down"))
	rec := doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev.AdminToken+"/feedback", feedbackBody("hi"), nil, "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "failed to send feedback") {
		t.Fatalf("expected 502, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestFeedbackUnavailableWithoutOperatorEmail(t *testing.T) {
	env := newTestEnvWith(t, func(c *config.Config) { c.OperatorEmail = "" })
	defer env.cleanup()
	ev, _ := seedDrawnEvent(t, env, 3)
	rec := doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev.AdminToken+"/feedback", feedbackBody("hi"), nil, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
