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
	Notify string // the item's notify when it fired: alarm, reminder or app
}

// CollectDueReminders finds every schedule item that should have been told
// by now, records it, and marks its source so it fires only once. It is safe
// to call repeatedly; the scheduler calls it on a timer.
//
// An item is told when its next occurrence is within its remind_before window;
// occurrences that already started are not reminded, except that a reminder
// set for the start itself (0 minutes) still fires if the scheduler reaches it
// up to startGrace late. Items with notify none, paused ones, done to-dos and
// items without a remind_before are never told.
func (s *Store) CollectDueReminders(ctx context.Context, now time.Time) ([]Reminder, error) {
	var fired []Reminder
	err := s.tx(ctx, func(tx *sql.Tx) error {
		evs, err := s.dueEvents(ctx, tx, now)
		if err != nil {
			return err
		}
		for _, d := range evs {
			kind, ref := KindEvent, d.ev.ID
			if d.ev.Source == SourceTask {
				kind, ref = KindTask, d.ev.TaskID
			}
			r := Reminder{ID: uuid.NewString(), UserID: d.ev.UserID, Kind: kind, RefID: ref,
				Title: d.ev.Title, Due: d.start, Fired: now, Space: d.ev.Space, Notify: d.ev.Notify}
			if err := insertReminder(ctx, tx, r); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE events SET reminded_for_ms = ? WHERE id = ?`, ms(d.start), d.ev.ID); err != nil {
				return err
			}
			if kind == KindTask { // kept for a rollback to the old scheduler
				if _, err := tx.ExecContext(ctx, `UPDATE tasks SET reminded = 1 WHERE id = ?`, ref); err != nil {
					return err
				}
			}
			fired = append(fired, r)
		}
		return nil
	})
	return fired, err
}

// startGrace is how late an "at the start" reminder may still be delivered.
const startGrace = 2 * time.Minute

type dueEvent struct {
	ev    Event
	start time.Time
}

func (s *Store) dueEvents(ctx context.Context, tx *sql.Tx, now time.Time) ([]dueEvent, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+eventCols+`, reminded_for_ms FROM events
		WHERE remind_before IS NOT NULL AND notify <> 'none' AND enabled = 1 AND completed = 0
		AND (rrule <> '' OR rule <> '' OR start_ms > ?)`, ms(now.Add(-startGrace)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []dueEvent
	for rows.Next() {
		var last sql.NullInt64
		e, err := scanEvent(rows, &last)
		if err != nil {
			return nil, err
		}
		lead := time.Duration(*e.RemindBefore) * time.Minute
		grace := time.Duration(0)
		if lead == 0 {
			grace = startGrace
		}
		// The next occurrence that has not started yet.
		for _, o := range e.Occurrences(now.Add(-grace), now.Add(lead+time.Minute), 8) {
			if late := now.Sub(o.Start); late > 0 && (grace == 0 || late >= grace) {
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
	_, err := tx.ExecContext(ctx, `INSERT INTO reminders(id, user_id, kind, ref_id, title, due_ms, fire_ms, space, notify)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.ID, r.UserID, r.Kind, r.RefID, r.Title, ms(r.Due), ms(r.Fired), r.Space, r.Notify)
	return err
}

// ListReminders returns reminders that fired at or after since, newest first.
func (s *Store) ListReminders(ctx context.Context, userID string, since time.Time, limit int, space string) ([]Reminder, error) {
	if err := checkSpaceFilter(space); err != nil {
		return nil, err
	}
	q := `SELECT id, user_id, kind, ref_id, title, due_ms, fire_ms, space, notify FROM reminders WHERE user_id = ? AND fire_ms >= ?`
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
		if err := rows.Scan(&r.ID, &r.UserID, &r.Kind, &r.RefID, &r.Title, &due, &fire, &r.Space, &r.Notify); err != nil {
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
