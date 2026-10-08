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

// A Goal is a recurring target: Target units per Period. Frequency goals
// ("3 times a week") and amount goals ("140 minutes a week") are one thing;
// every check-in carries an amount, progress is the sum over the current
// period.
type Goal struct {
	ID       string
	UserID   string
	Title    string
	Notes    string
	Period   string // "day", "week" (Monday start) or "month"
	Target   float64
	Unit     string
	TimeZone string
	EventID  string // optional recurring event that schedules the sessions
	Space    string
	// CounterTarget, when above 0, adds a running total that is not tied to a period:
	// 40 lessons, say, counted in CounterUnit. Check-ins feed it through their Tally.
	CounterUnit   string
	CounterTarget float64
	Archived      bool
	Milestones    []Milestone
	Created       time.Time
	Updated       time.Time
}

type Milestone struct {
	ID     string
	Title  string
	Done   bool
	DoneAt *time.Time
}

type CheckIn struct {
	ID     string
	GoalID string
	UserID string
	Time   time.Time
	Date   string // YYYY-MM-DD in the goal's zone
	Amount float64
	Tally  float64 // added to the goal\'s running total, if it has one
	Note   string
}

type GoalPatch struct {
	Title, Notes, Period *string
	Target               *float64
	Unit, TimeZone       *string
	EventID              *string
	Space                *string
	CounterUnit          *string
	CounterTarget        *float64
	Archived             *bool
	Milestones           *[]Milestone
}

type DayTotal struct {
	Date   string
	Amount float64
}

type GoalProgress struct {
	PeriodStart, PeriodEnd time.Time
	Done, Target           float64
	Remaining, Percent     float64
	Achieved, Behind       bool
	Streak                 int
	Days                   []DayTotal // every day of the current period
	Recent                 []DayTotal // the last 14 days, oldest first
	MilestonesDone         int
	MilestonesTotal        int
	MilestonePercent       float64
	CounterDone            float64 // the running total so far
	CounterPercent         float64 // CounterDone / CounterTarget; 0 when there is no counter
}

type GoalView struct {
	Goal     Goal
	Progress GoalProgress
}

const (
	maxMilestones = 50
	maxAmount     = 1e6
)

func (g *Goal) normalize() error {
	g.Title = strings.TrimSpace(g.Title)
	if g.Title == "" {
		return invalid("title is empty")
	}
	if utf8.RuneCountInString(g.Title) > maxTitle {
		return invalid("title is longer than %d characters", maxTitle)
	}
	switch g.Period {
	case "day", "week", "month":
	default:
		return invalid("period must be day, week or month")
	}
	if !(g.Target > 0) || g.Target > maxAmount {
		return invalid("target must be greater than 0")
	}
	g.Unit = strings.TrimSpace(g.Unit)
	if g.Unit == "" {
		g.Unit = "times"
	}
	if utf8.RuneCountInString(g.Unit) > 20 {
		return invalid("unit is longer than 20 characters")
	}
	if g.TimeZone == "" {
		g.TimeZone = "UTC"
	}
	if _, err := time.LoadLocation(g.TimeZone); err != nil {
		return invalid("unknown time_zone %q", g.TimeZone)
	}
	g.CounterUnit = strings.TrimSpace(g.CounterUnit)
	if g.CounterTarget < 0 || g.CounterTarget > maxAmount {
		return invalid("counter_target must be between 0 and %g", maxAmount)
	}
	if g.CounterTarget == 0 {
		g.CounterUnit = ""
	} else if g.CounterUnit == "" || utf8.RuneCountInString(g.CounterUnit) > 20 {
		return invalid("counter_unit is required with a counter_target, and at most 20 characters")
	}
	if len(g.Milestones) > maxMilestones {
		return invalid("at most %d milestones", maxMilestones)
	}
	for i := range g.Milestones {
		m := &g.Milestones[i]
		m.Title = strings.TrimSpace(m.Title)
		if m.Title == "" || utf8.RuneCountInString(m.Title) > 200 {
			return invalid("a milestone title must be 1-200 characters")
		}
	}
	return nil
}

func (g Goal) location() *time.Location {
	loc, err := time.LoadLocation(g.TimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (s *Store) checkEvent(ctx context.Context, userID, id string) error {
	if id == "" {
		return nil
	}
	if _, err := s.GetEvent(ctx, userID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return invalid("unknown event %q", id)
		}
		return err
	}
	return nil
}

func (s *Store) CreateGoal(ctx context.Context, g Goal) (Goal, error) {
	if err := g.normalize(); err != nil {
		return Goal{}, err
	}
	if err := s.checkEvent(ctx, g.UserID, g.EventID); err != nil {
		return Goal{}, err
	}
	var err error
	if g.Space, err = s.resolveSpace(ctx, g.UserID, g.Space, ""); err != nil {
		return Goal{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	g.ID, g.Created, g.Updated = uuid.NewString(), now, now
	for i := range g.Milestones {
		g.Milestones[i].ID, g.Milestones[i].Done, g.Milestones[i].DoneAt = uuid.NewString(), false, nil
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO goals(id, user_id, title, notes, period, target, unit, tz, event_id, space, counter_unit, counter_target, archived, created_ms, updated_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			g.ID, g.UserID, g.Title, g.Notes, g.Period, g.Target, g.Unit, g.TimeZone, g.EventID, g.Space, g.CounterUnit, g.CounterTarget, b2i(g.Archived),
			ms(g.Created), ms(g.Updated)); err != nil {
			return err
		}
		return writeMilestones(ctx, tx, g)
	})
	return g, err
}

func writeMilestones(ctx context.Context, tx *sql.Tx, g Goal) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM goal_milestones WHERE goal_id = ?`, g.ID); err != nil {
		return err
	}
	for i, m := range g.Milestones {
		if _, err := tx.ExecContext(ctx, `INSERT INTO goal_milestones(id, goal_id, position, title, done_ms) VALUES (?, ?, ?, ?, ?)`,
			m.ID, g.ID, i, m.Title, nullMS(m.DoneAt)); err != nil {
			return err
		}
	}
	return nil
}

const goalCols = `id, user_id, title, notes, period, target, unit, tz, event_id, space, counter_unit, counter_target, archived, created_ms, updated_ms`

func scanGoal(r scanner) (Goal, error) {
	var g Goal
	var archived int
	var created, updated int64
	if err := r.Scan(&g.ID, &g.UserID, &g.Title, &g.Notes, &g.Period, &g.Target, &g.Unit, &g.TimeZone, &g.EventID,
		&g.Space, &g.CounterUnit, &g.CounterTarget, &archived, &created, &updated); err != nil {
		return Goal{}, err
	}
	g.Archived = archived != 0
	g.Created, g.Updated = fromMS(created), fromMS(updated)
	return g, nil
}

func (s *Store) loadMilestones(ctx context.Context, goals []Goal) error {
	if len(goals) == 0 {
		return nil
	}
	idx := map[string]int{}
	args := make([]any, len(goals))
	for i, g := range goals {
		idx[g.ID] = i
		args[i] = g.ID
	}
	rows, err := s.db.QueryContext(ctx, `SELECT goal_id, id, title, done_ms FROM goal_milestones
		WHERE goal_id IN (?`+strings.Repeat(",?", len(goals)-1)+`) ORDER BY goal_id, position`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var gid string
		var m Milestone
		var done sql.NullInt64
		if err := rows.Scan(&gid, &m.ID, &m.Title, &done); err != nil {
			return err
		}
		m.DoneAt = ptrTime(done)
		m.Done = done.Valid
		goals[idx[gid]].Milestones = append(goals[idx[gid]].Milestones, m)
	}
	return rows.Err()
}

func (s *Store) GetGoal(ctx context.Context, userID, id string) (Goal, error) {
	g, err := scanGoal(s.db.QueryRowContext(ctx, `SELECT `+goalCols+` FROM goals WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Goal{}, ErrNotFound
	}
	if err != nil {
		return Goal{}, err
	}
	gs := []Goal{g}
	if err := s.loadMilestones(ctx, gs); err != nil {
		return Goal{}, err
	}
	return gs[0], nil
}

func (s *Store) UpdateGoal(ctx context.Context, userID, id string, p GoalPatch) (Goal, error) {
	g, err := s.GetGoal(ctx, userID, id)
	if err != nil {
		return Goal{}, err
	}
	oldTZ := g.TimeZone
	if p.Title != nil {
		g.Title = *p.Title
	}
	if p.Notes != nil {
		g.Notes = *p.Notes
	}
	if p.Period != nil {
		g.Period = *p.Period
	}
	if p.Target != nil {
		g.Target = *p.Target
	}
	if p.Unit != nil {
		g.Unit = *p.Unit
	}
	if p.TimeZone != nil {
		g.TimeZone = *p.TimeZone
	}
	if p.EventID != nil {
		g.EventID = *p.EventID
		if err := s.checkEvent(ctx, userID, g.EventID); err != nil {
			return Goal{}, err
		}
	}
	if p.Archived != nil {
		g.Archived = *p.Archived
	}
	if p.Space != nil {
		if *p.Space != SpaceWork && *p.Space != SpaceLife {
			return Goal{}, invalid("space must be work or life")
		}
		g.Space = *p.Space
	}
	if p.CounterUnit != nil {
		g.CounterUnit = *p.CounterUnit
	}
	if p.CounterTarget != nil {
		g.CounterTarget = *p.CounterTarget
	}
	if p.Milestones != nil {
		existing := map[string]Milestone{}
		for _, m := range g.Milestones {
			existing[m.ID] = m
		}
		next := make([]Milestone, 0, len(*p.Milestones))
		for _, m := range *p.Milestones {
			if old, ok := existing[m.ID]; ok && m.ID != "" {
				old.Title = m.Title // keeps its id and done state
				next = append(next, old)
			} else {
				next = append(next, Milestone{ID: uuid.NewString(), Title: m.Title})
			}
		}
		g.Milestones = next
	}
	if err := g.normalize(); err != nil {
		return Goal{}, err
	}
	g.Updated = time.Now().UTC().Truncate(time.Millisecond)
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE goals SET title = ?, notes = ?, period = ?, target = ?, unit = ?, tz = ?, event_id = ?, space = ?, counter_unit = ?, counter_target = ?, archived = ?, updated_ms = ?
			WHERE id = ? AND user_id = ?`,
			g.Title, g.Notes, g.Period, g.Target, g.Unit, g.TimeZone, g.EventID, g.Space, g.CounterUnit, g.CounterTarget, b2i(g.Archived), ms(g.Updated), id, userID); err != nil {
			return err
		}
		if p.Milestones != nil {
			if err := writeMilestones(ctx, tx, g); err != nil {
				return err
			}
		}
		if g.TimeZone != oldTZ {
			return recomputeDates(ctx, tx, g)
		}
		return nil
	})
	return g, err
}

// recomputeDates re-buckets every check-in into days of the goal's new zone.
func recomputeDates(ctx context.Context, tx *sql.Tx, g Goal) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, time_ms FROM goal_checkins WHERE goal_id = ?`, g.ID)
	if err != nil {
		return err
	}
	type row struct {
		id string
		ms int64
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.ms); err != nil {
			_ = rows.Close()
			return err
		}
		all = append(all, r)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	loc := g.location()
	for _, r := range all {
		if _, err := tx.ExecContext(ctx, `UPDATE goal_checkins SET local_date = ? WHERE id = ?`,
			fromMS(r.ms).In(loc).Format("2006-01-02"), r.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteGoal(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM goals WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListGoals returns goals with progress, those behind pace first, then by title.
func (s *Store) ListGoals(ctx context.Context, userID string, includeArchived bool, now time.Time, space string) ([]GoalView, error) {
	if err := checkSpaceFilter(space); err != nil {
		return nil, err
	}
	q := `SELECT ` + goalCols + ` FROM goals WHERE user_id = ?`
	args := []any{userID}
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q, args = spaceClause(q, args, "space", space)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var gs []Goal
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		gs = append(gs, g)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.loadMilestones(ctx, gs); err != nil {
		return nil, err
	}
	out := make([]GoalView, 0, len(gs))
	for _, g := range gs {
		v, err := s.ViewGoal(ctx, g, now)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Progress.Behind != out[j].Progress.Behind {
			return out[i].Progress.Behind
		}
		return out[i].Goal.Title < out[j].Goal.Title
	})
	return out, nil
}

// ViewGoal attaches computed progress to a goal.
func (s *Store) ViewGoal(ctx context.Context, g Goal, now time.Time) (GoalView, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT local_date, SUM(amount) FROM goal_checkins WHERE goal_id = ? GROUP BY local_date`, g.ID)
	if err != nil {
		return GoalView{}, err
	}
	defer func() { _ = rows.Close() }()
	daily := map[string]float64{}
	for rows.Next() {
		var d string
		var a float64
		if err := rows.Scan(&d, &a); err != nil {
			return GoalView{}, err
		}
		daily[d] = a
	}
	if err := rows.Err(); err != nil {
		return GoalView{}, err
	}
	p := ComputeProgress(g, daily, now)
	if g.CounterTarget > 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(tally), 0) FROM goal_checkins WHERE goal_id = ?`, g.ID).Scan(&p.CounterDone); err != nil {
			return GoalView{}, err
		}
		p.CounterPercent = p.CounterDone / g.CounterTarget
	}
	return GoalView{Goal: g, Progress: p}, nil
}

// RecordCheckIn adds progress to a goal: amount toward the period target, tally
// toward the running total. With neither given it counts as one unit of amount;
// with only a tally it counts toward the total alone (a lesson finished, no
// minutes logged).
func (s *Store) RecordCheckIn(ctx context.Context, userID, goalID string, amount, tally float64, at time.Time, note string, now time.Time) (CheckIn, error) {
	g, err := s.GetGoal(ctx, userID, goalID)
	if err != nil {
		return CheckIn{}, err
	}
	if amount == 0 && tally == 0 {
		amount = 1
	}
	if amount < 0 || amount > maxAmount {
		return CheckIn{}, invalid("amount must be greater than 0")
	}
	if tally < 0 || tally > maxAmount {
		return CheckIn{}, invalid("count must not be negative")
	}
	if tally > 0 && g.CounterTarget == 0 {
		return CheckIn{}, invalid("this goal has no counter; set counter_target first")
	}
	if at.IsZero() {
		at = now
	}
	if at.After(now.Add(5 * time.Minute)) {
		return CheckIn{}, invalid("a check-in cannot be in the future")
	}
	if at.Before(now.AddDate(-5, 0, 0)) {
		return CheckIn{}, invalid("a check-in cannot be more than 5 years old")
	}
	if utf8.RuneCountInString(note) > 500 {
		return CheckIn{}, invalid("note is longer than 500 characters")
	}
	c := CheckIn{ID: uuid.NewString(), GoalID: goalID, UserID: userID, Time: at.UTC().Truncate(time.Millisecond),
		Date: at.In(g.location()).Format("2006-01-02"), Amount: amount, Tally: tally, Note: note}
	_, err = s.db.ExecContext(ctx, `INSERT INTO goal_checkins(id, goal_id, user_id, time_ms, local_date, amount, tally, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, c.GoalID, c.UserID, ms(c.Time), c.Date, c.Amount, c.Tally, c.Note)
	return c, err
}

// DeleteCheckIn removes a check-in and returns the goal it belonged to.
func (s *Store) DeleteCheckIn(ctx context.Context, userID, id string) (string, error) {
	var goalID string
	err := s.db.QueryRowContext(ctx, `SELECT goal_id FROM goal_checkins WHERE id = ? AND user_id = ?`, id, userID).Scan(&goalID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM goal_checkins WHERE id = ? AND user_id = ?`, id, userID)
	return goalID, err
}

func (s *Store) ListCheckIns(ctx context.Context, userID, goalID string, from, to *time.Time, limit int) ([]CheckIn, error) {
	if _, err := s.GetGoal(ctx, userID, goalID); err != nil {
		return nil, err
	}
	q := `SELECT id, goal_id, user_id, time_ms, local_date, amount, tally, note FROM goal_checkins WHERE goal_id = ? AND user_id = ?`
	args := []any{goalID, userID}
	if from != nil {
		q += ` AND time_ms >= ?`
		args = append(args, ms(*from))
	}
	if to != nil {
		q += ` AND time_ms < ?`
		args = append(args, ms(*to))
	}
	q += ` ORDER BY time_ms DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CheckIn
	for rows.Next() {
		var c CheckIn
		var t int64
		if err := rows.Scan(&c.ID, &c.GoalID, &c.UserID, &t, &c.Date, &c.Amount, &c.Tally, &c.Note); err != nil {
			return nil, err
		}
		c.Time = fromMS(t)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) SetMilestoneDone(ctx context.Context, userID, goalID, milestoneID string, done bool) (Goal, error) {
	if _, err := s.GetGoal(ctx, userID, goalID); err != nil {
		return Goal{}, err
	}
	var doneMS any
	if done {
		doneMS = ms(time.Now())
	}
	res, err := s.db.ExecContext(ctx, `UPDATE goal_milestones SET done_ms = ? WHERE id = ? AND goal_id = ?`, doneMS, milestoneID, goalID)
	if err != nil {
		return Goal{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Goal{}, ErrNotFound
	}
	return s.GetGoal(ctx, userID, goalID)
}

// ---- progress (pure) ----

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// periodBounds returns the period containing t, in t's zone.
func periodBounds(period string, t time.Time) (time.Time, time.Time) {
	ds := dayStart(t)
	switch period {
	case "week":
		start := ds.AddDate(0, 0, -((int(ds.Weekday()) + 6) % 7)) // Monday
		return start, start.AddDate(0, 0, 7)
	case "month":
		start := time.Date(ds.Year(), ds.Month(), 1, 0, 0, 0, 0, ds.Location())
		return start, start.AddDate(0, 1, 0)
	default:
		return ds, ds.AddDate(0, 0, 1)
	}
}

func sumDays(daily map[string]float64, from, to time.Time) float64 {
	var total float64
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		total += daily[d.Format("2006-01-02")]
	}
	return total
}

const eps = 1e-9

// ComputeProgress derives a goal's standing at now from its per-day totals.
func ComputeProgress(g Goal, daily map[string]float64, now time.Time) GoalProgress {
	loc := g.location()
	t := now.In(loc)
	start, end := periodBounds(g.Period, t)
	p := GoalProgress{PeriodStart: start.UTC(), PeriodEnd: end.UTC(), Target: g.Target}

	p.Done = sumDays(daily, start, end)
	p.Remaining = math.Max(g.Target-p.Done, 0)
	p.Percent = p.Done / g.Target
	p.Achieved = p.Done >= g.Target-eps

	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		p.Days = append(p.Days, DayTotal{Date: k, Amount: daily[k]})
	}
	today := dayStart(t)
	for i := 13; i >= 0; i-- {
		k := today.AddDate(0, 0, -i).Format("2006-01-02")
		p.Recent = append(p.Recent, DayTotal{Date: k, Amount: daily[k]})
	}

	// Streak: this period if achieved, then every achieved period before it,
	// back to the period the goal was created in.
	createdStart, _ := periodBounds(g.Period, g.Created.In(loc))
	if p.Achieved {
		p.Streak = 1
	}
	cur := start
	for i := 0; i < 520; i++ {
		prevStart, prevEnd := periodBounds(g.Period, cur.AddDate(0, 0, -1))
		if prevStart.Before(createdStart) || sumDays(daily, prevStart, prevEnd) < g.Target-eps {
			break
		}
		p.Streak++
		cur = prevStart
	}

	// Behind: below the pace the target implies for the days before today.
	elapsed := math.Round(today.Sub(start).Hours() / 24)
	length := math.Round(end.Sub(start).Hours() / 24)
	expected := g.Target * elapsed / length
	p.Behind = !p.Achieved && p.Done+eps < math.Floor(expected+eps)

	p.MilestonesTotal = len(g.Milestones)
	for _, m := range g.Milestones {
		if m.Done {
			p.MilestonesDone++
		}
	}
	if p.MilestonesTotal > 0 {
		p.MilestonePercent = float64(p.MilestonesDone) / float64(p.MilestonesTotal)
	}
	return p
}
