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

// An Objective is a larger goal such as losing weight. It is made of dimensions
// (ordinary goals: swimming 2 times a week, sugar-free days, ...) and, when the
// result can be measured, a metric with readings over time (weight in kg).
type Objective struct {
	ID           string
	UserID       string
	Title        string
	Notes        string
	Space        string
	Start, Due   *time.Time
	MetricName   string // "体重"
	MetricUnit   string // "kg"; empty: the objective has no metric
	MetricStart  float64
	MetricTarget float64
	Archived     bool
	ProjectID    string      // the project this objective belongs to; empty: none
	Milestones   []Milestone // the steps on the way, in order
	Created      time.Time
	Updated      time.Time
}

func (o Objective) HasMetric() bool { return o.MetricUnit != "" }

type ObjectivePatch struct {
	Title, Notes, Space       *string
	Start, Due                **time.Time // non-nil: set; *Start == nil clears
	MetricName, MetricUnit    *string
	MetricStart, MetricTarget *float64
	Archived                  *bool
	ProjectID                 *string
	// Replaces the list: entries carrying an existing id keep their done state.
	Milestones *[]Milestone
}

type Measurement struct {
	ID          string
	ObjectiveID string
	UserID      string
	Time        time.Time
	Value       float64
	Note        string
}

type ObjectiveProgress struct {
	GoalsTotal, GoalsAchieved int
	GoalsPercent              float64 // mean of each dimension's progress this period, each capped at 1
	Behind                    bool    // some dimension is behind its pace
	HasMetric, HasReading     bool
	MetricCurrent             float64
	MetricChange              float64 // current minus start
	MetricPercent             float64 // how far from start to target, 0..1
	LastReading               *time.Time
	// The headline: the metric when there is one with a reading, else
	// MilestonePercent when there are milestones, else GoalsPercent.
	Percent  float64
	DaysLeft *int
	// Metric progress is not behind the share of time already used and no
	// undone milestone is past its due time; true when unknown.
	OnTrack                         bool
	MilestonesDone, MilestonesTotal int
	MilestonePercent                float64    // MilestonesDone / MilestonesTotal; 0 when there are none
	MilestonesOverdue               int        // undone and past their due time
	NextMilestone                   *Milestone // the first undone one, in list order
}

type ObjectiveView struct {
	Objective Objective
	Progress  ObjectiveProgress
	Goals     []GoalView
}

const maxMetric = 1e9

func (o *Objective) normalize() error {
	o.Title = strings.TrimSpace(o.Title)
	if o.Title == "" {
		return invalid("title is empty")
	}
	if utf8.RuneCountInString(o.Title) > maxTitle {
		return invalid("title is longer than %d characters", maxTitle)
	}
	o.MetricName, o.MetricUnit = strings.TrimSpace(o.MetricName), strings.TrimSpace(o.MetricUnit)
	if o.MetricUnit == "" {
		o.MetricName, o.MetricStart, o.MetricTarget = "", 0, 0
	} else {
		if utf8.RuneCountInString(o.MetricUnit) > 20 || utf8.RuneCountInString(o.MetricName) > 50 {
			return invalid("metric_unit is at most 20 characters and metric_name at most 50")
		}
		for _, v := range []float64{o.MetricStart, o.MetricTarget} {
			if math.IsNaN(v) || math.Abs(v) > maxMetric {
				return invalid("metric values must be numbers within ±%g", maxMetric)
			}
		}
		if o.MetricStart == o.MetricTarget {
			return invalid("metric_start and metric_target must differ")
		}
	}
	if o.Start != nil && o.Due != nil && o.Due.Before(*o.Start) {
		return invalid("due_time is before start_time")
	}
	return normalizeMilestones(o.Milestones)
}

func (s *Store) checkObjective(ctx context.Context, userID, id string) error {
	if id == "" {
		return nil
	}
	if _, err := s.GetObjective(ctx, userID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return invalid("unknown objective %q", id)
		}
		return err
	}
	return nil
}

const objectiveCols = `id, user_id, title, notes, space, start_ms, due_ms, metric_name, metric_unit, metric_start, metric_target, archived, project_id, created_ms, updated_ms`

func scanObjective(r scanner) (Objective, error) {
	var o Objective
	var start, due sql.NullInt64
	var archived int
	var created, updated int64
	if err := r.Scan(&o.ID, &o.UserID, &o.Title, &o.Notes, &o.Space, &start, &due, &o.MetricName, &o.MetricUnit,
		&o.MetricStart, &o.MetricTarget, &archived, &o.ProjectID, &created, &updated); err != nil {
		return Objective{}, err
	}
	o.Start, o.Due = ptrTime(start), ptrTime(due)
	o.Archived = archived != 0
	o.Created, o.Updated = fromMS(created), fromMS(updated)
	return o, nil
}

func (s *Store) CreateObjective(ctx context.Context, o Objective) (Objective, error) {
	if err := o.normalize(); err != nil {
		return Objective{}, err
	}
	if err := s.checkProject(ctx, o.UserID, o.ProjectID); err != nil {
		return Objective{}, err
	}
	var err error
	if o.Space, err = s.resolveSpace(ctx, o.UserID, o.Space, ""); err != nil {
		return Objective{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	o.ID, o.Created, o.Updated = uuid.NewString(), now, now
	newMilestones(o.Milestones)
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO objectives(`+objectiveCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			o.ID, o.UserID, o.Title, o.Notes, o.Space, nullMS(o.Start), nullMS(o.Due), o.MetricName, o.MetricUnit, o.MetricStart, o.MetricTarget,
			b2i(o.Archived), o.ProjectID, ms(o.Created), ms(o.Updated)); err != nil {
			return err
		}
		return objectiveMilestones.write(ctx, tx, o.ID, o.Milestones)
	})
	return o, err
}

func (s *Store) GetObjective(ctx context.Context, userID, id string) (Objective, error) {
	o, err := scanObjective(s.db.QueryRowContext(ctx, `SELECT `+objectiveCols+` FROM objectives WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Objective{}, ErrNotFound
	}
	if err != nil {
		return Objective{}, err
	}
	byObjective, err := objectiveMilestones.load(ctx, s.db, []string{o.ID})
	if err != nil {
		return Objective{}, err
	}
	o.Milestones = byObjective[o.ID]
	return o, nil
}

func (s *Store) UpdateObjective(ctx context.Context, userID, id string, p ObjectivePatch) (Objective, error) {
	o, err := s.GetObjective(ctx, userID, id)
	if err != nil {
		return Objective{}, err
	}
	if p.Title != nil {
		o.Title = *p.Title
	}
	if p.Notes != nil {
		o.Notes = *p.Notes
	}
	if p.Space != nil {
		if *p.Space != SpaceWork && *p.Space != SpaceLife {
			return Objective{}, invalid("space must be work or life")
		}
		o.Space = *p.Space
	}
	if p.Start != nil {
		o.Start = *p.Start
	}
	if p.Due != nil {
		o.Due = *p.Due
	}
	if p.MetricName != nil {
		o.MetricName = *p.MetricName
	}
	if p.MetricUnit != nil {
		o.MetricUnit = *p.MetricUnit
	}
	if p.MetricStart != nil {
		o.MetricStart = *p.MetricStart
	}
	if p.MetricTarget != nil {
		o.MetricTarget = *p.MetricTarget
	}
	if p.Archived != nil {
		o.Archived = *p.Archived
	}
	if p.ProjectID != nil {
		if err := s.checkProject(ctx, userID, *p.ProjectID); err != nil {
			return Objective{}, err
		}
		o.ProjectID = *p.ProjectID
	}
	if p.Milestones != nil {
		o.Milestones = mergeMilestones(o.Milestones, *p.Milestones)
	}
	if err := o.normalize(); err != nil {
		return Objective{}, err
	}
	o.Updated = time.Now().UTC().Truncate(time.Millisecond)
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE objectives SET title = ?, notes = ?, space = ?, start_ms = ?, due_ms = ?, metric_name = ?, metric_unit = ?,
			metric_start = ?, metric_target = ?, archived = ?, project_id = ?, updated_ms = ? WHERE id = ? AND user_id = ?`,
			o.Title, o.Notes, o.Space, nullMS(o.Start), nullMS(o.Due), o.MetricName, o.MetricUnit, o.MetricStart, o.MetricTarget, b2i(o.Archived), o.ProjectID, ms(o.Updated), id, userID); err != nil {
			return err
		}
		if p.Milestones != nil {
			return objectiveMilestones.write(ctx, tx, o.ID, o.Milestones)
		}
		return nil
	})
	return o, err
}

// SetObjectiveMilestoneDone ticks (or unticks) one of the objective's milestones.
func (s *Store) SetObjectiveMilestoneDone(ctx context.Context, userID, objectiveID, milestoneID string, done bool) error {
	if _, err := s.GetObjective(ctx, userID, objectiveID); err != nil {
		return err
	}
	if err := objectiveMilestones.setDone(ctx, s.db, objectiveID, milestoneID, done, time.Now()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE objectives SET updated_ms = ? WHERE id = ? AND user_id = ?`, ms(time.Now()), objectiveID, userID)
	return err
}

// DeleteObjective removes the objective and its readings. Its dimensions are
// kept and become stand-alone goals.
func (s *Store) DeleteObjective(ctx context.Context, userID, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM objectives WHERE id = ? AND user_id = ?`, id, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM objective_milestones WHERE objective_id = ?`, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE goals SET objective_id = '' WHERE objective_id = ? AND user_id = ?`, id, userID)
		return err
	})
}

// ListObjectives returns objectives with their dimensions and progress; those
// behind pace first, then newest.
func (s *Store) ListObjectives(ctx context.Context, userID string, includeArchived bool, now time.Time, space string) ([]ObjectiveView, error) {
	if err := checkSpaceFilter(space); err != nil {
		return nil, err
	}
	q := `SELECT ` + objectiveCols + ` FROM objectives WHERE user_id = ?`
	args := []any{userID}
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q, args = spaceClause(q, args, "space", space)
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_ms DESC, id`, args...)
	if err != nil {
		return nil, err
	}
	var os []Objective
	for rows.Next() {
		o, err := scanObjective(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		os = append(os, o)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]string, len(os))
	for i, o := range os {
		ids[i] = o.ID
	}
	milestones, err := objectiveMilestones.load(ctx, s.db, ids)
	if err != nil {
		return nil, err
	}
	for i := range os {
		os[i].Milestones = milestones[os[i].ID]
	}
	gs, err := s.ListGoals(ctx, userID, false, now, "")
	if err != nil {
		return nil, err
	}
	byObjective := map[string][]GoalView{}
	for _, g := range gs {
		if g.Goal.ObjectiveID != "" {
			byObjective[g.Goal.ObjectiveID] = append(byObjective[g.Goal.ObjectiveID], g)
		}
	}
	out := make([]ObjectiveView, 0, len(os))
	for _, o := range os {
		v, err := s.viewObjective(ctx, o, byObjective[o.ID], now)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := out[i].Progress.Behind || !out[i].Progress.OnTrack, out[j].Progress.Behind || !out[j].Progress.OnTrack
		return bi && !bj
	})
	return out, nil
}

// ViewObjective returns one objective with its dimensions and progress.
func (s *Store) ViewObjective(ctx context.Context, userID, id string, now time.Time) (ObjectiveView, error) {
	o, err := s.GetObjective(ctx, userID, id)
	if err != nil {
		return ObjectiveView{}, err
	}
	gs, err := s.ListGoals(ctx, userID, false, now, "")
	if err != nil {
		return ObjectiveView{}, err
	}
	var mine []GoalView
	for _, g := range gs {
		if g.Goal.ObjectiveID == id {
			mine = append(mine, g)
		}
	}
	return s.viewObjective(ctx, o, mine, now)
}

func (s *Store) viewObjective(ctx context.Context, o Objective, goals []GoalView, now time.Time) (ObjectiveView, error) {
	p := ObjectiveProgress{GoalsTotal: len(goals), HasMetric: o.HasMetric(), OnTrack: true}
	sum := 0.0
	for _, g := range goals {
		sum += math.Min(1, g.Progress.Percent)
		if g.Progress.Achieved {
			p.GoalsAchieved++
		}
		if g.Progress.Behind {
			p.Behind = true
		}
	}
	if len(goals) > 0 {
		p.GoalsPercent = sum / float64(len(goals))
	}
	p.MilestonesTotal = len(o.Milestones)
	for i, m := range o.Milestones {
		if m.Done {
			p.MilestonesDone++
			continue
		}
		if p.NextMilestone == nil {
			p.NextMilestone = &o.Milestones[i]
		}
		if m.Overdue(now) {
			p.MilestonesOverdue++
		}
	}
	if p.MilestonesTotal > 0 {
		p.MilestonePercent = float64(p.MilestonesDone) / float64(p.MilestonesTotal)
	}
	p.Percent = p.GoalsPercent
	if p.MilestonesTotal > 0 {
		p.Percent = p.MilestonePercent
	}
	if o.HasMetric() {
		last, err := s.lastMeasurement(ctx, o.UserID, o.ID)
		if err != nil {
			return ObjectiveView{}, err
		}
		if last != nil {
			p.HasReading = true
			p.MetricCurrent = last.Value
			p.MetricChange = last.Value - o.MetricStart
			p.MetricPercent = math.Max(0, math.Min(1, (last.Value-o.MetricStart)/(o.MetricTarget-o.MetricStart)))
			t := last.Time
			p.LastReading = &t
			p.Percent = p.MetricPercent
		}
	}
	if o.Due != nil {
		d := int(math.Ceil(o.Due.Sub(now).Hours() / 24))
		p.DaysLeft = &d
		if o.Start != nil && p.HasReading && o.Due.After(*o.Start) {
			elapsed := math.Max(0, math.Min(1, now.Sub(*o.Start).Seconds()/o.Due.Sub(*o.Start).Seconds()))
			p.OnTrack = p.MetricPercent >= elapsed-0.1
		}
	}
	if p.MilestonesOverdue > 0 {
		p.Behind, p.OnTrack = true, false
	}
	sort.SliceStable(goals, func(i, j int) bool { return goals[i].Goal.Title < goals[j].Goal.Title })
	return ObjectiveView{Objective: o, Progress: p, Goals: goals}, nil
}

func (s *Store) lastMeasurement(ctx context.Context, userID, objectiveID string) (*Measurement, error) {
	var m Measurement
	var t int64
	err := s.db.QueryRowContext(ctx, `SELECT id, objective_id, user_id, time_ms, value, note FROM objective_measurements
		WHERE objective_id = ? AND user_id = ? ORDER BY time_ms DESC, id DESC LIMIT 1`, objectiveID, userID).
		Scan(&m.ID, &m.ObjectiveID, &m.UserID, &t, &m.Value, &m.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.Time = fromMS(t)
	return &m, nil
}

// RecordMeasurement stores a reading of the objective's metric. A zero at means now.
func (s *Store) RecordMeasurement(ctx context.Context, userID, objectiveID string, at time.Time, value float64, note string) (Measurement, error) {
	o, err := s.GetObjective(ctx, userID, objectiveID)
	if err != nil {
		return Measurement{}, err
	}
	if !o.HasMetric() {
		return Measurement{}, invalid("this objective has no metric to record")
	}
	if math.IsNaN(value) || math.Abs(value) > maxMetric {
		return Measurement{}, invalid("value must be a number within ±%g", maxMetric)
	}
	if utf8.RuneCountInString(note) > 500 {
		return Measurement{}, invalid("note is longer than 500 characters")
	}
	if at.IsZero() {
		at = time.Now()
	}
	m := Measurement{ID: uuid.NewString(), ObjectiveID: objectiveID, UserID: userID, Time: at.UTC().Truncate(time.Millisecond), Value: value, Note: strings.TrimSpace(note)}
	_, err = s.db.ExecContext(ctx, `INSERT INTO objective_measurements(id, objective_id, user_id, time_ms, value, note) VALUES (?, ?, ?, ?, ?, ?)`,
		m.ID, m.ObjectiveID, m.UserID, ms(m.Time), m.Value, m.Note)
	return m, err
}

func (s *Store) DeleteMeasurement(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM objective_measurements WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListMeasurements returns readings, oldest first, optionally within [from, to).
func (s *Store) ListMeasurements(ctx context.Context, userID, objectiveID string, from, to *time.Time, limit int) ([]Measurement, error) {
	if _, err := s.GetObjective(ctx, userID, objectiveID); err != nil {
		return nil, err
	}
	q := `SELECT id, objective_id, user_id, time_ms, value, note FROM objective_measurements WHERE objective_id = ? AND user_id = ?`
	args := []any{objectiveID, userID}
	if from != nil {
		q += ` AND time_ms >= ?`
		args = append(args, ms(*from))
	}
	if to != nil {
		q += ` AND time_ms < ?`
		args = append(args, ms(*to))
	}
	// The newest `limit` readings, returned oldest first.
	q = `SELECT * FROM (` + q + ` ORDER BY time_ms DESC, id DESC LIMIT ?) ORDER BY time_ms, id`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Measurement
	for rows.Next() {
		var m Measurement
		var t int64
		if err := rows.Scan(&m.ID, &m.ObjectiveID, &m.UserID, &t, &m.Value, &m.Note); err != nil {
			return nil, err
		}
		m.Time = fromMS(t)
		out = append(out, m)
	}
	return out, rows.Err()
}
