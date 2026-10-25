package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"secret-santa/internal/config"
	"secret-santa/internal/db"
	"secret-santa/internal/mailer"
)

type testEnv struct {
	handler http.Handler
	repo    *db.Repository
	mailer  *mailer.MockMailer
	cfg     *config.Config
	cleanup func()
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	sqlDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	cfg := &config.Config{
		AppEnv:             "development",
		Port:               "8080",
		AppBaseURL:         "https://santa.example.com",
		DBPath:             ":memory:",
		SuperAdminEmail:    "super@example.com",
		SuperAdminPassword: "secret-password-123",
		SessionSecret:      "0123456789abcdef0123456789abcdef",
	}
	repo := db.NewRepository(sqlDB)
	mockMail := mailer.NewMockMailer()
	handler := NewRouter(cfg, repo, mockMail, nil)

	return &testEnv{
		handler: handler,
		repo:    repo,
		mailer:  mockMail,
		cfg:     cfg,
		cleanup: func() { _ = sqlDB.Close() },
	}
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode json: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEndToEndAllUserStories(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	// 1. Security Headers & Public Config
	rec := doJSON(t, env.handler, http.MethodGet, "/api/public/config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("expected Referrer-Policy: no-referrer, got %q", got)
	}

	// 2. Create Event Publicly (US-1.2)
	rec = doJSON(t, env.handler, http.MethodPost, "/api/events", CreateEventRequest{
		Title:     "Noël des Amis",
		FirstName: "Alice",
		LastName:  "Martin",
		Email:     "Alice@example.com",
		Locale:    "fr",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create event, got %d: %s", rec.Code, rec.Body.String())
	}
	var created CreateEventResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.AdminToken == "" {
		t.Fatal("expected non-empty admin_token")
	}
	if len(env.mailer.SentMessages()) != 1 {
		t.Fatalf("expected 1 admin magic link email, got %d", len(env.mailer.SentMessages()))
	}

	// 3. Access Event Admin Dashboard (US-2.1) & Check Organizer Participation (US-2.2)
	rec = doJSON(t, env.handler, http.MethodGet, "/api/admin/events/"+created.AdminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on get admin event, got %d", rec.Code)
	}
	var adminEv AdminEventResponseDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &adminEv)
	if len(adminEv.Participants) != 1 || !adminEv.Participants[0].IsOrganizer {
		t.Fatalf("expected organizer pre-added as participant, got %+v", adminEv.Participants)
	}

	// Uncheck "I am participating" -> removes organizer row
	f := false
	rec = doJSON(t, env.handler, http.MethodPut, "/api/admin/events/"+created.AdminToken, UpdateEventRequest{
		Title:                 "Noël des Amis 2026",
		Description:           "Au chalet",
		EventDate:             "2026-12-25",
		Budget:                "30 €",
		OrganizerParticipates: &f,
	})
	_ = json.Unmarshal(rec.Body.Bytes(), &adminEv)
	if len(adminEv.Participants) != 0 || adminEv.OrganizerParticipates {
		t.Fatalf("expected 0 participants after unchecking organizer_participates, got %d", len(adminEv.Participants))
	}

	// Re-check "I am participating" -> re-adds organizer row
	tr := true
	rec = doJSON(t, env.handler, http.MethodPut, "/api/admin/events/"+created.AdminToken, UpdateEventRequest{
		Title:                 "Noël des Amis 2026",
		Description:           "Au chalet",
		EventDate:             "2026-12-25",
		Budget:                "30 €",
		OrganizerParticipates: &tr,
	})
	_ = json.Unmarshal(rec.Body.Bytes(), &adminEv)
	if len(adminEv.Participants) != 1 || !adminEv.Participants[0].IsOrganizer {
		t.Fatalf("expected organizer re-added, got %+v", adminEv.Participants)
	}

	// 4. Add Participants (US-2.3) & Duplicate Check
	for _, p := range []UpsertParticipantRequest{
		{FirstName: "Bob", LastName: "Dupont", Email: "bob@example.com"},
		{FirstName: "Charlie", LastName: "Durand", Email: "charlie@example.com"},
		{FirstName: "Diane", LastName: "Moreau", Email: "diane@example.com"},
	} {
		rec = doJSON(t, env.handler, http.MethodPost, "/api/admin/events/"+created.AdminToken+"/participants", p)
		if rec.Code != http.StatusCreated {
			t.Fatalf("add participant %s failed: %d %s", p.Email, rec.Code, rec.Body.String())
		}
	}
	// Duplicate email case-insensitive
	rec = doJSON(t, env.handler, http.MethodPost, "/api/admin/events/"+created.AdminToken+"/participants", UpsertParticipantRequest{
		FirstName: "Bob2", LastName: "Dupont", Email: "BOB@EXAMPLE.COM",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate email, got %d", rec.Code)
	}
	rec = doJSON(t, env.handler, http.MethodGet, "/api/admin/events/"+created.AdminToken, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &adminEv)
	if len(adminEv.Participants) != 4 {
		t.Fatalf("expected 4 participants, got %d", len(adminEv.Participants))
	}

	// 5. Mutual Exclusions & Feasibility (US-2.4, US-2.5)
	pAlice := adminEv.Participants[0].ID
	pBob := adminEv.Participants[1].ID
	rec = doJSON(t, env.handler, http.MethodPost, "/api/admin/events/"+created.AdminToken+"/exclusions", CreateExclusionRequest{
		ParticipantAID: pBob,
		ParticipantBID: pAlice,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on add exclusion, got %d: %s", rec.Code, rec.Body.String())
	}
	// Duplicate exclusion in reverse order -> 409 Conflict
	rec = doJSON(t, env.handler, http.MethodPost, "/api/admin/events/"+created.AdminToken+"/exclusions", CreateExclusionRequest{
		ParticipantAID: pAlice,
		ParticipantBID: pBob,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on symmetric duplicate exclusion, got %d", rec.Code)
	}
	_ = json.Unmarshal(doJSON(t, env.handler, http.MethodGet, "/api/admin/events/"+created.AdminToken, nil).Body.Bytes(), &adminEv)
	if adminEv.Feasibility.Mode != "single_cycle" {
		t.Fatalf("expected feasibility single_cycle, got %s", adminEv.Feasibility.Mode)
	}

	// 6. Execute Draw with Partial SMTP Failure & Double-Click Protection (US-3.1, US-4.1)
	env.mailer.Reset()
	env.mailer.SetFailFor("diane@example.com", errors.New("smtp 451 temporary failure"))
	rec = doJSON(t, env.handler, http.MethodPost, "/api/admin/events/"+created.AdminToken+"/draw", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on draw, got %d: %s", rec.Code, rec.Body.String())
	}
	rawAdminJSON := rec.Body.String()
	if strings.Contains(rawAdminJSON, "assigned_to") {
		t.Fatalf("PRIVACY LEAK: admin response contains assigned_to: %s", rawAdminJSON)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &adminEv)
	if adminEv.Status != "drawn" || adminEv.OrganizerParticipantToken == "" {
		t.Fatalf("expected drawn status and organizer_participant_token, got %+v", adminEv)
	}

	// Double-click draw -> 409 Conflict
	rec = doJSON(t, env.handler, http.MethodPost, "/api/admin/events/"+created.AdminToken+"/draw", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on second draw, got %d", rec.Code)
	}

	// Verify email status recorded: 3 sent, 1 failed (Diane)
	failedCount := 0
	for _, p := range adminEv.Participants {
		if p.Email == "diane@example.com" {
			if p.EmailSentAt != nil || p.EmailError == nil {
				t.Fatalf("expected Diane email to have failed, got %+v", p)
			}
			failedCount++
		} else if p.EmailSentAt == nil {
			t.Fatalf("expected %s email to have succeeded", p.Email)
		}
	}
	if failedCount != 1 {
		t.Fatalf("expected 1 failed email, got %d", failedCount)
	}

	// Resend failed emails -> succeeds now
	env.mailer.SetFailFor("diane@example.com", nil)
	rec = doJSON(t, env.handler, http.MethodPost, "/api/admin/events/"+created.AdminToken+"/resend-failed", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on resend-failed, got %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &adminEv)
	for _, p := range adminEv.Participants {
		if p.EmailSentAt == nil || p.EmailError != nil {
			t.Fatalf("expected all emails sent after resend-failed, got %+v", p)
		}
	}

	// 7. Participant Space: Read-Only GET vs Explicit POST /reveal (US-5.1, US-5.2, US-5.3, US-5.4)
	aliceToken := adminEv.OrganizerParticipantToken
	rec = doJSON(t, env.handler, http.MethodGet, "/api/p/"+aliceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET participant space, got %d", rec.Code)
	}
	var aliceSpace ParticipantSelfDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &aliceSpace)
	if aliceSpace.Revealed || aliceSpace.Target != nil || aliceSpace.LinkClickedAt != nil {
		t.Fatalf("PRIVACY/BOT LEAK: GET /api/p/:token revealed target before POST /reveal: %+v", aliceSpace)
	}

	// Now explicitly call POST /reveal
	rec = doJSON(t, env.handler, http.MethodPost, "/api/p/"+aliceToken+"/reveal", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST /reveal, got %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &aliceSpace)
	if !aliceSpace.Revealed || aliceSpace.Target == nil || aliceSpace.Target.FirstName == "" {
		t.Fatalf("expected target to be revealed after POST /reveal, got %+v", aliceSpace)
	}

	// Wishlist CRUD & URL validation (US-5.4)
	rec = doJSON(t, env.handler, http.MethodPost, "/api/p/"+aliceToken+"/wishlist", UpsertWishlistItemRequest{
		Title: "XSS attempt",
		URL:   "javascript:alert(1)",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on javascript: URL, got %d", rec.Code)
	}

	rec = doJSON(t, env.handler, http.MethodPost, "/api/p/"+aliceToken+"/wishlist", UpsertWishlistItemRequest{
		Title: "Livre Go",
		URL:   "amazon.fr/dp/12345",
		Note:  "Broché",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on valid wishlist item, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &aliceSpace)
	if len(aliceSpace.OwnWishlist) != 1 || aliceSpace.OwnWishlist[0].URL != "https://amazon.fr/dp/12345" {
		t.Fatalf("expected auto-prefixed https:// URL, got %+v", aliceSpace.OwnWishlist)
	}

	// 8. Post-Draw Participant Email Edit Rotates Token & Resets Reveal/Email Status (US-4.3)
	rec = doJSON(t, env.handler, http.MethodPut, "/api/admin/events/"+created.AdminToken+"/participants/"+pAlice, UpsertParticipantRequest{
		FirstName: "Alice",
		LastName:  "Martin-New",
		Email:     "alice.new@example.com",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on post-draw participant update, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &adminEv)
	if adminEv.OrganizerEmail != "alice.new@example.com" || adminEv.OrganizerLastName != "Martin-New" {
		t.Fatalf("expected organizer_* synced on event, got %+v", adminEv)
	}
	if adminEv.OrganizerParticipantToken == aliceToken {
		t.Fatal("expected participant_token to rotate when email changed post-draw")
	}
	// Old token must return 404
	if doJSON(t, env.handler, http.MethodGet, "/api/p/"+aliceToken, nil).Code != http.StatusNotFound {
		t.Fatal("expected old participant token to be revoked (404)")
	}
	// LinkClickedAt and EmailSentAt must be reset to nil for Alice
	if adminEv.Participants[0].LinkClickedAt != nil || adminEv.Participants[0].EmailSentAt != nil {
		t.Fatalf("expected LinkClickedAt and EmailSentAt reset to nil after email change, got %+v", adminEv.Participants[0])
	}

	// 9. Unified Recovery of Lost Links (US-1.4)
	env.mailer.Reset()
	rec = doJSON(t, env.handler, http.MethodPost, "/api/recover", RecoverLinksRequest{
		Email:  "alice.new@example.com",
		Locale: "fr",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on recover, got %d", rec.Code)
	}
	msgs := env.mailer.SentMessages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Body, created.AdminToken) || !strings.Contains(msgs[0].Body, adminEv.OrganizerParticipantToken) {
		t.Fatalf("expected recovery email with both admin and participant links, got %+v", msgs)
	}

	// 10. Super Admin Back-Office (Epic 6: US-6.1, US-6.2, US-6.3, US-6.4)
	rec = doJSON(t, env.handler, http.MethodPost, "/api/superadmin/login", SuperAdminLoginRequest{
		Email:    "super@example.com",
		Password: "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong superadmin password, got %d", rec.Code)
	}

	rec = doJSON(t, env.handler, http.MethodPost, "/api/superadmin/login", SuperAdminLoginRequest{
		Email:    "super@example.com",
		Password: "secret-password-123",
	})
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("expected 200 and session cookie on superadmin login, got %d", rec.Code)
	}
	saCookie := rec.Result().Cookies()[0]

	// Disable public event creation (US-6.2 & US-1.3)
	rec = doJSON(t, env.handler, http.MethodPut, "/api/superadmin/settings", SuperAdminSettingsRequest{
		AllowPublicEventCreation: false,
	}, saCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on update settings, got %d", rec.Code)
	}

	rec = doJSON(t, env.handler, http.MethodPost, "/api/events", CreateEventRequest{
		Title: "Blocked", FirstName: "X", LastName: "Y", Email: "x@example.com",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when public event creation is disabled, got %d", rec.Code)
	}

	// Super Admin can still invite an organizer (US-6.3)
	rec = doJSON(t, env.handler, http.MethodPost, "/api/superadmin/invite", CreateEventRequest{
		Title: "Invited Event", FirstName: "Paul", LastName: "Bernard", Email: "paul@example.com", Locale: "en",
	}, saCookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on superadmin invite, got %d: %s", rec.Code, rec.Body.String())
	}

	// Super Admin overview & SMTP test (US-6.4)
	rec = doJSON(t, env.handler, http.MethodGet, "/api/superadmin/overview", nil, saCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on superadmin overview, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "assigned_to") {
		t.Fatalf("PRIVACY LEAK in superadmin overview: %s", rec.Body.String())
	}

	rec = doJSON(t, env.handler, http.MethodPost, "/api/superadmin/test-smtp", nil, saCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on test-smtp, got %d", rec.Code)
	}

	// Delete event via organizer (US-4.4)
	rec = doJSON(t, env.handler, http.MethodDelete, "/api/admin/events/"+created.AdminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete event, got %d", rec.Code)
	}
	if doJSON(t, env.handler, http.MethodGet, "/api/admin/events/"+created.AdminToken, nil).Code != http.StatusNotFound {
		t.Fatal("expected 404 after event deletion")
	}
}
