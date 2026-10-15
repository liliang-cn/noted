package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// complete marks a task done and returns the open follower it got, if any.
func complete(t *testing.T, s *Store, uid, id string) (done Task, next *Task) {
	t.Helper()
	ctx := context.Background()
	yes := true
	done, err := s.UpdateTask(ctx, uid, id, TaskPatch{Done: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if !done.Done || done.ID != id {
		t.Fatalf("completing returns the completed task: %+v", done)
	}
	return done, follower(t, s, uid, done.ID)
}

func follower(t *testing.T, s *Store, uid, id string) *Task {
	t.Helper()
	got, err := s.GetTask(context.Background(), uid, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.NextID == "" {
		return nil
	}
	n, err := s.GetTask(context.Background(), uid, got.NextID)
	if err != nil {
		t.Fatal(err)
	}
	return &n
}

func at(loc *time.Location, y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, loc)
}

func TestRepeatingTaskDaily(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	sh := zone(t, "Asia/Shanghai")
	s.SetLocation(sh)
	p, err := s.CreateProject(ctx, Project{UserID: uid, Title: "Health"})
	if err != nil {
		t.Fatal(err)
	}
	due, remind := at(sh, 2026, 10, 12, 9, 0), at(sh, 2026, 10, 12, 8, 30)
	task, err := s.CreateTask(ctx, Task{UserID: uid, Title: "吃药", Notes: "饭后", Due: &due, Remind: &remind, Priority: 3,
		Tags: []string{"health"}, ProjectID: p.ID, Space: SpaceWork, RRule: "RRULE:FREQ=DAILY"})
	if err != nil {
		t.Fatal(err)
	}
	if task.RRule != "FREQ=DAILY" || task.Occurrence != 1 {
		t.Fatalf("stored rule: %q #%d", task.RRule, task.Occurrence)
	}
	// Some work on the first one, which must not carry over.
	if _, err := s.LogTaskWork(ctx, uid, task.ID, time.Time{}, 5, nil, "half"); err != nil {
		t.Fatal(err)
	}

	_, next := complete(t, s, uid, task.ID)
	if next == nil {
		t.Fatal("no next task")
	}
	if !next.Due.Equal(at(sh, 2026, 10, 13, 9, 0)) || !next.Remind.Equal(at(sh, 2026, 10, 13, 8, 30)) {
		t.Fatalf("next due %v remind %v", next.Due, next.Remind)
	}
	if next.Done || next.Title != "吃药" || next.Notes != "饭后" || next.Priority != 3 || len(next.Tags) != 1 || next.Tags[0] != "health" ||
		next.ProjectID != p.ID || next.Space != SpaceWork || next.RRule != "FREQ=DAILY" || next.Occurrence != 2 {
		t.Fatalf("next copies the to-do: %+v", next)
	}
	if next.WorkSessions != 0 || next.WorkedMinutes != 0 || next.Progress != 0 || next.LastWorked != nil {
		t.Fatalf("sittings carried over: %+v", next)
	}
	open, err := s.ListTasks(ctx, uid, TaskFilter{State: "open", Limit: 10})
	if err != nil || len(open) != 1 || open[0].ID != next.ID {
		t.Fatalf("open tasks: %v %+v", err, open)
	}
	old, err := s.GetTask(ctx, uid, task.ID)
	if err != nil || !old.Done || old.WorkSessions != 1 {
		t.Fatalf("the completed one stays as history: %v %+v", err, old)
	}

	// Reopening keeps the follower; completing again does not make another.
	no := false
	if _, err := s.UpdateTask(ctx, uid, task.ID, TaskPatch{Done: &no}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTask(ctx, uid, next.ID); err != nil {
		t.Fatalf("reopening deleted the follower: %v", err)
	}
	if _, again := complete(t, s, uid, task.ID); again == nil || again.ID != next.ID {
		t.Fatalf("completing again: %+v", again)
	}
	all, _ := s.ListTasks(ctx, uid, TaskFilter{State: "all", Limit: 10})
	if len(all) != 2 {
		t.Fatalf("tasks after re-completing: %d", len(all))
	}
}

func TestRepeatingTaskWeeklyByDay(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	sh := zone(t, "Asia/Shanghai")
	s.SetLocation(sh)
	// Monday 07:00 in Shanghai is still Sunday in UTC: the weekday is read
	// in the server's zone.
	due := at(sh, 2026, 10, 12, 7, 0)
	task, err := s.CreateTask(ctx, Task{UserID: uid, Title: "Gym", Due: &due, RRule: "FREQ=WEEKLY;BYDAY=MO,WE,FR"})
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Time{at(sh, 2026, 10, 14, 7, 0), at(sh, 2026, 10, 16, 7, 0), at(sh, 2026, 10, 19, 7, 0)}
	id := task.ID
	for i, w := range want {
		_, next := complete(t, s, uid, id)
		if next == nil || !next.Due.Equal(w) {
			t.Fatalf("step %d: want %v, got %+v", i, w, next)
		}
		id = next.ID
	}

	// Every other week.
	due2 := at(sh, 2026, 10, 14, 7, 0) // a Wednesday
	t2, err := s.CreateTask(ctx, Task{UserID: uid, Title: "Swim", Due: &due2, RRule: "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE"})
	if err != nil {
		t.Fatal(err)
	}
	if _, next := complete(t, s, uid, t2.ID); next == nil || !next.Due.Equal(at(sh, 2026, 10, 26, 7, 0)) {
		t.Fatalf("biweekly: %+v", next)
	}
}

func TestRepeatingTaskMonthly(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	sh := zone(t, "Asia/Shanghai")
	s.SetLocation(sh)
	due := at(sh, 2027, 1, 31, 20, 0)
	task, err := s.CreateTask(ctx, Task{UserID: uid, Title: "Pay card", Due: &due, RRule: "FREQ=MONTHLY"})
	if err != nil {
		t.Fatal(err)
	}
	// February has no 31st: the rule skips it, as RFC 5545 says.
	_, next := complete(t, s, uid, task.ID)
	if next == nil || !next.Due.Equal(at(sh, 2027, 3, 31, 20, 0)) {
		t.Fatalf("monthly on the 31st: %+v", next)
	}
	_, next = complete(t, s, uid, next.ID)
	if next == nil || !next.Due.Equal(at(sh, 2027, 5, 31, 20, 0)) {
		t.Fatalf("monthly on the 31st, again: %+v", next)
	}
}

func TestRepeatingTaskKeepsWallClockAcrossDST(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	ny := zone(t, "America/New_York")
	s.SetLocation(ny)
	due := at(ny, 2026, 10, 31, 9, 0) // DST ends on 1 November
	remind := due.Add(-15 * time.Minute)
	task, err := s.CreateTask(ctx, Task{UserID: uid, Title: "Walk", Due: &due, Remind: &remind, RRule: "FREQ=DAILY"})
	if err != nil {
		t.Fatal(err)
	}
	_, next := complete(t, s, uid, task.ID)
	if next == nil || !next.Due.Equal(at(ny, 2026, 11, 1, 9, 0)) || !next.Remind.Equal(at(ny, 2026, 11, 1, 8, 45)) {
		t.Fatalf("across DST: %+v", next)
	}
}

func TestRepeatingTaskCountAndUntil(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	sh := zone(t, "Asia/Shanghai")
	s.SetLocation(sh)

	due := at(sh, 2026, 10, 12, 9, 0)
	task, err := s.CreateTask(ctx, Task{UserID: uid, Title: "Three times", Due: &due, RRule: "FREQ=DAILY;COUNT=3"})
	if err != nil {
		t.Fatal(err)
	}
	_, n2 := complete(t, s, uid, task.ID)
	_, n3 := complete(t, s, uid, n2.ID)
	if n3 == nil || n3.Occurrence != 3 || n3.RRule != "FREQ=DAILY;COUNT=3" {
		t.Fatalf("third: %+v", n3)
	}
	if _, n4 := complete(t, s, uid, n3.ID); n4 != nil {
		t.Fatalf("COUNT=3 made a fourth: %+v", n4)
	}

	due = at(sh, 2026, 10, 12, 21, 0)
	task, err = s.CreateTask(ctx, Task{UserID: uid, Title: "Until", Due: &due, RRule: "FREQ=DAILY;UNTIL=20261013T235959Z"})
	if err != nil {
		t.Fatal(err)
	}
	_, n2 = complete(t, s, uid, task.ID)
	if n2 == nil || !n2.Due.Equal(at(sh, 2026, 10, 13, 21, 0)) {
		t.Fatalf("until, second: %+v", n2)
	}
	if _, n3 := complete(t, s, uid, n2.ID); n3 != nil {
		t.Fatalf("past UNTIL: %+v", n3)
	}

	open, _ := s.ListTasks(ctx, uid, TaskFilter{State: "open", Limit: 10})
	if len(open) != 0 {
		t.Fatalf("open after both series ended: %+v", open)
	}

	// A new rule starts a new series.
	due = at(sh, 2026, 10, 20, 9, 0)
	task, err = s.CreateTask(ctx, Task{UserID: uid, Title: "Twice", Due: &due, RRule: "FREQ=DAILY;COUNT=2"})
	if err != nil {
		t.Fatal(err)
	}
	_, n2 = complete(t, s, uid, task.ID)
	rule := "FREQ=DAILY;COUNT=2"
	if n2.Occurrence != 2 {
		t.Fatalf("occurrence %d", n2.Occurrence)
	}
	other := "FREQ=WEEKLY;COUNT=2"
	if got, err := s.UpdateTask(ctx, uid, n2.ID, TaskPatch{RRule: &other}); err != nil || got.Occurrence != 1 {
		t.Fatalf("changing the rule: %v %+v", err, got)
	}
	if got, err := s.UpdateTask(ctx, uid, n2.ID, TaskPatch{RRule: &rule}); err != nil || got.Occurrence != 1 {
		t.Fatalf("changing it back: %v %+v", err, got)
	}
}

func TestRepeatingTaskCompletedByWork(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	sh := zone(t, "Asia/Shanghai")
	s.SetLocation(sh)
	due := at(sh, 2026, 10, 12, 18, 0)
	task, err := s.CreateTask(ctx, Task{UserID: uid, Title: "Weekly report", Due: &due, RRule: "FREQ=WEEKLY"})
	if err != nil {
		t.Fatal(err)
	}
	hundred := 100
	got, err := s.LogTaskWork(ctx, uid, task.ID, time.Time{}, 30, &hundred, "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Done || got.ID != task.ID {
		t.Fatalf("progress 100 returns the completed task: %+v", got)
	}
	next := follower(t, s, uid, task.ID)
	if next == nil || !next.Due.Equal(at(sh, 2026, 10, 19, 18, 0)) || next.Progress != 0 || next.WorkSessions != 0 {
		t.Fatalf("follower: %+v", next)
	}
	// Logging 100% again on the done task makes nothing new.
	if _, err := s.LogTaskWork(ctx, uid, task.ID, time.Time{}, 5, &hundred, ""); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListTasks(ctx, uid, TaskFilter{State: "all", Limit: 10})
	if len(all) != 2 {
		t.Fatalf("tasks: %d", len(all))
	}
}

func TestRepeatingTaskValidation(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	due := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		task Task
	}{
		{"bad rule", Task{UserID: uid, Title: "x", Due: &due, RRule: "FREQ=HOURLY"}},
		{"bad part", Task{UserID: uid, Title: "x", Due: &due, RRule: "FREQ=DAILY;BYMONTH=3"}},
		{"BYDAY not weekly", Task{UserID: uid, Title: "x", Due: &due, RRule: "FREQ=DAILY;BYDAY=MO"}},
		{"no due", Task{UserID: uid, Title: "x", RRule: "FREQ=DAILY"}},
	} {
		if _, err := s.CreateTask(ctx, c.task); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	task, err := s.CreateTask(ctx, Task{UserID: uid, Title: "x", Due: &due, RRule: "FREQ=DAILY"})
	if err != nil {
		t.Fatal(err)
	}
	var none *time.Time
	if _, err := s.UpdateTask(ctx, uid, task.ID, TaskPatch{Due: &none}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("clearing the due of a repeating task: %v", err)
	}
	empty := ""
	if got, err := s.UpdateTask(ctx, uid, task.ID, TaskPatch{Due: &none, RRule: &empty}); err != nil || got.RRule != "" || got.Due != nil {
		t.Fatalf("making it one-off and undated: %v %+v", err, got)
	}
	if _, next := complete(t, s, uid, task.ID); next != nil {
		t.Fatalf("a one-off task made a follower: %+v", next)
	}
}
