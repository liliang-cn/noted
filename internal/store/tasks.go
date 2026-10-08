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

type Task struct {
	ID       string
	UserID   string
	Title    string
	Notes    string
	Due      *time.Time
	Priority int // 0 unset, 1 low, 2 medium, 3 high
	Done     bool
	DoneAt   *time.Time
	Remind   *time.Time
	Tags     []string
	Created  time.Time
	Updated  time.Time
}

type TaskPatch struct {
	Title, Notes *string
	Due          **time.Time // non-nil: set; *Due == nil clears
	Priority     *int
	Done         *bool
	Remind       **time.Time
	Tags         *[]string
}

type TaskFilter struct {
	State     string // "open" (default), "done" or "all"
	Tag       string
	DueBefore *time.Time
	Limit     int
	Offset    int
}

func (t *Task) normalize() error {
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
		return invalid("title is empty")
	}
	if utf8.RuneCountInString(t.Title) > maxTitle {
		return invalid("title is longer than %d characters", maxTitle)
	}
	if t.Priority < 0 || t.Priority > 3 {
		return invalid("priority must be 0-3")
	}
	var err error
	t.Tags, err = NormalizeTags(t.Tags)
	return err
}

func (s *Store) CreateTask(ctx context.Context, t Task) (Task, error) {
	if err := t.normalize(); err != nil {
		return Task{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	t.ID, t.Created, t.Updated = uuid.NewString(), now, now
	if t.Done {
		t.DoneAt = &now
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO tasks(id, user_id, title, notes, due_ms, priority, completed, complete_ms, remind_ms, created_ms, updated_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.UserID, t.Title, t.Notes, nullMS(t.Due), t.Priority, b2i(t.Done), nullMS(t.DoneAt), nullMS(t.Remind),
			ms(t.Created), ms(t.Updated))
		if err != nil {
			return err
		}
		return writeTaskTags(ctx, tx, t)
	})
	return t, err
}

func writeTaskTags(ctx context.Context, tx *sql.Tx, t Task) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_tags WHERE task_id = ?`, t.ID); err != nil {
		return err
	}
	for _, tag := range t.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_tags(task_id, tag) VALUES (?, ?)`, t.ID, tag); err != nil {
			return err
		}
	}
	return nil
}

const taskCols = `id, user_id, title, notes, due_ms, priority, completed, complete_ms, remind_ms, created_ms, updated_ms`

func scanTask(r scanner) (Task, error) {
	var t Task
	var due, doneAt, remind sql.NullInt64
	var done int
	var created, updated int64
	if err := r.Scan(&t.ID, &t.UserID, &t.Title, &t.Notes, &due, &t.Priority, &done, &doneAt, &remind, &created, &updated); err != nil {
		return Task{}, err
	}
	t.Due, t.DoneAt, t.Remind = ptrTime(due), ptrTime(doneAt), ptrTime(remind)
	t.Done = done != 0
	t.Created, t.Updated = fromMS(created), fromMS(updated)
	return t, nil
}

func (s *Store) withTaskTags(ctx context.Context, ts []Task) error {
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.ID
	}
	tags, err := s.loadTags(ctx, "task_tags", "task_id", ids)
	if err != nil {
		return err
	}
	for i := range ts {
		ts[i].Tags = tags[ts[i].ID]
	}
	return nil
}

func (s *Store) GetTask(ctx context.Context, userID, id string) (Task, error) {
	t, err := scanTask(s.db.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	ts := []Task{t}
	if err := s.withTaskTags(ctx, ts); err != nil {
		return Task{}, err
	}
	return ts[0], nil
}

func (s *Store) UpdateTask(ctx context.Context, userID, id string, p TaskPatch) (Task, error) {
	t, err := s.GetTask(ctx, userID, id)
	if err != nil {
		return Task{}, err
	}
	oldRemind := nullMS(t.Remind)
	if p.Title != nil {
		t.Title = *p.Title
	}
	if p.Notes != nil {
		t.Notes = *p.Notes
	}
	if p.Due != nil {
		t.Due = *p.Due
	}
	if p.Priority != nil {
		t.Priority = *p.Priority
	}
	if p.Remind != nil {
		t.Remind = *p.Remind
	}
	if p.Tags != nil {
		t.Tags = *p.Tags
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if p.Done != nil && *p.Done != t.Done {
		t.Done = *p.Done
		if t.Done {
			t.DoneAt = &now
		} else {
			t.DoneAt = nil
		}
	}
	if err := t.normalize(); err != nil {
		return Task{}, err
	}
	t.Updated = now
	rearm := oldRemind != nullMS(t.Remind) || (p.Done != nil && !t.Done)
	err = s.tx(ctx, func(tx *sql.Tx) error {
		q := `UPDATE tasks SET title = ?, notes = ?, due_ms = ?, priority = ?, completed = ?, complete_ms = ?,
			remind_ms = ?, updated_ms = ?`
		if rearm {
			q += `, reminded = 0`
		}
		q += ` WHERE id = ? AND user_id = ?`
		if _, err := tx.ExecContext(ctx, q, t.Title, t.Notes, nullMS(t.Due), t.Priority, b2i(t.Done), nullMS(t.DoneAt),
			nullMS(t.Remind), ms(t.Updated), id, userID); err != nil {
			return err
		}
		return writeTaskTags(ctx, tx, t)
	})
	return t, err
}

func (s *Store) DeleteTask(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTasks orders open tasks by due date (undated last), then priority.
func (s *Store) ListTasks(ctx context.Context, userID string, f TaskFilter) ([]Task, error) {
	q := `SELECT ` + taskCols + ` FROM tasks t WHERE user_id = ?`
	args := []any{userID}
	switch f.State {
	case "done":
		q += ` AND completed = 1`
	case "all":
	default:
		q += ` AND completed = 0`
	}
	if f.Tag != "" {
		q += ` AND EXISTS (SELECT 1 FROM task_tags g WHERE g.task_id = t.id AND g.tag = ?)`
		args = append(args, strings.ToLower(f.Tag))
	}
	if f.DueBefore != nil {
		q += ` AND due_ms IS NOT NULL AND due_ms < ?`
		args = append(args, ms(*f.DueBefore))
	}
	q += ` ORDER BY completed, due_ms IS NULL, due_ms, priority DESC, created_ms, id LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, s.withTaskTags(ctx, out)
}
