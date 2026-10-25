package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"secret-santa/internal/db"
)

func (s *Server) buildParticipantSelfDTO(p *db.Participant, ev *db.Event) (*ParticipantSelfDTO, error) {
	ownItems, err := s.repo.ListWishlistItems(p.ID)
	if err != nil {
		return nil, err
	}
	ownDTOs := make([]WishlistItemDTO, 0, len(ownItems))
	for _, it := range ownItems {
		ownDTOs = append(ownDTOs, WishlistItemDTO{
			ID:        it.ID,
			Title:     it.Title,
			URL:       it.URL,
			Note:      it.Note,
			SortOrder: it.SortOrder,
			CreatedAt: it.CreatedAt,
			UpdatedAt: it.UpdatedAt,
		})
	}

	dto := &ParticipantSelfDTO{
		FirstName:     p.FirstName,
		LastName:      p.LastName,
		Email:         p.Email,
		EventTitle:    ev.Title,
		EventDate:     ev.EventDate,
		Budget:        ev.Budget,
		Description:   ev.Description,
		Locale:        ev.Locale,
		Revealed:      p.LinkClickedAt != nil,
		LinkClickedAt: p.LinkClickedAt,
		OwnWishlist:   ownDTOs,
	}

	// Strictly expose target identity & wishlist ONLY after explicit reveal (link_clicked_at != nil)
	if p.LinkClickedAt != nil && p.AssignedToID != nil {
		target, err := s.repo.GetParticipantByID(ev.ID, *p.AssignedToID)
		if err == nil {
			targetItems, err := s.repo.ListWishlistItems(target.ID)
			if err != nil {
				return nil, err
			}
			targetDTOs := make([]WishlistItemDTO, 0, len(targetItems))
			for _, it := range targetItems {
				targetDTOs = append(targetDTOs, WishlistItemDTO{
					ID:        it.ID,
					Title:     it.Title,
					URL:       it.URL,
					Note:      it.Note,
					SortOrder: it.SortOrder,
					CreatedAt: it.CreatedAt,
					UpdatedAt: it.UpdatedAt,
				})
			}
			dto.Target = &TargetDTO{
				FirstName: target.FirstName,
				LastName:  target.LastName,
				Wishlist:  targetDTOs,
			}
		}
	}

	return dto, nil
}

func (s *Server) handleGetParticipantSpace(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("participantToken")
	p, ev, err := s.repo.GetParticipantByToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load participant")
		return
	}
	if ev.Status != "drawn" {
		writeError(w, http.StatusNotFound, "draw has not taken place yet")
		return
	}

	dto, err := s.buildParticipantSelfDTO(p, ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build participant response")
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handleRevealTarget(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("participantToken")
	p, ev, err := s.repo.GetParticipantByToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load participant")
		return
	}
	if ev.Status != "drawn" {
		writeError(w, http.StatusNotFound, "draw has not taken place yet")
		return
	}

	if err := s.repo.RevealTarget(p.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reveal target")
		return
	}

	p, ev, err = s.repo.GetParticipantByToken(token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload participant")
		return
	}

	dto, err := s.buildParticipantSelfDTO(p, ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build participant response")
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func validateAndNormalizeWishlistInput(title, rawURL, note string) (string, string, string, error) {
	title = strings.TrimSpace(title)
	rawURL = strings.TrimSpace(rawURL)
	note = strings.TrimSpace(note)

	if title == "" || utf8.RuneCountInString(title) > 200 {
		return "", "", "", errors.New("title is required and must be at most 200 characters")
	}
	if utf8.RuneCountInString(note) > 1000 {
		return "", "", "", errors.New("note must be at most 1000 characters")
	}

	if rawURL != "" {
		lower := strings.ToLower(rawURL)
		if strings.Contains(lower, ":") && !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			// Check if the colon is before the first slash (i.e., a non-http scheme like javascript: or data:)
			colonIdx := strings.Index(lower, ":")
			slashIdx := strings.Index(lower, "/")
			if slashIdx == -1 || colonIdx < slashIdx {
				return "", "", "", errors.New("only http:// and https:// URLs are allowed")
			}
		}
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			rawURL = "https://" + rawURL
		}
		parsed, err := url.Parse(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return "", "", "", errors.New("invalid URL")
		}
	}

	return title, rawURL, note, nil
}

func (s *Server) handleAddWishlistItem(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("participantToken")
	p, ev, err := s.repo.GetParticipantByToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load participant")
		return
	}
	if ev.Status != "drawn" {
		writeError(w, http.StatusNotFound, "draw has not taken place yet")
		return
	}

	var req UpsertWishlistItemRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title, normURL, note, valErr := validateAndNormalizeWishlistInput(req.Title, req.URL, req.Note)
	if valErr != nil {
		writeError(w, http.StatusBadRequest, valErr.Error())
		return
	}

	if _, err := s.repo.AddWishlistItem(p.ID, title, normURL, note); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add wishlist item")
		return
	}

	dto, err := s.buildParticipantSelfDTO(p, ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build participant response")
		return
	}
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleUpdateWishlistItem(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("participantToken")
	itemID := r.PathValue("itemID")

	p, ev, err := s.repo.GetParticipantByToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load participant")
		return
	}
	if ev.Status != "drawn" {
		writeError(w, http.StatusNotFound, "draw has not taken place yet")
		return
	}

	var req UpsertWishlistItemRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title, normURL, note, valErr := validateAndNormalizeWishlistInput(req.Title, req.URL, req.Note)
	if valErr != nil {
		writeError(w, http.StatusBadRequest, valErr.Error())
		return
	}

	if _, err := s.repo.UpdateWishlistItem(p.ID, itemID, title, normURL, note); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "wishlist item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update wishlist item")
		return
	}

	dto, err := s.buildParticipantSelfDTO(p, ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build participant response")
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handleDeleteWishlistItem(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("participantToken")
	itemID := r.PathValue("itemID")

	p, ev, err := s.repo.GetParticipantByToken(token)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load participant")
		return
	}
	if ev.Status != "drawn" {
		writeError(w, http.StatusNotFound, "draw has not taken place yet")
		return
	}

	if err := s.repo.DeleteWishlistItem(p.ID, itemID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "wishlist item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete wishlist item")
		return
	}

	dto, err := s.buildParticipantSelfDTO(p, ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build participant response")
		return
	}
	writeJSON(w, http.StatusOK, dto)
}
