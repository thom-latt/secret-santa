package api

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"secret-santa/internal/db"
	"secret-santa/internal/mailer"
)

// Error strings the frontend maps in web/src/stores/http.ts.
const (
	errFeedbackInvalid     = "invalid feedback"
	errFeedbackUnavailable = "feedback is not available on this instance"
	errFeedbackSend        = "failed to send feedback"
	errFeedbackRate        = "too many feedback reports, please try again later"
)

const (
	feedbackMaxRunes     = 2000
	feedbackMaxUserAgent = 200
)

type feedbackSender struct {
	role  string
	name  string
	email string
}

func (s *Server) handleParticipantFeedback(w http.ResponseWriter, r *http.Request) {
	p, ev, err := s.repo.GetParticipantByToken(r.PathValue("participantToken"))
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load participant")
		return
	}
	s.sendFeedback(w, r, ev, feedbackSender{"participant", p.FirstName + " " + p.LastName, p.Email})
}

func (s *Server) handleAdminFeedback(w http.ResponseWriter, r *http.Request) {
	ev, err := s.repo.GetEventByAdminToken(r.PathValue("adminToken"))
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}
	s.sendFeedback(w, r, ev, feedbackSender{"organizer", ev.OrganizerFirstName + " " + ev.OrganizerLastName, ev.OrganizerEmail})
}

func (s *Server) sendFeedback(w http.ResponseWriter, r *http.Request, ev *db.Event, from feedbackSender) {
	if s.cfg.OperatorEmail == "" {
		writeError(w, http.StatusServiceUnavailable, errFeedbackUnavailable)
		return
	}
	var req FeedbackRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if !isValidTextLen(req.Message, 1, feedbackMaxRunes) || !validFeedbackKind(req.Kind) {
		writeError(w, http.StatusBadRequest, errFeedbackInvalid)
		return
	}
	if !s.limiter.take("feedback-ip:"+s.clientIP(r), feedbackIPLimit, time.Hour) ||
		!s.limiter.take("feedback-event:"+ev.ID, feedbackEventLimit, time.Hour) {
		writeError(w, http.StatusTooManyRequests, errFeedbackRate)
		return
	}
	ua := []rune(r.UserAgent())
	if len(ua) > feedbackMaxUserAgent {
		ua = ua[:feedbackMaxUserAgent]
	}
	msg := mailer.BuildFeedbackEmail(s.cfg.OperatorEmail, mailer.FeedbackData{
		Kind: req.Kind, Message: req.Message, Role: from.role,
		EventID: ev.ID, EventTitle: ev.Title, EventStatus: ev.Status,
		SenderName: strings.TrimSpace(from.name), SenderEmail: from.email, UserAgent: string(ua),
	})
	extendWriteDeadline(w)
	if err := s.mailer.Send(msg); err != nil {
		log.Printf("feedback email failed for event %s", ev.ID)
		writeError(w, http.StatusBadGateway, errFeedbackSend)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validFeedbackKind(k string) bool {
	return k == "bug" || k == "feedback" || k == "idea"
}
