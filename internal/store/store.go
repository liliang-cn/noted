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
	ErrConflict = errors.New("version conflict")
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
	var dsn string
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
		_ = db.Close()
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

CREATE TABLE IF NOT EXISTS projects (
	id         TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title      TEXT NOT NULL,
	notes      TEXT NOT NULL DEFAULT '',
	pinned     INTEGER NOT NULL DEFAULT 0,
	archived   INTEGER NOT NULL DEFAULT 0,
	start_ms   INTEGER,
	due_ms     INTEGER,
	created_ms INTEGER NOT NULL,
	updated_ms INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS projects_user ON projects(user_id, pinned DESC, updated_ms DESC);

CREATE TABLE IF NOT EXISTS goals (
	id         TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title      TEXT NOT NULL,
	notes      TEXT NOT NULL DEFAULT '',
	period     TEXT NOT NULL,
	target     REAL NOT NULL,
	unit       TEXT NOT NULL DEFAULT '',
	tz         TEXT NOT NULL DEFAULT 'UTC',
	event_id   TEXT NOT NULL DEFAULT '',
	archived   INTEGER NOT NULL DEFAULT 0,
	created_ms INTEGER NOT NULL,
	updated_ms INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS goals_user ON goals(user_id, archived);
CREATE TABLE IF NOT EXISTS goal_milestones (
	id       TEXT PRIMARY KEY,
	goal_id  TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
	position INTEGER NOT NULL,
	title    TEXT NOT NULL,
	done_ms  INTEGER
);
CREATE INDEX IF NOT EXISTS goal_milestones_goal ON goal_milestones(goal_id, position);
CREATE TABLE IF NOT EXISTS goal_checkins (
	id         TEXT PRIMARY KEY,
	goal_id    TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	time_ms    INTEGER NOT NULL,
	local_date TEXT NOT NULL,
	amount     REAL NOT NULL,
	note       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS goal_checkins_goal ON goal_checkins(goal_id, local_date);

CREATE TABLE IF NOT EXISTS objectives (
	id           TEXT PRIMARY KEY,
	user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title        TEXT NOT NULL,
	notes        TEXT NOT NULL DEFAULT '',
	space        TEXT NOT NULL DEFAULT 'life',
	start_ms     INTEGER,
	due_ms       INTEGER,
	metric_name  TEXT NOT NULL DEFAULT '',
	metric_unit  TEXT NOT NULL DEFAULT '',
	metric_start REAL NOT NULL DEFAULT 0,
	metric_target REAL NOT NULL DEFAULT 0,
	archived     INTEGER NOT NULL DEFAULT 0,
	created_ms   INTEGER NOT NULL,
	updated_ms   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS objectives_user ON objectives(user_id, archived);
CREATE TABLE IF NOT EXISTS objective_measurements (
	id           TEXT PRIMARY KEY,
	objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
	user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	time_ms      INTEGER NOT NULL,
	value        REAL NOT NULL,
	note         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS objective_measurements_objective ON objective_measurements(objective_id, time_ms);
CREATE TABLE IF NOT EXISTS holdings (
	id            TEXT PRIMARY KEY,
	user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	symbol        TEXT NOT NULL,
	name          TEXT NOT NULL DEFAULT '',
	currency      TEXT NOT NULL DEFAULT 'USD',
	notes         TEXT NOT NULL DEFAULT '',
	last_price    REAL,
	last_price_ms INTEGER,
	dca_amount    REAL NOT NULL DEFAULT 0,
	dca_day       INTEGER NOT NULL DEFAULT 0,
	dca_time      TEXT NOT NULL DEFAULT '09:00',
	dca_event_id  TEXT NOT NULL DEFAULT '',
	tz            TEXT NOT NULL DEFAULT 'UTC',
	archived      INTEGER NOT NULL DEFAULT 0,
	created_ms    INTEGER NOT NULL,
	updated_ms    INTEGER NOT NULL,
	UNIQUE (user_id, symbol)
);
CREATE TABLE IF NOT EXISTS holding_trades (
	id         TEXT PRIMARY KEY,
	holding_id TEXT NOT NULL REFERENCES holdings(id) ON DELETE CASCADE,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	time_ms    INTEGER NOT NULL,
	side       TEXT NOT NULL,
	shares     REAL NOT NULL,
	price      REAL NOT NULL,
	fee        REAL NOT NULL DEFAULT 0,
	note       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS holding_trades_holding ON holding_trades(holding_id, time_ms);
CREATE TABLE IF NOT EXISTS proposals (
	id          TEXT PRIMARY KEY,
	user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind        TEXT NOT NULL,
	status      TEXT NOT NULL,
	title       TEXT NOT NULL,
	reason      TEXT NOT NULL DEFAULT '',
	space       TEXT NOT NULL DEFAULT 'life',
	source      TEXT NOT NULL,
	fingerprint TEXT NOT NULL DEFAULT '',
	ops         TEXT NOT NULL,
	inputs      TEXT NOT NULL DEFAULT '[]',
	created_ms  INTEGER NOT NULL,
	decided_ms  INTEGER
);
CREATE INDEX IF NOT EXISTS proposals_user ON proposals(user_id, status, created_ms DESC);
CREATE INDEX IF NOT EXISTS proposals_fp ON proposals(user_id, fingerprint);
CREATE TABLE IF NOT EXISTS changes (
	id          TEXT PRIMARY KEY,
	user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	proposal_id TEXT NOT NULL DEFAULT '',
	summary     TEXT NOT NULL,
	entries     TEXT NOT NULL,
	created_ms  INTEGER NOT NULL,
	undone_ms   INTEGER
);
CREATE INDEX IF NOT EXISTS changes_user ON changes(user_id, created_ms DESC);

CREATE TABLE IF NOT EXISTS preferences (
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	key        TEXT NOT NULL,
	value      TEXT NOT NULL,
	version    INTEGER NOT NULL,
	updated_ms INTEGER NOT NULL,
	PRIMARY KEY (user_id, key)
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
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}
	// Columns added after the first release. SQLite has no ADD COLUMN IF NOT
	// EXISTS, so look first.
	for _, table := range []string{"notes", "events", "tasks"} {
		if err := s.ensureColumn(ctx, table, "project_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS `+table+`_project ON `+table+`(project_id)`); err != nil {
			return err
		}
	}
	// The work/life dimension. Rows that predate it are life.
	for _, table := range []string{"notes", "events", "tasks", "goals", "projects", "reminders"} {
		if err := s.ensureColumn(ctx, table, "space", "TEXT NOT NULL DEFAULT 'life'"); err != nil {
			return err
		}
	}
	// A goal may carry a running total next to its per-period one (19 of 40 lessons).
	for _, c := range []struct{ table, col, ddl string }{
		{"goals", "counter_unit", "TEXT NOT NULL DEFAULT ''"},
		{"goals", "counter_target", "REAL NOT NULL DEFAULT 0"},
		{"goal_checkins", "tally", "REAL NOT NULL DEFAULT 0"},
		{"goals", "objective_id", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.ensureColumn(ctx, c.table, c.col, c.ddl); err != nil {
			return err
		}
	}
	return nil
}

// The two spaces. In a filter, "" means both ("combined" mode).
const (
	SpaceWork = "work"
	SpaceLife = "life"
)

// checkSpaceFilter validates a read filter: "", "work" or "life".
func checkSpaceFilter(space string) error {
	switch space {
	case "", SpaceWork, SpaceLife:
		return nil
	}
	return invalid("space must be work or life")
}

// resolveSpace picks the space for a new row: the one given, else the
// project's, else life.
func (s *Store) resolveSpace(ctx context.Context, userID, space, projectID string) (string, error) {
	if space != "" {
		if space != SpaceWork && space != SpaceLife {
			return "", invalid("space must be work or life")
		}
		return space, nil
	}
	if projectID != "" {
		var ps string
		err := s.db.QueryRowContext(ctx, `SELECT space FROM projects WHERE id = ? AND user_id = ?`, projectID, userID).Scan(&ps)
		if err == nil {
			return ps, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	return SpaceLife, nil
}

// spaceClause appends " AND <col> = ?" when a filter is set.
func spaceClause(q string, args []any, col, space string) (string, []any) {
	if space == "" {
		return q, args
	}
	return q + ` AND ` + col + ` = ?`, append(args, space)
}

func (s *Store) ensureColumn(ctx context.Context, table, column, ddl string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	_ = rows.Close()
	if found {
		return rows.Err()
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+ddl)
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
