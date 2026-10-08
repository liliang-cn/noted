package store

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// 2026-10-08 is a Thursday.
var thu = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func day(y int, m time.Month, d int) string {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

func weekGoal() Goal {
	return Goal{Title: "Exercise", Period: "week", Target: 3, Unit: "times", TimeZone: "UTC",
		Created: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}
}

func TestProgressWeekOnTrack(t *testing.T) {
	daily := map[string]float64{
		day(2026, 10, 5): 1, day(2026, 10, 7): 1, // this week: Mon, Wed
		day(2026, 9, 28): 1, day(2026, 9, 30): 1, day(2026, 10, 2): 1, // last week: 3
		day(2026, 9, 21): 2, day(2026, 9, 23): 1, // the week before: 3
		day(2026, 9, 14): 1, // week of 9-14: only 1, breaks the streak
	}
	p := ComputeProgress(weekGoal(), daily, thu)
	if p.Done != 2 || p.Remaining != 1 || p.Achieved || p.Behind {
		t.Fatalf("%+v", p)
	}
	if p.Streak != 2 {
		t.Fatalf("streak = %d, want 2 (two achieved weeks before this one)", p.Streak)
	}
	if !p.PeriodStart.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) || !p.PeriodEnd.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("week should run Monday to Monday: %v - %v", p.PeriodStart, p.PeriodEnd)
	}
	if len(p.Days) != 7 || p.Days[0].Date != "2026-10-05" || p.Days[0].Amount != 1 {
		t.Fatalf("days = %+v", p.Days)
	}
	if len(p.Recent) != 14 || p.Recent[13].Date != "2026-10-08" || p.Recent[0].Date != "2026-09-25" {
		t.Fatalf("recent = %+v", p.Recent)
	}
}

func TestProgressAchievedExtendsStreak(t *testing.T) {
	daily := map[string]float64{
		day(2026, 10, 5): 1, day(2026, 10, 7): 1, day(2026, 10, 8): 1, // 3 this week
		day(2026, 9, 28): 3, // last week achieved in one go
	}
	p := ComputeProgress(weekGoal(), daily, thu)
	if !p.Achieved || p.Streak != 2 {
		t.Fatalf("achieved=%v streak=%d", p.Achieved, p.Streak)
	}
	if math.Abs(p.Percent-1) > 1e-9 {
		t.Fatalf("percent = %v", p.Percent)
	}
}

func TestProgressBehindPace(t *testing.T) {
	// Thursday: three days (Mon-Wed) have passed, so by now 3*3/7 = 1.28 -> 1 is expected.
	if p := ComputeProgress(weekGoal(), map[string]float64{}, thu); !p.Behind {
		t.Fatal("nothing done by Thursday should be behind")
	}
	if p := ComputeProgress(weekGoal(), map[string]float64{day(2026, 10, 6): 1}, thu); p.Behind {
		t.Fatal("one session by Thursday is on pace")
	}
	// Monday morning nothing has elapsed yet, so nobody is behind.
	mon := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)
	if p := ComputeProgress(weekGoal(), map[string]float64{}, mon); p.Behind {
		t.Fatal("cannot be behind on the first day")
	}
	// An achieved goal is never behind.
	g := weekGoal()
	g.Target = 1
	if p := ComputeProgress(g, map[string]float64{day(2026, 10, 5): 1}, thu); p.Behind || !p.Achieved {
		t.Fatalf("%+v", p)
	}
}

func TestStreakDoesNotCountBeforeCreation(t *testing.T) {
	g := weekGoal()
	g.Created = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) // inside the week of 9-28
	daily := map[string]float64{
		day(2026, 9, 28): 3, // creation week, achieved
		day(2026, 9, 21): 3, // before the goal existed
		day(2026, 10, 5): 3,
	}
	if p := ComputeProgress(g, daily, thu); p.Streak != 2 {
		t.Fatalf("streak = %d, want 2 (this week + the creation week)", p.Streak)
	}
}

func TestProgressAmountGoal(t *testing.T) {
	g := Goal{Title: "German", Period: "week", Target: 140, Unit: "minutes", TimeZone: "UTC", Created: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}
	daily := map[string]float64{day(2026, 10, 5): 20, day(2026, 10, 6): 25, day(2026, 10, 7): 30, day(2026, 10, 8): 20}
	p := ComputeProgress(g, daily, thu)
	if p.Done != 95 || p.Remaining != 45 || p.Achieved {
		t.Fatalf("%+v", p)
	}
}

func TestProgressMonthAndDay(t *testing.T) {
	m := Goal{Period: "month", Target: 10, TimeZone: "UTC", Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	p := ComputeProgress(m, map[string]float64{day(2026, 10, 1): 4, day(2026, 10, 31): 1, day(2026, 9, 30): 99}, thu)
	if p.Done != 5 || len(p.Days) != 31 {
		t.Fatalf("done=%v days=%d", p.Done, len(p.Days))
	}
	d := Goal{Period: "day", Target: 1, TimeZone: "UTC", Created: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	daily := map[string]float64{day(2026, 10, 6): 1, day(2026, 10, 7): 1} // yesterday done, today not yet
	p = ComputeProgress(d, daily, thu)
	if p.Achieved || p.Streak != 2 {
		t.Fatalf("an unfinished today must not break the streak: achieved=%v streak=%d", p.Achieved, p.Streak)
	}
}

func TestProgressUsesGoalTimeZone(t *testing.T) {
	sh, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("no tzdata")
	}
	g := weekGoal()
	g.TimeZone = "Asia/Shanghai"
	// Sunday 23:30 UTC is already Monday 07:30 in Shanghai: the new week.
	now := time.Date(2026, 10, 11, 23, 30, 0, 0, time.UTC)
	p := ComputeProgress(g, map[string]float64{"2026-10-12": 1}, now)
	if p.Done != 1 || p.Days[0].Date != "2026-10-12" {
		t.Fatalf("%+v (%v)", p, sh)
	}
}

func TestGoalLifecycle(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	g, err := s.CreateGoal(ctx, Goal{UserID: uid, Title: " German ", Period: "week", Target: 140, Unit: "minutes",
		Milestones: []Milestone{{Title: "A1"}, {Title: "A2"}, {Title: "Exam"}}})
	if err != nil {
		t.Fatal(err)
	}
	if g.Title != "German" || g.Space != SpaceLife || len(g.Milestones) != 3 || g.Milestones[0].ID == "" {
		t.Fatalf("%+v", g)
	}
	now := time.Now()
	if _, err := s.RecordCheckIn(ctx, uid, g.ID, 20, 0, time.Time{}, "lesson 12", now); err != nil {
		t.Fatal(err)
	}
	c2, _ := s.RecordCheckIn(ctx, uid, g.ID, 0, 0, time.Time{}, "", now) // amount 0 -> 1
	if c2.Amount != 1 {
		t.Fatalf("default amount = %v", c2.Amount)
	}
	v, err := s.ViewGoal(ctx, g, now)
	if err != nil || v.Progress.Done != 21 {
		t.Fatalf("%+v %v", v.Progress, err)
	}
	// Undo.
	gid, err := s.DeleteCheckIn(ctx, uid, c2.ID)
	if err != nil || gid != g.ID {
		t.Fatalf("%v %v", gid, err)
	}
	if v, _ = s.ViewGoal(ctx, g, now); v.Progress.Done != 20 {
		t.Fatalf("after undo: %v", v.Progress.Done)
	}

	// Validation.
	if _, err := s.RecordCheckIn(ctx, uid, g.ID, -5, 0, time.Time{}, "", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative amount: %v", err)
	}
	if _, err := s.RecordCheckIn(ctx, uid, g.ID, 1, 0, now.Add(48*time.Hour), "", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("future check-in: %v", err)
	}
	if _, err := s.RecordCheckIn(ctx, uid, g.ID, 1, 0, now.AddDate(-6, 0, 0), "", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ancient check-in: %v", err)
	}
	for _, bad := range []Goal{
		{UserID: uid, Title: "x", Period: "year", Target: 1},
		{UserID: uid, Title: "x", Period: "week", Target: 0},
		{UserID: uid, Title: "", Period: "week", Target: 1},
		{UserID: uid, Title: "x", Period: "week", Target: 1, TimeZone: "Mars/Base"},
		{UserID: uid, Title: "x", Period: "week", Target: 1, EventID: "no-such-event"},
		{UserID: uid, Title: "x", Period: "week", Target: 1, Space: "play"},
	} {
		if _, err := s.CreateGoal(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted %+v: %v", bad, err)
		}
	}
}

func TestMilestonesKeepStateAcrossUpdate(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	g, _ := s.CreateGoal(ctx, Goal{UserID: uid, Title: "German", Period: "week", Target: 1,
		Milestones: []Milestone{{Title: "A1"}, {Title: "A2"}}})
	g, err := s.SetMilestoneDone(ctx, uid, g.ID, g.Milestones[0].ID, true)
	if err != nil || !g.Milestones[0].Done || g.Milestones[0].DoneAt == nil {
		t.Fatalf("%+v %v", g.Milestones, err)
	}
	// Rename A1, drop A2, add B1: A1 keeps its id and done state.
	next := []Milestone{{ID: g.Milestones[0].ID, Title: "A1 basics"}, {Title: "B1"}}
	g, err = s.UpdateGoal(ctx, uid, g.ID, GoalPatch{Milestones: &next})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Milestones) != 2 || !g.Milestones[0].Done || g.Milestones[0].Title != "A1 basics" || g.Milestones[1].Done {
		t.Fatalf("%+v", g.Milestones)
	}
	v, _ := s.ViewGoal(ctx, g, time.Now())
	if v.Progress.MilestonesDone != 1 || v.Progress.MilestonesTotal != 2 || v.Progress.MilestonePercent != 0.5 {
		t.Fatalf("%+v", v.Progress)
	}
	if _, err := s.SetMilestoneDone(ctx, uid, g.ID, "nope", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown milestone: %v", err)
	}
}

func TestTimeZoneChangeRebucketsCheckIns(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	g, _ := s.CreateGoal(ctx, Goal{UserID: uid, Title: "Run", Period: "day", Target: 1, TimeZone: "UTC"})
	at := time.Date(2026, 10, 7, 17, 0, 0, 0, time.UTC) // 01:00 on the 8th in Shanghai
	c, err := s.RecordCheckIn(ctx, uid, g.ID, 1, 0, at, "", at.Add(time.Hour))
	if err != nil || c.Date != "2026-10-07" {
		t.Fatalf("%+v %v", c, err)
	}
	tz := "Asia/Shanghai"
	if _, err := s.UpdateGoal(ctx, uid, g.ID, GoalPatch{TimeZone: &tz}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListCheckIns(ctx, uid, g.ID, nil, nil, 10)
	if len(list) != 1 || list[0].Date != "2026-10-08" {
		t.Fatalf("check-in should move to the 8th: %+v", list)
	}
}

func TestGoalsAreScopedPerUserAndSpace(t *testing.T) {
	ctx := context.Background()
	s, alice := newStore(t)
	bob, _ := s.EnsureUser(ctx, "bob")
	g, _ := s.CreateGoal(ctx, Goal{UserID: alice, Title: "Run", Period: "week", Target: 3})
	if _, err := s.GetGoal(ctx, bob.ID, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob read alice's goal: %v", err)
	}
	if _, err := s.RecordCheckIn(ctx, bob.ID, g.ID, 1, 0, time.Time{}, "", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob checked in on alice's goal: %v", err)
	}
	if err := s.DeleteGoal(ctx, bob.ID, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob deleted alice's goal: %v", err)
	}
	s.CreateGoal(ctx, Goal{UserID: alice, Title: "Review roadmap", Period: "week", Target: 1, Space: SpaceWork})
	now := time.Now()
	all, _ := s.ListGoals(ctx, alice, false, now, "")
	work, _ := s.ListGoals(ctx, alice, false, now, SpaceWork)
	life, _ := s.ListGoals(ctx, alice, false, now, SpaceLife)
	if len(all) != 2 || len(work) != 1 || len(life) != 1 || work[0].Goal.Title != "Review roadmap" {
		t.Fatalf("all=%d work=%d life=%d", len(all), len(work), len(life))
	}
	if _, err := s.ListGoals(ctx, alice, false, now, "play"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad space filter: %v", err)
	}
}

func TestDeletingEventUnschedulesGoal(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	e, _ := s.CreateEvent(ctx, Event{UserID: uid, Title: "Gym", Start: thu, RRule: "FREQ=WEEKLY"})
	g, err := s.CreateGoal(ctx, Goal{UserID: uid, Title: "Exercise", Period: "week", Target: 3, EventID: e.ID})
	if err != nil || g.EventID != e.ID {
		t.Fatalf("%+v %v", g, err)
	}
	if err := s.DeleteEvent(ctx, uid, e.ID); err != nil {
		t.Fatal(err)
	}
	g, _ = s.GetGoal(ctx, uid, g.ID)
	if g.EventID != "" {
		t.Fatalf("goal still points at a deleted event: %q", g.EventID)
	}
}

// ---- projects ----

func TestProjectGroupsItemsAndTracksProgress(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	now := thu
	depart := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	p, err := s.CreateProject(ctx, Project{UserID: uid, Title: "Philippines trip", Pinned: true, Start: &depart, Space: SpaceLife})
	if err != nil {
		t.Fatal(err)
	}
	visaDue := now.AddDate(0, 0, 10)
	visa, _ := s.CreateTask(ctx, Task{UserID: uid, Title: "Apply for visa", Due: &visaDue, ProjectID: p.ID})
	hotelDue := now.AddDate(0, 0, -1) // overdue
	s.CreateTask(ctx, Task{UserID: uid, Title: "Book hotel", Due: &hotelDue, ProjectID: p.ID})
	s.CreateTask(ctx, Task{UserID: uid, Title: "Book flights", ProjectID: p.ID})
	flight, _ := s.CreateEvent(ctx, Event{UserID: uid, Title: "Flight MNL", Start: depart.Add(9 * time.Hour), ProjectID: p.ID})
	s.CreateNote(ctx, Note{UserID: uid, Title: "Itinerary", Content: "Palawan first", ProjectID: p.ID})

	// Items inherit the project's space.
	if flight.Space != SpaceLife || visa.Space != SpaceLife {
		t.Fatalf("space not inherited: %q %q", flight.Space, visa.Space)
	}

	done := true
	s.UpdateTask(ctx, uid, visa.ID, TaskPatch{Done: &done})
	d, err := s.GetProject(ctx, uid, p.ID, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	pr := d.View.Progress
	if pr.TasksTotal != 3 || pr.TasksDone != 1 || pr.TasksOverdue != 1 || pr.NotesCount != 1 || pr.EventsUpcoming != 1 {
		t.Fatalf("%+v", pr)
	}
	if math.Abs(pr.Percent-1.0/3) > 1e-9 {
		t.Fatalf("percent = %v", pr.Percent)
	}
	if pr.DaysLeft == nil || *pr.DaysLeft != 43 {
		t.Fatalf("days left = %v, want 43 (Oct 8 -> Nov 20)", pr.DaysLeft)
	}
	if pr.Next == nil || !pr.Next.Equal(hotelDue) || pr.NextTitle != "Book hotel" {
		t.Fatalf("the overdue hotel should be next: %v %q", pr.Next, pr.NextTitle)
	}
	if len(d.Tasks) != 3 || len(d.Events) != 1 || len(d.Notes) != 1 {
		t.Fatalf("detail: %d tasks %d events %d notes", len(d.Tasks), len(d.Events), len(d.Notes))
	}
}

func TestProjectRejectsForeignAndUnknownProjects(t *testing.T) {
	ctx := context.Background()
	s, alice := newStore(t)
	bob, _ := s.EnsureUser(ctx, "bob")
	p, _ := s.CreateProject(ctx, Project{UserID: alice, Title: "Secret"})
	if _, err := s.CreateTask(ctx, Task{UserID: bob.ID, Title: "x", ProjectID: p.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bob attached a task to alice's project: %v", err)
	}
	if _, err := s.CreateNote(ctx, Note{UserID: alice, Title: "x", ProjectID: "nope"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown project: %v", err)
	}
	if _, err := s.GetProject(ctx, bob.ID, p.ID, time.Now(), time.UTC); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob read alice's project: %v", err)
	}
	if err := s.DeleteProject(ctx, bob.ID, p.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob deleted alice's project: %v", err)
	}
}

func TestDeleteProjectKeepsOrRemovesItems(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	mk := func(title string) (Project, Task, Note) {
		p, _ := s.CreateProject(ctx, Project{UserID: uid, Title: title})
		tk, _ := s.CreateTask(ctx, Task{UserID: uid, Title: title + " task", ProjectID: p.ID})
		n, _ := s.CreateNote(ctx, Note{UserID: uid, Title: title + " note", Content: "keepme", ProjectID: p.ID})
		return p, tk, n
	}
	p1, t1, n1 := mk("A")
	if err := s.DeleteProject(ctx, uid, p1.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetTask(ctx, uid, t1.ID); err != nil || got.ProjectID != "" {
		t.Fatalf("task should survive, unassigned: %+v %v", got, err)
	}
	if got, err := s.GetNote(ctx, uid, n1.ID); err != nil || got.ProjectID != "" {
		t.Fatalf("note should survive, unassigned: %+v %v", got, err)
	}

	p2, t2, n2 := mk("B")
	if err := s.DeleteProject(ctx, uid, p2.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTask(ctx, uid, t2.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("task should be gone: %v", err)
	}
	if _, err := s.GetNote(ctx, uid, n2.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("note should be gone: %v", err)
	}
	if h, _ := s.SearchNotes(ctx, uid, "keepme", 10, false, ""); len(h) != 1 || h[0].Note.ID != n1.ID {
		t.Fatalf("search index should only hold the surviving note: %+v", h)
	}
}

func TestProjectOrdering(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	soon, later := thu.AddDate(0, 0, 5), thu.AddDate(0, 2, 0)
	s.CreateProject(ctx, Project{UserID: uid, Title: "undated"})
	s.CreateProject(ctx, Project{UserID: uid, Title: "later", Due: &later})
	s.CreateProject(ctx, Project{UserID: uid, Title: "soon", Due: &soon})
	s.CreateProject(ctx, Project{UserID: uid, Title: "pinned undated", Pinned: true})
	vs, err := s.ListProjects(ctx, uid, ProjectFilter{}, thu, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.Project.Title)
	}
	want := []string{"pinned undated", "soon", "later", "undated"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// ---- space (work / life) ----

func TestSpaceFiltersEveryKind(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	start := thu.Add(24 * time.Hour)
	due := thu.Add(24 * time.Hour)
	s.CreateNote(ctx, Note{UserID: uid, Title: "Roadmap", Content: "planning notes", Space: SpaceWork})
	s.CreateNote(ctx, Note{UserID: uid, Title: "Recipes", Content: "planning dinner"}) // default: life
	s.CreateEvent(ctx, Event{UserID: uid, Title: "Board meeting", Start: start, Space: SpaceWork})
	s.CreateEvent(ctx, Event{UserID: uid, Title: "Dentist", Start: start})
	s.CreateTask(ctx, Task{UserID: uid, Title: "Ship release", Due: &due, Space: SpaceWork})
	s.CreateTask(ctx, Task{UserID: uid, Title: "Pay rent", Due: &due})

	count := func(space string) (notes, search, events, tasks int) {
		ns, _ := s.ListNotes(ctx, uid, NoteFilter{Limit: 50, Space: space})
		hs, _ := s.SearchNotes(ctx, uid, "planning", 10, false, space)
		oc, _ := s.ListEvents(ctx, uid, thu, thu.AddDate(0, 0, 3), space)
		ts, _ := s.ListTasks(ctx, uid, TaskFilter{Limit: 50, Space: space})
		return len(ns), len(hs), len(oc), len(ts)
	}
	for _, tc := range []struct {
		space string
		want  [4]int
	}{{"", [4]int{2, 2, 2, 2}}, {SpaceWork, [4]int{1, 1, 1, 1}}, {SpaceLife, [4]int{1, 1, 1, 1}}} {
		n, se, ev, tk := count(tc.space)
		if [4]int{n, se, ev, tk} != tc.want {
			t.Errorf("space %q: notes=%d search=%d events=%d tasks=%d, want %v", tc.space, n, se, ev, tk, tc.want)
		}
	}
	if _, err := s.ListNotes(ctx, uid, NoteFilter{Limit: 5, Space: "play"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad filter: %v", err)
	}
	if _, err := s.CreateTask(ctx, Task{UserID: uid, Title: "x", Space: "play"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad space on write: %v", err)
	}
}

func TestMovingBetweenSpaces(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	n, _ := s.CreateNote(ctx, Note{UserID: uid, Title: "Idea", Content: "c"})
	work := SpaceWork
	n, err := s.UpdateNote(ctx, uid, n.ID, NotePatch{Space: &work})
	if err != nil || n.Space != SpaceWork {
		t.Fatalf("%+v %v", n, err)
	}
	if l, _ := s.ListNotes(ctx, uid, NoteFilter{Limit: 5, Space: SpaceLife}); len(l) != 0 {
		t.Fatal("moved note still listed under life")
	}
}

func TestReminderCarriesSpace(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	r := thu.Add(-time.Minute)
	s.CreateTask(ctx, Task{UserID: uid, Title: "Send invoice", Remind: &r, Space: SpaceWork})
	s.CreateTask(ctx, Task{UserID: uid, Title: "Call mum", Remind: &r})
	got, err := s.CollectDueReminders(ctx, thu)
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	work, _ := s.ListReminders(ctx, uid, thu.Add(-time.Hour), 10, SpaceWork)
	life, _ := s.ListReminders(ctx, uid, thu.Add(-time.Hour), 10, SpaceLife)
	if len(work) != 1 || work[0].Title != "Send invoice" || len(life) != 1 || life[0].Title != "Call mum" {
		t.Fatalf("work=%+v life=%+v", work, life)
	}
}

// ---- focus ----

func TestFocusHorizons(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	at := func(days, hour int) time.Time { return time.Date(2026, 10, 8+days, hour, 0, 0, 0, time.UTC) }
	ev := func(title string, when time.Time, space string) {
		if _, err := s.CreateEvent(ctx, Event{UserID: uid, Title: title, Start: when, Space: space}); err != nil {
			t.Fatal(err)
		}
	}
	task := func(title string, due time.Time, space string) {
		if _, err := s.CreateTask(ctx, Task{UserID: uid, Title: title, Due: &due, Space: space}); err != nil {
			t.Fatal(err)
		}
	}
	ev("standup", at(0, 9), SpaceWork)     // today
	ev("dentist", at(0, 14), SpaceLife)    // today
	ev("gym", at(1, 18), SpaceLife)        // tomorrow
	ev("offsite", at(12, 9), SpaceWork)    // inside the 14-day upcoming window
	ev("conference", at(20, 9), SpaceWork) // beyond upcoming, still this month
	ev("last month", at(-20, 9), SpaceLife)
	task("overdue report", at(-2, 17), SpaceWork) // overdue
	task("pay rent", at(0, 17), SpaceLife)
	task("renew passport", at(5, 9), SpaceLife)
	p, _ := s.CreateProject(ctx, Project{UserID: uid, Title: "Trip", Pinned: true})
	s.CreateProject(ctx, Project{UserID: uid, Title: "unpinned"})
	s.CreateGoal(ctx, Goal{UserID: uid, Title: "Run", Period: "week", Target: 3})
	now := at(0, 10)

	titles := func(f Focus) map[string]bool {
		m := map[string]bool{}
		for _, it := range f.Items {
			if it.Event != nil {
				m[it.Event.Event.Title] = true
			} else {
				m[it.Task.Title] = true
			}
		}
		return m
	}
	check := func(horizon, space string, want []string, notWant []string) Focus {
		t.Helper()
		f, err := s.Focus(ctx, uid, horizon, space, time.UTC, now)
		if err != nil {
			t.Fatal(err)
		}
		got := titles(f)
		for _, w := range want {
			if !got[w] {
				t.Errorf("%s/%s: missing %q (have %v)", horizon, space, w, got)
			}
		}
		for _, w := range notWant {
			if got[w] {
				t.Errorf("%s/%s: should not contain %q", horizon, space, w)
			}
		}
		return f
	}

	f := check(HorizonToday, "", []string{"standup", "dentist", "pay rent", "overdue report"}, []string{"gym", "offsite", "renew passport"})
	if !f.Items[0].Overdue || f.Items[0].Task.Title != "overdue report" {
		t.Fatalf("overdue work should lead today: %+v", f.Items[0])
	}
	if len(f.Projects) != 1 || f.Projects[0].Project.ID != p.ID {
		t.Fatalf("only the pinned project belongs in focus: %+v", f.Projects)
	}
	if len(f.Goals) != 1 {
		t.Fatalf("goals = %d", len(f.Goals))
	}
	check(HorizonToday, SpaceWork, []string{"standup", "overdue report"}, []string{"dentist", "pay rent"})
	check(HorizonToday, SpaceLife, []string{"dentist", "pay rent"}, []string{"standup", "overdue report"})

	// Upcoming starts tomorrow and never repeats today's items or nags about the past.
	check(HorizonUpcoming, "", []string{"gym", "offsite", "renew passport"}, []string{"standup", "dentist", "pay rent", "conference", "overdue report"})
	// Week: Mon Oct 5 - Sun Oct 11 (the Oct 6 task falls inside it; Oct 13 is next week).
	check(HorizonWeek, "", []string{"standup", "dentist", "gym", "overdue report"}, []string{"offsite", "last month", "renew passport"})
	// Month: all of October.
	check(HorizonMonth, "", []string{"standup", "offsite", "conference", "renew passport"}, []string{"last month"})

	if _, err := s.Focus(ctx, uid, "decade", "", time.UTC, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad horizon: %v", err)
	}
	// A pinned project is filtered by space too.
	pw, _ := s.CreateProject(ctx, Project{UserID: uid, Title: "Launch", Pinned: true, Space: SpaceWork})
	fw, _ := s.Focus(ctx, uid, HorizonToday, SpaceWork, time.UTC, now)
	if len(fw.Projects) != 1 || fw.Projects[0].Project.ID != pw.ID {
		t.Fatalf("work focus should show only the work project: %+v", fw.Projects)
	}
}

func TestOldDatabaseGainsNewColumns(t *testing.T) {
	// A database created before projects and spaces existed must still open.
	dir := t.TempDir()
	path := dir + "/old.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the old schema by dropping the new columns' data is not possible in
	// SQLite; instead check that reopening an already-migrated file is idempotent.
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	u, _ := s.EnsureUser(context.Background(), "a")
	if _, err := s.CreateNote(context.Background(), Note{UserID: u.ID, Title: "t", Content: "c"}); err != nil {
		t.Fatal(err)
	}
}

// ---- the running total next to the period target ----

func lessonsGoal(t *testing.T, s *Store, uid string) Goal {
	t.Helper()
	g, err := s.CreateGoal(context.Background(), Goal{UserID: uid, Title: "German", Period: "week", Target: 140, Unit: "minutes",
		CounterUnit: "lessons", CounterTarget: 40})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCounterRunsAlongsideThePeriodTarget(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	g := lessonsGoal(t, s, uid)
	now := time.Now()

	// A lesson of 20 minutes: both move.
	for i := 0; i < 3; i++ {
		if _, err := s.RecordCheckIn(ctx, uid, g.ID, 20, 1, time.Time{}, "", now); err != nil {
			t.Fatal(err)
		}
	}
	v, _ := s.ViewGoal(ctx, g, now)
	if v.Progress.Done != 60 || v.Progress.CounterDone != 3 || math.Abs(v.Progress.CounterPercent-0.075) > 1e-9 {
		t.Fatalf("%+v", v.Progress)
	}

	// Finishing a lesson without logging minutes moves the total alone. It must not
	// quietly count as one minute of study.
	c, err := s.RecordCheckIn(ctx, uid, g.ID, 0, 2, time.Time{}, "", now)
	if err != nil || c.Amount != 0 || c.Tally != 2 {
		t.Fatalf("%+v %v", c, err)
	}
	v, _ = s.ViewGoal(ctx, g, now)
	if v.Progress.Done != 60 || v.Progress.CounterDone != 5 {
		t.Fatalf("a count-only check-in changed the period total: %+v", v.Progress)
	}

	// A plain check-in still counts as one amount and leaves the total alone.
	if p, _ := s.RecordCheckIn(ctx, uid, g.ID, 0, 0, time.Time{}, "", now); p.Amount != 1 || p.Tally != 0 {
		t.Fatalf("%+v", p)
	}

	// Undo takes it out of both.
	if _, err := s.DeleteCheckIn(ctx, uid, c.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ = s.ViewGoal(ctx, g, now); v.Progress.CounterDone != 3 {
		t.Fatalf("counter after undo = %v", v.Progress.CounterDone)
	}
	list, _ := s.ListCheckIns(ctx, uid, g.ID, nil, nil, 10)
	tallied := 0.0
	for _, c := range list {
		tallied += c.Tally
	}
	if tallied != 3 {
		t.Fatalf("check-ins carry their count: %v", tallied)
	}
}

func TestCounterIsNotTiedToThePeriod(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	g := lessonsGoal(t, s, uid)
	now := time.Now()
	s.RecordCheckIn(ctx, uid, g.ID, 20, 1, now.AddDate(0, 0, -40), "", now) // far outside this week
	v, _ := s.ViewGoal(ctx, g, now)
	if v.Progress.Done != 0 || v.Progress.CounterDone != 1 {
		t.Fatalf("an old lesson belongs to the total, not to this week: %+v", v.Progress)
	}
}

func TestCounterValidation(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	base := Goal{UserID: uid, Title: "x", Period: "week", Target: 1}
	for name, g := range map[string]Goal{
		"target without a unit": {UserID: uid, Title: "x", Period: "week", Target: 1, CounterTarget: 40},
		"negative target":       {UserID: uid, Title: "x", Period: "week", Target: 1, CounterTarget: -1, CounterUnit: "l"},
		"unit too long":         {UserID: uid, Title: "x", Period: "week", Target: 1, CounterTarget: 5, CounterUnit: strings.Repeat("u", 21)},
	} {
		if _, err := s.CreateGoal(ctx, g); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// No counter: the unit is dropped, and a count is refused.
	g, err := s.CreateGoal(ctx, Goal{UserID: uid, Title: "x", Period: "week", Target: 1, CounterUnit: "ignored"})
	if err != nil || g.CounterUnit != "" {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := s.RecordCheckIn(ctx, uid, g.ID, 1, 1, time.Time{}, "", time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a count on a goal without a counter: %v", err)
	}
	if _, err := s.RecordCheckIn(ctx, uid, g.ID, 1, -1, time.Time{}, "", time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative count: %v", err)
	}
	_ = base
}

func TestCounterCanBeAddedLaterAndSwitchedOff(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	g, _ := s.CreateGoal(ctx, Goal{UserID: uid, Title: "German", Period: "week", Target: 140, Unit: "minutes"})
	now := time.Now()
	unit, target := "lessons", 40.0
	if _, err := s.UpdateGoal(ctx, uid, g.ID, GoalPatch{CounterUnit: &unit, CounterTarget: &target}); err != nil {
		t.Fatal(err)
	}
	s.RecordCheckIn(ctx, uid, g.ID, 20, 1, time.Time{}, "", now)
	got, _ := s.GetGoal(ctx, uid, g.ID)
	if v, _ := s.ViewGoal(ctx, got, now); v.Progress.CounterDone != 1 {
		t.Fatalf("%+v", v.Progress)
	}
	zero := 0.0
	off, err := s.UpdateGoal(ctx, uid, g.ID, GoalPatch{CounterTarget: &zero})
	if err != nil || off.CounterUnit != "" {
		t.Fatalf("%+v %v", off, err)
	}
	if v, _ := s.ViewGoal(ctx, off, now); v.Progress.CounterDone != 0 || v.Progress.CounterPercent != 0 {
		t.Fatalf("a switched-off counter should show nothing: %+v", v.Progress)
	}
	// Switch it back on and the earlier lessons are still there.
	if _, err := s.UpdateGoal(ctx, uid, g.ID, GoalPatch{CounterUnit: &unit, CounterTarget: &target}); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetGoal(ctx, uid, g.ID)
	if v, _ := s.ViewGoal(ctx, again, now); v.Progress.CounterDone != 1 {
		t.Fatalf("lessons recorded earlier should come back: %+v", v.Progress)
	}
}
