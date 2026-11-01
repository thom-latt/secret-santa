package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"secret-santa/internal/config"
	"secret-santa/internal/db"
	"secret-santa/internal/mailer"
)

// ---------------------------------------------------------------- helpers

func doRaw(h http.Handler, method, path, body string, headers map[string]string, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if remote != "" {
		req.RemoteAddr = remote
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func uniqueRemote(i int) string { return fmt.Sprintf("10.1.%d.%d:4000", i/250, i%250+1) }

var seedCounter struct {
	sync.Mutex
	n int
}

func nextSeed() int {
	seedCounter.Lock()
	defer seedCounter.Unlock()
	seedCounter.n++
	return seedCounter.n
}

// seedEvent creates an event with n participants (organizer included).
func seedEvent(t *testing.T, env *testEnv, n int) (*db.Event, []db.Participant) {
	t.Helper()
	id := nextSeed()
	ev, _, err := env.repo.CreateEvent(fmt.Sprintf("Event %d", id), "Org", "Anizer", fmt.Sprintf("org%d@example.com", id), "en")
	if err != nil {
		t.Fatal(err)
	}
	parts, err := env.repo.ListParticipants(ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	for len(parts) < n {
		i := len(parts)
		if _, err := env.repo.AddParticipant(ev.ID, "Part", fmt.Sprint(i), fmt.Sprintf("e%d-p%d@example.com", id, i)); err != nil {
			t.Fatal(err)
		}
		parts, _ = env.repo.ListParticipants(ev.ID)
	}
	return ev, parts
}

func seedDrawnEvent(t *testing.T, env *testEnv, n int) (*db.Event, []db.Participant) {
	t.Helper()
	ev, parts := seedEvent(t, env, n)
	assign := make(map[string]string, len(parts))
	for i, p := range parts {
		assign[p.ID] = parts[(i+1)%len(parts)].ID
	}
	if err := env.repo.SaveDraw(ev.ID, assign); err != nil {
		t.Fatal(err)
	}
	ev, _ = env.repo.GetEventByID(ev.ID)
	parts, _ = env.repo.ListParticipants(ev.ID)
	return ev, parts
}

func loginSuper(t *testing.T, env *testEnv) *http.Cookie {
	t.Helper()
	return loginSuperOn(t, env.handler)
}

func loginSuperOn(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	rec := doJSON(t, h, http.MethodPost, "/api/superadmin/login", SuperAdminLoginRequest{Email: "super@example.com", Password: "secret-password-123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("super admin login failed: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == superAdminCookieName {
			return c
		}
	}
	t.Fatal("no session cookie issued")
	return nil
}

// forgeSuperToken re-implements the documented token format independently of the server code.
func forgeSuperToken(secret string, exp int64, epoch uint64, email, hash string) string {
	payload := fmt.Sprintf("superadmin:%d:%d:%s:%s", exp, epoch, strings.ToLower(email), hash)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return fmt.Sprintf("%d.%s", exp, hex.EncodeToString(mac.Sum(nil)))
}

func assertDrawConsistent(t *testing.T, env *testEnv, eventID string) {
	t.Helper()
	ev, err := env.repo.GetEventByID(eventID)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != "drawn" {
		return
	}
	parts, _ := env.repo.ListParticipants(eventID)
	ids := map[string]bool{}
	for _, p := range parts {
		ids[p.ID] = true
	}
	received := map[string]bool{}
	target := map[string]string{}
	for _, p := range parts {
		if p.AssignedToID == nil {
			t.Fatalf("participant %s of a drawn event has no target", p.Email)
		}
		to := *p.AssignedToID
		if !ids[to] || to == p.ID || received[to] {
			t.Fatalf("invalid assignment %s -> %s", p.ID, to)
		}
		received[to] = true
		target[p.ID] = to
	}
	exs, _ := env.repo.ListExclusions(eventID)
	for _, ex := range exs {
		if target[ex.ParticipantAID] == ex.ParticipantBID {
			t.Fatalf("exclusion %s -/-> %s violated by the draw", ex.ParticipantAID, ex.ParticipantBID)
		}
	}
}

// ---------------------------------------------------- regression: logout

func TestSuperAdminLogoutRequiresValidSession(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	cookie := loginSuper(t, env)

	overview := func(c *http.Cookie) int {
		return doJSON(t, env.handler, http.MethodGet, "/api/superadmin/overview", nil, c).Code
	}

	// Unauthenticated / forged logout calls must not end the legitimate session.
	doJSON(t, env.handler, http.MethodPost, "/api/superadmin/logout", nil)
	doJSON(t, env.handler, http.MethodPost, "/api/superadmin/logout", nil, &http.Cookie{Name: superAdminCookieName, Value: "garbage"})
	doJSON(t, env.handler, http.MethodPost, "/api/superadmin/logout", nil, &http.Cookie{Name: superAdminCookieName, Value: cookie.Value + "0"})
	if code := overview(cookie); code != http.StatusOK {
		t.Fatalf("an unauthenticated logout must not invalidate the session, overview returned %d", code)
	}

	// A legitimate logout invalidates the session, and clears the cookie.
	rec := doJSON(t, env.handler, http.MethodPost, "/api/superadmin/logout", nil, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout returned %d", rec.Code)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == superAdminCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout must clear the cookie")
	}
	if code := overview(cookie); code != http.StatusUnauthorized {
		t.Fatalf("the old session must be rejected after logout, got %d", code)
	}
	// and logging in again works
	if code := overview(loginSuper(t, env)); code != http.StatusOK {
		t.Fatalf("new session must work, got %d", code)
	}
}

func TestSuperAdminCookieValidation(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	handler, srv := newRouter(env.cfg, env.repo, env.mailer, nil)
	epoch := srv.sessionEpoch.Load()
	good := loginSuperOn(t, handler)
	secret, hash := env.cfg.SessionSecret, env.cfg.SuperAdminPasswordHash
	future := time.Now().Add(time.Hour).Unix()

	tampered := good.Value[:len(good.Value)-1] + map[bool]string{true: "1", false: "0"}[good.Value[len(good.Value)-1] == '0']
	cases := map[string]string{
		"empty":                 "",
		"garbage":               "not-a-token",
		"no signature":          fmt.Sprint(future) + ".",
		"non-numeric expiry":    "abc." + strings.Repeat("0", 64),
		"tampered signature":    tampered,
		"expired":               forgeSuperToken(secret, time.Now().Add(-time.Minute).Unix(), epoch, env.cfg.SuperAdminEmail, hash),
		"wrong secret":          forgeSuperToken("another-secret-another-secret-xx", future, epoch, env.cfg.SuperAdminEmail, hash),
		"wrong epoch":           forgeSuperToken(secret, future, epoch+7, env.cfg.SuperAdminEmail, hash),
		"wrong email":           forgeSuperToken(secret, future, 0, "intruder@example.com", hash),
		"wrong password hash":   forgeSuperToken(secret, future, epoch, env.cfg.SuperAdminEmail, "x"),
		"extended expiry reuse": fmt.Sprint(future+86400) + "." + strings.SplitN(good.Value, ".", 2)[1],
	}
	for name, v := range cases {
		rec := doJSON(t, handler, http.MethodGet, "/api/superadmin/overview", nil, &http.Cookie{Name: superAdminCookieName, Value: v})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401, got %d", name, rec.Code)
		}
	}
	// control: an independently forged token with the right parameters is accepted
	forged := forgeSuperToken(secret, future, epoch, env.cfg.SuperAdminEmail, hash)
	if rec := doJSON(t, handler, http.MethodGet, "/api/superadmin/overview", nil, &http.Cookie{Name: superAdminCookieName, Value: forged}); rec.Code != http.StatusOK {
		t.Fatalf("test helper out of sync with the server token format: %d", rec.Code)
	}

	// changing the super admin password invalidates all sessions
	env2 := newTestEnvWith(t, func(c *config.Config) { c.SuperAdminPasswordHash = mustHashPassword(t, "another-password-456") })
	defer env2.cleanup()
	if rec := doJSON(t, env2.handler, http.MethodGet, "/api/superadmin/overview", nil, good); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a session from before a password change must be rejected, got %d", rec.Code)
	}
}

func TestSuperAdminRoutesAllRequireAuthentication(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, _ := seedEvent(t, env, 3)
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/superadmin/overview"},
		{http.MethodPut, "/api/superadmin/settings"},
		{http.MethodPost, "/api/superadmin/invite"},
		{http.MethodPost, "/api/superadmin/test-smtp"},
		{http.MethodPost, "/api/superadmin/events/" + ev.ID + "/resend-admin"},
		{http.MethodDelete, "/api/superadmin/events/" + ev.ID},
	}
	for _, rt := range routes {
		if rec := doRaw(env.handler, rt.method, rt.path, `{}`, nil, ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without a session: expected 401, got %d", rt.method, rt.path, rec.Code)
		}
	}
	if _, err := env.repo.GetEventByID(ev.ID); err != nil {
		t.Fatal("an unauthenticated delete must not remove the event")
	}
	if n := len(env.mailer.SentMessages()); n != 0 {
		t.Fatalf("unauthenticated calls must not send emails, got %d", n)
	}
}

// ---------------------------------------------------- regression: rate limits

func TestLoginRateLimitPerClientAddress(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	bad := `{"email":"super@example.com","password":"wrong"}`
	good := `{"email":"super@example.com","password":"secret-password-123"}`

	for i := 0; i < loginFailureLimit; i++ {
		if rec := doRaw(env.handler, http.MethodPost, "/api/superadmin/login", bad, nil, "192.0.2.10:1111"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, rec.Code)
		}
	}
	// blocked, even with the right password
	if rec := doRaw(env.handler, http.MethodPost, "/api/superadmin/login", good, nil, "192.0.2.10:2222"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after %d failures, got %d", loginFailureLimit, rec.Code)
	}
	// spoofing X-Forwarded-For does not help when the proxy is not trusted
	if rec := doRaw(env.handler, http.MethodPost, "/api/superadmin/login", good, map[string]string{"X-Forwarded-For": "203.0.113.99"}, "192.0.2.10:3333"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("X-Forwarded-For must be ignored without TRUST_PROXY, got %d", rec.Code)
	}
	// another client is unaffected
	if rec := doRaw(env.handler, http.MethodPost, "/api/superadmin/login", good, nil, "192.0.2.11:1111"); rec.Code != http.StatusOK {
		t.Fatalf("another address must not be throttled, got %d", rec.Code)
	}
}

func TestRateLimitBehindTrustedProxyUsesForwardedAddress(t *testing.T) {
	env := newTestEnvWith(t, func(c *config.Config) { c.TrustProxy = true })
	defer env.cleanup()
	proxy := "10.0.0.1:5555" // every request comes from the reverse proxy
	bad := `{"email":"super@example.com","password":"wrong"}`
	good := `{"email":"super@example.com","password":"secret-password-123"}`
	attacker := map[string]string{"X-Forwarded-For": "198.51.100.7"}

	for i := 0; i < loginFailureLimit; i++ {
		doRaw(env.handler, http.MethodPost, "/api/superadmin/login", bad, attacker, proxy)
	}
	if rec := doRaw(env.handler, http.MethodPost, "/api/superadmin/login", good, attacker, proxy); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the attacker must be throttled, got %d", rec.Code)
	}
	// the real admin, seen by the proxy at another address, is not locked out
	if rec := doRaw(env.handler, http.MethodPost, "/api/superadmin/login", good, map[string]string{"X-Forwarded-For": "203.0.113.5"}, proxy); rec.Code != http.StatusOK {
		t.Fatalf("a different client behind the same proxy must not be throttled, got %d", rec.Code)
	}
	// a client cannot dodge the limit by prepending fake addresses: the proxy's own (right-most) entry counts
	spoof := map[string]string{"X-Forwarded-For": "1.2.3.4, 198.51.100.7"}
	if rec := doRaw(env.handler, http.MethodPost, "/api/superadmin/login", good, spoof, proxy); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("prepended spoofed addresses must not bypass the limit, got %d", rec.Code)
	}
	// a malformed header falls back to the peer address
	if rec := doRaw(env.handler, http.MethodPost, "/api/superadmin/login", good, map[string]string{"X-Forwarded-For": "not-an-ip"}, "10.0.0.9:1"); rec.Code != http.StatusOK {
		t.Fatalf("malformed XFF must fall back to the peer address, got %d", rec.Code)
	}
}

func TestRecoverEndpointThrottling(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	_, parts := seedDrawnEvent(t, env, 3)
	target := parts[1].Email

	// per-address: only 3 emails per hour, but every answer is the same generic 200
	var first string
	for i := 0; i < recoverEmailLimit+3; i++ {
		rec := doRaw(env.handler, http.MethodPost, "/api/recover", fmt.Sprintf(`{"email":%q,"locale":"en"}`, target), nil, uniqueRemote(i))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
		if i == 0 {
			first = rec.Body.String()
		} else if rec.Body.String() != first {
			t.Fatal("answers must be identical so throttling is not observable")
		}
	}
	if n := len(env.mailer.SentMessages()); n != recoverEmailLimit {
		t.Fatalf("expected %d recovery emails, got %d", recoverEmailLimit, n)
	}
	// unknown addresses get the same answer
	unknown := doRaw(env.handler, http.MethodPost, "/api/recover", `{"email":"nobody@example.com","locale":"en"}`, nil, uniqueRemote(50))
	if unknown.Code != http.StatusOK || unknown.Body.String() != first {
		t.Fatalf("unknown address must be indistinguishable: %d %s", unknown.Code, unknown.Body.String())
	}

	// per-client: blocked after recoverIPLimit requests, whatever the address
	env2 := newTestEnv(t)
	defer env2.cleanup()
	for i := 0; i < recoverIPLimit; i++ {
		rec := doRaw(env2.handler, http.MethodPost, "/api/recover", fmt.Sprintf(`{"email":"u%d@example.com"}`, i), nil, "192.0.2.50:1")
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}
	if rec := doRaw(env2.handler, http.MethodPost, "/api/recover", `{"email":"another@example.com"}`, nil, "192.0.2.50:1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 beyond %d requests, got %d", recoverIPLimit, rec.Code)
	}
}

func TestRecoverDoesNotLeakParticipantLinksBeforeDraw(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	_, parts := seedEvent(t, env, 3) // draft
	doRaw(env.handler, http.MethodPost, "/api/recover", fmt.Sprintf(`{"email":%q,"locale":"en"}`, parts[1].Email), nil, "")
	for _, m := range env.mailer.SentMessages() {
		if strings.Contains(m.Body, "/p/") {
			t.Fatalf("participant links must not be sent for an undrawn event:\n%s", m.Body)
		}
	}
	if len(env.mailer.SentMessages()) != 0 {
		t.Fatal("a participant of an undrawn event has nothing to recover")
	}
}

func TestEmailsAreSentAfterTheResponse(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	_, parts := seedDrawnEvent(t, env, 3)

	var queued []func()
	runBackground = func(f func()) { queued = append(queued, f) }

	rec := doRaw(env.handler, http.MethodPost, "/api/recover", fmt.Sprintf(`{"email":%q}`, parts[1].Email), nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	rec = doRaw(env.handler, http.MethodPost, "/api/events", `{"title":"T","first_name":"A","last_name":"B","email":"a@example.com"}`, nil, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if len(env.mailer.SentMessages()) != 0 || len(queued) != 2 {
		t.Fatalf("emails must be deferred until after the response: sent=%d queued=%d", len(env.mailer.SentMessages()), len(queued))
	}
	for _, f := range queued {
		f()
	}
	if len(env.mailer.SentMessages()) != 2 {
		t.Fatalf("expected 2 emails once the background work ran, got %d", len(env.mailer.SentMessages()))
	}
}

func TestBackgroundTasksCanBeAwaited(t *testing.T) {
	done := make(chan struct{})
	orig := runBackground
	// use the real implementation, captured before newTestEnv-style overrides
	runBackground = func(f func()) {
		backgroundTasks.Add(1)
		go func() { defer backgroundTasks.Done(); f() }()
	}
	defer func() { runBackground = orig }()
	runBackground(func() { time.Sleep(50 * time.Millisecond); close(done) })
	WaitForBackgroundTasks()
	select {
	case <-done:
	default:
		t.Fatal("WaitForBackgroundTasks must wait for pending work")
	}
}

func TestPublicEventCreationRateLimit(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	for i := 0; i < createEventLimit; i++ {
		rec := doRaw(env.handler, http.MethodPost, "/api/events", fmt.Sprintf(`{"title":"T","first_name":"A","last_name":"B","email":"o%d@example.com"}`, i), nil, "192.0.2.77:1")
		if rec.Code != http.StatusCreated {
			t.Fatalf("creation %d: expected 201, got %d", i, rec.Code)
		}
	}
	if rec := doRaw(env.handler, http.MethodPost, "/api/events", `{"title":"T","first_name":"A","last_name":"B","email":"x@example.com"}`, nil, "192.0.2.77:1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if rec := doRaw(env.handler, http.MethodPost, "/api/events", `{"title":"T","first_name":"A","last_name":"B","email":"y@example.com"}`, nil, "192.0.2.78:1"); rec.Code != http.StatusCreated {
		t.Fatalf("other clients must not be throttled, got %d", rec.Code)
	}
}

func TestRateLimiterWindowSlides(t *testing.T) {
	now := time.Now()
	l := newRateLimiter()
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if !l.take("k", 3, time.Minute) {
			t.Fatalf("hit %d must be allowed", i)
		}
	}
	if l.take("k", 3, time.Minute) {
		t.Fatal("4th hit within the window must be refused")
	}
	now = now.Add(61 * time.Second)
	if !l.take("k", 3, time.Minute) {
		t.Fatal("hits must expire after the window")
	}
	if l.count("k", time.Minute) != 1 {
		t.Fatal("expected one live hit")
	}
	// sweeping keeps the map bounded
	for i := 0; i < rateSweepThreshold+10; i++ {
		l.record(fmt.Sprintf("old-%d", i))
	}
	now = now.Add(2 * time.Hour)
	l.record("trigger")
	if len(l.hits) > 2 {
		t.Fatalf("stale keys must be swept, %d remain", len(l.hits))
	}
}

// ---------------------------------------------------- regression: JSON decoding

func TestStrictJSONDecoding(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	valid := `{"title":"T","first_name":"A","last_name":"B","email":"a@example.com"}`
	cases := []struct {
		name, body string
		want       int
	}{
		{"valid", valid, http.StatusCreated},
		{"unknown field", `{"title":"T","first_name":"A","last_name":"B","email":"b@example.com","admin":true}`, http.StatusBadRequest},
		{"trailing object", valid + `{"title":"x"}`, http.StatusBadRequest},
		{"trailing garbage", valid + ` garbage`, http.StatusBadRequest},
		{"not an object", `["a"]`, http.StatusBadRequest},
		{"wrong type", `{"title":123}`, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
		{"truncated", `{"title":"T"`, http.StatusBadRequest},
		{"too large", `{"title":"` + strings.Repeat("a", maxJSONBodyBytes+10) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for i, tc := range cases {
		rec := doRaw(env.handler, http.MethodPost, "/api/events", tc.body, nil, uniqueRemote(i))
		if rec.Code != tc.want {
			t.Fatalf("%s: expected %d, got %d (%s)", tc.name, tc.want, rec.Code, rec.Body.String())
		}
		var e map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatalf("%s: error responses must be JSON: %v", tc.name, err)
		}
	}
	// trailing whitespace is fine
	if rec := doRaw(env.handler, http.MethodPost, "/api/events", `{"title":"T","first_name":"A","last_name":"B","email":"ws@example.com"}`+" \n\n", nil, uniqueRemote(99)); rec.Code != http.StatusCreated {
		t.Fatalf("trailing whitespace must be accepted, got %d", rec.Code)
	}
}

// ---------------------------------------------------- regression: draw races

func TestConcurrentDrawsProduceExactlyOneDrawAndOneEmailPerParticipant(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, _ := seedEvent(t, env, 6)

	const workers = 8
	codes := make([]int, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev.AdminToken+"/draw", ``, nil, "").Code
		}(i)
	}
	wg.Wait()

	ok, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected status %d in %v", c, codes)
		}
	}
	if ok != 1 || conflict != workers-1 {
		t.Fatalf("expected exactly one 200 and %d 409, got %v", workers-1, codes)
	}
	assertDrawConsistent(t, env, ev.ID)
	if n := len(env.mailer.SentMessages()); n != 6 {
		t.Fatalf("expected exactly one email per participant (6), got %d", n)
	}
}

func TestConcurrentEditsDuringDrawNeverLeaveAnInconsistentEvent(t *testing.T) {
	for round := 0; round < 15; round++ {
		env := newTestEnv(t)
		ev, parts := seedEvent(t, env, 5)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev.AdminToken+"/draw", ``, nil, "")
		}()
		for k := 0; k < 3; k++ {
			wg.Add(1)
			go func(k int) {
				defer wg.Done()
				body := fmt.Sprintf(`{"first_name":"Late","last_name":"%d","email":"late%d-%d@example.com"}`, k, round, k)
				rec := doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev.AdminToken+"/participants", body, nil, "")
				if rec.Code != http.StatusCreated && rec.Code != http.StatusConflict {
					t.Errorf("add participant: unexpected status %d", rec.Code)
				}
			}(k)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := fmt.Sprintf(`{"participant_a_id":%q,"participant_b_id":%q,"reciprocal":true}`, parts[0].ID, parts[1].ID)
			rec := doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev.AdminToken+"/exclusions", body, nil, "")
			if rec.Code != http.StatusCreated && rec.Code != http.StatusConflict {
				t.Errorf("add exclusion: unexpected status %d", rec.Code)
			}
		}()
		wg.Wait()

		assertDrawConsistent(t, env, ev.ID)
		env.cleanup()
	}
}

func TestDraftOnlyOperationsRefusedAfterDraw(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, parts := seedDrawnEvent(t, env, 4)
	base := "/api/admin/events/" + ev.AdminToken
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"add participant":    doRaw(env.handler, http.MethodPost, base+"/participants", `{"first_name":"L","last_name":"C","email":"late@example.com"}`, nil, ""),
		"delete participant": doRaw(env.handler, http.MethodDelete, base+"/participants/"+parts[1].ID, ``, nil, ""),
		"add exclusion":      doRaw(env.handler, http.MethodPost, base+"/exclusions", fmt.Sprintf(`{"participant_a_id":%q,"participant_b_id":%q}`, parts[0].ID, parts[1].ID), nil, ""),
		"draw again":         doRaw(env.handler, http.MethodPost, base+"/draw", ``, nil, ""),
	} {
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s after the draw: expected 409, got %d", name, rec.Code)
		}
	}
	assertDrawConsistent(t, env, ev.ID)
}

// ---------------------------------------------------- cross-event authorization

func TestCrossEventAuthorizationMatrix(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	evA, partsA := seedEvent(t, env, 4)
	evB, partsB := seedEvent(t, env, 4)
	exB, err := env.repo.AddExclusion(evB.ID, partsB[1].ID, partsB[2].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	baseA := "/api/admin/events/" + evA.AdminToken
	foreign := partsB[1] // participant of B used with A's token

	notFound := []struct{ name, method, path, body string }{
		{"update foreign participant", http.MethodPut, baseA + "/participants/" + foreign.ID, `{"first_name":"Hacked","last_name":"X","email":"hacked@example.com"}`},
		{"delete foreign participant", http.MethodDelete, baseA + "/participants/" + foreign.ID, ``},
		{"exclusion with foreign giver", http.MethodPost, baseA + "/exclusions", fmt.Sprintf(`{"participant_a_id":%q,"participant_b_id":%q}`, foreign.ID, partsA[0].ID)},
		{"exclusion with foreign receiver", http.MethodPost, baseA + "/exclusions", fmt.Sprintf(`{"participant_a_id":%q,"participant_b_id":%q,"reciprocal":true}`, partsA[0].ID, foreign.ID)},
		{"exclusion between foreign participants", http.MethodPost, baseA + "/exclusions", fmt.Sprintf(`{"participant_a_id":%q,"participant_b_id":%q}`, partsB[0].ID, partsB[3].ID)},
		{"delete foreign exclusion", http.MethodDelete, baseA + "/exclusions/" + exB[0].ID, ``},
		{"participant token as admin token", http.MethodGet, "/api/admin/events/" + partsA[0].ParticipantToken, ``},
		{"admin token as participant token", http.MethodGet, "/api/p/" + evA.AdminToken, ``},
		{"event id as admin token", http.MethodGet, "/api/admin/events/" + evA.ID, ``},
	}
	for _, tc := range notFound {
		rec := doRaw(env.handler, tc.method, tc.path, tc.body, nil, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d (%s)", tc.name, rec.Code, rec.Body.String())
		}
	}

	// nothing of event B changed, nothing leaked into A
	after, _ := env.repo.ListParticipants(evB.ID)
	if len(after) != 4 {
		t.Fatalf("event B lost participants: %d", len(after))
	}
	if after[1].FirstName == "Hacked" || after[1].Email == "hacked@example.com" {
		t.Fatal("event B participant was modified through event A's token")
	}
	if exs, _ := env.repo.ListExclusions(evB.ID); len(exs) != 1 {
		t.Fatalf("event B exclusion was affected: %d", len(exs))
	}
	if exs, _ := env.repo.ListExclusions(evA.ID); len(exs) != 0 {
		t.Fatalf("event A gained exclusions from foreign participants: %d", len(exs))
	}

	// drawn events: resend and wishlists are scoped to the event / participant
	evC, partsC := seedDrawnEvent(t, env, 4)
	evD, partsD := seedDrawnEvent(t, env, 4)
	baseC := "/api/admin/events/" + evC.AdminToken
	if rec := doRaw(env.handler, http.MethodPost, baseC+"/participants/"+partsD[0].ID+"/resend", ``, nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("resend to a foreign participant: expected 404, got %d", rec.Code)
	}
	if n := len(env.mailer.SentMessages()); n != 0 {
		t.Fatalf("a rejected resend must not send email, %d sent", n)
	}

	itemD, err := env.repo.AddWishlistItem(partsD[0].ID, "Private gift", "", "")
	if err != nil {
		t.Fatal(err)
	}
	itemSameEvent, _ := env.repo.AddWishlistItem(partsC[1].ID, "Neighbour gift", "", "")
	tokC := partsC[0].ParticipantToken
	for _, id := range []string{itemD.ID, itemSameEvent.ID} {
		if rec := doRaw(env.handler, http.MethodPut, "/api/p/"+tokC+"/wishlist/"+id, `{"title":"Overwritten"}`, nil, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("update of someone else's wishlist item: expected 404, got %d", rec.Code)
		}
		if rec := doRaw(env.handler, http.MethodDelete, "/api/p/"+tokC+"/wishlist/"+id, ``, nil, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("delete of someone else's wishlist item: expected 404, got %d", rec.Code)
		}
	}
	itemsD, _ := env.repo.ListWishlistItems(partsD[0].ID)
	itemsC, _ := env.repo.ListWishlistItems(partsC[1].ID)
	if len(itemsD) != 1 || itemsD[0].Title != "Private gift" || len(itemsC) != 1 || itemsC[0].Title != "Neighbour gift" {
		t.Fatal("wishlist items of other participants were modified")
	}
	_ = evD
}

func TestUnknownTokensNeverReachHandlersLogic(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	admin := "/api/admin/events/nope"
	routes := []struct{ method, path string }{
		{http.MethodGet, admin},
		{http.MethodPut, admin},
		{http.MethodDelete, admin},
		{http.MethodPost, admin + "/participants"},
		{http.MethodPut, admin + "/participants/x"},
		{http.MethodDelete, admin + "/participants/x"},
		{http.MethodPost, admin + "/exclusions"},
		{http.MethodDelete, admin + "/exclusions/x"},
		{http.MethodPost, admin + "/draw"},
		{http.MethodPost, admin + "/participants/x/resend"},
		{http.MethodPost, admin + "/resend-failed"},
		{http.MethodGet, "/api/p/nope"},
		{http.MethodPost, "/api/p/nope/reveal"},
		{http.MethodPost, "/api/p/nope/wishlist"},
		{http.MethodPut, "/api/p/nope/wishlist/x"},
		{http.MethodDelete, "/api/p/nope/wishlist/x"},
	}
	for _, rt := range routes {
		rec := doRaw(env.handler, rt.method, rt.path, `{}`, nil, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: expected 404, got %d", rt.method, rt.path, rec.Code)
		}
	}
	// wrong HTTP method -> 405, unknown API path -> 404 JSON (with a static FS configured)
	if rec := doRaw(env.handler, http.MethodPatch, "/api/events", `{}`, nil, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// ---------------------------------------------------- privacy of assignments

func TestAssignmentsNeverLeakBeforeReveal(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, parts := seedDrawnEvent(t, env, 4)
	giver := parts[0]
	target := parts[1] // seedDrawnEvent assigns i -> i+1

	rec := doRaw(env.handler, http.MethodGet, "/api/p/"+giver.ParticipantToken, ``, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if _, has := raw["target"]; has {
		t.Fatal("target must be omitted before the reveal")
	}
	for _, secret := range []string{target.FirstName + " " + target.LastName, target.Email, target.ID, target.ParticipantToken, "assigned"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("response leaks %q before the reveal: %s", secret, rec.Body.String())
		}
	}
	// the organizer's admin view and the super admin overview never expose assignments
	adm := doRaw(env.handler, http.MethodGet, "/api/admin/events/"+ev.AdminToken, ``, nil, "")
	if strings.Contains(adm.Body.String(), "assigned") || strings.Contains(adm.Body.String(), `"target"`) {
		t.Fatalf("admin view must not expose assignments: %s", adm.Body.String())
	}
	ov := doJSON(t, env.handler, http.MethodGet, "/api/superadmin/overview", nil, loginSuper(t, env))
	if strings.Contains(ov.Body.String(), "assigned") || strings.Contains(ov.Body.String(), `"target"`) {
		t.Fatalf("super admin overview must not expose assignments: %s", ov.Body.String())
	}

	// after the reveal the target is exposed, and only to the giver
	rec = doRaw(env.handler, http.MethodPost, "/api/p/"+giver.ParticipantToken+"/reveal", ``, nil, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), target.FirstName) {
		t.Fatalf("reveal failed: %d %s", rec.Code, rec.Body.String())
	}
	other := doRaw(env.handler, http.MethodGet, "/api/p/"+parts[2].ParticipantToken, ``, nil, "")
	if strings.Contains(other.Body.String(), "\"target\"") {
		t.Fatal("revealing one participant must not reveal another's target")
	}
}

// ---------------------------------------------------- headers, cookies, CSRF, static files

func TestSecurityHeadersOnEveryKindOfResponse(t *testing.T) {
	static := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>app</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	build := func(mutate func(*config.Config)) http.Handler {
		env := newTestEnvWith(t, mutate)
		t.Cleanup(env.cleanup)
		return NewRouter(env.cfg, env.repo, mailer.NewMockMailer(), static)
	}
	secure := build(nil) // https base URL
	plain := build(func(c *config.Config) { c.AppBaseURL = "http://localhost:8080" })

	requests := []struct{ name, method, path, body string }{
		{"api ok", http.MethodGet, "/api/health", ""},
		{"api 404", http.MethodGet, "/api/nope", ""},
		{"api 400", http.MethodPost, "/api/recover", `{}`},
		{"api 401", http.MethodGet, "/api/superadmin/overview", ""},
		{"spa index", http.MethodGet, "/", ""},
		{"spa deep link", http.MethodGet, "/admin/events/abc", ""},
		{"static asset", http.MethodGet, "/assets/app.js", ""},
		{"missing asset", http.MethodGet, "/assets/missing.js", ""},
	}
	for _, rq := range requests {
		for label, h := range map[string]http.Handler{"https": secure, "http": plain} {
			rec := doRaw(h, rq.method, rq.path, rq.body, nil, "")
			hd := rec.Header()
			want := map[string]string{
				"Referrer-Policy":              "no-referrer",
				"X-Content-Type-Options":       "nosniff",
				"X-Frame-Options":              "DENY",
				"Cross-Origin-Opener-Policy":   "same-origin",
				"Cross-Origin-Resource-Policy": "same-origin",
			}
			for k, v := range want {
				if got := hd.Get(k); got != v {
					t.Fatalf("[%s %s] %s = %q, want %q", label, rq.name, k, got, v)
				}
			}
			csp := hd.Get("Content-Security-Policy")
			for _, must := range []string{"default-src 'self'", "script-src 'self'", "frame-ancestors 'none'", "base-uri 'self'", "form-action 'self'", "connect-src 'self'"} {
				if !strings.Contains(csp, must) {
					t.Fatalf("[%s %s] CSP lacks %q: %s", label, rq.name, must, csp)
				}
			}
			scriptSrc := strings.SplitN(strings.SplitN(csp, "script-src", 2)[1], ";", 2)[0]
			if strings.Contains(scriptSrc, "unsafe") || strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "*") {
				t.Fatalf("[%s %s] CSP must not allow inline/eval scripts or wildcards: %s", label, rq.name, csp)
			}
			if !strings.Contains(hd.Get("Permissions-Policy"), "camera=()") {
				t.Fatalf("[%s %s] missing Permissions-Policy", label, rq.name)
			}
			hsts := hd.Get("Strict-Transport-Security")
			if label == "https" && !strings.Contains(hsts, "max-age=") {
				t.Fatalf("[https %s] HSTS expected", rq.name)
			}
			if label == "http" && hsts != "" {
				t.Fatalf("[http %s] HSTS must not be sent over plain http, got %q", rq.name, hsts)
			}
			if strings.HasPrefix(rq.path, "/api/") && hd.Get("Cache-Control") != "no-store" {
				t.Fatalf("[%s %s] API responses must be no-store, got %q", label, rq.name, hd.Get("Cache-Control"))
			}
		}
	}
}

func TestStaticFileServingIsSafe(t *testing.T) {
	static := fstest.MapFS{
		"index.html":    {Data: []byte("INDEX")},
		"assets/app.js": {Data: []byte("JS")},
	}
	env := newTestEnv(t)
	defer env.cleanup()
	h := NewRouter(env.cfg, env.repo, mailer.NewMockMailer(), static)

	get := func(path string) *httptest.ResponseRecorder { return doRaw(h, http.MethodGet, path, "", nil, "") }
	if rec := get("/assets/app.js"); rec.Code != http.StatusOK || rec.Body.String() != "JS" {
		t.Fatalf("asset: %d %q", rec.Code, rec.Body.String())
	}
	for _, p := range []string{"/", "/admin/events/xyz", "/p/xyz", "/superadmin"} {
		if rec := get(p); rec.Code != http.StatusOK || rec.Body.String() != "INDEX" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("SPA fallback for %s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
	if rec := get("/assets/missing.js"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset must 404 rather than return index.html, got %d", rec.Code)
	}
	for _, p := range []string{"/../go.mod", "/%2e%2e/go.mod", "/assets/../../go.mod", "/..%2f..%2fetc/passwd", "//etc/passwd"} {
		rec := get(p)
		if strings.Contains(rec.Body.String(), "module ") || strings.Contains(rec.Body.String(), "root:") {
			t.Fatalf("path traversal via %s leaked a file: %s", p, rec.Body.String())
		}
	}
	rec := get("/api/unknown")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unknown API route must return a JSON 404, got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestCrossOriginRequestsAreRejected(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	body := `{}` // reaches /api/recover's validation => 400 when not blocked

	cases := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{"cross-site fetch metadata", http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, http.StatusForbidden},
		{"same-site (sibling subdomain)", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "https://evil.example.com"}, http.StatusForbidden},
		{"foreign Origin without fetch metadata", http.MethodPost, map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"same-origin fetch metadata", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusBadRequest},
		{"matching Origin", http.MethodPost, map[string]string{"Origin": "http://example.com"}, http.StatusBadRequest},
		{"non-browser client (no Origin)", http.MethodPost, nil, http.StatusBadRequest},
		{"cross-site GET is safe", http.MethodGet, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		rec := doRaw(env.handler, tc.method, "/api/recover", body, tc.headers, "")
		if rec.Code != tc.want {
			t.Fatalf("%s: expected %d, got %d", tc.name, tc.want, rec.Code)
		}
	}
	// state-changing admin endpoints are protected too
	ev, _ := seedEvent(t, env, 3)
	rec := doRaw(env.handler, http.MethodDelete, "/api/admin/events/"+ev.AdminToken, ``, map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site delete must be rejected, got %d", rec.Code)
	}
	if _, err := env.repo.GetEventByID(ev.ID); err != nil {
		t.Fatal("event must survive a cross-site delete attempt")
	}
}

func TestSuperAdminCookieAttributes(t *testing.T) {
	for label, mutate := range map[string]func(*config.Config){
		"https": nil,
		"production": func(c *config.Config) {
			c.AppEnv = "production"
			c.AppBaseURL = "http://internal.example"
		},
		"http-dev": func(c *config.Config) { c.AppBaseURL = "http://localhost:8080" },
	} {
		env := newTestEnvWith(t, mutate)
		c := loginSuper(t, env)
		wantSecure := label != "http-dev"
		if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/api/superadmin" || c.MaxAge != 86400 || c.Secure != wantSecure {
			t.Fatalf("[%s] unexpected cookie attributes: %+v", label, c)
		}
		rec := doJSON(t, env.handler, http.MethodPost, "/api/superadmin/logout", nil, c)
		for _, lc := range rec.Result().Cookies() {
			if lc.Name == superAdminCookieName && (!lc.HttpOnly || lc.SameSite != http.SameSiteStrictMode || lc.Secure != wantSecure || lc.Value != "") {
				t.Fatalf("[%s] logout cookie must keep the same protections: %+v", label, lc)
			}
		}
		env.cleanup()
	}
}

// ---------------------------------------------------- input validation boundaries

func TestEventCreationInputBoundaries(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	n := 0
	create := func(title, first, last, email string) *httptest.ResponseRecorder {
		n++
		body, _ := json.Marshal(map[string]string{"title": title, "first_name": first, "last_name": last, "email": email})
		return doRaw(env.handler, http.MethodPost, "/api/events", string(body), nil, uniqueRemote(n))
	}
	email := func(total int) string {
		local, domain := strings.Repeat("a", 64), ""
		for len(local)+1+len(domain) < total {
			label := strings.Repeat("b", min(63, total-len(local)-1-len(domain)-1))
			domain += label + "."
		}
		return local + "@" + strings.TrimSuffix(domain, ".") + ".com"
	}
	e254, e255 := email(254), email(255)
	if len(e254) > 254 {
		t.Skipf("email generator produced %d chars", len(e254))
	}

	ok := []struct{ name, title, first, last, email string }{
		{"200-char title", strings.Repeat("a", 200), "A", "B", "t1@example.com"},
		{"200 multibyte runes (>200 bytes)", strings.Repeat("é", 200), "A", "B", "t2@example.com"},
		{"100-char names", "T", strings.Repeat("n", 100), strings.Repeat("m", 100), "t3@example.com"},
		{"plus addressing", "T", "A", "B", "first+tag@example.com"},
		{"emoji names", "T", "🎅", "🧑‍🎄", "t4@example.com"},
		{"surrounding spaces are trimmed", "  T  ", " A ", " B ", "  t5@example.com  "},
		{"upper-case email is normalised", "T", "A", "B", "UPPER@Example.COM"},
	}
	for _, tc := range ok {
		if rec := create(tc.title, tc.first, tc.last, tc.email); rec.Code != http.StatusCreated {
			t.Fatalf("%s: expected 201, got %d (%s)", tc.name, rec.Code, rec.Body.String())
		}
	}
	bad := []struct{ name, title, first, last, email string }{
		{"201-char title", strings.Repeat("a", 201), "A", "B", "b1@example.com"},
		{"201 multibyte runes", strings.Repeat("é", 201), "A", "B", "b2@example.com"},
		{"empty title", "", "A", "B", "b3@example.com"},
		{"blank title", "   ", "A", "B", "b4@example.com"},
		{"101-char first name", "T", strings.Repeat("n", 101), "B", "b5@example.com"},
		{"101-char last name", "T", "A", strings.Repeat("n", 101), "b6@example.com"},
		{"empty first name", "T", "", "B", "b7@example.com"},
		{"email without domain dot", "T", "A", "B", "user@localhost"},
		{"email with display name", "T", "A", "B", "Bob <bob@example.com>"},
		{"email with newline", "T", "A", "B", "a@example.com\nBcc: x@example.com"},
		{"email with space", "T", "A", "B", "a b@example.com"},
		{"email without at", "T", "A", "B", "example.com"},
		{"two ats", "T", "A", "B", "a@b@example.com"},
		{"empty email", "T", "A", "B", ""},
		{"255-char email", "T", "A", "B", e255},
	}
	for _, tc := range bad {
		if rec := create(tc.title, tc.first, tc.last, tc.email); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d (%s)", tc.name, rec.Code, rec.Body.String())
		}
	}
	if rec := create("T", "A", "B", e254); rec.Code != http.StatusCreated {
		t.Fatalf("254-char email: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	// normalisation is persisted
	if links, _, _ := env.repo.FindLinksByEmail("upper@example.com"); len(links) != 1 {
		t.Fatal("upper-case email must be stored normalised")
	}
}

func TestEventUpdateAndParticipantBoundaries(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, parts := seedEvent(t, env, 3)
	base := "/api/admin/events/" + ev.AdminToken
	put := func(title, desc, date, budget string) int {
		body, _ := json.Marshal(map[string]string{"title": title, "description": desc, "event_date": date, "budget": budget})
		return doRaw(env.handler, http.MethodPut, base, string(body), nil, "").Code
	}
	if c := put(strings.Repeat("t", 200), strings.Repeat("d", 2000), "2026-12-25", strings.Repeat("b", 100)); c != http.StatusOK {
		t.Fatalf("max lengths must be accepted, got %d", c)
	}
	for name, c := range map[string]int{
		"title":       put(strings.Repeat("t", 201), "", "", ""),
		"empty title": put("", "", "", ""),
		"description": put("T", strings.Repeat("d", 2001), "", ""),
		"date":        put("T", "", strings.Repeat("x", 101), ""),
		"bad date":    put("T", "", "25/12/2026", ""),
		"not a date":  put("T", "", "next friday", ""),
		"budget":      put("T", "", "", strings.Repeat("b", 101)),
	} {
		if c != http.StatusBadRequest {
			t.Fatalf("%s too long: expected 400, got %d", name, c)
		}
	}

	if c := put("T", "", "", ""); c != http.StatusOK {
		t.Fatalf("empty date must be accepted, got %d", c)
	}

	// duplicate emails are refused, case-insensitively, on add and on update
	dup := fmt.Sprintf(`{"first_name":"X","last_name":"Y","email":%q}`, strings.ToUpper(parts[1].Email))
	if c := doRaw(env.handler, http.MethodPost, base+"/participants", dup, nil, "").Code; c != http.StatusConflict {
		t.Fatalf("duplicate on add: expected 409, got %d", c)
	}
	if c := doRaw(env.handler, http.MethodPut, base+"/participants/"+parts[2].ID, dup, nil, "").Code; c != http.StatusConflict {
		t.Fatalf("duplicate on update: expected 409, got %d", c)
	}

	// 100 participants maximum
	for i := len(parts); i < 100; i++ {
		if _, err := env.repo.AddParticipant(ev.ID, "P", fmt.Sprint(i), fmt.Sprintf("bulk%d@example.com", i)); err != nil {
			t.Fatal(err)
		}
	}
	if c := doRaw(env.handler, http.MethodPost, base+"/participants", `{"first_name":"One","last_name":"Too many","email":"overflow@example.com"}`, nil, "").Code; c != http.StatusBadRequest {
		t.Fatalf("101st participant: expected 400, got %d", c)
	}
}

func TestWishlistValidationOnAddAndUpdate(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	_, parts := seedDrawnEvent(t, env, 3)
	tok := parts[0].ParticipantToken
	post := func(title, url, note string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]string{"title": title, "url": url, "note": note})
		return doRaw(env.handler, http.MethodPost, "/api/p/"+tok+"/wishlist", string(b), nil, "")
	}
	rec := post("Valid", "example.com/item", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var self ParticipantSelfDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &self)
	if len(self.OwnWishlist) != 1 || self.OwnWishlist[0].URL != "https://example.com/item" {
		t.Fatalf("scheme-less URLs must be normalised to https, got %+v", self.OwnWishlist)
	}
	itemID := self.OwnWishlist[0].ID

	badURLs := []string{
		"javascript:alert(1)", "JaVaScRiPt:alert(1)", "data:text/html,<script>alert(1)</script>", "vbscript:msgbox(1)",
		"ftp://example.com/file", "file:///etc/passwd", "//evil.example/x", "https://user:pass@example.com/",
		"http://", "https://exa mple.com", "https://example.com/\nX: y", "https://example.com/\x00",
		"https://" + strings.Repeat("a", 2100),
	}
	for _, u := range badURLs {
		if r := post("Gift", u, ""); r.Code != http.StatusBadRequest {
			t.Fatalf("add with URL %q: expected 400, got %d", truncate(u), r.Code)
		}
		b, _ := json.Marshal(map[string]string{"title": "Gift", "url": u})
		if r := doRaw(env.handler, http.MethodPut, "/api/p/"+tok+"/wishlist/"+itemID, string(b), nil, ""); r.Code != http.StatusBadRequest {
			t.Fatalf("update with URL %q: expected 400, got %d", truncate(u), r.Code)
		}
	}
	if r := post(strings.Repeat("t", 201), "", ""); r.Code != http.StatusBadRequest {
		t.Fatalf("201-char title: got %d", r.Code)
	}
	if r := post("   ", "", ""); r.Code != http.StatusBadRequest {
		t.Fatalf("blank title: got %d", r.Code)
	}
	if r := post("T", "", strings.Repeat("n", 1001)); r.Code != http.StatusBadRequest {
		t.Fatalf("1001-char note: got %d", r.Code)
	}
	if r := post(strings.Repeat("é", 200), "", strings.Repeat("é", 1000)); r.Code != http.StatusCreated {
		t.Fatalf("limits are counted in runes: got %d", r.Code)
	}
	// HTML is stored verbatim as data (the SPA escapes it); it must never be altered into markup server-side
	r := post(`<img src=x onerror=alert(1)>`, "", `<script>alert(1)</script>`)
	if r.Code != http.StatusCreated || r.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("got %d %s", r.Code, r.Header().Get("Content-Type"))
	}
	// 50 items maximum
	items, _ := env.repo.ListWishlistItems(parts[0].ID)
	for i := len(items); i < 50; i++ {
		_, _ = env.repo.AddWishlistItem(parts[0].ID, fmt.Sprintf("Item %d", i), "", "")
	}
	if r := post("One more", "", ""); r.Code != http.StatusBadRequest {
		t.Fatalf("51st item: expected 400, got %d", r.Code)
	}
}

func truncate(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}

// ---------------------------------------------------- email failure handling

func TestResendFailedEmailsOnlyTargetsFailures(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, parts := seedEvent(t, env, 4)
	env.mailer.SetFailFor(parts[2].Email, fmt.Errorf("smtp 451 try later"))

	if rec := doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev.AdminToken+"/draw", ``, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("draw: %d", rec.Code)
	}
	after, _ := env.repo.ListParticipants(ev.ID)
	failed := 0
	for _, p := range after {
		if p.EmailSentAt == nil {
			failed++
			if p.EmailError == nil || !strings.Contains(*p.EmailError, "451") {
				t.Fatalf("failure reason must be recorded, got %v", p.EmailError)
			}
		}
	}
	if failed != 1 {
		t.Fatalf("expected exactly 1 failed email, got %d", failed)
	}

	env.mailer.Reset() // SMTP recovered
	if rec := doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev.AdminToken+"/resend-failed", ``, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("resend-failed: %d", rec.Code)
	}
	msgs := env.mailer.SentMessages()
	if len(msgs) != 1 || msgs[0].To != parts[2].Email {
		t.Fatalf("only the failed participant must be re-sent, got %+v", msgs)
	}
	for _, p := range func() []db.Participant { l, _ := env.repo.ListParticipants(ev.ID); return l }() {
		if p.EmailSentAt == nil || p.EmailError != nil {
			t.Fatalf("all participants must now be marked as sent: %+v", p)
		}
	}
	// a second pass is a no-op
	env.mailer.Reset()
	doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev.AdminToken+"/resend-failed", ``, nil, "")
	if len(env.mailer.SentMessages()) != 0 {
		t.Fatal("nothing left to resend")
	}
	// resend endpoints are refused before the draw
	ev2, parts2 := seedEvent(t, env, 3)
	for _, path := range []string{"/resend-failed", "/participants/" + parts2[0].ID + "/resend"} {
		if c := doRaw(env.handler, http.MethodPost, "/api/admin/events/"+ev2.AdminToken+path, ``, nil, "").Code; c != http.StatusBadRequest {
			t.Fatalf("%s before the draw: expected 400, got %d", path, c)
		}
	}
}

// ---------------------------------------------------- database failure

func TestDatabaseFailureYieldsCleanJSONErrorsOnEveryRoute(t *testing.T) {
	env := newTestEnv(t)
	cookie := loginSuper(t, env)
	ev, parts := seedDrawnEvent(t, env, 4)
	draft, _ := seedEvent(t, env, 3)
	item, _ := env.repo.AddWishlistItem(parts[0].ID, "Gift", "", "")

	_ = env.db.Close() // simulate the SQLite file becoming unavailable

	adm := "/api/admin/events/" + ev.AdminToken
	pt := "/api/p/" + parts[0].ParticipantToken
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/public/config", ``},
		{http.MethodPost, "/api/events", `{"title":"T","first_name":"A","last_name":"B","email":"a@example.com"}`},
		{http.MethodGet, adm, ``},
		{http.MethodPut, adm, `{"title":"T"}`},
		{http.MethodDelete, adm, ``},
		{http.MethodPost, adm + "/participants", `{"first_name":"A","last_name":"B","email":"a@example.com"}`},
		{http.MethodPut, adm + "/participants/" + parts[1].ID, `{"first_name":"A","last_name":"B","email":"a@example.com"}`},
		{http.MethodDelete, adm + "/participants/" + parts[1].ID, ``},
		{http.MethodPost, adm + "/exclusions", `{"participant_a_id":"a","participant_b_id":"b"}`},
		{http.MethodDelete, adm + "/exclusions/x", ``},
		{http.MethodPost, "/api/admin/events/" + draft.AdminToken + "/draw", ``},
		{http.MethodPost, adm + "/participants/" + parts[1].ID + "/resend", ``},
		{http.MethodPost, adm + "/resend-failed", ``},
		{http.MethodGet, pt, ``},
		{http.MethodPost, pt + "/reveal", ``},
		{http.MethodPost, pt + "/wishlist", `{"title":"T"}`},
		{http.MethodPut, pt + "/wishlist/" + item.ID, `{"title":"T"}`},
		{http.MethodDelete, pt + "/wishlist/" + item.ID, ``},
		{http.MethodGet, "/api/superadmin/overview", ``},
		{http.MethodPut, "/api/superadmin/settings", `{"allow_public_event_creation":true}`},
		{http.MethodPost, "/api/superadmin/invite", `{"title":"T","first_name":"A","last_name":"B","email":"a@example.com"}`},
		{http.MethodPost, "/api/superadmin/events/" + ev.ID + "/resend-admin", ``},
		{http.MethodDelete, "/api/superadmin/events/" + ev.ID, ``},
	}
	for _, rt := range routes {
		req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req) // must not panic
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s %s: expected a clean 500 with the database down, got %d (%s)", rt.method, rt.path, rec.Code, rec.Body.String())
		}
		var e map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e["error"] == "" {
			t.Fatalf("%s %s: error body must be JSON with an error message, got %q", rt.method, rt.path, rec.Body.String())
		}
		if strings.Contains(strings.ToLower(rec.Body.String()), "sql") || strings.Contains(rec.Body.String(), "closed") {
			t.Fatalf("%s %s: internal error details leaked to the client: %s", rt.method, rt.path, rec.Body.String())
		}
	}
	// health reports the broken database, see TestHealthReturns503WhenDBIsDown
}

// ---------------------------------------------------- slow SMTP vs server write timeout

type slowBatchMailer struct {
	*mailer.MockMailer
	delay time.Duration
}

func (m slowBatchMailer) SendBatch(msgs []mailer.Message) []error {
	time.Sleep(m.delay)
	return m.MockMailer.SendBatch(msgs)
}

// The draw sends all emails before answering. With a slow SMTP server this must
// not trip the server-wide WriteTimeout (which would drop the response although
// the draw succeeded).
func TestDrawResponseSurvivesSlowEmailBatchOnRealServer(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ev, _ := seedEvent(t, env, 4)

	h := NewRouter(env.cfg, env.repo, slowBatchMailer{MockMailer: env.mailer, delay: 600 * time.Millisecond}, nil)
	srv := httptest.NewUnstartedServer(h)
	srv.Config.WriteTimeout = 200 * time.Millisecond // far shorter than the email batch
	srv.Start()
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/admin/events/"+ev.AdminToken+"/draw", "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatalf("the response must not be cut by the write timeout: %v", err)
	}
	defer resp.Body.Close()
	var out AdminEventResponseDTO
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK || out.Status != "drawn" {
		t.Fatalf("expected a complete 200 drawn response, got %d (decode error: %v)", resp.StatusCode, err)
	}
	for _, p := range out.Participants {
		if p.EmailSentAt == nil {
			t.Fatalf("emails were sent before answering, %s should be marked as sent", p.Email)
		}
	}
}

func TestHealthReturns503WhenDBIsDown(t *testing.T) {
	env := newTestEnv(t)
	_ = env.db.Close()
	if rec := doRaw(env.handler, http.MethodGet, "/api/health", ``, nil, ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health: %d", rec.Code)
	}
}
