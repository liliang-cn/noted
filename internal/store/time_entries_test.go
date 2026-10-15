package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func goalDone(t *testing.T, s *Store, userID, id string, now time.Time) float64 {
	t.Helper()
	g, err := s.GetGoal(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.ViewGoal(context.Background(), g, now)
	if err != nil {
		t.Fatal(err)
	}
	return v.Progress.Done
}

func TestTimeEntryDefaultsAndValidation(t *testing.T) {
	s, u := newStore(t)
	ctx := context.Background()
	now := time.Now()

	e, err := s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: "  游泳 ", Minutes: 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "游泳" || e.Space != SpaceLife || e.Running() {
		t.Fatalf("%+v", e)
	}
	if d := now.Sub(e.Start); d < 29*time.Minute || d > 31*time.Minute || !e.End.Equal(e.Start.Add(30*time.Minute)) {
		t.Fatalf("a bare entry should have just ended: %v - %v", e.Start, e.End)
	}

	// Start and end without minutes.
	from := now.Add(-2 * time.Hour)
	to := from.Add(45*time.Minute + 20*time.Second)
	e, err = s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: "英语学习", Start: from, End: &to}, now)
	if err != nil || e.Minutes != 45 {
		t.Fatalf("%v %+v", err, e)
	}

	for name, bad := range map[string]TimeEntry{
		"no title":   {Title: " ", Minutes: 10},
		"no minutes": {Title: "x"},
		"too long":   {Title: "x", Minutes: 1441},
		"future":     {Title: "x", Minutes: 10, Start: now.Add(time.Hour)},
		"project":    {Title: "x", Minutes: 10, ProjectID: "nope"},
		"goal":       {Title: "x", Minutes: 10, GoalID: "nope"},
	} {
		bad.UserID = u
		if _, err := s.CreateTimeEntry(ctx, bad, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTimeEntryCountsTowardGoal(t *testing.T) {
	s, u := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) // a Wednesday
	minutesGoal, err := s.CreateGoal(ctx, Goal{UserID: u, Title: "English", Period: "week", Target: 140, Unit: "分钟", TimeZone: "UTC", Space: SpaceWork})
	if err != nil {
		t.Fatal(err)
	}
	countGoal, err := s.CreateGoal(ctx, Goal{UserID: u, Title: "Swim", Period: "week", Target: 2, Unit: "times", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}

	e, err := s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: "英语学习", Minutes: 30, GoalID: minutesGoal.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	if e.CheckInID == "" || e.Space != SpaceWork {
		t.Fatalf("the entry should have made a check-in and taken the goal's space: %+v", e)
	}
	if d := goalDone(t, s, u, minutesGoal.ID, now); d != 30 {
		t.Fatalf("minutes goal done = %v, want 30", d)
	}
	sw, err := s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: "游泳", Minutes: 45, GoalID: countGoal.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	if d := goalDone(t, s, u, countGoal.ID, now); d != 1 {
		t.Fatalf("a count goal gets one session, got %v", d)
	}

	// Editing keeps the one check-in in step.
	m := 50
	e2, err := s.UpdateTimeEntry(ctx, u, e.ID, TimeEntryPatch{Minutes: &m}, now)
	if err != nil {
		t.Fatal(err)
	}
	if e2.CheckInID != e.CheckInID {
		t.Fatalf("the check-in should keep its id: %s -> %s", e.CheckInID, e2.CheckInID)
	}
	if d := goalDone(t, s, u, minutesGoal.ID, now); d != 50 {
		t.Fatalf("after editing the minutes: %v", d)
	}
	cs, _ := s.ListCheckIns(ctx, u, minutesGoal.ID, nil, nil, 10)
	if len(cs) != 1 || cs[0].TimeEntryID != e.ID {
		t.Fatalf("one check-in, tied to the entry: %+v", cs)
	}
	// It cannot be undone on its own.
	if _, err := s.DeleteCheckIn(ctx, u, cs[0].ID); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("DeleteCheckIn on an entry's check-in: %v", err)
	}

	// Moving it to the other goal moves the check-in.
	gid := countGoal.ID
	if _, err := s.UpdateTimeEntry(ctx, u, e.ID, TimeEntryPatch{GoalID: &gid}, now); err != nil {
		t.Fatal(err)
	}
	if a, b := goalDone(t, s, u, minutesGoal.ID, now), goalDone(t, s, u, countGoal.ID, now); a != 0 || b != 2 {
		t.Fatalf("after moving goals: minutes %v, count %v", a, b)
	}
	// Moving it back in time out of this week takes it out of this week's progress.
	old := now.AddDate(0, 0, -14)
	if _, err := s.UpdateTimeEntry(ctx, u, e.ID, TimeEntryPatch{Start: &old}, now); err != nil {
		t.Fatal(err)
	}
	if b := goalDone(t, s, u, countGoal.ID, now); b != 1 {
		t.Fatalf("an entry two weeks back should not count this week: %v", b)
	}
	// Unlinking removes it.
	none := ""
	if _, err := s.UpdateTimeEntry(ctx, u, sw.ID, TimeEntryPatch{GoalID: &none}, now); err != nil {
		t.Fatal(err)
	}
	if b := goalDone(t, s, u, countGoal.ID, now); b != 0 {
		t.Fatalf("after unlinking: %v", b)
	}

	// Changing the goal's unit recounts what entries add.
	back := now.Add(-time.Hour)
	if _, err := s.UpdateTimeEntry(ctx, u, e.ID, TimeEntryPatch{Start: &back}, now); err != nil {
		t.Fatal(err)
	}
	unit := "小时"
	if _, err := s.UpdateGoal(ctx, u, countGoal.ID, GoalPatch{Unit: &unit}); err != nil {
		t.Fatal(err)
	}
	if b := goalDone(t, s, u, countGoal.ID, now); b < 0.83 || b > 0.84 {
		t.Fatalf("50 minutes in hours: %v", b)
	}

	// Deleting the entry removes its check-in; deleting a goal keeps entries, unlinked.
	if err := s.DeleteTimeEntry(ctx, u, e.ID); err != nil {
		t.Fatal(err)
	}
	if b := goalDone(t, s, u, countGoal.ID, now); b != 0 {
		t.Fatalf("after deleting the entry: %v", b)
	}
	e3, _ := s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: "English", Minutes: 20, GoalID: minutesGoal.ID}, now)
	if err := s.DeleteGoal(ctx, u, minutesGoal.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetTimeEntry(ctx, u, e3.ID); err != nil || got.GoalID != "" || got.CheckInID != "" {
		t.Fatalf("the entry should stay without its goal: %v %+v", err, got)
	}
}

func TestTimerRunsOneAtATime(t *testing.T) {
	s, u := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	g, _ := s.CreateGoal(ctx, Goal{UserID: u, Title: "Study", Period: "day", Target: 60, Unit: "minutes", TimeZone: "UTC"})

	run, err := s.StartTimer(ctx, TimeEntry{UserID: u, Title: "英语学习", GoalID: g.ID, Start: now.Add(-40 * time.Second)}, now)
	if err != nil || !run.Running() || run.CheckInID != "" {
		t.Fatalf("%v %+v", err, run)
	}
	if goalDone(t, s, u, g.ID, now) != 0 {
		t.Fatal("a running timer must not count yet")
	}
	if _, err := s.StartTimer(ctx, TimeEntry{UserID: u, Title: "游泳"}, now); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("a second timer: %v", err)
	}
	m := 5
	if _, err := s.UpdateTimeEntry(ctx, u, run.ID, TimeEntryPatch{Minutes: &m}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("minutes on a running timer: %v", err)
	}
	if sum, _ := s.SummarizeTime(ctx, u, TimeFilter{}, TimeByTitle, time.UTC); sum.Count != 0 {
		t.Fatalf("a running timer is not in summaries: %+v", sum)
	}

	stopped, err := s.StopTimer(ctx, u, nil, now)
	if err != nil || stopped.Running() || stopped.Minutes != 1 || stopped.CheckInID == "" {
		t.Fatalf("under a minute counts as one: %v %+v", err, stopped)
	}
	if goalDone(t, s, u, g.ID, now) != 1 {
		t.Fatal("stopping should check in")
	}
	if _, err := s.StopTimer(ctx, u, nil, now); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("stop with none running: %v", err)
	}
	if _, ok, _ := s.RunningTimer(ctx, u); ok {
		t.Fatal("no timer should run")
	}

	// A timer forgotten for more than a day needs a real end.
	if _, err := s.StartTimer(ctx, TimeEntry{UserID: u, Title: "x", Start: now.Add(-30 * time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StopTimer(ctx, u, nil, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a 30-hour timer: %v", err)
	}
	end := now.Add(-29 * time.Hour)
	if e, err := s.StopTimer(ctx, u, &end, now); err != nil || e.Minutes != 60 {
		t.Fatalf("stopped with an end: %v %+v", err, e)
	}
}

func TestTimeSummaryAndList(t *testing.T) {
	s, u := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	p, _ := s.CreateProject(ctx, Project{UserID: u, Title: "App", Space: SpaceWork})
	add := func(title string, min int, at time.Time, pid string) {
		t.Helper()
		if _, err := s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: title, Minutes: min, Start: at, ProjectID: pid}, now); err != nil {
			t.Fatal(err)
		}
	}
	day1 := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	add("Swimming", 30, day1, "")
	add(" swimming ", 20, day2, "")
	add("英语学习", 45, day1.Add(time.Hour), "")
	add("Coding", 90, day2.Add(time.Hour), p.ID)

	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 2)
	sum, err := s.SummarizeTime(ctx, u, TimeFilter{From: &from, To: &to}, TimeByTitle, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Minutes != 185 || sum.Count != 4 || len(sum.Rows) != 3 {
		t.Fatalf("%+v", sum)
	}
	if r := sum.Rows[0]; r.Label != "Coding" || r.Minutes != 90 {
		t.Fatalf("most minutes first: %+v", sum.Rows)
	}
	if r := sum.Rows[1]; r.Key != "swimming" || r.Label != "swimming" || r.Minutes != 50 || r.Count != 2 {
		t.Fatalf("swimming should group ignoring case, labelled as last written: %+v", r)
	}

	byDay, _ := s.SummarizeTime(ctx, u, TimeFilter{From: &from, To: &to}, TimeByDay, time.UTC)
	if len(byDay.Rows) != 2 || byDay.Rows[0].Key != "2026-10-12" || byDay.Rows[0].Minutes != 75 || byDay.Rows[1].Minutes != 110 {
		t.Fatalf("by day: %+v", byDay.Rows)
	}
	// Day bounds follow the zone: 08:00 UTC on the 12th is still the 12th in Shanghai, 16:00.
	sh, _ := time.LoadLocation("Asia/Shanghai")
	if d, _ := s.SummarizeTime(ctx, u, TimeFilter{From: &from, To: &to}, TimeByDay, sh); d.Rows[0].Key != "2026-10-12" {
		t.Fatalf("by day in Shanghai: %+v", d.Rows)
	}
	byProject, _ := s.SummarizeTime(ctx, u, TimeFilter{From: &from, To: &to}, TimeByProject, time.UTC)
	if len(byProject.Rows) != 2 || byProject.Rows[0].Key != "" || byProject.Rows[0].Minutes != 95 || byProject.Rows[1].Label != "App" {
		t.Fatalf("by project: %+v", byProject.Rows)
	}
	if work, _ := s.SummarizeTime(ctx, u, TimeFilter{From: &from, To: &to, Space: SpaceWork}, TimeByTitle, time.UTC); work.Minutes != 90 {
		t.Fatalf("the project's entry should be work: %+v", work)
	}
	if _, err := s.SummarizeTime(ctx, u, TimeFilter{}, "week", time.UTC); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad group: %v", err)
	}

	l, _ := s.ListTimeEntries(ctx, u, TimeFilter{Title: "SWIM"})
	if len(l) != 2 || !l[0].Start.After(l[1].Start) {
		t.Fatalf("list by title, newest first: %+v", l)
	}

	d, err := s.GetProject(ctx, u, p.ID, now, time.UTC)
	if err != nil || len(d.Time) != 1 || d.View.Progress.TimeMinutes != 90 {
		t.Fatalf("project detail: %v %d %+v", err, len(d.Time), d.View.Progress)
	}
	if err := s.DeleteProject(ctx, u, p.ID, true); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListTimeEntries(ctx, u, TimeFilter{Title: "coding"}); len(l) != 1 || l[0].ProjectID != "" {
		t.Fatalf("entries stay when their project goes: %+v", l)
	}

	// Another user sees none of it.
	bob, _ := s.EnsureUser(ctx, "bob")
	if l, _ := s.ListTimeEntries(ctx, bob.ID, TimeFilter{}); len(l) != 0 {
		t.Fatalf("bob sees %d entries", len(l))
	}
	if _, err := s.GetTimeEntry(ctx, bob.ID, l[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob read alice's entry: %v", err)
	}
	if err := s.DeleteTimeEntry(ctx, bob.ID, l[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob deleted alice's entry: %v", err)
	}
}

func TestGoalAmount(t *testing.T) {
	for unit, want := range map[string]float64{"分钟": 90, "Minutes": 90, "min": 90, "小时": 1.5, "hours": 1.5, "times": 1, "次": 1, "": 1, "lessons": 1} {
		if got := GoalAmount(unit, 90); got != want {
			t.Errorf("%q: %v, want %v", unit, got, want)
		}
	}
}

// TestTimeEntryTimed checks which entries have a real start and end: those
// given a start or an end and timers, but not those logged with only minutes.
func TestTimeEntryTimed(t *testing.T) {
	s, u := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 13, 15, 22, 0, 0, time.UTC) // 23:22 in Shanghai

	bare, err := s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: "游泳", Minutes: 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if bare.Timed || !bare.Start.Equal(now.Add(-30*time.Minute)) || !bare.End.Equal(now) {
		t.Fatalf("minutes only: not timed, ending now: %+v", bare)
	}

	from := time.Date(2026, 10, 13, 5, 0, 0, 0, time.UTC)
	withStart, err := s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: "游泳", Minutes: 30, Start: from}, now)
	if err != nil || !withStart.Timed || !withStart.Start.Equal(from) {
		t.Fatalf("with a start: %v %+v", err, withStart)
	}
	to := from.Add(40 * time.Minute)
	startEnd, err := s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: "跑步", Start: from, End: &to}, now)
	if err != nil || !startEnd.Timed || startEnd.Minutes != 40 {
		t.Fatalf("start and end: %v %+v", err, startEnd)
	}
	endOnly, err := s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: "跑步", Minutes: 20, End: &to}, now)
	if err != nil || !endOnly.Timed || !endOnly.Start.Equal(to.Add(-20*time.Minute)) || !endOnly.End.Equal(to) {
		t.Fatalf("an end and minutes place the start: %v %+v", err, endOnly)
	}

	timer, err := s.StartTimer(ctx, TimeEntry{UserID: u, Title: "英语学习", Start: now.Add(-25 * time.Minute)}, now)
	if err != nil || !timer.Timed {
		t.Fatalf("timer: %v %+v", err, timer)
	}
	if timer, err = s.StopTimer(ctx, u, nil, now); err != nil || !timer.Timed || timer.Minutes != 25 {
		t.Fatalf("stopped timer: %v %+v", err, timer)
	}

	// Stored, not just returned.
	for _, want := range []TimeEntry{bare, withStart, startEnd, endOnly, timer} {
		got, err := s.GetTimeEntry(ctx, u, want.ID)
		if err != nil || got.Timed != want.Timed {
			t.Fatalf("%s: stored timed %v, want %v (%v)", want.ID, got.Timed, want.Timed, err)
		}
	}

	// New minutes keep the start and move the end; an untimed entry stays so.
	m := 45
	e, err := s.UpdateTimeEntry(ctx, u, bare.ID, TimeEntryPatch{Minutes: &m}, now)
	if err != nil || e.Timed || !e.Start.Equal(bare.Start) || !e.End.Equal(bare.Start.Add(45*time.Minute)) {
		t.Fatalf("minutes on an untimed entry: %v %+v", err, e)
	}
	e, err = s.UpdateTimeEntry(ctx, u, withStart.ID, TimeEntryPatch{Minutes: &m}, now)
	if err != nil || !e.Timed || !e.Start.Equal(from) || e.Minutes != 45 {
		t.Fatalf("minutes on a timed entry: %v %+v", err, e)
	}

	// An end alone on an untimed entry keeps its minutes and places them.
	end := time.Date(2026, 10, 13, 5, 30, 0, 0, time.UTC)
	e, err = s.UpdateTimeEntry(ctx, u, bare.ID, TimeEntryPatch{End: &end}, now)
	if err != nil || !e.Timed || e.Minutes != 45 || !e.Start.Equal(end.Add(-45*time.Minute)) || !e.End.Equal(end) {
		t.Fatalf("end on an untimed entry: %v %+v", err, e)
	}
	// On a timed one the end measures from the start, as before.
	end = from.Add(50 * time.Minute)
	e, err = s.UpdateTimeEntry(ctx, u, withStart.ID, TimeEntryPatch{End: &end}, now)
	if err != nil || !e.Timed || e.Minutes != 50 || !e.Start.Equal(from) {
		t.Fatalf("end on a timed entry: %v %+v", err, e)
	}

	// A start makes an untimed entry timed, keeping its minutes.
	other, err := s.CreateTimeEntry(ctx, TimeEntry{UserID: u, Title: "读书", Minutes: 15}, now)
	if err != nil {
		t.Fatal(err)
	}
	e, err = s.UpdateTimeEntry(ctx, u, other.ID, TimeEntryPatch{Start: &from}, now)
	if err != nil || !e.Timed || e.Minutes != 15 || !e.Start.Equal(from) {
		t.Fatalf("start on an untimed entry: %v %+v", err, e)
	}
	if got, _ := s.GetTimeEntry(ctx, u, other.ID); !got.Timed {
		t.Fatal("the update should be stored as timed")
	}
}

// TestTimedMigration opens a database from before the timed column and checks
// the guess: an entry whose end is when it was saved had its start defaulted.
func TestTimedMigration(t *testing.T) {
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
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE time_entries DROP COLUMN timed`); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 10, 13, 15, 22, 0, 0, time.UTC).UnixMilli()
	for _, r := range []struct {
		id         string
		start, end any
	}{
		{"same", created - 30*60000, created},                // logged with minutes only
		{"close", created - 30*60000 - 1200, created - 1200}, // the same, saved a moment later
		{"given", created - 5*3600000, created - 4*3600000},  // a start the person gave
		{"running", created, nil},                            // a timer still running
	} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO time_entries(id, user_id, title, title_key, minutes, start_ms, end_ms, created_ms, updated_ms)
			VALUES (?, ?, 'x', 'x', 30, ?, ?, ?, ?)`, r.id, u.ID, r.start, r.end, created, created); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.Close()

	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"same": false, "close": false, "given": true, "running": true}
	for id, timed := range want {
		e, err := s.GetTimeEntry(ctx, u.ID, id)
		if err != nil || e.Timed != timed {
			t.Errorf("%s: timed %v, want %v (%v)", id, e.Timed, timed, err)
		}
	}
	// The guess runs once: what is stored afterwards stays.
	if _, err := s.db.ExecContext(ctx, `UPDATE time_entries SET timed = 1 WHERE id = 'same'`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if e, _ := s.GetTimeEntry(ctx, u.ID, "same"); !e.Timed {
		t.Fatal("reopening should not guess again")
	}
}
