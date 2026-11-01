package db

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestRepo(t *testing.T) (*Repository, *sql.DB) {
	t.Helper()
	conn, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewRepository(conn), conn
}

// seedEvent creates an event with n participants in total (the organizer
// counts as the first one) and returns them in listing order.
func seedEvent(t *testing.T, repo *Repository, n int) (*Event, []Participant) {
	t.Helper()
	ev, _, err := repo.CreateEvent("Test", "Org", "Anizer", fmt.Sprintf("org-%s@example.com", GenerateUUID()[:8]), "en")
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	parts, err := repo.ListParticipants(ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	for len(parts) < n {
		i := len(parts)
		if _, err := repo.AddParticipant(ev.ID, "P", fmt.Sprint(i), fmt.Sprintf("p%d-%s@example.com", i, GenerateUUID()[:6])); err != nil {
			t.Fatalf("add participant: %v", err)
		}
		parts, err = repo.ListParticipants(ev.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	return ev, parts
}

// cycle returns a valid single-cycle assignment over parts.
func cycle(parts []Participant) map[string]string {
	m := make(map[string]string, len(parts))
	for i, p := range parts {
		m[p.ID] = parts[(i+1)%len(parts)].ID
	}
	return m
}

func TestSaveDrawHappyPath(t *testing.T) {
	repo, _ := newTestRepo(t)
	ev, parts := seedEvent(t, repo, 4)
	if err := repo.SaveDraw(ev.ID, cycle(parts)); err != nil {
		t.Fatalf("SaveDraw: %v", err)
	}
	got, _ := repo.GetEventByID(ev.ID)
	if got.Status != "drawn" || got.DrawnAt == nil {
		t.Fatalf("expected drawn event, got %+v", got)
	}
	after, _ := repo.ListParticipants(ev.ID)
	for _, p := range after {
		if p.AssignedToID == nil {
			t.Fatalf("participant %s has no target", p.ID)
		}
	}
	// A second draw is refused
	if err := repo.SaveDraw(ev.ID, cycle(parts)); !errors.Is(err, ErrAlreadyDrawn) {
		t.Fatalf("expected ErrAlreadyDrawn, got %v", err)
	}
}

func TestSaveDrawRejectsStaleOrInvalidAssignments(t *testing.T) {
	repo, _ := newTestRepo(t)
	ev, parts := seedEvent(t, repo, 4)
	full := cycle(parts)

	// A participant was added after the draw was computed
	missing := cycle(parts[:3])
	// An unknown participant
	unknown := cycle(parts)
	unknown[parts[0].ID] = "ghost"
	// Self-assignment
	self := cycle(parts)
	self[parts[0].ID] = parts[0].ID
	// Two givers share a receiver (not a permutation)
	dup := cycle(parts)
	dup[parts[0].ID] = parts[2].ID

	for name, a := range map[string]map[string]string{"missing participant": missing, "unknown": unknown, "self": self, "duplicate receiver": dup} {
		if err := repo.SaveDraw(ev.ID, a); !errors.Is(err, ErrDrawStale) {
			t.Fatalf("%s: expected ErrDrawStale, got %v", name, err)
		}
		got, _ := repo.GetEventByID(ev.ID)
		if got.Status != "draft" {
			t.Fatalf("%s: a rejected draw must leave the event in draft, got %s", name, got.Status)
		}
	}

	// An exclusion added after the draw was computed and violated by it
	if _, err := repo.AddExclusion(ev.ID, parts[0].ID, full[parts[0].ID], false); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveDraw(ev.ID, full); !errors.Is(err, ErrDrawStale) {
		t.Fatalf("violated exclusion: expected ErrDrawStale, got %v", err)
	}
}

func TestMutationsRefusedAfterDraw(t *testing.T) {
	repo, _ := newTestRepo(t)
	ev, parts := seedEvent(t, repo, 3)
	if err := repo.SaveDraw(ev.ID, cycle(parts)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddParticipant(ev.ID, "Late", "Comer", "late@example.com"); !errors.Is(err, ErrAlreadyDrawn) {
		t.Fatalf("AddParticipant after draw: expected ErrAlreadyDrawn, got %v", err)
	}
	if err := repo.DeleteParticipant(ev.ID, parts[1].ID); !errors.Is(err, ErrAlreadyDrawn) {
		t.Fatalf("DeleteParticipant after draw: expected ErrAlreadyDrawn, got %v", err)
	}
	if _, err := repo.AddExclusion(ev.ID, parts[0].ID, parts[1].ID, true); !errors.Is(err, ErrAlreadyDrawn) {
		t.Fatalf("AddExclusion after draw: expected ErrAlreadyDrawn, got %v", err)
	}
	after, _ := repo.ListParticipants(ev.ID)
	if len(after) != 3 {
		t.Fatalf("participant list must be unchanged, got %d", len(after))
	}
}

func TestConcurrentSaveDrawOnlyOneWins(t *testing.T) {
	repo, _ := newTestRepo(t)
	ev, parts := seedEvent(t, repo, 5)

	const workers = 12
	var wg sync.WaitGroup
	results := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = repo.SaveDraw(ev.ID, cycle(parts))
		}(i)
	}
	wg.Wait()

	wins := 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrAlreadyDrawn):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly one successful draw, got %d", wins)
	}
}

// A participant added while draws are being saved must never end up in a drawn
// event without a target.
func TestConcurrentAddParticipantAndDrawKeepsEventConsistent(t *testing.T) {
	for round := 0; round < 25; round++ {
		repo, _ := newTestRepo(t)
		ev, parts := seedEvent(t, repo, 4)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = repo.AddParticipant(ev.ID, "Late", "Comer", "late@example.com")
		}()
		var drawErr error
		go func() {
			defer wg.Done()
			drawErr = repo.SaveDraw(ev.ID, cycle(parts))
		}()
		wg.Wait()

		got, _ := repo.GetEventByID(ev.ID)
		after, _ := repo.ListParticipants(ev.ID)
		if got.Status == "drawn" {
			if drawErr != nil {
				t.Fatalf("event drawn but SaveDraw returned %v", drawErr)
			}
			for _, p := range after {
				if p.AssignedToID == nil {
					t.Fatalf("round %d: participant %s in a drawn event has no target", round, p.ID)
				}
			}
		} else if !errors.Is(drawErr, ErrDrawStale) {
			t.Fatalf("round %d: event still draft but SaveDraw returned %v", round, drawErr)
		}
	}
}

func TestAddExclusionDirectionalSemantics(t *testing.T) {
	repo, _ := newTestRepo(t)
	ev, parts := seedEvent(t, repo, 3)
	a, b := parts[0].ID, parts[1].ID

	created, err := repo.AddExclusion(ev.ID, a, b, false)
	if err != nil || len(created) != 1 || created[0].ParticipantAID != a || created[0].ParticipantBID != b {
		t.Fatalf("expected single a->b row, got %+v err=%v", created, err)
	}
	if _, err := repo.AddExclusion(ev.ID, a, b, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("exact duplicate: expected ErrConflict, got %v", err)
	}
	// reciprocal adds only the missing opposite direction
	created, err = repo.AddExclusion(ev.ID, a, b, true)
	if err != nil || len(created) != 1 || created[0].ParticipantAID != b || created[0].ParticipantBID != a {
		t.Fatalf("expected only b->a to be created, got %+v err=%v", created, err)
	}
	if _, err := repo.AddExclusion(ev.ID, a, b, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("everything exists: expected ErrConflict, got %v", err)
	}
	list, _ := repo.ListExclusions(ev.ID)
	if len(list) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(list))
	}

	if _, err := repo.AddExclusion(ev.ID, a, a, false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("self exclusion: expected ErrInvalidInput, got %v", err)
	}
	if _, err := repo.AddExclusion(ev.ID, "", b, false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty id: expected ErrInvalidInput, got %v", err)
	}
	// participant of another event
	_, other := seedEvent(t, repo, 3)
	if _, err := repo.AddExclusion(ev.ID, a, other[0].ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign participant: expected ErrNotFound, got %v", err)
	}
	if list, _ := repo.ListExclusions(ev.ID); len(list) != 2 {
		t.Fatalf("a rejected request must not leave rows behind, got %d", len(list))
	}
}

func TestForeignKeyCascades(t *testing.T) {
	repo, conn := newTestRepo(t)
	ev, parts := seedEvent(t, repo, 4)
	if _, err := repo.AddExclusion(ev.ID, parts[1].ID, parts[2].ID, true); err != nil {
		t.Fatal(err)
	}
	item, err := repo.AddWishlistItem(parts[2].ID, "Book", "", "")
	if err != nil || item == nil {
		t.Fatalf("add wishlist: %v", err)
	}
	count := func(q string, args ...any) int {
		var n int
		if err := conn.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// deleting a participant removes their exclusions (both directions) and wishlist
	if err := repo.DeleteParticipant(ev.ID, parts[2].ID); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT COUNT(*) FROM exclusions WHERE event_id = ?`, ev.ID); n != 0 {
		t.Fatalf("expected exclusions cascaded, got %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM wishlist_items WHERE participant_id = ?`, parts[2].ID); n != 0 {
		t.Fatalf("expected wishlist cascaded, got %d", n)
	}

	// deleting an event removes everything
	if err := repo.DeleteEvent(ev.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT COUNT(*) FROM participants WHERE event_id = ?`, ev.ID); n != 0 {
		t.Fatalf("expected participants cascaded, got %d", n)
	}
	if err := repo.DeleteEvent(ev.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: expected ErrNotFound, got %v", err)
	}
}

func TestOrganizerUniqueIndexAndDuplicateEmail(t *testing.T) {
	repo, conn := newTestRepo(t)
	ev, parts := seedEvent(t, repo, 3)

	// case-insensitive duplicate email
	if _, err := repo.AddParticipant(ev.ID, "X", "Y", strings.ToUpper(parts[1].Email)); !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("expected ErrDuplicateEmail, got %v", err)
	}
	// same email in another event is fine
	ev2, _, err := repo.CreateEvent("Other", "O", "O", "other@example.com", "fr")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddParticipant(ev2.ID, "X", "Y", parts[1].Email); err != nil {
		t.Fatalf("same email in another event must be allowed: %v", err)
	}
	// a second organizer row is refused by the partial unique index
	_, err = conn.Exec(`INSERT INTO participants (id, event_id, first_name, last_name, email, is_organizer, participant_token) VALUES ('x', ?, 'a', 'b', 'second-org@example.com', 1, 'tok-second')`, ev.ID)
	if err == nil {
		t.Fatal("expected unique violation for a second organizer")
	}
}

func TestDeletingAssignedParticipantNullsAssignment(t *testing.T) {
	_, conn := newTestRepo(t)
	// assigned_to_id is ON DELETE SET NULL: raw delete (the API forbids it after the draw)
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO events (id, title, organizer_first_name, organizer_last_name, organizer_email, admin_token) VALUES ('e', 't', 'a', 'b', 'o@example.com', 'adm')`)
	mustExec(`INSERT INTO participants (id, event_id, first_name, last_name, email, participant_token) VALUES ('p1', 'e', 'a', 'a', 'a@example.com', 't1')`)
	mustExec(`INSERT INTO participants (id, event_id, first_name, last_name, email, participant_token, assigned_to_id) VALUES ('p2', 'e', 'b', 'b', 'b@example.com', 't2', 'p1')`)
	mustExec(`DELETE FROM participants WHERE id = 'p1'`)
	var assigned sql.NullString
	if err := conn.QueryRow(`SELECT assigned_to_id FROM participants WHERE id = 'p2'`).Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	if assigned.Valid {
		t.Fatalf("expected assigned_to_id NULL, got %q", assigned.String)
	}
}

func TestOpenSetsSchemaVersionAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d", "santa.db")
	conn, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var v int
	if err := conn.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != schemaVersion {
		t.Fatalf("user_version = %d (err %v), want %d", v, err, schemaVersion)
	}
	_ = conn.Close()

	conn, err = Open(path)
	if err != nil {
		t.Fatalf("reopening must succeed: %v", err)
	}
	_ = conn.Close()
}

func TestOpenRefusesLegacyMutualExclusionsSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE exclusions (
		id TEXT PRIMARY KEY,
		event_id TEXT NOT NULL,
		participant_a_id TEXT NOT NULL,
		participant_b_id TEXT NOT NULL,
		CHECK (participant_a_id < participant_b_id),
		UNIQUE(event_id, participant_a_id, participant_b_id)
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	conn, err := Open(path)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected Open to refuse a database with the legacy exclusions schema")
	}
	if !strings.Contains(err.Error(), "outdated") {
		t.Fatalf("expected an explicit 'outdated' error, got %v", err)
	}
}

func TestFindLinksByEmailOnlyListsDrawnParticipantEvents(t *testing.T) {
	repo, _ := newTestRepo(t)
	ev, parts := seedEvent(t, repo, 3)
	email := parts[1].Email

	_, partLinks, err := repo.FindLinksByEmail(email)
	if err != nil || len(partLinks) != 0 {
		t.Fatalf("undrawn event must not leak participant links, got %+v err=%v", partLinks, err)
	}
	if err := repo.SaveDraw(ev.ID, cycle(parts)); err != nil {
		t.Fatal(err)
	}
	_, partLinks, err = repo.FindLinksByEmail(strings.ToUpper(email))
	if err != nil || len(partLinks) != 1 {
		t.Fatalf("expected 1 participant link (case-insensitive), got %+v err=%v", partLinks, err)
	}
}
