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
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid argument")
	ErrConflict = errors.New("version conflict")
	// ErrPrecondition: the request is fine, but the state does not allow it
	// now (a timer is already running).
	ErrPrecondition = errors.New("failed precondition")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

func precondition(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrPrecondition, fmt.Sprintf(format, a...))
}

type Store struct {
	db  *sql.DB
	loc atomic.Pointer[time.Location] // the server's zone; nil means UTC
}

// SetLocation sets the zone that wall-clock rules are read in, such as the
// day a repeating task falls on next. The default is UTC.
func (s *Store) SetLocation(loc *time.Location) { s.loc.Store(loc) }

func (s *Store) location() *time.Location {
	if l := s.loc.Load(); l != nil {
		return l
	}
	return time.UTC
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
CREATE TABLE IF NOT EXISTS apple_ids (
	sub        TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
	created_ms INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS user_emails (
	email         TEXT PRIMARY KEY COLLATE NOCASE,
	user_id       TEXT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
	password_hash TEXT NOT NULL,
	created_ms    INTEGER NOT NULL,
	updated_ms    INTEGER NOT NULL
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
CREATE TABLE IF NOT EXISTS task_work (
	id       TEXT PRIMARY KEY,
	task_id  TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	user_id  TEXT NOT NULL,
	time_ms  INTEGER NOT NULL,
	minutes  INTEGER NOT NULL,
	progress INTEGER NOT NULL DEFAULT -1,
	note     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS task_work_task ON task_work(task_id, time_ms);

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
CREATE TABLE IF NOT EXISTS objective_milestones (
	id           TEXT PRIMARY KEY,
	objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
	position     INTEGER NOT NULL,
	title        TEXT NOT NULL,
	done_ms      INTEGER,
	due_ms       INTEGER,
	criteria     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS objective_milestones_objective ON objective_milestones(objective_id, position);
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

-- Time tracking. end_ms is NULL while the entry is a running timer, and a user
-- has at most one of those. title_key is the activity folded for grouping.
CREATE TABLE IF NOT EXISTS time_entries (
	id         TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title      TEXT NOT NULL,
	title_key  TEXT NOT NULL,
	minutes    INTEGER NOT NULL DEFAULT 0,
	start_ms   INTEGER NOT NULL,
	end_ms     INTEGER,
	note       TEXT NOT NULL DEFAULT '',
	project_id TEXT NOT NULL DEFAULT '',
	goal_id    TEXT NOT NULL DEFAULT '',
	space      TEXT NOT NULL DEFAULT 'life',
	created_ms INTEGER NOT NULL,
	updated_ms INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS time_entries_user_start ON time_entries(user_id, start_ms DESC);
CREATE INDEX IF NOT EXISTS time_entries_project ON time_entries(project_id);
CREATE INDEX IF NOT EXISTS time_entries_goal ON time_entries(goal_id);
CREATE UNIQUE INDEX IF NOT EXISTS time_entries_running ON time_entries(user_id) WHERE end_ms IS NULL;

-- Alarms (闹钟): definitions the phone rings. schedule is the JSON of
-- alarm.Schedule (kind, at, anchor, time, timeZone, recurrence).
CREATE TABLE IF NOT EXISTS alarms (
	id             TEXT PRIMARY KEY,
	user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	label          TEXT NOT NULL,
	mode           TEXT NOT NULL DEFAULT 'alarm',
	enabled        INTEGER NOT NULL DEFAULT 1,
	schedule       TEXT NOT NULL,
	holiday_policy TEXT NOT NULL DEFAULT 'none',
	region         TEXT NOT NULL DEFAULT '',
	note           TEXT NOT NULL DEFAULT '',
	project_id     TEXT NOT NULL DEFAULT '',
	space          TEXT NOT NULL DEFAULT 'life',
	original_text  TEXT NOT NULL DEFAULT '',
	created_ms     INTEGER NOT NULL,
	updated_ms     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS alarms_user ON alarms(user_id, created_ms);

CREATE TABLE IF NOT EXISTS preferences (
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	key        TEXT NOT NULL,
	value      TEXT NOT NULL,
	version    INTEGER NOT NULL,
	updated_ms INTEGER NOT NULL,
	PRIMARY KEY (user_id, key)
);

-- The one-way sync into a CortexDB brain (package cortexsync): where a user's
-- items went and a hash of each one as sent, so a run sends only what changed
-- and finds deletes (the tables above delete rows outright).
CREATE TABLE IF NOT EXISTS cortex_sync_state (
	user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	target  TEXT NOT NULL,
	run_ms  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS cortex_sync_items (
	user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	item_key  TEXT NOT NULL,
	hash      TEXT NOT NULL,
	synced_ms INTEGER NOT NULL,
	PRIMARY KEY (user_id, item_key)
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

-- MCP authorization (OAuth 2.1): clients that registered themselves, the
-- pairings their browser sign-ins wait on, and the codes they trade for a token.
CREATE TABLE IF NOT EXISTS oauth_clients (
	id            TEXT PRIMARY KEY,
	name          TEXT NOT NULL,
	redirect_uris TEXT NOT NULL,
	created_ms    INTEGER NOT NULL,
	last_used_ms  INTEGER
);
CREATE TABLE IF NOT EXISTS pairings (
	code           TEXT PRIMARY KEY,
	poll_hash      TEXT NOT NULL UNIQUE,
	client_id      TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
	redirect_uri   TEXT NOT NULL,
	code_challenge TEXT NOT NULL,
	state          TEXT NOT NULL DEFAULT '',
	resource       TEXT NOT NULL DEFAULT '',
	created_ms     INTEGER NOT NULL,
	expires_ms     INTEGER NOT NULL,
	user_id        TEXT REFERENCES users(id) ON DELETE CASCADE,
	label          TEXT NOT NULL DEFAULT '',
	denied         INTEGER NOT NULL DEFAULT 0,
	issued         INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS oauth_codes (
	hash           TEXT PRIMARY KEY,
	client_id      TEXT NOT NULL,
	redirect_uri   TEXT NOT NULL,
	code_challenge TEXT NOT NULL,
	resource       TEXT NOT NULL DEFAULT '',
	user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	label          TEXT NOT NULL,
	expires_ms     INTEGER NOT NULL
);
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
		{"users", "pro", "INTEGER NOT NULL DEFAULT 0"},
		{"goals", "project_id", "TEXT NOT NULL DEFAULT ''"},
		{"objectives", "project_id", "TEXT NOT NULL DEFAULT ''"},
		// A task worked on over several sittings (task_work), totalled here.
		{"tasks", "progress", "INTEGER NOT NULL DEFAULT 0"},
		{"tasks", "worked_minutes", "INTEGER NOT NULL DEFAULT 0"},
		{"tasks", "work_sessions", "INTEGER NOT NULL DEFAULT 0"},
		{"tasks", "last_worked_ms", "INTEGER"},
		// The assistant conversation a proposal came from, so a follow-up can replace it.
		{"proposals", "session", "TEXT NOT NULL DEFAULT ''"},
		// When a token last authenticated a call (throttled), shown in ListTokens.
		{"tokens", "last_used_ms", "INTEGER"},
		// Repeating to-dos: the rule, this task's place in its series (for
		// COUNT), and the task its completion created (so it is made once).
		{"tasks", "rrule", "TEXT NOT NULL DEFAULT ''"},
		{"tasks", "occurrence", "INTEGER NOT NULL DEFAULT 1"},
		{"tasks", "next_task_id", "TEXT NOT NULL DEFAULT ''"},
		// The time entry a check-in was made by; it follows that entry.
		{"goal_checkins", "time_entry_id", "TEXT NOT NULL DEFAULT ''"},
		// A milestone's due time and what counts as done (objective_milestones
		// has them from the start).
		{"goal_milestones", "due_ms", "INTEGER"},
		{"goal_milestones", "criteria", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.ensureColumn(ctx, c.table, c.col, c.ddl); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS goal_checkins_entry ON goal_checkins(time_entry_id) WHERE time_entry_id <> ''`); err != nil {
		return err
	}
	return s.migrateTimed(ctx)
}

// timedGuess is how close a finished entry's end may be to when it was made
// for it to count as logged with only a duration: the default start was then
// "now minus minutes", so the end is the moment it was saved, a few
// milliseconds before the row's create time.
const timedGuess = 5 * time.Second

// migrateTimed adds time_entries.timed (are the start and end real, or was
// the start defaulted from the minutes?) and, the first time, guesses it for
// the rows already there: a running or stopped timer, or an entry whose end is
// not when it was saved, is timed.
func (s *Store) migrateTimed(ctx context.Context) error {
	had, err := s.hasColumn(ctx, "time_entries", "timed")
	if err != nil || had {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE time_entries ADD COLUMN timed INTEGER NOT NULL DEFAULT 1`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE time_entries SET timed = 0 WHERE end_ms IS NOT NULL AND abs(end_ms - created_ms) < ?`,
			timedGuess.Milliseconds())
		return err
	})
}

func (s *Store) hasColumn(ctx context.Context, table, column string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	return n > 0, err
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
