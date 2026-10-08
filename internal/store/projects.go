package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Project is a finite piece of work, such as a trip, made of tasks, events
// and notes that point at it through their ProjectID.
type Project struct {
	ID       string
	UserID   string
	Title    string
	Notes    string
	Pinned   bool
	Archived bool
	Start    *time.Time // e.g. the departure date
	Due      *time.Time // deadline or end date
	Space    string
	Created  time.Time
	Updated  time.Time
}

type ProjectPatch struct {
	Title, Notes     *string
	Pinned, Archived *bool
	Start, Due       **time.Time // non-nil: set; *Start == nil clears
	Space            *string
}

type ProjectFilter struct {
	IncludeArchived bool
	PinnedOnly      bool
	Space           string
}

type ProjectProgress struct {
	TasksTotal     int
	TasksDone      int
	TasksOverdue   int
	Percent        float64
	EventsUpcoming int
	NotesCount     int
	Next           *time.Time // earliest open task due date or upcoming event
	NextTitle      string
	DaysLeft       *int // days from today to Start (or Due); negative once past
}

type ProjectView struct {
	Project  Project
	Progress ProjectProgress
}

type ProjectDetail struct {
	View   ProjectView
	Tasks  []Task
	Events []Event
	Notes  []Note
}

// checkProject accepts an empty id (no project) or one the user owns.
func (s *Store) checkProject(ctx context.Context, userID, id string) error {
	if id == "" {
		return nil
	}
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ? AND user_id = ?`, id, userID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return invalid("unknown project %q", id)
	}
	return err
}

func (p *Project) normalize() error {
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		return invalid("title is empty")
	}
	if utf8.RuneCountInString(p.Title) > maxTitle {
		return invalid("title is longer than %d characters", maxTitle)
	}
	if p.Start != nil && p.Due != nil && p.Due.Before(*p.Start) {
		return invalid("due_time is before start_time")
	}
	return nil
}

func (s *Store) CreateProject(ctx context.Context, p Project) (Project, error) {
	if err := p.normalize(); err != nil {
		return Project{}, err
	}
	var err error
	if p.Space, err = s.resolveSpace(ctx, p.UserID, p.Space, ""); err != nil {
		return Project{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	p.ID, p.Created, p.Updated = uuid.NewString(), now, now
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO projects(id, user_id, title, notes, pinned, archived, start_ms, due_ms, space, created_ms, updated_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.UserID, p.Title, p.Notes, b2i(p.Pinned), b2i(p.Archived), nullMS(p.Start), nullMS(p.Due), p.Space, ms(p.Created), ms(p.Updated))
	return p, err
}

const projectCols = `id, user_id, title, notes, pinned, archived, start_ms, due_ms, space, created_ms, updated_ms`

func scanProject(r scanner) (Project, error) {
	var p Project
	var pinned, archived int
	var start, due sql.NullInt64
	var created, updated int64
	if err := r.Scan(&p.ID, &p.UserID, &p.Title, &p.Notes, &pinned, &archived, &start, &due, &p.Space, &created, &updated); err != nil {
		return Project{}, err
	}
	p.Pinned, p.Archived = pinned != 0, archived != 0
	p.Start, p.Due = ptrTime(start), ptrTime(due)
	p.Created, p.Updated = fromMS(created), fromMS(updated)
	return p, nil
}

func (s *Store) getProject(ctx context.Context, userID, id string) (Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *Store) UpdateProject(ctx context.Context, userID, id string, patch ProjectPatch, now time.Time, loc *time.Location) (ProjectView, error) {
	p, err := s.getProject(ctx, userID, id)
	if err != nil {
		return ProjectView{}, err
	}
	if patch.Title != nil {
		p.Title = *patch.Title
	}
	if patch.Notes != nil {
		p.Notes = *patch.Notes
	}
	if patch.Pinned != nil {
		p.Pinned = *patch.Pinned
	}
	if patch.Archived != nil {
		p.Archived = *patch.Archived
	}
	if patch.Start != nil {
		p.Start = *patch.Start
	}
	if patch.Due != nil {
		p.Due = *patch.Due
	}
	if patch.Space != nil {
		if *patch.Space != SpaceWork && *patch.Space != SpaceLife {
			return ProjectView{}, invalid("space must be work or life")
		}
		p.Space = *patch.Space
	}
	if err := p.normalize(); err != nil {
		return ProjectView{}, err
	}
	p.Updated = time.Now().UTC().Truncate(time.Millisecond)
	_, err = s.db.ExecContext(ctx, `
		UPDATE projects SET title = ?, notes = ?, pinned = ?, archived = ?, start_ms = ?, due_ms = ?, space = ?, updated_ms = ?
		WHERE id = ? AND user_id = ?`,
		p.Title, p.Notes, b2i(p.Pinned), b2i(p.Archived), nullMS(p.Start), nullMS(p.Due), p.Space, ms(p.Updated), id, userID)
	if err != nil {
		return ProjectView{}, err
	}
	return s.viewOf(ctx, p, now, loc)
}

// DeleteProject removes the project. Its tasks, events and notes are kept and
// leave the project, unless deleteItems asks for them to go too.
func (s *Store) DeleteProject(ctx context.Context, userID, id string, deleteItems bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ? AND user_id = ?`, id, userID).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if deleteItems {
			for _, q := range []string{
				`DELETE FROM notes_fts WHERE note_id IN (SELECT id FROM notes WHERE project_id = ? AND user_id = ?)`,
				`DELETE FROM notes WHERE project_id = ? AND user_id = ?`,
				`DELETE FROM tasks WHERE project_id = ? AND user_id = ?`,
				`DELETE FROM events WHERE project_id = ? AND user_id = ?`,
			} {
				if _, err := tx.ExecContext(ctx, q, id, userID); err != nil {
					return err
				}
			}
		} else {
			for _, table := range []string{"notes", "tasks", "events"} {
				if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET project_id = '' WHERE project_id = ? AND user_id = ?`, id, userID); err != nil {
					return err
				}
			}
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ? AND user_id = ?`, id, userID)
		return err
	})
}

// ListProjects returns pinned projects first, then the nearest date, then the newest.
func (s *Store) ListProjects(ctx context.Context, userID string, f ProjectFilter, now time.Time, loc *time.Location) ([]ProjectView, error) {
	if err := checkSpaceFilter(f.Space); err != nil {
		return nil, err
	}
	q := `SELECT ` + projectCols + ` FROM projects WHERE user_id = ?`
	args := []any{userID}
	if !f.IncludeArchived {
		q += ` AND archived = 0`
	}
	if f.PinnedOnly {
		q += ` AND pinned = 1`
	}
	q, args = spaceClause(q, args, "space", f.Space)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var ps []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		ps = append(ps, p)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ProjectView, 0, len(ps))
	for _, p := range ps {
		v, err := s.viewOf(ctx, p, now, loc)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Project, out[j].Project
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		da, db := a.anchor(), b.anchor()
		if (da == nil) != (db == nil) {
			return da != nil // dated projects before undated ones
		}
		if da != nil && !da.Equal(*db) {
			return da.Before(*db)
		}
		return a.Created.After(b.Created)
	})
	return out, nil
}

// anchor is the date a project counts down to: its start, else its due date.
func (p Project) anchor() *time.Time {
	if p.Start != nil {
		return p.Start
	}
	return p.Due
}

func (s *Store) GetProject(ctx context.Context, userID, id string, now time.Time, loc *time.Location) (ProjectDetail, error) {
	p, err := s.getProject(ctx, userID, id)
	if err != nil {
		return ProjectDetail{}, err
	}
	v, err := s.viewOf(ctx, p, now, loc)
	if err != nil {
		return ProjectDetail{}, err
	}
	tasks, err := s.ListTasks(ctx, userID, TaskFilter{State: "all", ProjectID: id, Limit: 500})
	if err != nil {
		return ProjectDetail{}, err
	}
	events, err := s.projectEvents(ctx, userID, id)
	if err != nil {
		return ProjectDetail{}, err
	}
	notes, err := s.ListNotes(ctx, userID, NoteFilter{ProjectID: id, Limit: 200})
	if err != nil {
		return ProjectDetail{}, err
	}
	return ProjectDetail{View: v, Tasks: tasks, Events: events, Notes: notes}, nil
}

func (s *Store) projectEvents(ctx context.Context, userID, projectID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventCols+` FROM events WHERE user_id = ? AND project_id = ? ORDER BY start_ms`, userID, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) viewOf(ctx context.Context, p Project, now time.Time, loc *time.Location) (ProjectView, error) {
	pr, err := s.projectProgress(ctx, p, now, loc)
	return ProjectView{Project: p, Progress: pr}, err
}

func (s *Store) projectProgress(ctx context.Context, p Project, now time.Time, loc *time.Location) (ProjectProgress, error) {
	var pr ProjectProgress
	var done, overdue sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), SUM(completed),
		       SUM(CASE WHEN completed = 0 AND due_ms IS NOT NULL AND due_ms < ? THEN 1 ELSE 0 END)
		FROM tasks WHERE project_id = ? AND user_id = ?`, ms(now), p.ID, p.UserID).Scan(&pr.TasksTotal, &done, &overdue)
	if err != nil {
		return pr, err
	}
	pr.TasksDone, pr.TasksOverdue = int(done.Int64), int(overdue.Int64)
	if pr.TasksTotal > 0 {
		pr.Percent = float64(pr.TasksDone) / float64(pr.TasksTotal)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes WHERE project_id = ? AND user_id = ? AND archived = 0`, p.ID, p.UserID).Scan(&pr.NotesCount); err != nil {
		return pr, err
	}

	// The next thing that needs attention: an open task's due date or an upcoming event.
	var dueMS sql.NullInt64
	var title string
	err = s.db.QueryRowContext(ctx, `SELECT due_ms, title FROM tasks
		WHERE project_id = ? AND user_id = ? AND completed = 0 AND due_ms IS NOT NULL ORDER BY due_ms LIMIT 1`,
		p.ID, p.UserID).Scan(&dueMS, &title)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return pr, err
	}
	if dueMS.Valid {
		t := fromMS(dueMS.Int64)
		pr.Next, pr.NextTitle = &t, title
	}
	events, err := s.projectEvents(ctx, p.UserID, p.ID)
	if err != nil {
		return pr, err
	}
	for _, e := range events {
		occ := e.Occurrences(now, now.AddDate(1, 0, 0), 1)
		if len(occ) == 0 {
			continue
		}
		pr.EventsUpcoming++
		if pr.Next == nil || occ[0].Start.Before(*pr.Next) {
			t := occ[0].Start
			pr.Next, pr.NextTitle = &t, e.Title
		}
	}

	if a := p.anchor(); a != nil {
		d := daysBetween(now.In(loc), a.In(loc))
		pr.DaysLeft = &d
	}
	return pr, nil
}

// daysBetween counts calendar days from a to b in their own zone (b later is positive).
func daysBetween(a, b time.Time) int {
	ay, am, ad := a.Date()
	by, bm, bd := b.In(a.Location()).Date()
	da := time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)
	db := time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC)
	return int(db.Sub(da).Hours() / 24)
}

// ViewOf attaches computed progress to a project.
func (s *Store) ViewOf(ctx context.Context, p Project, now time.Time, loc *time.Location) (ProjectView, error) {
	return s.viewOf(ctx, p, now, loc)
}
