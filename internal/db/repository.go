package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrAlreadyDrawn   = errors.New("event already drawn")
	ErrDuplicateEmail = errors.New("duplicate email in event")
	ErrInvalidInput   = errors.New("invalid input")
)

type Event struct {
	ID                    string
	Title                 string
	Description           string
	EventDate             string
	Budget                string
	OrganizerFirstName    string
	OrganizerLastName     string
	OrganizerEmail        string
	OrganizerParticipates bool
	AdminToken            string
	Status                string
	Locale                string
	CreatedAt             time.Time
	DrawnAt               *time.Time
}

type Participant struct {
	ID               string
	EventID          string
	FirstName        string
	LastName         string
	Email            string
	IsOrganizer      bool
	ParticipantToken string
	AssignedToID     *string
	LinkClickedAt    *time.Time
	EmailSentAt      *time.Time
	EmailError       *string
	WishlistCount    int
	CreatedAt        time.Time
}

type Exclusion struct {
	ID             string
	EventID        string
	ParticipantAID string
	ParticipantBID string
}

type WishlistItem struct {
	ID            string
	ParticipantID string
	Title         string
	URL           string
	Note          string
	SortOrder     int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SuperAdminEventSummary struct {
	Event
	ParticipantCount     int
	RevealedTargetsCount int
	FilledWishlistsCount int
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func GenerateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func GenerateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (r *Repository) GetAllowPublicEventCreation() (bool, error) {
	var allow int
	err := r.db.QueryRow(`SELECT allow_public_event_creation FROM app_settings WHERE id = 1`).Scan(&allow)
	if err != nil {
		return false, err
	}
	return allow == 1, nil
}

func (r *Repository) SetAllowPublicEventCreation(allow bool) error {
	val := 0
	if allow {
		val = 1
	}
	_, err := r.db.Exec(`UPDATE app_settings SET allow_public_event_creation = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`, val)
	return err
}

func (r *Repository) CreateEvent(title, firstName, lastName, email, locale string) (*Event, *Participant, error) {
	if locale != "en" {
		locale = "fr"
	}
	email = strings.ToLower(strings.TrimSpace(email))
	title = strings.TrimSpace(title)
	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)

	tx, err := r.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	ev := &Event{
		ID:                    GenerateUUID(),
		Title:                 title,
		OrganizerFirstName:    firstName,
		OrganizerLastName:     lastName,
		OrganizerEmail:        email,
		OrganizerParticipates: true,
		AdminToken:            GenerateToken(),
		Status:                "draft",
		Locale:                locale,
		CreatedAt:             now,
	}

	_, err = tx.Exec(`
		INSERT INTO events (id, title, organizer_first_name, organizer_last_name, organizer_email, organizer_participates, admin_token, status, locale, created_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, 'draft', ?, ?)
	`, ev.ID, ev.Title, ev.OrganizerFirstName, ev.OrganizerLastName, ev.OrganizerEmail, ev.AdminToken, ev.Locale, ev.CreatedAt)
	if err != nil {
		return nil, nil, err
	}

	p := &Participant{
		ID:               GenerateUUID(),
		EventID:          ev.ID,
		FirstName:        firstName,
		LastName:         lastName,
		Email:            email,
		IsOrganizer:      true,
		ParticipantToken: GenerateToken(),
		CreatedAt:        now,
	}

	_, err = tx.Exec(`
		INSERT INTO participants (id, event_id, first_name, last_name, email, is_organizer, participant_token, created_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)
	`, p.ID, p.EventID, p.FirstName, p.LastName, p.Email, p.ParticipantToken, p.CreatedAt)
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return ev, p, nil
}

func (r *Repository) GetEventByAdminToken(token string) (*Event, error) {
	return r.scanEvent(r.db.QueryRow(`
		SELECT id, title, description, event_date, budget, organizer_first_name, organizer_last_name, organizer_email, organizer_participates, admin_token, status, locale, created_at, drawn_at
		FROM events WHERE admin_token = ?
	`, token))
}

func (r *Repository) GetEventByID(id string) (*Event, error) {
	return r.scanEvent(r.db.QueryRow(`
		SELECT id, title, description, event_date, budget, organizer_first_name, organizer_last_name, organizer_email, organizer_participates, admin_token, status, locale, created_at, drawn_at
		FROM events WHERE id = ?
	`, id))
}

func (r *Repository) scanEvent(row *sql.Row) (*Event, error) {
	var ev Event
	var orgPart int
	var drawnAt sql.NullTime
	err := row.Scan(
		&ev.ID, &ev.Title, &ev.Description, &ev.EventDate, &ev.Budget,
		&ev.OrganizerFirstName, &ev.OrganizerLastName, &ev.OrganizerEmail,
		&orgPart, &ev.AdminToken, &ev.Status, &ev.Locale, &ev.CreatedAt, &drawnAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ev.OrganizerParticipates = orgPart == 1
	if drawnAt.Valid {
		t := drawnAt.Time
		ev.DrawnAt = &t
	}
	return &ev, nil
}

func (r *Repository) UpdateEventDetails(eventID, title, description, eventDate, budget string, organizerParticipates *bool) (*Event, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	ev, err := r.scanEvent(tx.QueryRow(`
		SELECT id, title, description, event_date, budget, organizer_first_name, organizer_last_name, organizer_email, organizer_participates, admin_token, status, locale, created_at, drawn_at
		FROM events WHERE id = ?
	`, eventID))
	if err != nil {
		return nil, err
	}

	newParticipates := ev.OrganizerParticipates
	if organizerParticipates != nil && ev.Status == "draft" {
		newParticipates = *organizerParticipates
	}

	orgInt := 0
	if newParticipates {
		orgInt = 1
	}

	_, err = tx.Exec(`
		UPDATE events SET title = ?, description = ?, event_date = ?, budget = ?, organizer_participates = ?
		WHERE id = ?
	`, strings.TrimSpace(title), strings.TrimSpace(description), strings.TrimSpace(eventDate), strings.TrimSpace(budget), orgInt, eventID)
	if err != nil {
		return nil, err
	}

	if ev.Status == "draft" && organizerParticipates != nil && *organizerParticipates != ev.OrganizerParticipates {
		if *organizerParticipates {
			// Check if a participant with organizer_email already exists
			var existingID string
			err := tx.QueryRow(`SELECT id FROM participants WHERE event_id = ? AND email = ? COLLATE NOCASE`, eventID, ev.OrganizerEmail).Scan(&existingID)
			if errors.Is(err, sql.ErrNoRows) {
				_, err = tx.Exec(`
					INSERT INTO participants (id, event_id, first_name, last_name, email, is_organizer, participant_token, created_at)
					VALUES (?, ?, ?, ?, ?, 1, ?, ?)
				`, GenerateUUID(), eventID, ev.OrganizerFirstName, ev.OrganizerLastName, ev.OrganizerEmail, GenerateToken(), time.Now().UTC())
				if err != nil {
					return nil, err
				}
			} else if err == nil {
				_, err = tx.Exec(`
					UPDATE participants SET is_organizer = 1, first_name = ?, last_name = ?, email = ?
					WHERE id = ?
				`, ev.OrganizerFirstName, ev.OrganizerLastName, ev.OrganizerEmail, existingID)
				if err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else {
			_, err = tx.Exec(`DELETE FROM participants WHERE event_id = ? AND is_organizer = 1`, eventID)
			if err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetEventByID(eventID)
}

func (r *Repository) ListParticipants(eventID string) ([]Participant, error) {
	rows, err := r.db.Query(`
		SELECT p.id, p.event_id, p.first_name, p.last_name, p.email, p.is_organizer, p.participant_token,
		       p.assigned_to_id, p.link_clicked_at, p.email_sent_at, p.email_error, p.created_at,
		       (SELECT COUNT(*) FROM wishlist_items w WHERE w.participant_id = p.id) AS wishlist_count
		FROM participants p
		WHERE p.event_id = ?
		ORDER BY p.created_at ASC, p.id ASC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Participant
	for rows.Next() {
		p, err := scanParticipantRow(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *p)
	}
	return list, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanParticipantRow(s rowScanner) (*Participant, error) {
	var p Participant
	var isOrg int
	var assignedTo sql.NullString
	var clickedAt, sentAt sql.NullTime
	var emailErr sql.NullString
	err := s.Scan(
		&p.ID, &p.EventID, &p.FirstName, &p.LastName, &p.Email, &isOrg, &p.ParticipantToken,
		&assignedTo, &clickedAt, &sentAt, &emailErr, &p.CreatedAt, &p.WishlistCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.IsOrganizer = isOrg == 1
	if assignedTo.Valid {
		v := assignedTo.String
		p.AssignedToID = &v
	}
	if clickedAt.Valid {
		t := clickedAt.Time
		p.LinkClickedAt = &t
	}
	if sentAt.Valid {
		t := sentAt.Time
		p.EmailSentAt = &t
	}
	if emailErr.Valid {
		v := emailErr.String
		p.EmailError = &v
	}
	return &p, nil
}

func (r *Repository) GetParticipantByID(eventID, participantID string) (*Participant, error) {
	return scanParticipantRow(r.db.QueryRow(`
		SELECT p.id, p.event_id, p.first_name, p.last_name, p.email, p.is_organizer, p.participant_token,
		       p.assigned_to_id, p.link_clicked_at, p.email_sent_at, p.email_error, p.created_at,
		       (SELECT COUNT(*) FROM wishlist_items w WHERE w.participant_id = p.id) AS wishlist_count
		FROM participants p
		WHERE p.event_id = ? AND p.id = ?
	`, eventID, participantID))
}

func (r *Repository) GetParticipantByToken(token string) (*Participant, *Event, error) {
	p, err := scanParticipantRow(r.db.QueryRow(`
		SELECT p.id, p.event_id, p.first_name, p.last_name, p.email, p.is_organizer, p.participant_token,
		       p.assigned_to_id, p.link_clicked_at, p.email_sent_at, p.email_error, p.created_at,
		       (SELECT COUNT(*) FROM wishlist_items w WHERE w.participant_id = p.id) AS wishlist_count
		FROM participants p
		WHERE p.participant_token = ?
	`, token))
	if err != nil {
		return nil, nil, err
	}
	ev, err := r.GetEventByID(p.EventID)
	if err != nil {
		return nil, nil, err
	}
	return p, ev, nil
}

func (r *Repository) AddParticipant(eventID, firstName, lastName, email string) (*Participant, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)

	p := &Participant{
		ID:               GenerateUUID(),
		EventID:          eventID,
		FirstName:        firstName,
		LastName:         lastName,
		Email:            email,
		IsOrganizer:      false,
		ParticipantToken: GenerateToken(),
		CreatedAt:        time.Now().UTC(),
	}

	_, err := r.db.Exec(`
		INSERT INTO participants (id, event_id, first_name, last_name, email, is_organizer, participant_token, created_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
	`, p.ID, p.EventID, p.FirstName, p.LastName, p.Email, p.ParticipantToken, p.CreatedAt)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrDuplicateEmail
		}
		return nil, err
	}
	return p, nil
}

func (r *Repository) UpdateParticipant(eventID, participantID, firstName, lastName, email string) (*Participant, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)

	tx, err := r.db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	ev, err := r.scanEvent(tx.QueryRow(`
		SELECT id, title, description, event_date, budget, organizer_first_name, organizer_last_name, organizer_email, organizer_participates, admin_token, status, locale, created_at, drawn_at
		FROM events WHERE id = ?
	`, eventID))
	if err != nil {
		return nil, false, err
	}

	p, err := scanParticipantRow(tx.QueryRow(`
		SELECT p.id, p.event_id, p.first_name, p.last_name, p.email, p.is_organizer, p.participant_token,
		       p.assigned_to_id, p.link_clicked_at, p.email_sent_at, p.email_error, p.created_at,
		       (SELECT COUNT(*) FROM wishlist_items w WHERE w.participant_id = p.id) AS wishlist_count
		FROM participants p
		WHERE p.event_id = ? AND p.id = ?
	`, eventID, participantID))
	if err != nil {
		return nil, false, err
	}

	emailChanged := !strings.EqualFold(p.Email, email)
	if ev.Status == "drawn" && emailChanged {
		newToken := GenerateToken()
		_, err = tx.Exec(`
			UPDATE participants
			SET first_name = ?, last_name = ?, email = ?, participant_token = ?, link_clicked_at = NULL, email_sent_at = NULL, email_error = NULL
			WHERE id = ? AND event_id = ?
		`, firstName, lastName, email, newToken, participantID, eventID)
	} else {
		_, err = tx.Exec(`
			UPDATE participants
			SET first_name = ?, last_name = ?, email = ?
			WHERE id = ? AND event_id = ?
		`, firstName, lastName, email, participantID, eventID)
	}
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, false, ErrDuplicateEmail
		}
		return nil, false, err
	}

	if p.IsOrganizer {
		_, err = tx.Exec(`
			UPDATE events SET organizer_first_name = ?, organizer_last_name = ?, organizer_email = ?
			WHERE id = ?
		`, firstName, lastName, email, eventID)
		if err != nil {
			return nil, false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, false, err
	}

	updated, err := r.GetParticipantByID(eventID, participantID)
	return updated, emailChanged, err
}

func (r *Repository) DeleteParticipant(eventID, participantID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var isOrg int
	err = tx.QueryRow(`SELECT is_organizer FROM participants WHERE id = ? AND event_id = ?`, participantID, eventID).Scan(&isOrg)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	_, err = tx.Exec(`DELETE FROM participants WHERE id = ? AND event_id = ?`, participantID, eventID)
	if err != nil {
		return err
	}

	if isOrg == 1 {
		_, err = tx.Exec(`UPDATE events SET organizer_participates = 0 WHERE id = ?`, eventID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *Repository) ListExclusions(eventID string) ([]Exclusion, error) {
	rows, err := r.db.Query(`
		SELECT id, event_id, participant_a_id, participant_b_id
		FROM exclusions
		WHERE event_id = ?
		ORDER BY id ASC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Exclusion
	for rows.Next() {
		var ex Exclusion
		if err := rows.Scan(&ex.ID, &ex.EventID, &ex.ParticipantAID, &ex.ParticipantBID); err != nil {
			return nil, err
		}
		list = append(list, ex)
	}
	return list, rows.Err()
}

func (r *Repository) AddExclusion(eventID, partA, partB string) (*Exclusion, error) {
	if partA == partB || partA == "" || partB == "" {
		return nil, ErrInvalidInput
	}
	if partA > partB {
		partA, partB = partB, partA
	}

	// Verify both participants belong to eventID
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM participants WHERE event_id = ? AND id IN (?, ?)`, eventID, partA, partB).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count != 2 {
		return nil, ErrNotFound
	}

	ex := &Exclusion{
		ID:             GenerateUUID(),
		EventID:        eventID,
		ParticipantAID: partA,
		ParticipantBID: partB,
	}
	_, err = r.db.Exec(`
		INSERT INTO exclusions (id, event_id, participant_a_id, participant_b_id)
		VALUES (?, ?, ?, ?)
	`, ex.ID, ex.EventID, ex.ParticipantAID, ex.ParticipantBID)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	return ex, nil
}

func (r *Repository) DeleteExclusion(eventID, exclusionID string) error {
	res, err := r.db.Exec(`DELETE FROM exclusions WHERE id = ? AND event_id = ?`, exclusionID, eventID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SaveDraw(eventID string, assignments map[string]string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	res, err := tx.Exec(`
		UPDATE events SET status = 'drawn', drawn_at = ?
		WHERE id = ? AND status = 'draft'
	`, now, eventID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAlreadyDrawn
	}

	for giverID, receiverID := range assignments {
		_, err := tx.Exec(`
			UPDATE participants SET assigned_to_id = ?
			WHERE id = ? AND event_id = ?
		`, receiverID, giverID, eventID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *Repository) RecordEmailStatus(participantID string, sendErr error) error {
	if sendErr == nil {
		_, err := r.db.Exec(`
			UPDATE participants SET email_sent_at = ?, email_error = NULL
			WHERE id = ?
		`, time.Now().UTC(), participantID)
		return err
	}
	errMsg := sendErr.Error()
	_, err := r.db.Exec(`
		UPDATE participants SET email_error = ?
		WHERE id = ?
	`, errMsg, participantID)
	return err
}

func (r *Repository) RevealTarget(participantID string) error {
	_, err := r.db.Exec(`
		UPDATE participants SET link_clicked_at = COALESCE(link_clicked_at, ?)
		WHERE id = ?
	`, time.Now().UTC(), participantID)
	return err
}

func (r *Repository) ListWishlistItems(participantID string) ([]WishlistItem, error) {
	rows, err := r.db.Query(`
		SELECT id, participant_id, title, url, note, sort_order, created_at, updated_at
		FROM wishlist_items
		WHERE participant_id = ?
		ORDER BY sort_order ASC, created_at ASC, id ASC
	`, participantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []WishlistItem
	for rows.Next() {
		var it WishlistItem
		if err := rows.Scan(&it.ID, &it.ParticipantID, &it.Title, &it.URL, &it.Note, &it.SortOrder, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (r *Repository) AddWishlistItem(participantID, title, urlStr, note string) (*WishlistItem, error) {
	now := time.Now().UTC()
	var maxOrder sql.NullInt64
	_ = r.db.QueryRow(`SELECT MAX(sort_order) FROM wishlist_items WHERE participant_id = ?`, participantID).Scan(&maxOrder)
	order := 1
	if maxOrder.Valid {
		order = int(maxOrder.Int64) + 1
	}

	it := &WishlistItem{
		ID:            GenerateUUID(),
		ParticipantID: participantID,
		Title:         title,
		URL:           urlStr,
		Note:          note,
		SortOrder:     order,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	_, err := r.db.Exec(`
		INSERT INTO wishlist_items (id, participant_id, title, url, note, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, it.ID, it.ParticipantID, it.Title, it.URL, it.Note, it.SortOrder, it.CreatedAt, it.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return it, nil
}

func (r *Repository) UpdateWishlistItem(participantID, itemID, title, urlStr, note string) (*WishlistItem, error) {
	now := time.Now().UTC()
	res, err := r.db.Exec(`
		UPDATE wishlist_items SET title = ?, url = ?, note = ?, updated_at = ?
		WHERE id = ? AND participant_id = ?
	`, title, urlStr, note, now, itemID, participantID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}

	var it WishlistItem
	err = r.db.QueryRow(`
		SELECT id, participant_id, title, url, note, sort_order, created_at, updated_at
		FROM wishlist_items WHERE id = ? AND participant_id = ?
	`, itemID, participantID).Scan(&it.ID, &it.ParticipantID, &it.Title, &it.URL, &it.Note, &it.SortOrder, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func (r *Repository) DeleteWishlistItem(participantID, itemID string) error {
	res, err := r.db.Exec(`DELETE FROM wishlist_items WHERE id = ? AND participant_id = ?`, itemID, participantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) DeleteEvent(eventID string) error {
	res, err := r.db.Exec(`DELETE FROM events WHERE id = ?`, eventID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type RecoveredOrganizerLink struct {
	EventTitle string
	AdminToken string
	Status     string
	Locale     string
}

type RecoveredParticipantLink struct {
	EventTitle       string
	ParticipantToken string
	Locale           string
}

func (r *Repository) FindLinksByEmail(email string) ([]RecoveredOrganizerLink, []RecoveredParticipantLink, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	orgRows, err := r.db.Query(`
		SELECT title, admin_token, status, locale
		FROM events
		WHERE organizer_email = ? COLLATE NOCASE
		ORDER BY created_at DESC
	`, email)
	if err != nil {
		return nil, nil, err
	}
	defer orgRows.Close()

	var orgLinks []RecoveredOrganizerLink
	for orgRows.Next() {
		var l RecoveredOrganizerLink
		if err := orgRows.Scan(&l.EventTitle, &l.AdminToken, &l.Status, &l.Locale); err != nil {
			return nil, nil, err
		}
		orgLinks = append(orgLinks, l)
	}
	if err := orgRows.Err(); err != nil {
		return nil, nil, err
	}

	partRows, err := r.db.Query(`
		SELECT e.title, p.participant_token, e.locale
		FROM participants p
		JOIN events e ON e.id = p.event_id
		WHERE p.email = ? COLLATE NOCASE AND e.status = 'drawn'
		ORDER BY e.created_at DESC
	`, email)
	if err != nil {
		return nil, nil, err
	}
	defer partRows.Close()

	var partLinks []RecoveredParticipantLink
	for partRows.Next() {
		var l RecoveredParticipantLink
		if err := partRows.Scan(&l.EventTitle, &l.ParticipantToken, &l.Locale); err != nil {
			return nil, nil, err
		}
		partLinks = append(partLinks, l)
	}
	return orgLinks, partLinks, partRows.Err()
}

func (r *Repository) ListAllEventsForSuperAdmin() ([]SuperAdminEventSummary, error) {
	rows, err := r.db.Query(`
		SELECT e.id, e.title, e.description, e.event_date, e.budget,
		       e.organizer_first_name, e.organizer_last_name, e.organizer_email, e.organizer_participates,
		       e.admin_token, e.status, e.locale, e.created_at, e.drawn_at,
		       (SELECT COUNT(*) FROM participants p WHERE p.event_id = e.id) AS participant_count,
		       (SELECT COUNT(*) FROM participants p WHERE p.event_id = e.id AND p.link_clicked_at IS NOT NULL) AS revealed_count,
		       (SELECT COUNT(DISTINCT p.id) FROM participants p JOIN wishlist_items w ON w.participant_id = p.id WHERE p.event_id = e.id) AS wishlist_filled_count
		FROM events e
		ORDER BY e.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []SuperAdminEventSummary
	for rows.Next() {
		var s SuperAdminEventSummary
		var orgPart int
		var drawnAt sql.NullTime
		err := rows.Scan(
			&s.ID, &s.Title, &s.Description, &s.EventDate, &s.Budget,
			&s.OrganizerFirstName, &s.OrganizerLastName, &s.OrganizerEmail, &orgPart,
			&s.AdminToken, &s.Status, &s.Locale, &s.CreatedAt, &drawnAt,
			&s.ParticipantCount, &s.RevealedTargetsCount, &s.FilledWishlistsCount,
		)
		if err != nil {
			return nil, err
		}
		s.OrganizerParticipates = orgPart == 1
		if drawnAt.Valid {
			t := drawnAt.Time
			s.DrawnAt = &t
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") || strings.Contains(msg, "constraint failed")
}
