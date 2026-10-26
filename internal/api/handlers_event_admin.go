package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"secret-santa/internal/db"
	"secret-santa/internal/draw"
	"secret-santa/internal/mailer"
)

func (s *Server) buildAdminEventResponse(ev *db.Event) (*AdminEventResponseDTO, error) {
	participants, err := s.repo.ListParticipants(ev.ID)
	if err != nil {
		return nil, err
	}
	exclusions, err := s.repo.ListExclusions(ev.ID)
	if err != nil {
		return nil, err
	}

	partDTOs := make([]AdminParticipantDTO, 0, len(participants))
	partIDs := make([]string, 0, len(participants))
	nameByID := make(map[string]string, len(participants))
	var orgToken string
	for _, p := range participants {
		partIDs = append(partIDs, p.ID)
		nameByID[p.ID] = strings.TrimSpace(p.FirstName + " " + p.LastName)
		if p.IsOrganizer && ev.OrganizerParticipates && ev.Status == "drawn" {
			orgToken = p.ParticipantToken
		}
		partDTOs = append(partDTOs, AdminParticipantDTO{
			ID:            p.ID,
			FirstName:     p.FirstName,
			LastName:      p.LastName,
			Email:         p.Email,
			IsOrganizer:   p.IsOrganizer,
			LinkClickedAt: p.LinkClickedAt,
			EmailSentAt:   p.EmailSentAt,
			EmailError:    p.EmailError,
			WishlistCount: p.WishlistCount,
		})
	}

	exDTOs := make([]ExclusionDTO, 0, len(exclusions))
	exPairs := make([]draw.ExclusionPair, 0, len(exclusions))
	for _, ex := range exclusions {
		exDTOs = append(exDTOs, ExclusionDTO{
			ID:             ex.ID,
			ParticipantAID: ex.ParticipantAID,
			ParticipantBID: ex.ParticipantBID,
		})
		exPairs = append(exPairs, draw.ExclusionPair{A: ex.ParticipantAID, B: ex.ParticipantBID})
	}

	var feas FeasibilityDTO
	if ev.Status == "drawn" {
		feas = FeasibilityDTO{
			Mode: string(draw.ModeSingleCycle),
		}
	} else if len(partIDs) < 3 {
		feas = FeasibilityDTO{
			Mode:       "too_few",
			Diagnostic: fmt.Sprintf("Need %d more participant(s) (minimum 3)", 3-len(partIDs)),
		}
	} else {
		// nosemgrep: trailofbits.go.invalid-usage-of-modified-variable.invalid-usage-of-modified-variable
	res, solveErr := draw.Solve(partIDs, exPairs)
		diag := res.Diagnostic
		for id, name := range nameByID {
			diag = strings.ReplaceAll(diag, id, name)
		}
		if solveErr != nil {
			feas = FeasibilityDTO{
				Mode:       string(draw.ModeImpossible),
				Diagnostic: diag,
			}
		} else {
			feas = FeasibilityDTO{
				Mode:       string(res.Mode),
				Diagnostic: diag,
			}
		}
	}

	return &AdminEventResponseDTO{
		ID:                        ev.ID,
		Title:                     ev.Title,
		Description:               ev.Description,
		EventDate:                 ev.EventDate,
		Budget:                    ev.Budget,
		OrganizerFirstName:        ev.OrganizerFirstName,
		OrganizerLastName:         ev.OrganizerLastName,
		OrganizerEmail:            ev.OrganizerEmail,
		OrganizerParticipates:     ev.OrganizerParticipates,
		OrganizerParticipantToken: orgToken,
		Status:                    ev.Status,
		Locale:                    ev.Locale,
		CreatedAt:                 ev.CreatedAt,
		DrawnAt:                   ev.DrawnAt,
		Participants:              partDTOs,
		Exclusions:                exDTOs,
		Feasibility:               feas,
	}, nil
}

func (s *Server) handleGetAdminEvent(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}

	resp, err := s.buildAdminEventResponse(ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build event response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUpdateAdminEvent(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}

	var req UpdateEventRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if !isValidTextLen(req.Title, 1, 200) || !isValidTextLen(req.Description, 0, 2000) || !isValidTextLen(req.EventDate, 0, 100) || !isValidTextLen(req.Budget, 0, 100) {
		writeError(w, http.StatusBadRequest, "invalid event details (title is required, max 200 chars; description max 2000 chars)")
		return
	}

	updated, err := s.repo.UpdateEventDetails(ev.ID, req.Title, req.Description, req.EventDate, req.Budget, req.OrganizerParticipates)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update event")
		return
	}

	resp, err := s.buildAdminEventResponse(updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build event response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteAdminEvent(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}

	if err := s.repo.DeleteEvent(ev.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete event")
		return
	}
	writeJSON(w, http.StatusOK, GenericMessageResponse{Message: "deleted"})
}

func (s *Server) handleAddParticipant(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}
	if ev.Status != "draft" {
		writeError(w, http.StatusConflict, "cannot add participants after the draw")
		return
	}

	existing, err := s.repo.ListParticipants(ev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list participants")
		return
	}
	if len(existing) >= 100 {
		writeError(w, http.StatusBadRequest, "maximum of 100 participants reached")
		return
	}

	var req UpsertParticipantRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	if !isValidTextLen(req.FirstName, 1, 100) || !isValidTextLen(req.LastName, 1, 100) || !isValidEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "first_name, last_name, and a valid email are required")
		return
	}

	_, err = s.repo.AddParticipant(ev.ID, req.FirstName, req.LastName, req.Email)
	if errors.Is(err, db.ErrDuplicateEmail) {
		writeError(w, http.StatusConflict, "this email is already in the participant list")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add participant")
		return
	}

	resp, err := s.buildAdminEventResponse(ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build event response")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleUpdateParticipant(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	participantID := r.PathValue("participantID")

	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}

	var req UpsertParticipantRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	if !isValidTextLen(req.FirstName, 1, 100) || !isValidTextLen(req.LastName, 1, 100) || !isValidEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "first_name, last_name, and a valid email are required")
		return
	}

	_, _, err = s.repo.UpdateParticipant(ev.ID, participantID, req.FirstName, req.LastName, req.Email)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if errors.Is(err, db.ErrDuplicateEmail) {
		writeError(w, http.StatusConflict, "this email is already in the participant list")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update participant")
		return
	}

	// Reload event in case organizer_* was updated
	ev, err = s.repo.GetEventByID(ev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload event")
		return
	}
	resp, err := s.buildAdminEventResponse(ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build event response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteParticipant(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	participantID := r.PathValue("participantID")

	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}
	if ev.Status != "draft" {
		writeError(w, http.StatusConflict, "cannot delete participants after the draw")
		return
	}

	if err := s.repo.DeleteParticipant(ev.ID, participantID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "participant not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete participant")
		return
	}

	ev, err = s.repo.GetEventByID(ev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload event")
		return
	}
	resp, err := s.buildAdminEventResponse(ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build event response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAddExclusion(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}
	if ev.Status != "draft" {
		writeError(w, http.StatusConflict, "cannot add exclusions after the draw")
		return
	}

	var req CreateExclusionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	_, err = s.repo.AddExclusion(ev.ID, req.ParticipantAID, req.ParticipantBID)
	if errors.Is(err, db.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, "two distinct participants must be selected")
		return
	}
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "participants not found")
		return
	}
	if errors.Is(err, db.ErrConflict) {
		writeError(w, http.StatusConflict, "this exclusion pair already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add exclusion")
		return
	}

	resp, err := s.buildAdminEventResponse(ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build event response")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleDeleteExclusion(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	exclusionID := r.PathValue("exclusionID")

	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}
	if ev.Status != "draft" {
		writeError(w, http.StatusConflict, "cannot delete exclusions after the draw")
		return
	}

	if err := s.repo.DeleteExclusion(ev.ID, exclusionID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "exclusion not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete exclusion")
		return
	}

	resp, err := s.buildAdminEventResponse(ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build event response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDrawEvent(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}
	if ev.Status != "draft" {
		writeError(w, http.StatusConflict, "the draw has already been executed")
		return
	}

	participants, err := s.repo.ListParticipants(ev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load participants")
		return
	}
	if len(participants) < 3 {
		writeError(w, http.StatusBadRequest, "at least 3 participants are required to launch the draw")
		return
	}

	exclusions, err := s.repo.ListExclusions(ev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load exclusions")
		return
	}

	partIDs := make([]string, len(participants))
	nameByID := make(map[string]string, len(participants))
	for i, p := range participants {
		partIDs[i] = p.ID
		nameByID[p.ID] = strings.TrimSpace(p.FirstName + " " + p.LastName)
	}
	exPairs := make([]draw.ExclusionPair, len(exclusions))
	for i, ex := range exclusions {
		exPairs[i] = draw.ExclusionPair{A: ex.ParticipantAID, B: ex.ParticipantBID}
	}

	// nosemgrep: trailofbits.go.invalid-usage-of-modified-variable.invalid-usage-of-modified-variable
	res, solveErr := draw.Solve(partIDs, exPairs)
	if solveErr != nil {
		diag := res.Diagnostic
		for id, name := range nameByID {
			diag = strings.ReplaceAll(diag, id, name)
		}
		writeError(w, http.StatusUnprocessableEntity, diag)
		return
	}

	// Step 1: Atomic SQLite transaction saving status='drawn' and assigned_to_id
	if err := s.repo.SaveDraw(ev.ID, res.Assignments); err != nil {
		if errors.Is(err, db.ErrAlreadyDrawn) {
			writeError(w, http.StatusConflict, "the draw has already been executed")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save draw")
		return
	}

	// Step 2: Outside SQLite transaction, send batch emails on pooled SMTP connection and record status per participant
	msgs := make([]mailer.Message, len(participants))
	for i, p := range participants {
		msgs[i] = mailer.BuildParticipantDrawEmail(s.cfg.AppBaseURL, ev.Locale, p.Email, mailer.ParticipantDrawData{
			ParticipantName: fmt.Sprintf("%s %s", p.FirstName, p.LastName),
			EventTitle:      ev.Title,
			EventDate:       ev.EventDate,
			Budget:          ev.Budget,
			Description:     ev.Description,
			Token:           p.ParticipantToken,
		})
	}
	sendErrs := s.mailer.SendBatch(msgs)
	for i, p := range participants {
		_ = s.repo.RecordEmailStatus(p.ID, sendErrs[i])
	}

	ev, err = s.repo.GetEventByID(ev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload event")
		return
	}
	resp, err := s.buildAdminEventResponse(ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build event response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleResendParticipantEmail(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	participantID := r.PathValue("participantID")

	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}
	if ev.Status != "drawn" {
		writeError(w, http.StatusBadRequest, "event is not drawn yet")
		return
	}

	p, err := s.repo.GetParticipantByID(ev.ID, participantID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load participant")
		return
	}

	msg := mailer.BuildParticipantDrawEmail(s.cfg.AppBaseURL, ev.Locale, p.Email, mailer.ParticipantDrawData{
		ParticipantName: fmt.Sprintf("%s %s", p.FirstName, p.LastName),
		EventTitle:      ev.Title,
		EventDate:       ev.EventDate,
		Budget:          ev.Budget,
		Description:     ev.Description,
		Token:           p.ParticipantToken,
	})
	sendErr := s.mailer.Send(msg)
	_ = s.repo.RecordEmailStatus(p.ID, sendErr)

	resp, err := s.buildAdminEventResponse(ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build event response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleResendFailedEmails(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("adminToken")
	ev, err := s.repo.GetEventByAdminToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load event")
		return
	}
	if ev.Status != "drawn" {
		writeError(w, http.StatusBadRequest, "event is not drawn yet")
		return
	}

	participants, err := s.repo.ListParticipants(ev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load participants")
		return
	}

	var failedParts []db.Participant
	var msgs []mailer.Message
	for _, p := range participants {
		if p.EmailSentAt == nil {
			failedParts = append(failedParts, p)
			msgs = append(msgs, mailer.BuildParticipantDrawEmail(s.cfg.AppBaseURL, ev.Locale, p.Email, mailer.ParticipantDrawData{
				ParticipantName: fmt.Sprintf("%s %s", p.FirstName, p.LastName),
				EventTitle:      ev.Title,
				EventDate:       ev.EventDate,
				Budget:          ev.Budget,
				Description:     ev.Description,
				Token:           p.ParticipantToken,
			}))
		}
	}

	if len(msgs) > 0 {
		sendErrs := s.mailer.SendBatch(msgs)
		for i, p := range failedParts {
			_ = s.repo.RecordEmailStatus(p.ID, sendErrs[i])
		}
	}

	resp, err := s.buildAdminEventResponse(ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build event response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
