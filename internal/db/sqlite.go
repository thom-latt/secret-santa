package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS app_settings (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	allow_public_event_creation INTEGER NOT NULL DEFAULT 1,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO app_settings (id, allow_public_event_creation) VALUES (1, 1);

CREATE TABLE IF NOT EXISTS events (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	event_date TEXT NOT NULL DEFAULT '',
	budget TEXT NOT NULL DEFAULT '',
	organizer_first_name TEXT NOT NULL,
	organizer_last_name TEXT NOT NULL,
	organizer_email TEXT NOT NULL,
	organizer_participates INTEGER NOT NULL DEFAULT 1,
	admin_token TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'drawn')),
	locale TEXT NOT NULL DEFAULT 'fr' CHECK (locale IN ('fr', 'en')),
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	drawn_at DATETIME
);

CREATE TABLE IF NOT EXISTS participants (
	id TEXT PRIMARY KEY,
	event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
	first_name TEXT NOT NULL,
	last_name TEXT NOT NULL,
	email TEXT NOT NULL,
	is_organizer INTEGER NOT NULL DEFAULT 0,
	participant_token TEXT NOT NULL UNIQUE,
	assigned_to_id TEXT REFERENCES participants(id) ON DELETE SET NULL,
	link_clicked_at DATETIME,
	email_sent_at DATETIME,
	email_error TEXT,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(event_id, email COLLATE NOCASE)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_event_organizer ON participants(event_id) WHERE is_organizer = 1;

CREATE TABLE IF NOT EXISTS exclusions (
	id TEXT PRIMARY KEY,
	event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
	participant_a_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
	participant_b_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
	CHECK (participant_a_id < participant_b_id),
	UNIQUE(event_id, participant_a_id, participant_b_id)
);

CREATE TABLE IF NOT EXISTS wishlist_items (
	id TEXT PRIMARY KEY,
	participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	url TEXT NOT NULL DEFAULT '',
	note TEXT NOT NULL DEFAULT '',
	sort_order INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func Open(dbPath string) (*sql.DB, error) {
	isFileDB := dbPath != ":memory:" && !strings.HasPrefix(dbPath, "file::memory:")
	if isFileDB {
		dir := filepath.Dir(dbPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("create db directory: %w", err)
			}
		}
	}

	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	dsn := dbPath + sep + "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if isFileDB {
		_ = os.Chmod(dbPath, 0o600)
	}

	if _, err := conn.Exec(schemaSQL); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("migrate sqlite schema: %w", err)
	}

	return conn, nil
}
