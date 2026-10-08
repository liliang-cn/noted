// Package store is noted's system of record: notes, events, tasks, reminders
// and users, all in one SQLite file. It knows nothing about AI; the optional
// AI layer reads from it and never the other way round.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid argument")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and migrates it.
// ":memory:" gives a private in-memory database, handy for tests.
func Open(path string) (*Store, error) {
	dsn := path
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared&_pragma=foreign_keys(1)"
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection avoids SQLITE_BUSY races
	// and keeps ":memory:" databases coherent. Reads are fast enough for a
	// personal-scale service.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL UNIQUE,
	created_ms INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tokens (
	hash       TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	label      TEXT NOT NULL DEFAULT '',
	created_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS notes (
	id            TEXT PRIMARY KEY,
	user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title         TEXT NOT NULL,
	content       TEXT NOT NULL,
	pinned        INTEGER NOT NULL DEFAULT 0,
	archived      INTEGER NOT NULL DEFAULT 0,
	created_ms    INTEGER NOT NULL,
	updated_ms    INTEGER NOT NULL,
	ai_indexed_ms INTEGER
);
CREATE INDEX IF NOT EXISTS notes_user_updated ON notes(user_id, pinned DESC, updated_ms DESC);
CREATE TABLE IF NOT EXISTS note_tags (
	note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
	tag     TEXT NOT NULL,
	PRIMARY KEY (note_id, tag)
);
CREATE INDEX IF NOT EXISTS note_tags_tag ON note_tags(tag);
CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
	note_id UNINDEXED, user_id UNINDEXED, title, content, tags,
	tokenize = 'trigram'
);

CREATE TABLE IF NOT EXISTS events (
	id              TEXT PRIMARY KEY,
	user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title           TEXT NOT NULL,
	description     TEXT NOT NULL DEFAULT '',
	location        TEXT NOT NULL DEFAULT '',
	start_ms        INTEGER NOT NULL,
	end_ms          INTEGER NOT NULL,
	all_day         INTEGER NOT NULL DEFAULT 0,
	tz              TEXT NOT NULL DEFAULT 'UTC',
	rrule           TEXT NOT NULL DEFAULT '',
	remind_before   INTEGER,
	reminded_for_ms INTEGER,
	created_ms      INTEGER NOT NULL,
	updated_ms      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS events_user_start ON events(user_id, start_ms);

CREATE TABLE IF NOT EXISTS tasks (
	id          TEXT PRIMARY KEY,
	user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title       TEXT NOT NULL,
	notes       TEXT NOT NULL DEFAULT '',
	due_ms      INTEGER,
	priority    INTEGER NOT NULL DEFAULT 0,
	completed   INTEGER NOT NULL DEFAULT 0,
	complete_ms INTEGER,
	remind_ms   INTEGER,
	reminded    INTEGER NOT NULL DEFAULT 0,
	created_ms  INTEGER NOT NULL,
	updated_ms  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS tasks_user_due ON tasks(user_id, completed, due_ms);
CREATE TABLE IF NOT EXISTS task_tags (
	task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	tag     TEXT NOT NULL,
	PRIMARY KEY (task_id, tag)
);

CREATE TABLE IF NOT EXISTS reminders (
	id      TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind    INTEGER NOT NULL,
	ref_id  TEXT NOT NULL,
	title   TEXT NOT NULL,
	due_ms  INTEGER NOT NULL,
	fire_ms INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS reminders_user_fire ON reminders(user_id, fire_ms DESC);
`

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// tx runs fn in a transaction.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	t, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(t); err != nil {
		_ = t.Rollback()
		return err
	}
	return t.Commit()
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func fromMS(v int64) time.Time { return time.UnixMilli(v).UTC() }

func nullMS(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

func ptrTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMS(v.Int64)
	return &t
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
