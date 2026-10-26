package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"secret-santa/internal/config"
	"secret-santa/internal/db"
	"secret-santa/internal/mailer"
)

const superAdminCookieName = "secret_santa_superadmin"

func (s *Server) signSuperAdminToken(expUnix int64) string {
	emailNorm := strings.ToLower(strings.TrimSpace(s.cfg.SuperAdminEmail))
	payload := fmt.Sprintf("superadmin:%d:%d:%s:%s", expUnix, s.sessionEpoch.Load(), emailNorm, s.cfg.SuperAdminPasswordHash)
	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	_, _ = mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d.%s", expUnix, sig)
}

func (s *Server) verifySuperAdminToken(token string) bool {
	expStr, sigHex, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > expUnix {
		return false
	}
	expected := s.signSuperAdminToken(expUnix)
	_, expectedSig, _ := strings.Cut(expected, ".")
	sigBytes, err1 := hex.DecodeString(sigHex)
	expBytes, err2 := hex.DecodeString(expectedSig)
	if err1 != nil || err2 != nil || len(sigBytes) != len(expBytes) {
		return false
	}
	return hmac.Equal(sigBytes, expBytes)
}

func (s *Server) requireSuperAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(superAdminCookieName)
		if err != nil || !s.verifySuperAdminToken(c.Value) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func (s *Server) checkLoginRateLimit(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	cutoff := time.Now().Add(-1 * time.Minute)
	recent := s.loginAttempts[ip][:0]
	for _, t := range s.loginAttempts[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(s.loginAttempts, ip)
	} else {
		s.loginAttempts[ip] = recent
	}
	return len(recent) < 10
}

func (s *Server) recordLoginFailure(ip string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	s.loginAttempts[ip] = append(s.loginAttempts[ip], time.Now())
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) handleSuperAdminLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.checkLoginRateLimit(ip) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts, please try again later")
		return
	}

	var req SuperAdminLoginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	inputEmailHash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(req.Email))))
	expectedEmailHash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(s.cfg.SuperAdminEmail))))

	emailMatch := subtle.ConstantTimeCompare(inputEmailHash[:], expectedEmailHash[:])
	passMatch := config.VerifyPassword(req.Password, s.cfg.SuperAdminPasswordHash)

	if emailMatch != 1 || !passMatch {
		s.recordLoginFailure(ip)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	exp := time.Now().Add(24 * time.Hour).Unix()
	http.SetCookie(w, &http.Cookie{
		Name:     superAdminCookieName,
		Value:    s.signSuperAdminToken(exp),
		Path:     "/api/superadmin",
		HttpOnly: true,
		Secure:   s.cfg.IsSecureCookie(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})

	writeJSON(w, http.StatusOK, GenericMessageResponse{Message: "authenticated"})
}

func (s *Server) handleSuperAdminLogout(w http.ResponseWriter, r *http.Request) {
	s.sessionEpoch.Add(1)
	http.SetCookie(w, &http.Cookie{
		Name:     superAdminCookieName,
		Value:    "",
		Path:     "/api/superadmin",
		HttpOnly: true,
		Secure:   s.cfg.IsSecureCookie(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, GenericMessageResponse{Message: "logged out"})
}

func (s *Server) handleSuperAdminOverview(w http.ResponseWriter, r *http.Request) {
	allow, err := s.repo.GetAllowPublicEventCreation()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	summaries, err := s.repo.ListAllEventsForSuperAdmin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load events")
		return
	}

	dtos := make([]SuperAdminEventDTO, 0, len(summaries))
	for _, sum := range summaries {
		dtos = append(dtos, SuperAdminEventDTO{
			ID:                   sum.ID,
			Title:                sum.Title,
			OrganizerFirstName:   sum.OrganizerFirstName,
			OrganizerLastName:    sum.OrganizerLastName,
			OrganizerEmail:       sum.OrganizerEmail,
			Status:               sum.Status,
			Locale:               sum.Locale,
			CreatedAt:            sum.CreatedAt,
			DrawnAt:              sum.DrawnAt,
			ParticipantCount:     sum.ParticipantCount,
			RevealedTargetsCount: sum.RevealedTargetsCount,
			FilledWishlistsCount: sum.FilledWishlistsCount,
		})
	}

	writeJSON(w, http.StatusOK, SuperAdminOverviewDTO{
		AllowPublicEventCreation: allow,
		SuperAdminEmail:          s.cfg.SuperAdminEmail,
		Events:                   dtos,
	})
}

func (s *Server) handleSuperAdminUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req SuperAdminSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.repo.SetAllowPublicEventCreation(req.AllowPublicEventCreation); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}
	writeJSON(w, http.StatusOK, PublicConfigDTO{AllowPublicEventCreation: req.AllowPublicEventCreation})
}

func (s *Server) handleSuperAdminInviteOrganizer(w http.ResponseWriter, r *http.Request) {
	var req CreateEventRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	if !isValidTextLen(req.Title, 1, 200) || !isValidTextLen(req.FirstName, 1, 100) || !isValidTextLen(req.LastName, 1, 100) || !isValidEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "title, first_name, last_name, and a valid email are required")
		return
	}

	ev, _, err := s.repo.CreateEvent(req.Title, req.FirstName, req.LastName, req.Email, req.Locale)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create event")
		return
	}

	msg := mailer.BuildAdminMagicLinkEmail(s.cfg.AppBaseURL, ev.Locale, ev.OrganizerEmail, mailer.AdminMagicLinkData{
		OrganizerName:  fmt.Sprintf("%s %s", ev.OrganizerFirstName, ev.OrganizerLastName),
		EventTitle:     ev.Title,
		AdminToken:     ev.AdminToken,
		InvitedByAdmin: true,
	})
	if err := s.mailer.Send(msg); err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("event created, but failed to send email: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, CreateEventResponse{
		ID:         ev.ID,
		Title:      ev.Title,
		AdminToken: ev.AdminToken,
	})
}

func (s *Server) handleSuperAdminTestSMTP(w http.ResponseWriter, r *http.Request) {
	msg := mailer.Message{
		To:      s.cfg.SuperAdminEmail,
		Subject: "Secret Santa — Test SMTP / Configuration Email",
		Body:    fmt.Sprintf("Your Secret Santa email configuration (%s) is working properly!", s.cfg.AppBaseURL),
	}
	if err := s.mailer.Send(msg); err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("SMTP test failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, GenericMessageResponse{Message: "test email sent successfully"})
}

func (s *Server) handleSuperAdminResendAdminLink(w http.ResponseWriter, r *http.Request) {
	eventID := r.PathValue("eventID")
	ev, err := s.repo.GetEventByID(eventID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}

	msg := mailer.BuildAdminMagicLinkEmail(s.cfg.AppBaseURL, ev.Locale, ev.OrganizerEmail, mailer.AdminMagicLinkData{
		OrganizerName: fmt.Sprintf("%s %s", ev.OrganizerFirstName, ev.OrganizerLastName),
		EventTitle:    ev.Title,
		AdminToken:    ev.AdminToken,
	})
	if err := s.mailer.Send(msg); err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to send email: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, GenericMessageResponse{Message: "admin link resent"})
}

func (s *Server) handleSuperAdminDeleteEvent(w http.ResponseWriter, r *http.Request) {
	eventID := r.PathValue("eventID")
	if err := s.repo.DeleteEvent(eventID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "event not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete event")
		return
	}
	writeJSON(w, http.StatusOK, GenericMessageResponse{Message: "deleted"})
}
