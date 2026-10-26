package api

import (
	"fmt"
	"net/http"
	"strings"

	"secret-santa/internal/mailer"
)

func (s *Server) handleGetPublicConfig(w http.ResponseWriter, r *http.Request) {
	allow, err := s.repo.GetAllowPublicEventCreation()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	writeJSON(w, http.StatusOK, PublicConfigDTO{AllowPublicEventCreation: allow})
}

func (s *Server) handleCreatePublicEvent(w http.ResponseWriter, r *http.Request) {
	allow, err := s.repo.GetAllowPublicEventCreation()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	if !allow {
		writeError(w, http.StatusForbidden, "public event creation is disabled on this instance")
		return
	}

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
		OrganizerName: fmt.Sprintf("%s %s", ev.OrganizerFirstName, ev.OrganizerLastName),
		EventTitle:    ev.Title,
		AdminToken:    ev.AdminToken,
	})
	_ = s.mailer.Send(msg)

	writeJSON(w, http.StatusCreated, CreateEventResponse{
		ID:         ev.ID,
		Title:      ev.Title,
		AdminToken: ev.AdminToken,
	})
}

func (s *Server) handleRecoverLinks(w http.ResponseWriter, r *http.Request) {
	var req RecoverLinksRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !isValidEmail(email) {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}

	orgLinks, partLinks, err := s.repo.FindLinksByEmail(email)
	if err == nil && (len(orgLinks) > 0 || len(partLinks) > 0) {
		locale := req.Locale
		if locale != "en" && locale != "fr" {
			if len(orgLinks) > 0 {
				locale = orgLinks[0].Locale
			} else if len(partLinks) > 0 {
				locale = partLinks[0].Locale
			} else {
				locale = "fr"
			}
		}

		baseURL := strings.TrimRight(s.cfg.AppBaseURL, "/")
		var orgItems []mailer.RecoveryItem
		for _, o := range orgLinks {
			orgItems = append(orgItems, mailer.RecoveryItem{
				Title: o.EventTitle,
				URL:   baseURL + "/admin/events/" + o.AdminToken,
			})
		}
		var partItems []mailer.RecoveryItem
		for _, p := range partLinks {
			partItems = append(partItems, mailer.RecoveryItem{
				Title: p.EventTitle,
				URL:   baseURL + "/p/" + p.ParticipantToken,
			})
		}

		msg := mailer.BuildRecoveryEmail(s.cfg.AppBaseURL, locale, email, orgItems, partItems)
		_ = s.mailer.Send(msg)
	}

	genericMsg := "Si un événement est associé à cette adresse, un email a été envoyé."
	if req.Locale == "en" {
		genericMsg = "If an event is associated with this address, an email has been sent."
	}
	writeJSON(w, http.StatusOK, GenericMessageResponse{Message: genericMsg})
}
