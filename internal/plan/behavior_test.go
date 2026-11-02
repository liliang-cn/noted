package plan

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

// A week of words against deeds: the swim target is missed, the study goal is
// met through a time entry's project, and time on something unlinked is named.
func TestWeeklyReviewBehavior(t *testing.T) {
	ctx := context.Background()
	s, uid, _, sg := newEnv(t)
	// The task below is finished "now" by the store's own clock, so the week is the real one.
	rn := time.Now().UTC()
	monday := time.Date(rn.Year(), rn.Month(), rn.Day(), 0, 0, 0, 0, time.UTC)
	monday = monday.AddDate(0, 0, -((int(monday.Weekday()) + 6) % 7))
	// Starts cannot be in the future, so the week's entries all sit just before now.
	at := func(day, hour int) time.Time { return rn.Add(-time.Duration(day*100+hour) * time.Millisecond) }

	obj, err := s.CreateObjective(ctx, store.Objective{UserID: uid, Title: "Fitness"})
	if err != nil {
		t.Fatal(err)
	}
	swim, err := s.CreateGoal(ctx, store.Goal{UserID: uid, Title: "Swim", Period: "week", Target: 2, Unit: "times", ObjectiveID: obj.ID})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := s.CreateProject(ctx, store.Project{UserID: uid, Title: "German"})
	if err != nil {
		t.Fatal(err)
	}
	study, err := s.CreateGoal(ctx, store.Goal{UserID: uid, Title: "Study", Period: "week", Target: 120, Unit: "minutes", ProjectID: proj.ID})
	if err != nil {
		t.Fatal(err)
	}
	idle, err := s.CreateObjective(ctx, store.Objective{UserID: uid, Title: "Write a book"})
	if err != nil {
		t.Fatal(err)
	}
	// One swim of two; 90 minutes of study through the project, 30 more by goal (which checks in 30).
	if _, err := s.RecordCheckIn(ctx, uid, swim.ID, 1, 0, at(6, 8), "", rn); err != nil {
		t.Fatal(err)
	}
	for _, e := range []store.TimeEntry{
		{UserID: uid, Title: "German lesson", Minutes: 90, Start: at(6, 18), ProjectID: proj.ID},
		{UserID: uid, Title: "Vocabulary", Minutes: 30, Start: at(7, 18), GoalID: study.ID},
		{UserID: uid, Title: "Scrolling", Minutes: 540, Start: at(7, 20)},
		{UserID: uid, Title: "scrolling", Minutes: 30, Start: at(8, 7)},
		{UserID: uid, Title: "Tea", Minutes: 20, Start: at(8, 8)},
		{UserID: uid, Title: "Last week", Minutes: 600, Start: monday.Add(-48 * time.Hour)},
	} {
		if _, err := s.CreateTimeEntry(ctx, e, rn); err != nil {
			t.Fatal(err)
		}
	}
	task, err := s.CreateTask(ctx, store.Task{UserID: uid, Title: "Read chapter", ProjectID: proj.ID})
	if err != nil {
		t.Fatal(err)
	}
	done := true
	if _, err := s.UpdateTask(ctx, uid, task.ID, store.TaskPatch{Done: &done}); err != nil {
		t.Fatal(err)
	}

	r, err := sg.WeeklyReview(ctx, uid, rn, "", rn)
	if err != nil {
		t.Fatal(err)
	}
	b := r.Behavior
	rows := map[string]BehaviorRow{}
	for _, x := range b.Rows {
		rows[x.Title] = x
	}
	if len(b.Rows) != 4 {
		t.Fatalf("rows: %+v", b.Rows)
	}
	if x := rows["Fitness"]; x.Kind != "objective" || x.CheckIns != 1 || x.Done != 1 || x.Short {
		t.Fatalf("Fitness: %+v", x)
	}
	if x := rows["Swim"]; x.Kind != "goal" || x.Target != 2 || x.Done != 1 || !x.Short {
		t.Fatalf("Swim: %+v", x)
	}
	if x := rows["Study"]; x.TimeMinutes != 120 || x.TasksDone != 1 || x.Done != 30 || !x.Short {
		t.Fatalf("Study: %+v", x)
	}
	if x := rows["Write a book"]; !x.Short || x.TimeMinutes != 0 {
		t.Fatalf("Write a book (%s): %+v", idle.ID, x)
	}

	// Unlinked: scrolling is folded into one place; last week's entry is not this week's.
	if b.UnlinkedMinutes != 590 || len(b.Unlinked) != 2 || b.Unlinked[0].Minutes != 570 || b.Unlinked[0].Count != 2 {
		t.Fatalf("unlinked: %+v (%d)", b.Unlinked, b.UnlinkedMinutes)
	}
	want := []string{"Swim 1/2 times", "Nothing went into \"Write a book\" this week", "Time linked to no goal: Scrolling 9.5h"}
	want = append([]string{"Study 30/120 minutes"}, want...)
	got := strings.Join(b.Mismatches, "|")
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Fatalf("mismatches %q lack %q", got, w)
		}
	}
	if strings.Contains(got, "Tea") {
		t.Fatalf("20 minutes is not worth naming: %q", got)
	}
}

func TestWeeklyReviewBehaviorEmpty(t *testing.T) {
	_, uid, _, sg := newEnv(t)
	r, err := sg.WeeklyReview(context.Background(), uid, now, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Behavior.Rows) != 0 || len(r.Behavior.Mismatches) != 0 || r.Behavior.UnlinkedMinutes != 0 {
		t.Fatalf("%+v", r.Behavior)
	}
}

func TestHours(t *testing.T) {
	for m, want := range map[int]string{40: "40m", 60: "1h", 90: "1.5h", 540: "9h"} {
		if got := hours(m); got != want {
			t.Fatalf("hours(%d) = %s, want %s", m, got, want)
		}
	}
}
