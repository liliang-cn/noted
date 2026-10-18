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
	// How the phone tells the person at Remind: alarm, reminder (default), app
	// or none, as Event.Notify. A to-do with a due or remind time is also a
	// schedule item (events, source task, the same id), kept in step here.
	Notify string
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
	Notify       *string
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
	if err := t.normalizeNotify(); err != nil {
		return err
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

// normalizeNotify fills in how the to-do is told. Unsaid: a reminder when it
// has a remind time, else nobody. One that is told needs a moment: without a
// remind time it is told at its due time.
func (t *Task) normalizeNotify() error {
	if t.Notify == "" {
		t.Notify = NotifyNone
		if t.Remind != nil {
			t.Notify = NotifyReminder
		}
		return nil
	}
	if err := CheckNotify(t.Notify); err != nil {
		return err
	}
	if t.Notify != NotifyNone && t.Remind == nil {
		if t.Due == nil {
			return invalid("notify %s needs a time: give the to-do a due or remind_time", t.Notify)
		}
		r := *t.Due
		t.Remind = &r
	}
	return nil
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
	err = s.tx(ctx, func(tx *sql.Tx) error { return insertTask(ctx, tx, t, s.location().String()) })
	return t, err
}

// cleanRRule drops surrounding space and a leading "RRULE:".
func cleanRRule(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "RRULE:"))
}

// insertTask writes a new to-do and its schedule item, shown in zone tz.
func insertTask(ctx context.Context, tx *sql.Tx, t Task, tz string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO tasks(id, user_id, title, notes, due_ms, priority, completed, complete_ms, remind_ms, project_id, space, created_ms, updated_ms,
			rrule, occurrence, notify)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Title, t.Notes, nullMS(t.Due), t.Priority, b2i(t.Done), nullMS(t.DoneAt), nullMS(t.Remind),
		t.ProjectID, t.Space, ms(t.Created), ms(t.Updated), t.RRule, t.Occurrence, t.Notify)
	if err != nil {
		return err
	}
	if err := writeTaskTags(ctx, tx, t); err != nil {
		return err
	}
	return syncTaskSchedule(ctx, tx, t.ID, tz)
}

// taskScheduleUpsert writes the schedule items (events, source task) of the
// to-dos the WHERE clause picks: one per to-do with a due or remind time, its
// start the due time (else the remind time), its lead the minutes between
// them, in zone tz. An existing item keeps its reminded_for_ms unless the
// time, the lead or a reopening re-arms it. reminded is the tasks column the
// old scheduler ticked; on first creation it carries over as "already told"
// when carry is set (the one-time migration), else a new item is fresh.
func taskScheduleUpsert(where string, carry bool) string {
	remindedFor := "NULL"
	if carry {
		remindedFor = "CASE WHEN reminded = 1 THEN COALESCE(due_ms, remind_ms) ELSE NULL END"
	}
	return `INSERT INTO events(id, user_id, title, description, location, start_ms, end_ms, all_day, tz, rrule, remind_before, project_id, space,
			created_ms, updated_ms, notify, rule, holiday_policy, region, enabled, original_text, source, task_id, completed, reminded_for_ms)
		SELECT id, user_id, title, notes, '', COALESCE(due_ms, remind_ms), COALESCE(due_ms, remind_ms), 0, ?1, '',
			CASE WHEN remind_ms IS NULL THEN NULL WHEN due_ms IS NULL THEN 0 ELSE MAX((due_ms - remind_ms) / 60000, 0) END,
			project_id, space, created_ms, updated_ms, CASE WHEN remind_ms IS NULL THEN 'none' ELSE notify END, '', 'none', '', 1, '', 'task', id, completed, ` + remindedFor + `
		FROM tasks WHERE (due_ms IS NOT NULL OR remind_ms IS NOT NULL) ` + where + `
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title, description = excluded.description, start_ms = excluded.start_ms, end_ms = excluded.end_ms,
			remind_before = excluded.remind_before, project_id = excluded.project_id, space = excluded.space, updated_ms = excluded.updated_ms,
			notify = excluded.notify, completed = excluded.completed, source = 'task', task_id = excluded.task_id,
			reminded_for_ms = CASE WHEN events.start_ms = excluded.start_ms AND COALESCE(events.remind_before, -1) = COALESCE(excluded.remind_before, -1)
				AND NOT (events.completed = 1 AND excluded.completed = 0) THEN events.reminded_for_ms ELSE NULL END`
}

// syncTaskSchedule brings one to-do's schedule item in step with it: written
// when it has a time, removed when it has none. tz is the zone the item is
// shown in; "" means UTC.
func syncTaskSchedule(ctx context.Context, tx *sql.Tx, taskID, tz string) error {
	if tz == "" {
		tz = "UTC"
	}
	if _, err := tx.ExecContext(ctx, taskScheduleUpsert(`AND id = ?2`, false), tz, taskID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM events WHERE source = 'task' AND task_id = ?
		AND NOT EXISTS (SELECT 1 FROM tasks WHERE id = events.task_id AND (due_ms IS NOT NULL OR remind_ms IS NOT NULL))`, taskID)
	return err
}

// unscheduledTasks are the open to-dos with no time at all, oldest first.
func (s *Store) unscheduledTasks(ctx context.Context, userID, space string) ([]Task, error) {
	q := `SELECT ` + taskCols + ` FROM tasks t WHERE user_id = ? AND completed = 0 AND due_ms IS NULL AND remind_ms IS NULL`
	args := []any{userID}
	q, args = spaceClause(q, args, "space", space)
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY priority DESC, created_ms, id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Task{}
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
		RRule: t.RRule, Occurrence: t.Occurrence + 1, Notify: t.Notify,
	}
	if t.Remind != nil {
		r := due.Add(t.Remind.Sub(*t.Due))
		n.Remind = &r
	}
	if err := insertTask(ctx, tx, n, s.location().String()); err != nil {
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
	progress, worked_minutes, work_sessions, last_worked_ms, rrule, occurrence, next_task_id, notify`

func scanTask(r scanner) (Task, error) {
	var t Task
	var due, doneAt, remind, worked sql.NullInt64
	var done int
	var created, updated int64
	if err := r.Scan(&t.ID, &t.UserID, &t.Title, &t.Notes, &due, &t.Priority, &done, &doneAt, &remind, &t.ProjectID, &t.Space, &created, &updated,
		&t.Progress, &t.WorkedMinutes, &t.WorkSessions, &worked, &t.RRule, &t.Occurrence, &t.NextID, &t.Notify); err != nil {
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
		// Told at its due time, it stays told at its due time.
		if p.Remind == nil && t.Remind != nil && t.Due != nil && t.Remind.Equal(*t.Due) && *p.Due != nil {
			r := **p.Due
			t.Remind = &r
		}
		t.Due = *p.Due
	}
	if p.Priority != nil {
		t.Priority = *p.Priority
	}
	if p.Remind != nil {
		t.Remind = *p.Remind
		// Without a notify of its own, a remind time keeps its old meaning:
		// setting one makes it a reminder, clearing it silences the to-do.
		if p.Notify == nil {
			if t.Remind == nil {
				t.Notify = NotifyNone
			} else if t.Notify == NotifyNone {
				t.Notify = NotifyReminder
			}
		}
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
	if p.Notify != nil {
		t.Notify = *p.Notify
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
			remind_ms = ?, project_id = ?, space = ?, updated_ms = ?, rrule = ?, occurrence = ?, notify = ?`
		if rearm {
			q += `, reminded = 0`
		}
		q += ` WHERE id = ? AND user_id = ?`
		if _, err := tx.ExecContext(ctx, q, t.Title, t.Notes, nullMS(t.Due), t.Priority, b2i(t.Done), nullMS(t.DoneAt),
			nullMS(t.Remind), t.ProjectID, t.Space, ms(t.Updated), t.RRule, t.Occurrence, t.Notify, id, userID); err != nil {
			return err
		}
		if err := writeTaskTags(ctx, tx, t); err != nil {
			return err
		}
		if err := syncTaskSchedule(ctx, tx, t.ID, s.location().String()); err != nil {
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
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ? AND user_id = ?`, id, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM events WHERE id = ? AND user_id = ? AND source = 'task'`, id, userID)
		return err
	})
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
