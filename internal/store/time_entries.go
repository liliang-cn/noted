package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// TimeEntry is time spent on something: 游泳 30 分钟. It may belong to a project
// and may count toward a goal, through a check-in it keeps in step with itself.
// A running timer is an entry without an end.
type TimeEntry struct {
	ID        string
	UserID    string
	Title     string // the activity
	Minutes   int    // 0 while running
	Start     time.Time
	End       *time.Time // nil while running
	Note      string
	ProjectID string
	GoalID    string
	Space     string
	CheckInID string // the goal check-in this entry made; read-only
	// Timed is true when Start and End are what the person gave or a timer
	// measured, false when only minutes were given and Start was defaulted.
	// Set by the store: callers do not choose it.
	Timed   bool
	Created time.Time
	Updated time.Time
}

func (e TimeEntry) Running() bool { return e.End == nil }

type TimeEntryPatch struct {
	Title, Note *string
	Minutes     *int
	Start       *time.Time // moving the start keeps the minutes
	// Without Minutes: the minutes are the time from the start, or, on an
	// entry that is not timed, the minutes stay and the start moves.
	End       *time.Time
	ProjectID *string
	GoalID    *string
	Space     *string
}

type TimeFilter struct {
	From, To  *time.Time // on the start: [From, To)
	ProjectID string
	GoalID    string
	Title     string // activity names containing this, ignoring case
	Space     string
	Limit     int
	Offset    int
}

// Ways to group a time summary.
const (
	TimeByTitle   = "title"
	TimeByProject = "project"
	TimeByGoal    = "goal"
	TimeByDay     = "day"
)

type TimeRow struct {
	Key     string
	Label   string
	Minutes int
	Count   int
}

type TimeSummary struct {
	Rows    []TimeRow
	Minutes int
	Count   int
}

const (
	maxEntryMinutes = 24 * 60
	maxEntryNote    = 2000
	// timeSkew is how far into the future a time may be, for clocks that differ a little.
	timeSkew = 5 * time.Minute
)

// TitleKey folds an activity name for grouping: 英语学习, " Swimming " and
// "swimming" are told apart only by what they say.
func TitleKey(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

// GoalAmount is what minutes of an activity add to a goal counted in unit:
// the minutes for a goal in minutes, hours for one in hours, else one session.
func GoalAmount(unit string, minutes int) float64 {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "分钟", "分", "minutes", "minute", "mins", "min", "m":
		return float64(minutes)
	case "小时", "钟头", "hours", "hour", "hrs", "hr", "h":
		return math.Round(float64(minutes)/60*100) / 100
	}
	return 1
}

// roundMinutes turns a duration into whole minutes, at least one.
func roundMinutes(d time.Duration) int {
	return max(int(math.Round(d.Minutes())), 1)
}

func (e *TimeEntry) normalize(now time.Time) error {
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" {
		return invalid("title is empty: say what the time was spent on")
	}
	if utf8.RuneCountInString(e.Title) > maxTitle {
		return invalid("title is longer than %d characters", maxTitle)
	}
	e.Note = strings.TrimSpace(e.Note)
	if utf8.RuneCountInString(e.Note) > maxEntryNote {
		return invalid("note is longer than %d characters", maxEntryNote)
	}
	if e.Start.After(now.Add(timeSkew)) {
		return invalid("start_time is in the future")
	}
	if e.End != nil {
		if e.Minutes < 1 || e.Minutes > maxEntryMinutes {
			return invalid("minutes must be 1-%d", maxEntryMinutes)
		}
		end := e.Start.Add(time.Duration(e.Minutes) * time.Minute)
		e.End = &end
	} else {
		e.Minutes = 0
	}
	return nil
}

// checkGoal accepts an empty id (no goal) or one the user owns.
func (s *Store) checkGoal(ctx context.Context, userID, id string) error {
	if id == "" {
		return nil
	}
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM goals WHERE id = ? AND user_id = ?`, id, userID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return invalid("unknown goal %q", id)
	}
	return err
}

// entrySpace picks the space for a new entry: the one given, else the
// project's, else the goal's, else life.
func (s *Store) entrySpace(ctx context.Context, userID, space, projectID, goalID string) (string, error) {
	if space != "" || projectID != "" || goalID == "" {
		return s.resolveSpace(ctx, userID, space, projectID)
	}
	var gs string
	err := s.db.QueryRowContext(ctx, `SELECT space FROM goals WHERE id = ? AND user_id = ?`, goalID, userID).Scan(&gs)
	if errors.Is(err, sql.ErrNoRows) {
		return SpaceLife, nil
	}
	return gs, err
}

// CreateTimeEntry records time spent. With no Minutes but a Start and an End,
// the minutes are the time between them. A zero Start is the End minus the
// minutes, or, with no End either, now minus the minutes: then the entry is not
// Timed, as only its day and length are known.
func (s *Store) CreateTimeEntry(ctx context.Context, e TimeEntry, now time.Time) (TimeEntry, error) {
	if e.Minutes == 0 && e.End != nil && !e.Start.IsZero() {
		if e.End.Before(e.Start) {
			return TimeEntry{}, invalid("end_time is before start_time")
		}
		e.Minutes = roundMinutes(e.End.Sub(e.Start))
	}
	if e.Minutes < 1 || e.Minutes > maxEntryMinutes {
		return TimeEntry{}, invalid("minutes must be 1-%d", maxEntryMinutes)
	}
	e.Timed = !e.Start.IsZero() || e.End != nil
	if e.Start.IsZero() {
		end := now
		if e.End != nil {
			end = *e.End
		}
		e.Start = end.Add(-time.Duration(e.Minutes) * time.Minute)
	}
	done := e.Start
	e.End = &done // finished: normalize sets the real end from the minutes
	return s.insertEntry(ctx, e, now)
}

// StartTimer starts a running entry. A user has one timer at most; starting
// another while one runs is refused with ErrPrecondition, naming it.
func (s *Store) StartTimer(ctx context.Context, e TimeEntry, now time.Time) (TimeEntry, error) {
	if e.Start.IsZero() {
		e.Start = now
	}
	e.End, e.Minutes, e.Timed = nil, 0, true
	if cur, ok, err := s.RunningTimer(ctx, e.UserID); err != nil {
		return TimeEntry{}, err
	} else if ok {
		return TimeEntry{}, runningErr(cur)
	}
	out, err := s.insertEntry(ctx, e, now)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") { // started elsewhere in between
		if cur, ok, _ := s.RunningTimer(ctx, e.UserID); ok {
			return TimeEntry{}, runningErr(cur)
		}
	}
	return out, err
}

func runningErr(cur TimeEntry) error {
	return precondition("a timer is already running: %q (id %s) since %s; stop it first",
		cur.Title, cur.ID, cur.Start.Format(time.RFC3339))
}

func (s *Store) insertEntry(ctx context.Context, e TimeEntry, now time.Time) (TimeEntry, error) {
	e.Start = e.Start.UTC().Truncate(time.Millisecond)
	if err := e.normalize(now); err != nil {
		return TimeEntry{}, err
	}
	if err := s.checkProject(ctx, e.UserID, e.ProjectID); err != nil {
		return TimeEntry{}, err
	}
	if err := s.checkGoal(ctx, e.UserID, e.GoalID); err != nil {
		return TimeEntry{}, err
	}
	var err error
	if e.Space, err = s.entrySpace(ctx, e.UserID, e.Space, e.ProjectID, e.GoalID); err != nil {
		return TimeEntry{}, err
	}
	stamp := time.Now().UTC().Truncate(time.Millisecond)
	e.ID, e.Created, e.Updated = uuid.NewString(), stamp, stamp
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO time_entries(id, user_id, title, title_key, minutes, start_ms, end_ms, note, project_id, goal_id, space, timed, created_ms, updated_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.ID, e.UserID, e.Title, TitleKey(e.Title), e.Minutes, ms(e.Start), nullMS(e.End), e.Note, e.ProjectID, e.GoalID, e.Space,
			e.Timed, ms(e.Created), ms(e.Updated)); err != nil {
			return err
		}
		return syncCheckIn(ctx, tx, &e)
	})
	if err != nil {
		return TimeEntry{}, err
	}
	return e, nil
}

// syncCheckIn makes the entry's goal check-in match it: none while it runs or
// has no goal, otherwise one at its start adding GoalAmount of its minutes.
// The check-in keeps its id across edits.
func syncCheckIn(ctx context.Context, tx *sql.Tx, e *TimeEntry) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM goal_checkins WHERE time_entry_id = ? AND user_id = ?`, e.ID, e.UserID).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM goal_checkins WHERE time_entry_id = ? AND user_id = ?`, e.ID, e.UserID); err != nil {
		return err
	}
	e.CheckInID = ""
	if e.Running() || e.GoalID == "" {
		return nil
	}
	var unit, tz string
	err = tx.QueryRowContext(ctx, `SELECT unit, tz FROM goals WHERE id = ? AND user_id = ?`, e.GoalID, e.UserID).Scan(&unit, &tz)
	if errors.Is(err, sql.ErrNoRows) {
		return invalid("unknown goal %q", e.GoalID)
	}
	if err != nil {
		return err
	}
	if id == "" {
		id = uuid.NewString()
	}
	date := e.Start.In(Goal{TimeZone: tz}.location()).Format("2006-01-02")
	if _, err := tx.ExecContext(ctx, `INSERT INTO goal_checkins(id, goal_id, user_id, time_ms, local_date, amount, tally, note, time_entry_id)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)`, id, e.GoalID, e.UserID, ms(e.Start), date, GoalAmount(unit, e.Minutes), e.Title, e.ID); err != nil {
		return err
	}
	e.CheckInID = id
	return nil
}

const timeCols = `id, user_id, title, minutes, start_ms, end_ms, note, project_id, goal_id, space, timed, created_ms, updated_ms,
	COALESCE((SELECT c.id FROM goal_checkins c WHERE c.time_entry_id = time_entries.id), '')`

func scanEntry(r scanner) (TimeEntry, error) {
	var e TimeEntry
	var start, created, updated int64
	var end sql.NullInt64
	if err := r.Scan(&e.ID, &e.UserID, &e.Title, &e.Minutes, &start, &end, &e.Note, &e.ProjectID, &e.GoalID, &e.Space,
		&e.Timed, &created, &updated, &e.CheckInID); err != nil {
		return TimeEntry{}, err
	}
	e.Start, e.End = fromMS(start), ptrTime(end)
	e.Created, e.Updated = fromMS(created), fromMS(updated)
	return e, nil
}

func (s *Store) GetTimeEntry(ctx context.Context, userID, id string) (TimeEntry, error) {
	e, err := scanEntry(s.db.QueryRowContext(ctx, `SELECT `+timeCols+` FROM time_entries WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return TimeEntry{}, ErrNotFound
	}
	return e, err
}

// RunningTimer returns the user's running timer, if there is one.
func (s *Store) RunningTimer(ctx context.Context, userID string) (TimeEntry, bool, error) {
	e, err := scanEntry(s.db.QueryRowContext(ctx, `SELECT `+timeCols+` FROM time_entries WHERE user_id = ? AND end_ms IS NULL`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return TimeEntry{}, false, nil
	}
	return e, err == nil, err
}

// StopTimer ends the running timer at end (nil: now) and returns the finished
// entry. Under a minute counts as one; over 24 hours is refused, so a timer
// left running overnight is not logged as a day's work by accident.
func (s *Store) StopTimer(ctx context.Context, userID string, end *time.Time, now time.Time) (TimeEntry, error) {
	e, ok, err := s.RunningTimer(ctx, userID)
	if err != nil {
		return TimeEntry{}, err
	}
	if !ok {
		return TimeEntry{}, precondition("no timer is running")
	}
	at := now
	if end != nil {
		at = *end
	}
	if at.Before(e.Start) {
		return TimeEntry{}, invalid("end_time is before the timer started (%s)", e.Start.Format(time.RFC3339))
	}
	if at.After(now.Add(timeSkew)) {
		return TimeEntry{}, invalid("end_time is in the future")
	}
	minutes := roundMinutes(at.Sub(e.Start))
	if minutes > maxEntryMinutes {
		return TimeEntry{}, invalid("the timer has run for more than 24 hours; give the end_time it really ended at, or delete it")
	}
	e.Minutes, e.End = minutes, &at
	return s.saveEntry(ctx, e, now)
}

// saveEntry writes an existing entry back, with its check-in.
func (s *Store) saveEntry(ctx context.Context, e TimeEntry, now time.Time) (TimeEntry, error) {
	if err := e.normalize(now); err != nil {
		return TimeEntry{}, err
	}
	e.Updated = time.Now().UTC().Truncate(time.Millisecond)
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE time_entries SET title = ?, title_key = ?, minutes = ?, start_ms = ?, end_ms = ?, note = ?, project_id = ?, goal_id = ?, space = ?, timed = ?, updated_ms = ?
			WHERE id = ? AND user_id = ?`,
			e.Title, TitleKey(e.Title), e.Minutes, ms(e.Start), nullMS(e.End), e.Note, e.ProjectID, e.GoalID, e.Space, e.Timed, ms(e.Updated),
			e.ID, e.UserID); err != nil {
			return err
		}
		return syncCheckIn(ctx, tx, &e)
	})
	if err != nil {
		return TimeEntry{}, err
	}
	return e, nil
}

func (s *Store) UpdateTimeEntry(ctx context.Context, userID, id string, p TimeEntryPatch, now time.Time) (TimeEntry, error) {
	e, err := s.GetTimeEntry(ctx, userID, id)
	if err != nil {
		return TimeEntry{}, err
	}
	if p.Title != nil {
		e.Title = *p.Title
	}
	if p.Note != nil {
		e.Note = *p.Note
	}
	if e.Running() && (p.Minutes != nil || p.End != nil) {
		return TimeEntry{}, invalid("the timer is still running; stop it to set its minutes or end")
	}
	// The start of an entry that is not timed was made up; an end alone then
	// places the minutes it already has rather than measuring from that start.
	moveStart := p.End != nil && p.Start == nil && p.Minutes == nil && !e.Timed
	if p.Start != nil {
		e.Start = p.Start.UTC().Truncate(time.Millisecond)
	}
	switch {
	case p.Minutes != nil && p.End != nil:
		return TimeEntry{}, invalid("give minutes or end_time, not both")
	case p.Minutes != nil:
		e.Minutes = *p.Minutes // the start stays, the end moves
	case moveStart:
		e.Start = p.End.Add(-time.Duration(e.Minutes) * time.Minute).UTC().Truncate(time.Millisecond)
	case p.End != nil:
		if p.End.Before(e.Start) {
			return TimeEntry{}, invalid("end_time is before start_time")
		}
		e.Minutes = roundMinutes(p.End.Sub(e.Start))
	}
	if p.Start != nil || p.End != nil {
		e.Timed = true
	}
	if p.ProjectID != nil {
		if err := s.checkProject(ctx, userID, *p.ProjectID); err != nil {
			return TimeEntry{}, err
		}
		e.ProjectID = *p.ProjectID
	}
	if p.GoalID != nil {
		if err := s.checkGoal(ctx, userID, *p.GoalID); err != nil {
			return TimeEntry{}, err
		}
		e.GoalID = *p.GoalID
	}
	if p.Space != nil {
		if *p.Space != SpaceWork && *p.Space != SpaceLife {
			return TimeEntry{}, invalid("space must be work or life")
		}
		e.Space = *p.Space
	}
	return s.saveEntry(ctx, e, now)
}

// DeleteTimeEntry removes the entry and the goal check-in it made.
func (s *Store) DeleteTimeEntry(ctx context.Context, userID, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM time_entries WHERE id = ? AND user_id = ?`, id, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM goal_checkins WHERE time_entry_id = ? AND user_id = ?`, id, userID)
		return err
	})
}

// timeWhere builds the WHERE clause shared by listing and summarising.
func timeWhere(userID string, f TimeFilter) (string, []any, error) {
	if err := checkSpaceFilter(f.Space); err != nil {
		return "", nil, err
	}
	q := ` WHERE user_id = ?`
	args := []any{userID}
	if f.From != nil {
		q += ` AND start_ms >= ?`
		args = append(args, ms(*f.From))
	}
	if f.To != nil {
		q += ` AND start_ms < ?`
		args = append(args, ms(*f.To))
	}
	if f.ProjectID != "" {
		q += ` AND project_id = ?`
		args = append(args, f.ProjectID)
	}
	if f.GoalID != "" {
		q += ` AND goal_id = ?`
		args = append(args, f.GoalID)
	}
	if k := TitleKey(f.Title); k != "" {
		q += ` AND instr(title_key, ?) > 0`
		args = append(args, k)
	}
	q, args = spaceClause(q, args, "space", f.Space)
	return q, args, nil
}

// ListTimeEntries returns entries newest start first, a running timer included.
func (s *Store) ListTimeEntries(ctx context.Context, userID string, f TimeFilter) ([]TimeEntry, error) {
	where, args, err := timeWhere(userID, f)
	if err != nil {
		return nil, err
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+timeCols+` FROM time_entries`+where+` ORDER BY start_ms DESC, id LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []TimeEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SummarizeTime totals the finished entries that match f, grouped by
// activity, project, goal or day (the day of the start, in loc).
func (s *Store) SummarizeTime(ctx context.Context, userID string, f TimeFilter, by string, loc *time.Location) (TimeSummary, error) {
	switch by {
	case "":
		by = TimeByTitle
	case TimeByTitle, TimeByProject, TimeByGoal, TimeByDay:
	default:
		return TimeSummary{}, invalid("group_by must be title, project, goal or day")
	}
	if loc == nil {
		loc = time.UTC
	}
	where, args, err := timeWhere(userID, f)
	if err != nil {
		return TimeSummary{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT title, title_key, minutes, start_ms, project_id, goal_id FROM time_entries`+where+
		` AND end_ms IS NOT NULL ORDER BY start_ms`, args...)
	if err != nil {
		return TimeSummary{}, err
	}
	var sum TimeSummary
	idx := map[string]int{}
	for rows.Next() {
		var title, key, pid, gid string
		var minutes int
		var start int64
		if err := rows.Scan(&title, &key, &minutes, &start, &pid, &gid); err != nil {
			_ = rows.Close()
			return TimeSummary{}, err
		}
		label := ""
		switch by {
		case TimeByTitle:
			label = title // oldest first, so the latest spelling wins
		case TimeByProject:
			key = pid
		case TimeByGoal:
			key = gid
		case TimeByDay:
			key = fromMS(start).In(loc).Format("2006-01-02")
			label = key
		}
		i, ok := idx[key]
		if !ok {
			i = len(sum.Rows)
			idx[key] = i
			sum.Rows = append(sum.Rows, TimeRow{Key: key})
		}
		r := &sum.Rows[i]
		r.Minutes += minutes
		r.Count++
		if label != "" {
			r.Label = label
		}
		sum.Minutes += minutes
		sum.Count++
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return TimeSummary{}, err
	}
	if by == TimeByProject || by == TimeByGoal {
		table := map[string]string{TimeByProject: "projects", TimeByGoal: "goals"}[by]
		for i := range sum.Rows {
			r := &sum.Rows[i]
			if r.Key == "" {
				continue
			}
			err := s.db.QueryRowContext(ctx, `SELECT title FROM `+table+` WHERE id = ? AND user_id = ?`, r.Key, userID).Scan(&r.Label)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return TimeSummary{}, err
			}
		}
	}
	if by == TimeByDay {
		sort.Slice(sum.Rows, func(i, j int) bool { return sum.Rows[i].Key < sum.Rows[j].Key })
	} else {
		sort.SliceStable(sum.Rows, func(i, j int) bool {
			a, b := sum.Rows[i], sum.Rows[j]
			if a.Minutes != b.Minutes {
				return a.Minutes > b.Minutes
			}
			if a.Count != b.Count {
				return a.Count > b.Count
			}
			return a.Label < b.Label
		})
	}
	return sum, nil
}
