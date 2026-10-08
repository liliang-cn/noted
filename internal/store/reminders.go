package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

const (
	KindEvent = 1
	KindTask  = 2
)

type Reminder struct {
	ID     string
	UserID string
	Kind   int
	RefID  string
	Title  string
	Due    time.Time
	Fired  time.Time
	Space  string
}

// CollectDueReminders finds every reminder that should have fired by now,
// records it, and marks its source so it fires only once. It is safe to call
// repeatedly; the scheduler calls it on a timer.
//
// An event reminder fires when the next occurrence is within its
// remind_before window; occurrences that already started are not reminded.
func (s *Store) CollectDueReminders(ctx context.Context, now time.Time) ([]Reminder, error) {
	var fired []Reminder
	err := s.tx(ctx, func(tx *sql.Tx) error {
		evs, err := s.dueEvents(ctx, tx, now)
		if err != nil {
			return err
		}
		for _, d := range evs {
			r := Reminder{ID: uuid.NewString(), UserID: d.ev.UserID, Kind: KindEvent, RefID: d.ev.ID,
				Title: d.ev.Title, Due: d.start, Fired: now, Space: d.ev.Space}
			if err := insertReminder(ctx, tx, r); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE events SET reminded_for_ms = ? WHERE id = ?`, ms(d.start), d.ev.ID); err != nil {
				return err
			}
			fired = append(fired, r)
		}

		rows, err := tx.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks
			WHERE completed = 0 AND reminded = 0 AND remind_ms IS NOT NULL AND remind_ms <= ?`, ms(now))
		if err != nil {
			return err
		}
		var tasks []Task
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				_ = rows.Close()
				return err
			}
			tasks = append(tasks, t)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, t := range tasks {
			due := now
			if t.Due != nil {
				due = *t.Due
			}
			r := Reminder{ID: uuid.NewString(), UserID: t.UserID, Kind: KindTask, RefID: t.ID, Title: t.Title, Due: due, Fired: now, Space: t.Space}
			if err := insertReminder(ctx, tx, r); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE tasks SET reminded = 1 WHERE id = ?`, t.ID); err != nil {
				return err
			}
			fired = append(fired, r)
		}
		return nil
	})
	return fired, err
}

type dueEvent struct {
	ev    Event
	start time.Time
}

func (s *Store) dueEvents(ctx context.Context, tx *sql.Tx, now time.Time) ([]dueEvent, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+eventCols+`, reminded_for_ms FROM events
		WHERE remind_before IS NOT NULL AND (rrule <> '' OR start_ms > ?)`, ms(now))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []dueEvent
	for rows.Next() {
		var e Event
		var start, end, created, updated int64
		var allDay int
		var remind, last sql.NullInt64
		if err := rows.Scan(&e.ID, &e.UserID, &e.Title, &e.Description, &e.Location, &start, &end, &allDay,
			&e.TimeZone, &e.RRule, &remind, &e.ProjectID, &e.Space, &created, &updated, &last); err != nil {
			return nil, err
		}
		e.Start, e.End, e.AllDay = fromMS(start), fromMS(end), allDay != 0
		lead := time.Duration(remind.Int64) * time.Minute
		// The next occurrence that has not started yet.
		for _, o := range e.Occurrences(now, now.Add(lead+time.Minute), 8) {
			if !o.Start.After(now) {
				continue // already started (or ongoing): too late to remind
			}
			if o.Start.Add(-lead).After(now) {
				break // not inside the reminder window yet
			}
			if !last.Valid || last.Int64 != ms(o.Start) {
				out = append(out, dueEvent{ev: e, start: o.Start})
			}
			break
		}
	}
	return out, rows.Err()
}

func insertReminder(ctx context.Context, tx *sql.Tx, r Reminder) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO reminders(id, user_id, kind, ref_id, title, due_ms, fire_ms, space)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, r.ID, r.UserID, r.Kind, r.RefID, r.Title, ms(r.Due), ms(r.Fired), r.Space)
	return err
}

// ListReminders returns reminders that fired at or after since, newest first.
func (s *Store) ListReminders(ctx context.Context, userID string, since time.Time, limit int, space string) ([]Reminder, error) {
	if err := checkSpaceFilter(space); err != nil {
		return nil, err
	}
	q := `SELECT id, user_id, kind, ref_id, title, due_ms, fire_ms, space FROM reminders WHERE user_id = ? AND fire_ms >= ?`
	args := []any{userID, ms(since)}
	q, args = spaceClause(q, args, "space", space)
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY fire_ms DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Reminder
	for rows.Next() {
		var r Reminder
		var due, fire int64
		if err := rows.Scan(&r.ID, &r.UserID, &r.Kind, &r.RefID, &r.Title, &due, &fire, &r.Space); err != nil {
			return nil, err
		}
		r.Due, r.Fired = fromMS(due), fromMS(fire)
		out = append(out, r)
	}
	return out, rows.Err()
}

// PruneReminders deletes reminder history older than cutoff.
func (s *Store) PruneReminders(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM reminders WHERE fire_ms < ?`, ms(cutoff))
	return err
}
