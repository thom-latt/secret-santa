package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/mail"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"secret-santa/internal/config"
	"secret-santa/internal/db"
	"secret-santa/internal/mailer"
)

type Server struct {
	cfg           *config.Config
	repo          *db.Repository
	mailer        mailer.Mailer
	staticFS      fs.FS
	sessionEpoch  atomic.Uint64
	loginMu       sync.Mutex
	loginAttempts map[string][]time.Time
}

func NewRouter(cfg *config.Config, repo *db.Repository, m mailer.Mailer, staticFS fs.FS) http.Handler {
	s := &Server{
		cfg:           cfg,
		repo:          repo,
		mailer:        m,
		staticFS:      staticFS,
		loginAttempts: make(map[string][]time.Time),
	}

	mux := http.NewServeMux()

	// Healthcheck
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Public endpoints (Epic 1)
	mux.HandleFunc("GET /api/public/config", s.handleGetPublicConfig)
	mux.HandleFunc("POST /api/events", s.handleCreatePublicEvent)
	mux.HandleFunc("POST /api/recover", s.handleRecoverLinks)

	// Event Administration endpoints (Epics 2, 3, 4)
	mux.HandleFunc("GET /api/admin/events/{adminToken}", s.handleGetAdminEvent)
	mux.HandleFunc("PUT /api/admin/events/{adminToken}", s.handleUpdateAdminEvent)
	mux.HandleFunc("DELETE /api/admin/events/{adminToken}", s.handleDeleteAdminEvent)
	mux.HandleFunc("POST /api/admin/events/{adminToken}/participants", s.handleAddParticipant)
	mux.HandleFunc("PUT /api/admin/events/{adminToken}/participants/{participantID}", s.handleUpdateParticipant)
	mux.HandleFunc("DELETE /api/admin/events/{adminToken}/participants/{participantID}", s.handleDeleteParticipant)
	mux.HandleFunc("POST /api/admin/events/{adminToken}/exclusions", s.handleAddExclusion)
	mux.HandleFunc("DELETE /api/admin/events/{adminToken}/exclusions/{exclusionID}", s.handleDeleteExclusion)
	mux.HandleFunc("POST /api/admin/events/{adminToken}/draw", s.handleDrawEvent)
	mux.HandleFunc("POST /api/admin/events/{adminToken}/participants/{participantID}/resend", s.handleResendParticipantEmail)
	mux.HandleFunc("POST /api/admin/events/{adminToken}/resend-failed", s.handleResendFailedEmails)

	// Participant space endpoints (Epic 5)
	mux.HandleFunc("GET /api/p/{participantToken}", s.handleGetParticipantSpace)
	mux.HandleFunc("POST /api/p/{participantToken}/reveal", s.handleRevealTarget)
	mux.HandleFunc("POST /api/p/{participantToken}/wishlist", s.handleAddWishlistItem)
	mux.HandleFunc("PUT /api/p/{participantToken}/wishlist/{itemID}", s.handleUpdateWishlistItem)
	mux.HandleFunc("DELETE /api/p/{participantToken}/wishlist/{itemID}", s.handleDeleteWishlistItem)

	// Super Admin endpoints (Epic 6)
	mux.HandleFunc("POST /api/superadmin/login", s.handleSuperAdminLogin)
	mux.HandleFunc("POST /api/superadmin/logout", s.handleSuperAdminLogout)
	mux.HandleFunc("GET /api/superadmin/overview", s.requireSuperAdmin(s.handleSuperAdminOverview))
	mux.HandleFunc("PUT /api/superadmin/settings", s.requireSuperAdmin(s.handleSuperAdminUpdateSettings))
	mux.HandleFunc("POST /api/superadmin/invite", s.requireSuperAdmin(s.handleSuperAdminInviteOrganizer))
	mux.HandleFunc("POST /api/superadmin/test-smtp", s.requireSuperAdmin(s.handleSuperAdminTestSMTP))
	mux.HandleFunc("POST /api/superadmin/events/{eventID}/resend-admin", s.requireSuperAdmin(s.handleSuperAdminResendAdminLink))
	mux.HandleFunc("DELETE /api/superadmin/events/{eventID}", s.requireSuperAdmin(s.handleSuperAdminDeleteEvent))

	// SPA Fallback for non-API routes
	if staticFS != nil {
		fileServer := http.FileServer(http.FS(staticFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
			if cleanPath != "" && cleanPath != "." {
				if f, err := staticFS.Open(cleanPath); err == nil {
					_ = f.Close()
					fileServer.ServeHTTP(w, r)
					return
				}
				// If path has a file extension and doesn't exist, return 404 rather than index.html
				if path.Ext(cleanPath) != "" {
					http.NotFound(w, r)
					return
				}
			}
			indexBytes, err := fs.ReadFile(staticFS, "index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(indexBytes)
		})
	}

	cop := http.NewCrossOriginProtection()
	return withSecurityHeaders(cfg, cop.Handler(mux))
}

func withSecurityHeaders(cfg *config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if cfg != nil && cfg.IsSecureCookie() {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10))
	return dec.Decode(dst)
}

func isValidTextLen(s string, minRunes, maxRunes int) bool {
	n := utf8.RuneCountInString(s)
	return n >= minRunes && n <= maxRunes
}

func isValidEmail(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 254 {
		return false
	}
	if strings.IndexFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) != -1 {
		return false
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Address != raw {
		return false
	}
	parts := strings.Split(addr.Address, "@")
	return len(parts) == 2 && strings.Contains(parts[1], ".")
}
