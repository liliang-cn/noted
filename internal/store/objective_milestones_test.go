package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func tp(t time.Time) *time.Time { return &t }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// The headline percent: the metric once it has a reading, else the milestones,
// else the dimensions.
func TestObjectivePercentRule(t *testing.T) {
	ctx := context.Background()
	s, u := newStore(t)
	now := time.Now()

	mk := func(o Objective) Objective {
		t.Helper()
		o.UserID = u
		o, err := s.CreateObjective(ctx, o)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	view := func(id string) ObjectiveProgress {
		t.Helper()
		v, err := s.ViewObjective(ctx, u, id, now)
		if err != nil {
			t.Fatal(err)
		}
		return v.Progress
	}
	steps := []Milestone{{Title: "A1"}, {Title: "A2"}, {Title: "B1"}, {Title: "exam"}}
	tick := func(o Objective, i int) {
		t.Helper()
		if err := s.SetObjectiveMilestoneDone(ctx, u, o.ID, o.Milestones[i].ID, true); err != nil {
			t.Fatal(err)
		}
	}

	// Goals only: the mean of the dimensions.
	plain := mk(Objective{Title: "fit"})
	g, err := s.CreateGoal(ctx, Goal{UserID: u, Title: "swim", Period: "week", Target: 2, ObjectiveID: plain.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordCheckIn(ctx, u, g.ID, 1, 0, now, "", now); err != nil {
		t.Fatal(err)
	}
	if p := view(plain.ID); !near(p.Percent, 0.5) || !near(p.GoalsPercent, 0.5) || p.MilestonesTotal != 0 || p.NextMilestone != nil {
		t.Fatalf("goals only: %+v", p)
	}

	// Milestones and dimensions: the milestones lead.
	ms := mk(Objective{Title: "German", Milestones: append([]Milestone(nil), steps...)})
	if _, err := s.CreateGoal(ctx, Goal{UserID: u, Title: "study", Period: "week", Target: 1, ObjectiveID: ms.ID}); err != nil {
		t.Fatal(err)
	}
	tick(ms, 0)
	p := view(ms.ID)
	if p.MilestonesDone != 1 || p.MilestonesTotal != 4 || !near(p.MilestonePercent, 0.25) || !near(p.Percent, 0.25) {
		t.Fatalf("milestones: %+v", p)
	}
	if p.NextMilestone == nil || p.NextMilestone.Title != "A2" {
		t.Fatalf("next milestone should be A2: %+v", p.NextMilestone)
	}

	// A metric without a reading does not lead yet; with one it does.
	m := mk(Objective{Title: "weight", MetricName: "weight", MetricUnit: "kg", MetricStart: 80, MetricTarget: 70,
		Milestones: append([]Milestone(nil), steps...)})
	tick(m, 0)
	tick(m, 1)
	if p := view(m.ID); !p.HasMetric || p.HasReading || !near(p.Percent, 0.5) {
		t.Fatalf("metric without a reading: %+v", p)
	}
	if _, err := s.RecordMeasurement(ctx, u, m.ID, now, 78, ""); err != nil {
		t.Fatal(err)
	}
	if p := view(m.ID); !p.HasReading || !near(p.MetricPercent, 0.2) || !near(p.Percent, 0.2) || !near(p.MilestonePercent, 0.5) {
		t.Fatalf("metric with a reading: %+v", p)
	}

	// All done: no next milestone.
	for i := range steps {
		tick(ms, i)
	}
	if p := view(ms.ID); p.NextMilestone != nil || !near(p.Percent, 1) {
		t.Fatalf("all done: %+v", p)
	}
}

func TestObjectiveOverdueMilestoneIsBehind(t *testing.T) {
	ctx := context.Background()
	s, u := newStore(t)
	now := time.Now()
	past, future := now.Add(-48*time.Hour).Truncate(time.Millisecond), now.Add(48*time.Hour).Truncate(time.Millisecond)
	o, err := s.CreateObjective(ctx, Objective{UserID: u, Title: "visa", Milestones: []Milestone{
		{Title: "forms", Due: &past, Criteria: "all signed"},
		{Title: "appointment", Due: &future},
	}})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.ViewObjective(ctx, u, o.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	p := v.Progress
	if !p.Behind || p.OnTrack || p.MilestonesOverdue != 1 {
		t.Fatalf("an undone milestone past its due time: %+v", p)
	}
	if p.NextMilestone == nil || p.NextMilestone.Title != "forms" || p.NextMilestone.Criteria != "all signed" || !p.NextMilestone.Due.Equal(past) {
		t.Fatalf("next = %+v", p.NextMilestone)
	}
	// Behind ones sort first in the list.
	if _, err := s.CreateObjective(ctx, Objective{UserID: u, Title: "calm"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListObjectives(ctx, u, false, now, "")
	if err != nil || len(list) != 2 || list[0].Objective.ID != o.ID || len(list[0].Objective.Milestones) != 2 {
		t.Fatalf("list: %v %+v", err, list)
	}

	if err := s.SetObjectiveMilestoneDone(ctx, u, o.ID, o.Milestones[0].ID, true); err != nil {
		t.Fatal(err)
	}
	v, _ = s.ViewObjective(ctx, u, o.ID, now)
	if v.Progress.Behind || !v.Progress.OnTrack || v.Progress.MilestonesOverdue != 0 {
		t.Fatalf("done, so no longer behind: %+v", v.Progress)
	}
	// The future one becomes overdue once its time passes.
	v, _ = s.ViewObjective(ctx, u, o.ID, now.Add(72*time.Hour))
	if !v.Progress.Behind || v.Progress.OnTrack {
		t.Fatalf("three days later: %+v", v.Progress)
	}
}

func TestObjectiveMilestonesReplaceKeepsDoneState(t *testing.T) {
	ctx := context.Background()
	s, u := newStore(t)
	due := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	o, err := s.CreateObjective(ctx, Objective{UserID: u, Title: "German", Milestones: []Milestone{
		{Title: " A1 ", Criteria: " passed "}, {Title: "A2"}, {Title: "B1"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if o.Milestones[0].Title != "A1" || o.Milestones[0].Criteria != "passed" {
		t.Fatalf("trimmed: %+v", o.Milestones[0])
	}
	a1, a2, b1 := o.Milestones[0], o.Milestones[1], o.Milestones[2]
	if err := s.SetObjectiveMilestoneDone(ctx, u, o.ID, a1.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetObjective(ctx, u, o.ID)
	doneAt := *got.Milestones[0].DoneAt

	// Reorder, rename A1, date B1, drop A2, add C1. A client's done flag is ignored.
	next := []Milestone{
		{ID: b1.ID, Title: "B1", Due: &due, Criteria: "certificate"},
		{ID: a1.ID, Title: "A1 (Goethe)"},
		{Title: "C1", Done: true},
	}
	if _, err := s.UpdateObjective(ctx, u, o.ID, ObjectivePatch{Milestones: &next}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetObjective(ctx, u, o.ID)
	if len(got.Milestones) != 3 {
		t.Fatalf("milestones = %+v", got.Milestones)
	}
	m0, m1, m2 := got.Milestones[0], got.Milestones[1], got.Milestones[2]
	if m0.ID != b1.ID || m0.Done || m0.Due == nil || !m0.Due.Equal(due) || m0.Criteria != "certificate" {
		t.Fatalf("B1 = %+v", m0)
	}
	if m1.ID != a1.ID || !m1.Done || m1.Title != "A1 (Goethe)" || !m1.DoneAt.Equal(doneAt) {
		t.Fatalf("A1 keeps its done state: %+v", m1)
	}
	if m2.ID == "" || m2.ID == a2.ID || m2.Done || m2.Title != "C1" {
		t.Fatalf("C1 is new and undone: %+v", m2)
	}
	if err := s.SetObjectiveMilestoneDone(ctx, u, o.ID, a2.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("A2 was removed: %v", err)
	}

	// Other fields leave the list alone; an empty list clears it.
	title := "Deutsch"
	if _, err := s.UpdateObjective(ctx, u, o.ID, ObjectivePatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetObjective(ctx, u, o.ID); len(got.Milestones) != 3 {
		t.Fatalf("a title change kept the milestones: %+v", got.Milestones)
	}
	empty := []Milestone{}
	if _, err := s.UpdateObjective(ctx, u, o.ID, ObjectivePatch{Milestones: &empty}); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetObjective(ctx, u, o.ID); len(got.Milestones) != 0 {
		t.Fatalf("cleared: %+v", got.Milestones)
	}

	bad := []Milestone{{Title: "  "}}
	if _, err := s.UpdateObjective(ctx, u, o.ID, ObjectivePatch{Milestones: &bad}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an empty title is invalid: %v", err)
	}
}

func TestSetObjectiveMilestoneDone(t *testing.T) {
	ctx := context.Background()
	s, u := newStore(t)
	o, err := s.CreateObjective(ctx, Objective{UserID: u, Title: "trip", Milestones: []Milestone{{Title: "visa"}}})
	if err != nil {
		t.Fatal(err)
	}
	mid := o.Milestones[0].ID
	if err := s.SetObjectiveMilestoneDone(ctx, u, o.ID, mid, true); err != nil {
		t.Fatal(err)
	}
	first, _ := s.GetObjective(ctx, u, o.ID)
	time.Sleep(5 * time.Millisecond)
	if err := s.SetObjectiveMilestoneDone(ctx, u, o.ID, mid, true); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetObjective(ctx, u, o.ID)
	if !again.Milestones[0].Done || !again.Milestones[0].DoneAt.Equal(*first.Milestones[0].DoneAt) {
		t.Fatalf("ticking twice keeps the first done time: %+v %+v", first.Milestones[0], again.Milestones[0])
	}
	if err := s.SetObjectiveMilestoneDone(ctx, u, o.ID, mid, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetObjective(ctx, u, o.ID); got.Milestones[0].Done || got.Milestones[0].DoneAt != nil {
		t.Fatalf("unticked: %+v", got.Milestones[0])
	}
	if err := s.SetObjectiveMilestoneDone(ctx, u, o.ID, "nope", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown milestone: %v", err)
	}
	// Another user's objective is not found.
	bob, _ := s.EnsureUser(ctx, "bob")
	if err := s.SetObjectiveMilestoneDone(ctx, bob.ID, o.ID, mid, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob: %v", err)
	}
	// Deleting the objective removes its milestones.
	if err := s.DeleteObjective(ctx, u, o.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM objective_milestones WHERE objective_id = ?`, o.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("left behind: %d %v", n, err)
	}
}

// Goal milestones carry a due time and criteria too, and goals keep working.
func TestGoalMilestonesKeepDueAndCriteria(t *testing.T) {
	ctx := context.Background()
	s, u := newStore(t)
	due := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	g, err := s.CreateGoal(ctx, Goal{UserID: u, Title: "German", Period: "week", Target: 140, Unit: "minutes",
		Milestones: []Milestone{{Title: "A1", Due: &due, Criteria: "exam passed"}, {Title: "A2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetMilestoneDone(ctx, u, g.ID, g.Milestones[0].ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetGoal(ctx, u, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	m := got.Milestones[0]
	if !m.Done || m.Due == nil || !m.Due.Equal(due) || m.Criteria != "exam passed" {
		t.Fatalf("goal milestone = %+v", m)
	}
}

// TestMilestoneMigration opens a database from before objective milestones:
// goal_milestones without due_ms and criteria, no objective_milestones. The
// existing rows stay and the new columns and table appear.
func TestMilestoneMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "noted.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.EnsureUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGoal(ctx, Goal{UserID: u.ID, Title: "German", Period: "week", Target: 1, Milestones: []Milestone{{Title: "A1"}, {Title: "A2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetMilestoneDone(ctx, u.ID, g.ID, g.Milestones[0].ID, true); err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateObjective(ctx, Objective{UserID: u.ID, Title: "B1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DROP TABLE objective_milestones`,
		`ALTER TABLE goal_milestones DROP COLUMN due_ms`,
		`ALTER TABLE goal_milestones DROP COLUMN criteria`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(q, err)
		}
	}
	_ = s.Close()

	for range 2 { // and reopening a migrated file changes nothing
		if s, err = Open(path); err != nil {
			t.Fatal(err)
		}
		for _, col := range []string{"due_ms", "criteria"} {
			if ok, err := s.hasColumn(ctx, "goal_milestones", col); err != nil || !ok {
				t.Fatalf("goal_milestones.%s: %v %v", col, ok, err)
			}
		}
		got, err := s.GetGoal(ctx, u.ID, g.ID)
		if err != nil || len(got.Milestones) != 2 || !got.Milestones[0].Done || got.Milestones[1].Done ||
			got.Milestones[0].Due != nil || got.Milestones[0].Criteria != "" {
			t.Fatalf("goal milestones after migration: %v %+v", err, got.Milestones)
		}
		v, err := s.ViewObjective(ctx, u.ID, o.ID, time.Now())
		if err != nil || len(v.Objective.Milestones) != 0 || v.Progress.MilestonesTotal != 0 {
			t.Fatalf("objective after migration: %v %+v", err, v)
		}
		_ = s.Close()
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	next := []Milestone{{Title: "forms", Due: tp(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))}}
	if _, err := s.UpdateObjective(ctx, u.ID, o.ID, ObjectivePatch{Milestones: &next}); err != nil {
		t.Fatal(err)
	}
	var due sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT due_ms FROM objective_milestones WHERE objective_id = ?`, o.ID).Scan(&due); err != nil || !due.Valid {
		t.Fatalf("written to the new table: %v %v", due, err)
	}
}
