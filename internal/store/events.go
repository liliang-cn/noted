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
	"github.com/liliang-cn/noted/internal/recur"
)

type Event struct {
	ID           string
	UserID       string
	Title        string
	Description  string
	Location     string
	Start        time.Time
	End          time.Time
	AllDay       bool
	TimeZone     string
	RRule        string
	RemindBefore *int // minutes; nil: no reminder
	ProjectID    string
	Space        string
	Created      time.Time
	Updated      time.Time
}

type EventPatch struct {
	Title, Description, Location *string
	Start, End                   *time.Time
	AllDay                       *bool
	TimeZone, RRule              *string
	RemindBefore                 **int // non-nil: set; *RemindBefore == nil clears
	ProjectID                    *string
	Space                        *string
}

type Occurrence struct {
	Event Event
	Start time.Time
	End   time.Time
}

const (
	maxRemindMinutes = 28 * 24 * 60
	maxRangeDays     = 366
)

// Normalize validates the event and fills defaults (zone, end time).
func (e *Event) Normalize() error {
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" {
		return invalid("title is empty")
	}
	if utf8.RuneCountInString(e.Title) > maxTitle {
		return invalid("title is longer than %d characters", maxTitle)
	}
	if e.Start.IsZero() {
		return invalid("start_time is required")
	}
	if e.TimeZone == "" {
		e.TimeZone = "UTC"
	}
	loc, err := time.LoadLocation(e.TimeZone)
	if err != nil {
		return invalid("unknown time_zone %q", e.TimeZone)
	}
	if e.AllDay {
		y, m, d := e.Start.In(loc).Date()
		e.Start = time.Date(y, m, d, 0, 0, 0, 0, loc).UTC()
		if e.End.IsZero() || !e.End.After(e.Start) {
			e.End = time.Date(y, m, d+1, 0, 0, 0, 0, loc).UTC()
		}
	} else if e.End.IsZero() {
		e.End = e.Start.Add(time.Hour)
	}
	if e.End.Before(e.Start) {
		return invalid("end_time is before start_time")
	}
	if e.RRule != "" {
		if _, err := recur.Parse(e.RRule); err != nil {
			return invalid("%v", err)
		}
	}
	if e.RemindBefore != nil && (*e.RemindBefore < 0 || *e.RemindBefore > maxRemindMinutes) {
		return invalid("remind_before_minutes must be between 0 and %d", maxRemindMinutes)
	}
	return nil
}

func (e Event) location() *time.Location {
	loc, err := time.LoadLocation(e.TimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Occurrences returns the instances of e that overlap [from, to).
func (e Event) Occurrences(from, to time.Time, limit int) []Occurrence {
	dur := e.End.Sub(e.Start)
	if e.RRule == "" {
		if (e.Start.Before(to) && e.End.After(from)) || (dur == 0 && !e.Start.Before(from) && e.Start.Before(to)) {
			return []Occurrence{{Event: e, Start: e.Start, End: e.End}}
		}
		return nil
	}
	rule, err := recur.Parse(e.RRule)
	if err != nil {
		return nil // validated on write; a corrupt row just yields nothing
	}
	var out []Occurrence
	for _, s := range rule.Occurrences(e.Start.In(e.location()), from.Add(-dur), to, limit) {
		end := s.Add(dur)
		if end.After(from) || dur == 0 {
			out = append(out, Occurrence{Event: e, Start: s.UTC(), End: end.UTC()})
		}
	}
	return out
}

func (s *Store) CreateEvent(ctx context.Context, e Event) (Event, error) {
	if err := e.Normalize(); err != nil {
		return Event{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	e.ID, e.Created, e.Updated = uuid.NewString(), now, now
	if err := s.checkProject(ctx, e.UserID, e.ProjectID); err != nil {
		return Event{}, err
	}
	var err error
	if e.Space, err = s.resolveSpace(ctx, e.UserID, e.Space, e.ProjectID); err != nil {
		return Event{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO events(id, user_id, title, description, location, start_ms, end_ms, all_day, tz, rrule,
		                   remind_before, project_id, space, created_ms, updated_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.UserID, e.Title, e.Description, e.Location, ms(e.Start), ms(e.End), b2i(e.AllDay), e.TimeZone, e.RRule,
		nullInt(e.RemindBefore), e.ProjectID, e.Space, ms(e.Created), ms(e.Updated))
	return e, err
}

func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

const eventCols = `id, user_id, title, description, location, start_ms, end_ms, all_day, tz, rrule, remind_before, project_id, space, created_ms, updated_ms`

func scanEvent(r scanner) (Event, error) {
	var e Event
	var start, end, created, updated int64
	var allDay int
	var remind sql.NullInt64
	if err := r.Scan(&e.ID, &e.UserID, &e.Title, &e.Description, &e.Location, &start, &end, &allDay,
		&e.TimeZone, &e.RRule, &remind, &e.ProjectID, &e.Space, &created, &updated); err != nil {
		return Event{}, err
	}
	e.Start, e.End, e.AllDay = fromMS(start), fromMS(end), allDay != 0
	e.Created, e.Updated = fromMS(created), fromMS(updated)
	if remind.Valid {
		v := int(remind.Int64)
		e.RemindBefore = &v
	}
	return e, nil
}

func (s *Store) GetEvent(ctx context.Context, userID, id string) (Event, error) {
	e, err := scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return e, err
}

func (s *Store) UpdateEvent(ctx context.Context, userID, id string, p EventPatch) (Event, error) {
	e, err := s.GetEvent(ctx, userID, id)
	if err != nil {
		return Event{}, err
	}
	oldStart := e.Start
	dur := e.End.Sub(e.Start)
	if p.Title != nil {
		e.Title = *p.Title
	}
	if p.Description != nil {
		e.Description = *p.Description
	}
	if p.Location != nil {
		e.Location = *p.Location
	}
	if p.AllDay != nil {
		e.AllDay = *p.AllDay
	}
	if p.TimeZone != nil {
		e.TimeZone = *p.TimeZone
	}
	if p.RRule != nil {
		e.RRule = *p.RRule
	}
	if p.RemindBefore != nil {
		e.RemindBefore = *p.RemindBefore
	}
	if p.ProjectID != nil {
		e.ProjectID = *p.ProjectID
		if err := s.checkProject(ctx, userID, e.ProjectID); err != nil {
			return Event{}, err
		}
	}
	if p.Space != nil {
		if *p.Space != SpaceWork && *p.Space != SpaceLife {
			return Event{}, invalid("space must be work or life")
		}
		e.Space = *p.Space
	}
	if p.Start != nil {
		e.Start = *p.Start
		if p.End == nil { // moving the start keeps the duration
			e.End = e.Start.Add(dur)
		}
	}
	if p.End != nil {
		e.End = *p.End
	}
	if err := e.Normalize(); err != nil {
		return Event{}, err
	}
	e.Updated = time.Now().UTC().Truncate(time.Millisecond)
	// Rescheduling re-arms the reminder.
	_, err = s.db.ExecContext(ctx, `
		UPDATE events SET title = ?, description = ?, location = ?, start_ms = ?, end_ms = ?, all_day = ?, tz = ?,
		                  rrule = ?, remind_before = ?, project_id = ?, space = ?, updated_ms = ?,
		                  reminded_for_ms = CASE WHEN start_ms = ? THEN reminded_for_ms ELSE NULL END
		WHERE id = ? AND user_id = ?`,
		e.Title, e.Description, e.Location, ms(e.Start), ms(e.End), b2i(e.AllDay), e.TimeZone,
		e.RRule, nullInt(e.RemindBefore), e.ProjectID, e.Space, ms(e.Updated), ms(oldStart), id, userID)
	return e, err
}

func (s *Store) DeleteEvent(ctx context.Context, userID, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM events WHERE id = ? AND user_id = ?`, id, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		// A goal scheduled by this event keeps going, just unscheduled.
		_, err = tx.ExecContext(ctx, `UPDATE goals SET event_id = '' WHERE event_id = ? AND user_id = ?`, id, userID)
		return err
	})
}

// ListEvents returns every occurrence overlapping [from, to), ordered by start.
func (s *Store) ListEvents(ctx context.Context, userID string, from, to time.Time, space string) ([]Occurrence, error) {
	if err := checkSpaceFilter(space); err != nil {
		return nil, err
	}
	if !to.After(from) {
		return nil, invalid("to must be after from")
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		return nil, invalid("range is longer than %d days", maxRangeDays)
	}
	// Recurring events can start long before the window, so they are always
	// candidates; one-offs are pre-filtered in SQL.
	q := `SELECT ` + eventCols + ` FROM events
		WHERE user_id = ? AND start_ms < ? AND (rrule <> '' OR end_ms > ? OR (end_ms = start_ms AND start_ms >= ?))`
	args := []any{userID, ms(to), ms(from), ms(from)}
	q, args = spaceClause(q, args, "space", space)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Occurrence
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e.Occurrences(from, to, 1000)...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}
