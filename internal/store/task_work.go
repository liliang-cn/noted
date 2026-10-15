package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// TaskWork is one sitting on a task: a task done a bit today, a bit tomorrow.
type TaskWork struct {
	ID       string
	TaskID   string
	UserID   string
	Time     time.Time
	Minutes  int
	Progress int // 0-100, -1 when not given
	Note     string
}

const maxWorkMinutes = 24 * 60

// LogTaskWork records a sitting and returns the task with its totals
// recomputed. A zero at means now; a nil progress means none was given.
// Progress 100 completes the task.
func (s *Store) LogTaskWork(ctx context.Context, userID, taskID string, at time.Time, minutes int, progress *int, note string) (Task, error) {
	t, err := s.GetTask(ctx, userID, taskID)
	if err != nil {
		return Task{}, err
	}
	note = strings.TrimSpace(note)
	if minutes < 0 || minutes > maxWorkMinutes {
		return Task{}, invalid("minutes must be 0-%d", maxWorkMinutes)
	}
	if progress != nil && (*progress < 0 || *progress > 100) {
		return Task{}, invalid("progress must be 0-100")
	}
	if utf8.RuneCountInString(note) > 500 {
		return Task{}, invalid("note is longer than 500 characters")
	}
	if minutes == 0 && progress == nil && note == "" {
		return Task{}, invalid("give minutes, progress or a note")
	}
	if at.IsZero() {
		at = time.Now()
	}
	w := TaskWork{ID: uuid.NewString(), TaskID: taskID, UserID: userID, Time: at.UTC().Truncate(time.Millisecond), Minutes: minutes, Progress: -1, Note: note}
	if progress != nil {
		w.Progress = *progress
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_work(id, task_id, user_id, time_ms, minutes, progress, note) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			w.ID, w.TaskID, w.UserID, ms(w.Time), w.Minutes, w.Progress, w.Note); err != nil {
			return err
		}
		if w.Progress == 100 {
			res, err := tx.ExecContext(ctx, `UPDATE tasks SET completed = 1, complete_ms = ? WHERE id = ? AND user_id = ? AND completed = 0`,
				ms(w.Time), taskID, userID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 1 { // just completed: a repeating task gets its next one
				if _, err := s.spawnNext(ctx, tx, t); err != nil {
					return err
				}
			}
		}
		return totalTaskWork(ctx, tx, userID, taskID)
	})
	if err != nil {
		return Task{}, err
	}
	return s.GetTask(ctx, userID, taskID)
}

// DeleteTaskWork removes a sitting and returns its task with the totals
// recomputed. A completed task stays completed.
func (s *Store) DeleteTaskWork(ctx context.Context, userID, id string) (Task, error) {
	var taskID string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT task_id FROM task_work WHERE id = ? AND user_id = ?`, id, userID).Scan(&taskID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM task_work WHERE id = ? AND user_id = ?`, id, userID); err != nil {
			return err
		}
		return totalTaskWork(ctx, tx, userID, taskID)
	})
	if err != nil {
		return Task{}, err
	}
	return s.GetTask(ctx, userID, taskID)
}

// totalTaskWork recomputes a task's totals from its sittings. The progress is
// that of the latest sitting (by time) that gave one.
func totalTaskWork(ctx context.Context, tx *sql.Tx, userID, taskID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE tasks SET
		progress = COALESCE((SELECT progress FROM task_work WHERE task_id = ?1 AND progress >= 0 ORDER BY time_ms DESC, rowid DESC LIMIT 1), 0),
		worked_minutes = (SELECT COALESCE(SUM(minutes), 0) FROM task_work WHERE task_id = ?1),
		work_sessions = (SELECT COUNT(*) FROM task_work WHERE task_id = ?1),
		last_worked_ms = (SELECT MAX(time_ms) FROM task_work WHERE task_id = ?1),
		updated_ms = ?2
		WHERE id = ?1 AND user_id = ?3`, taskID, ms(time.Now().UTC().Truncate(time.Millisecond)), userID)
	return err
}

// ListTaskWork returns a task's sittings, newest first.
func (s *Store) ListTaskWork(ctx context.Context, userID, taskID string) ([]TaskWork, error) {
	if _, err := s.GetTask(ctx, userID, taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, user_id, time_ms, minutes, progress, note FROM task_work
		WHERE task_id = ? AND user_id = ? ORDER BY time_ms DESC, rowid DESC`, taskID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []TaskWork
	for rows.Next() {
		var w TaskWork
		var t int64
		if err := rows.Scan(&w.ID, &w.TaskID, &w.UserID, &t, &w.Minutes, &w.Progress, &w.Note); err != nil {
			return nil, err
		}
		w.Time = fromMS(t)
		out = append(out, w)
	}
	return out, rows.Err()
}
