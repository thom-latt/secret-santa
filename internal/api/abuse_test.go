package api

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestResendParticipantEmailIsRateLimited(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, parts := seedDrawnEvent(t, env, 3)
	path := "/api/admin/events/" + ev.AdminToken + "/participants/" + parts[0].ID + "/resend"
	for i := 0; i < 3; i++ {
		if rec := doRaw(env.handler, http.MethodPost, path, ``, nil, ""); rec.Code != http.StatusOK {
			t.Fatalf("resend %d: expected 200, got %d", i, rec.Code)
		}
	}
	if rec := doRaw(env.handler, http.MethodPost, path, ``, nil, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after 3 resends, got %d", rec.Code)
	}
	// another participant has its own budget
	other := "/api/admin/events/" + ev.AdminToken + "/participants/" + parts[1].ID + "/resend"
	if rec := doRaw(env.handler, http.MethodPost, other, ``, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("other participant must not be throttled, got %d", rec.Code)
	}
}

func TestResendFailedEmailsIsRateLimited(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, _ := seedDrawnEvent(t, env, 3)
	path := "/api/admin/events/" + ev.AdminToken + "/resend-failed"
	for i := 0; i < 5; i++ {
		if rec := doRaw(env.handler, http.MethodPost, path, ``, nil, ""); rec.Code != http.StatusOK {
			t.Fatalf("resend-failed %d: expected 200, got %d", i, rec.Code)
		}
	}
	if rec := doRaw(env.handler, http.MethodPost, path, ``, nil, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after 5 calls, got %d", rec.Code)
	}
}

func TestParallelWrongPasswordLoginsStopAtLimit(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	bad := `{"email":"super@example.com","password":"wrong"}`
	const total = loginFailureLimit * 4
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := doRaw(env.handler, http.MethodPost, "/api/superadmin/login", bad, nil, "192.0.2.50:1")
			mu.Lock()
			codes[rec.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if codes[http.StatusUnauthorized] != loginFailureLimit {
		t.Fatalf("exactly %d attempts may reach the password check, got %v", loginFailureLimit, codes)
	}
}

func TestSessionDoesNotSurviveRestart(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	cookie := loginSuper(t, env)
	// a new router with the same config simulates a restart
	restarted := NewRouter(env.cfg, env.repo, env.mailer, nil)
	if rec := doJSON(t, env.handler, http.MethodGet, "/api/superadmin/overview", nil, cookie); rec.Code != http.StatusOK {
		t.Fatalf("cookie must work on the original server, got %d", rec.Code)
	}
	if rec := doJSON(t, restarted, http.MethodGet, "/api/superadmin/overview", nil, cookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("cookie must be rejected after restart, got %d", rec.Code)
	}
}

func TestSecurityHeadersKeepPagesOutOfSearchEngines(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	rec := doRaw(env.handler, http.MethodGet, "/api/health", ``, nil, "")
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Fatalf("X-Robots-Tag = %q", got)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "object-src 'none'") {
		t.Fatalf("CSP lacks object-src 'none': %s", csp)
	}
}
