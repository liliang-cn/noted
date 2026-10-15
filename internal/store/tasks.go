package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/liliang-cn/noted/internal/recur"
)

type Task struct {
	ID        string
	UserID    string
	Title     string
	Notes     string
	Due       *time.Time
	Priority  int // 0 unset, 1 low, 2 medium, 3 high
	Done      bool
	DoneAt    *time.Time
	Remind    *time.Time
	Tags      []string
	ProjectID string
	Space     string
	Created   time.Time
	Updated   time.Time
	// Totals of the task's sittings (task_work), kept by LogTaskWork and DeleteTaskWork.
	Progress      int // 0-100: the latest progress a sitting gave
	WorkedMinutes int
	WorkSessions  int
	LastWorked    *time.Time
	// Repetition: an RRULE (see package recur) anchored on Due, this task's
	// 1-based place in its series (COUNT is counted with it), and the task its
	// completion created, so completing it again does not make another.
	RRule      string
	Occurrence int
	NextID     string
}

type TaskPatch struct {
	Title, Notes *string
	Due          **time.Time // non-nil: set; *Due == nil clears
	Priority     *int
	Done         *bool
	Remind       **time.Time
	Tags         *[]string
	ProjectID    *string
	Space        *string
	RRule        *string // "" makes it one-off
}

type TaskFilter struct {
	State     string // "open" (default), "done" or "all"
	Tag       string
	DueBefore *time.Time
	ProjectID string
	Space     string
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
	t.RRule = cleanRRule(t.RRule)
	if t.RRule != "" {
		if _, err := recur.Parse(t.RRule); err != nil {
			return invalid("%v", err)
		}
		if t.Due == nil {
			return invalid("a repeating task needs a due_time")
		}
	}
	if t.Occurrence < 1 {
		t.Occurrence = 1
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
	if t.Done && t.DoneAt == nil {
		t.DoneAt = &now
	}
	if err := s.checkProject(ctx, t.UserID, t.ProjectID); err != nil {
		return Task{}, err
	}
	var err error
	if t.Space, err = s.resolveSpace(ctx, t.UserID, t.Space, t.ProjectID); err != nil {
		return Task{}, err
	}
	err = s.tx(ctx, func(tx *sql.Tx) error { return insertTask(ctx, tx, t) })
	return t, err
}

// cleanRRule drops surrounding space and a leading "RRULE:".
func cleanRRule(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "RRULE:"))
}

func insertTask(ctx context.Context, tx *sql.Tx, t Task) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO tasks(id, user_id, title, notes, due_ms, priority, completed, complete_ms, remind_ms, project_id, space, created_ms, updated_ms,
			rrule, occurrence)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Title, t.Notes, nullMS(t.Due), t.Priority, b2i(t.Done), nullMS(t.DoneAt), nullMS(t.Remind),
		t.ProjectID, t.Space, ms(t.Created), ms(t.Updated), t.RRule, t.Occurrence)
	if err != nil {
		return err
	}
	return writeTaskTags(ctx, tx, t)
}

// nextDue is when the task after t falls due: the rule's first occurrence
// after t.Due, read on the wall clock in loc. False when t does not repeat or
// its series is over (COUNT reached, or past UNTIL).
func nextDue(t Task, loc *time.Location) (time.Time, bool) {
	if t.RRule == "" || t.Due == nil {
		return time.Time{}, false
	}
	r, err := recur.Parse(t.RRule)
	if err != nil {
		return time.Time{}, false
	}
	if r.Count > 0 && t.Occurrence >= r.Count {
		return time.Time{}, false
	}
	r.Count = 0 // counted with Occurrence: the rule is anchored on this task
	due := t.Due.In(loc)
	next := r.Occurrences(due, due.Add(time.Nanosecond), due.AddDate(1000, 0, 0), 1)
	if len(next) == 0 {
		return time.Time{}, false
	}
	return next[0].UTC(), true
}

// spawnNext adds the task that follows t, which has just been completed: the
// same to-do due at the rule's next occurrence, its reminder as far ahead of
// that as t's was, and no sittings. Nothing when t does not repeat, its
// series is over, or its follower already exists. It returns the follower's id.
func (s *Store) spawnNext(ctx context.Context, tx *sql.Tx, t Task) (string, error) {
	if t.NextID != "" {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = ? AND user_id = ?`, t.NextID, t.UserID).Scan(&one)
		if err == nil {
			return t.NextID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	due, ok := nextDue(t, s.location())
	if !ok {
		return "", nil
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	n := Task{
		ID: uuid.NewString(), UserID: t.UserID, Title: t.Title, Notes: t.Notes, Due: &due, Priority: t.Priority,
		Tags: t.Tags, ProjectID: t.ProjectID, Space: t.Space, Created: now, Updated: now,
		RRule: t.RRule, Occurrence: t.Occurrence + 1,
	}
	if t.Remind != nil {
		r := due.Add(t.Remind.Sub(*t.Due))
		n.Remind = &r
	}
	if err := insertTask(ctx, tx, n); err != nil {
		return "", err
	}
	_, err := tx.ExecContext(ctx, `UPDATE tasks SET next_task_id = ? WHERE id = ? AND user_id = ?`, n.ID, t.ID, t.UserID)
	return n.ID, err
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

const taskCols = `id, user_id, title, notes, due_ms, priority, completed, complete_ms, remind_ms, project_id, space, created_ms, updated_ms,
	progress, worked_minutes, work_sessions, last_worked_ms, rrule, occurrence, next_task_id`

func scanTask(r scanner) (Task, error) {
	var t Task
	var due, doneAt, remind, worked sql.NullInt64
	var done int
	var created, updated int64
	if err := r.Scan(&t.ID, &t.UserID, &t.Title, &t.Notes, &due, &t.Priority, &done, &doneAt, &remind, &t.ProjectID, &t.Space, &created, &updated,
		&t.Progress, &t.WorkedMinutes, &t.WorkSessions, &worked, &t.RRule, &t.Occurrence, &t.NextID); err != nil {
		return Task{}, err
	}
	t.Due, t.DoneAt, t.Remind, t.LastWorked = ptrTime(due), ptrTime(doneAt), ptrTime(remind), ptrTime(worked)
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
	if p.ProjectID != nil {
		t.ProjectID = *p.ProjectID
		if err := s.checkProject(ctx, userID, t.ProjectID); err != nil {
			return Task{}, err
		}
	}
	if p.Space != nil {
		if *p.Space != SpaceWork && *p.Space != SpaceLife {
			return Task{}, invalid("space must be work or life")
		}
		t.Space = *p.Space
	}
	if p.RRule != nil && cleanRRule(*p.RRule) != t.RRule {
		t.RRule, t.Occurrence = cleanRRule(*p.RRule), 1 // a new rule starts a new series
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	completing := p.Done != nil && *p.Done && !t.Done
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
			remind_ms = ?, project_id = ?, space = ?, updated_ms = ?, rrule = ?, occurrence = ?`
		if rearm {
			q += `, reminded = 0`
		}
		q += ` WHERE id = ? AND user_id = ?`
		if _, err := tx.ExecContext(ctx, q, t.Title, t.Notes, nullMS(t.Due), t.Priority, b2i(t.Done), nullMS(t.DoneAt),
			nullMS(t.Remind), t.ProjectID, t.Space, ms(t.Updated), t.RRule, t.Occurrence, id, userID); err != nil {
			return err
		}
		if err := writeTaskTags(ctx, tx, t); err != nil {
			return err
		}
		if completing {
			next, err := s.spawnNext(ctx, tx, t)
			if err != nil {
				return err
			}
			if next != "" {
				t.NextID = next
			}
		}
		return nil
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
	if f.ProjectID != "" {
		q += ` AND project_id = ?`
		args = append(args, f.ProjectID)
	}
	if err := checkSpaceFilter(f.Space); err != nil {
		return nil, err
	}
	q, args = spaceClause(q, args, "space", f.Space)
	q += ` ORDER BY completed, due_ms IS NULL, due_ms, priority DESC, created_ms, id LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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
	_ = rows.Close()
	return out, s.withTaskTags(ctx, out)
}

// CountCompleted counts tasks finished in [from, to).
func (s *Store) CountCompleted(ctx context.Context, userID string, from, to time.Time, space string) (int, error) {
	if err := checkSpaceFilter(space); err != nil {
		return 0, err
	}
	q := `SELECT COUNT(*) FROM tasks WHERE user_id = ? AND completed = 1 AND complete_ms >= ? AND complete_ms < ?`
	args := []any{userID, ms(from), ms(to)}
	q, args = spaceClause(q, args, "space", space)
	var n int
	return n, s.db.QueryRowContext(ctx, q, args...).Scan(&n)
}
