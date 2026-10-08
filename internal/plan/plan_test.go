package plan

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

// Thursday 2026-10-08 10:00 UTC.
var now = time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)

func newEnv(t *testing.T) (*store.Store, string, Applier, Suggester) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "noted.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	u, err := s.EnsureUser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	return s, u.ID,
		Applier{Store: s, Loc: time.UTC, Now: func() time.Time { return now }},
		Suggester{Store: s, Loc: time.UTC, Locale: EN}
}

func at(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC) }

func TestApplyCreatesInOrderAndResolvesReferences(t *testing.T) {
	ctx := context.Background()
	s, uid, ap, _ := newEnv(t)
	ops := []Op{
		{Type: OpCreateProject, Label: "project", Args: map[string]any{"title": "Trip", "start": map[string]any{"input": "departure"}, "space": "life"}},
		{Type: OpCreateTask, Label: "visa", Args: map[string]any{"title": "Visa", "project_id": "$op0", "due": map[string]any{"input": "departure", "days_before": 30.0, "hour": 9.0}}},
		{Type: OpCreateTask, Label: "hotel", Args: map[string]any{"title": "Hotel", "project_id": "$op0"}},
	}
	entries, err := ap.Apply(ctx, uid, ops, nil, map[string]string{"departure": "2026-11-20"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("%d entries", len(entries))
	}
	ps, _ := s.ListProjects(ctx, uid, store.ProjectFilter{}, now, time.UTC)
	if len(ps) != 1 || ps[0].Project.Start == nil || ps[0].Project.Start.Format("2006-01-02") != "2026-11-20" {
		t.Fatalf("%+v", ps)
	}
	tasks, _ := s.ListTasks(ctx, uid, store.TaskFilter{State: "all", ProjectID: ps[0].Project.ID, Limit: 10})
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	for _, tk := range tasks {
		if tk.Title == "Visa" && (tk.Due == nil || tk.Due.Format("2006-01-02 15") != "2026-10-21 09") {
			t.Fatalf("visa due = %v, want 30 days before the departure at 09:00", tk.Due)
		}
		if tk.Space != store.SpaceLife {
			t.Fatalf("task should inherit the project's space: %q", tk.Space)
		}
	}
}

func TestApplyNeedsRequiredInputs(t *testing.T) {
	ctx := context.Background()
	s, uid, ap, _ := newEnv(t)
	ops := []Op{{Type: OpCreateProject, Args: map[string]any{"title": "Trip", "start": map[string]any{"input": "departure"}}}}
	_, err := ap.Apply(ctx, uid, ops, nil, nil)
	if !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a missing input must be an invalid-argument error: %v", err)
	}
	if ps, _ := s.ListProjects(ctx, uid, store.ProjectFilter{}, now, time.UTC); len(ps) != 0 {
		t.Fatal("nothing should be written when an input is missing")
	}
}

func TestApplyIsAllOrNothing(t *testing.T) {
	ctx := context.Background()
	s, uid, ap, _ := newEnv(t)
	ops := []Op{
		{Type: OpCreateTask, Label: "ok", Args: map[string]any{"title": "Fine"}},
		{Type: OpCreateTask, Label: "bad", Args: map[string]any{"title": ""}},
	}
	if _, err := ap.Apply(ctx, uid, ops, nil, nil); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("the failing step should be named: %v", err)
	}
	if ts, _ := s.ListTasks(ctx, uid, store.TaskFilter{State: "all", Limit: 10}); len(ts) != 0 {
		t.Fatalf("the first task must be rolled back, found %d", len(ts))
	}
}

func TestApplySelectionAndDependencies(t *testing.T) {
	ctx := context.Background()
	s, uid, ap, _ := newEnv(t)
	ops := []Op{
		{Type: OpCreateTask, Label: "a", Args: map[string]any{"title": "A"}},
		{Type: OpCreateTask, Label: "b", Args: map[string]any{"title": "B"}},
		{Type: OpCreateProject, Label: "p", Args: map[string]any{"title": "P"}},
		{Type: OpCreateTask, Label: "c", Args: map[string]any{"title": "C", "project_id": "$op2"}},
	}
	if _, err := ap.Apply(ctx, uid, ops, map[int]bool{0: true, 2: true}, nil); err != nil {
		t.Fatal(err)
	}
	ts, _ := s.ListTasks(ctx, uid, store.TaskFilter{State: "all", Limit: 10})
	if len(ts) != 1 || ts[0].Title != "A" {
		t.Fatalf("only A should exist: %+v", ts)
	}
	// Selecting a step without the one it depends on is refused.
	if _, err := ap.Apply(ctx, uid, ops, map[int]bool{3: true}, nil); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("dependency on a skipped step: %v", err)
	}
}

func TestUndoRestoresUpdatesAndRemovesCreations(t *testing.T) {
	ctx := context.Background()
	s, uid, ap, _ := newEnv(t)
	due := at(7, 17)
	task, _ := s.CreateTask(ctx, store.Task{UserID: uid, Title: "Pay rent", Due: &due})
	ev, _ := s.CreateEvent(ctx, store.Event{UserID: uid, Title: "Review", Start: at(9, 10), End: at(9, 11)})
	goal, _ := s.CreateGoal(ctx, store.Goal{UserID: uid, Title: "Run", Period: "week", Target: 3})

	ops := []Op{
		{Type: OpUpdateTask, Args: map[string]any{"id": task.ID, "due": rfc(at(9, 17))}},
		{Type: OpUpdateEvent, Args: map[string]any{"id": ev.ID, "start": rfc(at(9, 15))}},
		{Type: OpCreateEvent, Args: map[string]any{"title": "Run", "start": rfc(at(10, 9)), "end": rfc(at(10, 10))}},
		{Type: OpCheckIn, Args: map[string]any{"goal_id": goal.ID, "amount": 1.0}},
	}
	entries, err := ap.Apply(ctx, uid, ops, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	gotTask, _ := s.GetTask(ctx, uid, task.ID)
	gotEv, _ := s.GetEvent(ctx, uid, ev.ID)
	if !gotTask.Due.Equal(at(9, 17)) || !gotEv.Start.Equal(at(9, 15)) || !gotEv.End.Equal(at(9, 16)) {
		t.Fatalf("apply did not change them: task=%v event=%v-%v", gotTask.Due, gotEv.Start, gotEv.End)
	}

	if err := ap.Undo(ctx, uid, entries); err != nil {
		t.Fatal(err)
	}
	gotTask, _ = s.GetTask(ctx, uid, task.ID)
	gotEv, _ = s.GetEvent(ctx, uid, ev.ID)
	if !gotTask.Due.Equal(due) || !gotEv.Start.Equal(at(9, 10)) || !gotEv.End.Equal(at(9, 11)) {
		t.Fatalf("undo did not restore: task=%v event=%v-%v", gotTask.Due, gotEv.Start, gotEv.End)
	}
	occ, _ := s.ListEvents(ctx, uid, at(10, 0), at(11, 0), "")
	if len(occ) != 0 {
		t.Fatal("the created event should be gone")
	}
	v, _ := s.ViewGoal(ctx, goal, now)
	if v.Progress.Done != 0 {
		t.Fatalf("the check-in should be gone: %v", v.Progress.Done)
	}
	// Undoing twice is harmless: things that are gone are skipped.
	if err := ap.Undo(ctx, uid, entries); err != nil {
		t.Fatalf("second undo: %v", err)
	}
}

func TestOpsRoundTripThroughJSON(t *testing.T) {
	ops := []Op{{Type: OpCreateTask, Label: "x", Args: map[string]any{"title": "T", "priority": 2.0, "tags": []any{"a", "b"}}}}
	back, err := DecodeOps(EncodeOps(ops))
	if err != nil || len(back) != 1 || back[0].Args["title"] != "T" {
		t.Fatalf("%+v %v", back, err)
	}
	if in, err := DecodeInputs(EncodeInputs(nil)); err != nil || len(in) != 0 {
		t.Fatalf("%v %v", in, err)
	}
}

// ---- free slots ----

func TestFreeSlotAvoidsEventsAndRespectsHours(t *testing.T) {
	blocks := []busy{{at(8, 10), at(8, 12)}, {at(8, 12), at(8, 13)}}
	// From 10:00 on Oct 8, two back-to-back blocks end at 13:00.
	got, ok := freeSlot(blocks, time.UTC, at(8, 10), 3, time.Hour, 9, 21)
	if !ok || !got.Equal(at(8, 13)) {
		t.Fatalf("got %v %v, want 13:00", got, ok)
	}
	// Too long for the rest of the day: moves to 09:00 the next day.
	got, ok = freeSlot(blocks, time.UTC, at(8, 10), 3, 12*time.Hour, 9, 21)
	if !ok || !got.Equal(at(9, 9)) {
		t.Fatalf("got %v %v, want tomorrow 09:00", got, ok)
	}
	// Nothing fits.
	if _, ok := freeSlot(blocks, time.UTC, at(8, 10), 3, 13*time.Hour, 9, 21); ok {
		t.Fatal("a 13-hour slot cannot fit inside 09:00-21:00")
	}
	// Starts on the next half hour, never in the past.
	got, _ = freeSlot(nil, time.UTC, time.Date(2026, 10, 8, 10, 10, 0, 0, time.UTC), 1, time.Hour, 9, 21)
	if !got.Equal(time.Date(2026, 10, 8, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("got %v", got)
	}
}

func TestAllDayEventsDoNotBlock(t *testing.T) {
	blocks := busyFrom([]store.Occurrence{{Event: store.Event{AllDay: true}, Start: at(8, 0), End: at(9, 0)}})
	if len(blocks) != 0 {
		t.Fatal("an all-day event should not block time")
	}
}

// ---- rule suggestions ----

func find(drafts []Draft, kind string) []Draft {
	var out []Draft
	for _, d := range drafts {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

func TestSuggestGoalBehindPaceGetsASlotThatAvoidsEvents(t *testing.T) {
	ctx := context.Background()
	s, uid, _, sg := newEnv(t)
	g, _ := s.CreateGoal(ctx, store.Goal{UserID: uid, Title: "Exercise", Period: "week", Target: 3, Space: store.SpaceLife,
		Created: now.AddDate(0, -2, 0)})
	_ = g
	// A meeting occupies the next free half hours so the slot has to move past it.
	s.CreateEvent(ctx, store.Event{UserID: uid, Title: "Meeting", Start: at(8, 10), End: at(8, 13)})

	ds, err := sg.Rules(ctx, uid, now)
	if err != nil {
		t.Fatal(err)
	}
	slots := find(ds, KindGoalSlot)
	if len(slots) != 1 {
		t.Fatalf("want one goal suggestion, got %d: %+v", len(slots), ds)
	}
	d := slots[0]
	if d.Space != store.SpaceLife || len(d.Ops) != 1 || d.Ops[0].Type != OpCreateEvent {
		t.Fatalf("%+v", d)
	}
	start, _ := time.Parse(time.RFC3339, d.Ops[0].Args["start"].(string))
	if !start.Equal(at(8, 13)) {
		t.Fatalf("slot = %v, want right after the meeting (13:00)", start)
	}
	if !strings.Contains(d.Title, "Exercise") || d.Fingerprint == "" {
		t.Fatalf("%+v", d)
	}
}

func TestSuggestSkipsGoalsOnPace(t *testing.T) {
	ctx := context.Background()
	s, uid, _, sg := newEnv(t)
	g, _ := s.CreateGoal(ctx, store.Goal{UserID: uid, Title: "Exercise", Period: "week", Target: 3, Created: now.AddDate(0, -2, 0)})
	s.RecordCheckIn(ctx, uid, g.ID, 1, 0, at(6, 8), "", now)
	if ds, _ := sg.Rules(ctx, uid, now); len(find(ds, KindGoalSlot)) != 0 {
		t.Fatal("a goal that is on pace needs no suggestion")
	}
}

func TestSuggestProjectDatesForUndatedTasks(t *testing.T) {
	ctx := context.Background()
	s, uid, _, sg := newEnv(t)
	depart := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	p, _ := s.CreateProject(ctx, store.Project{UserID: uid, Title: "Trip", Start: &depart})
	s.CreateTask(ctx, store.Task{UserID: uid, Title: "Hotel", ProjectID: p.ID})
	dated := at(18, 9)
	s.CreateTask(ctx, store.Task{UserID: uid, Title: "Visa", ProjectID: p.ID, Due: &dated})

	ds, _ := sg.Rules(ctx, uid, now)
	got := find(ds, KindProjectDates)
	if len(got) != 1 || len(got[0].Ops) != 1 {
		t.Fatalf("only the undated task needs a date: %+v", got)
	}
	due, _ := time.Parse(time.RFC3339, got[0].Ops[0].Args["due"].(string))
	if due.Format("2006-01-02 15:04") != "2026-11-06 09:00" {
		t.Fatalf("due = %v, want 14 days before the departure", due)
	}

	// A project with no anchor date gets nothing.
	s.CreateProject(ctx, store.Project{UserID: uid, Title: "Someday"})
	if again, _ := sg.Rules(ctx, uid, now); len(find(again, KindProjectDates)) != 1 {
		t.Fatal("undated projects must not produce suggestions")
	}
}

func TestSuggestConflictMovesTheOneThatIsNotRecurring(t *testing.T) {
	ctx := context.Background()
	s, uid, _, sg := newEnv(t)
	s.CreateEvent(ctx, store.Event{UserID: uid, Title: "Standup", Start: at(9, 10), End: at(9, 11), RRule: "FREQ=DAILY;COUNT=3"})
	checkup, _ := s.CreateEvent(ctx, store.Event{UserID: uid, Title: "Check-up", Start: at(9, 10), End: at(9, 11)})

	ds, _ := sg.Rules(ctx, uid, now)
	got := find(ds, KindConflict)
	if len(got) != 1 {
		t.Fatalf("want one conflict, got %+v", ds)
	}
	op := got[0].Ops[0]
	if op.Type != OpUpdateEvent || op.Args["id"] != checkup.ID {
		t.Fatalf("the non-recurring event should move: %+v", op)
	}
	moved, _ := time.Parse(time.RFC3339, op.Args["start"].(string))
	if !moved.Equal(at(9, 11)) {
		t.Fatalf("moved to %v, want the next free half hour (11:00)", moved)
	}
}

func TestSuggestOverdueTaskMovesToTomorrow(t *testing.T) {
	ctx := context.Background()
	s, uid, _, sg := newEnv(t)
	due := at(6, 17)
	task, _ := s.CreateTask(ctx, store.Task{UserID: uid, Title: "Pay rent", Due: &due})
	s.CreateTask(ctx, store.Task{UserID: uid, Title: "Later", Due: ptr(at(12, 9))})

	ds, _ := sg.Rules(ctx, uid, now)
	got := find(ds, KindOverdue)
	if len(got) != 1 || got[0].Ops[0].Args["id"] != task.ID {
		t.Fatalf("%+v", got)
	}
	to, _ := time.Parse(time.RFC3339, got[0].Ops[0].Args["due"].(string))
	if !to.Equal(at(9, 17)) {
		t.Fatalf("due = %v, want tomorrow at the same time", to)
	}
	if !strings.Contains(got[0].Title, "2 days") {
		t.Fatalf("title = %q", got[0].Title)
	}
}

func ptr[T any](v T) *T { return &v }

func TestSuggestionsAreStableAcrossRuns(t *testing.T) {
	ctx := context.Background()
	s, uid, _, sg := newEnv(t)
	s.CreateTask(ctx, store.Task{UserID: uid, Title: "Pay rent", Due: ptr(at(6, 17))})
	a, _ := sg.Rules(ctx, uid, now)
	b, _ := sg.Rules(ctx, uid, now.Add(2*time.Hour))
	if len(a) != 1 || len(b) != 1 || a[0].Fingerprint != b[0].Fingerprint {
		t.Fatalf("fingerprints must not drift between runs: %v vs %v", a, b)
	}
}

func TestChineseWording(t *testing.T) {
	ctx := context.Background()
	s, uid, _, _ := newEnv(t)
	s.CreateTask(ctx, store.Task{UserID: uid, Title: "交房租", Due: ptr(at(6, 17))})
	ds, _ := Suggester{Store: s, Loc: time.UTC, Locale: ZH}.Rules(ctx, uid, now)
	if len(ds) != 1 || !strings.Contains(ds[0].Title, "已逾期") || !strings.Contains(ds[0].Ops[0].Label, "周日") && !strings.Contains(ds[0].Ops[0].Label, "周五") {
		t.Fatalf("%+v", ds)
	}
}

// ---- scheduling ----

func TestScheduleFillsFreeTimeInOrder(t *testing.T) {
	ctx := context.Background()
	s, uid, _, sg := newEnv(t)
	s.CreateEvent(ctx, store.Event{UserID: uid, Title: "Meeting", Start: at(8, 10), End: at(8, 12)})
	a, _ := s.CreateTask(ctx, store.Task{UserID: uid, Title: "Budget", Space: store.SpaceWork})
	b, _ := s.CreateTask(ctx, store.Task{UserID: uid, Title: "JD", Space: store.SpaceWork})

	d, err := sg.Schedule(ctx, uid, []string{a.ID, b.ID}, time.Hour, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Ops) != 2 || d.Space != store.SpaceWork {
		t.Fatalf("%+v", d)
	}
	s1, _ := time.Parse(time.RFC3339, d.Ops[0].Args["start"].(string))
	s2, _ := time.Parse(time.RFC3339, d.Ops[1].Args["start"].(string))
	if !s1.Equal(at(8, 12)) || !s2.Equal(at(8, 13)) {
		t.Fatalf("slots %v and %v: want 12:00 then 13:00, never overlapping", s1, s2)
	}
	if !strings.Contains(d.Title, "2") {
		t.Fatalf("title = %q", d.Title)
	}
}

func TestScheduleErrors(t *testing.T) {
	ctx := context.Background()
	s, uid, _, sg := newEnv(t)
	if _, err := sg.Schedule(ctx, uid, nil, time.Hour, 3, now); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("no tasks: %v", err)
	}
	if _, err := sg.Schedule(ctx, uid, []string{"nope"}, time.Hour, 3, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown task: %v", err)
	}
	tk, _ := s.CreateTask(ctx, store.Task{UserID: uid, Title: "Huge"})
	if _, err := sg.Schedule(ctx, uid, []string{tk.ID}, 14*time.Hour, 2, now); !errors.Is(err, ErrNoSlot) {
		t.Fatalf("nothing fits: %v", err)
	}
	other, _ := s.EnsureUser(ctx, "bob")
	if _, err := sg.Schedule(ctx, other.ID, []string{tk.ID}, time.Hour, 2, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("another user's task: %v", err)
	}
}

// ---- weekly review ----

func TestWeeklyReview(t *testing.T) {
	ctx := context.Background()
	s, uid, _, sg := newEnv(t)
	// This week: Mon Oct 5 - Sun Oct 11. Now is Thursday.
	done, _ := s.CreateTask(ctx, store.Task{UserID: uid, Title: "Done one", Due: ptr(at(6, 9))})
	yes := true
	s.UpdateTask(ctx, uid, done.ID, store.TaskPatch{Done: &yes})
	s.CreateTask(ctx, store.Task{UserID: uid, Title: "Slipped", Due: ptr(at(7, 9)), Space: store.SpaceWork})
	s.CreateTask(ctx, store.Task{UserID: uid, Title: "Next week's", Due: ptr(at(14, 9))})
	g, _ := s.CreateGoal(ctx, store.Goal{UserID: uid, Title: "Run", Period: "week", Target: 3, Created: now.AddDate(0, -2, 0)})
	s.RecordCheckIn(ctx, uid, g.ID, 1, 0, at(6, 8), "", now)

	r, err := sg.WeeklyReview(ctx, uid, now, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.From.Equal(at(5, 0)) || !r.To.Equal(at(12, 0)) {
		t.Fatalf("week = %v - %v", r.From, r.To)
	}
	if r.TasksDone != 1 || r.TasksTotal != 2 || len(r.Carried) != 1 || r.Carried[0].Title != "Slipped" {
		t.Fatalf("done=%d total=%d carried=%v", r.TasksDone, r.TasksTotal, r.Carried)
	}
	if len(r.Goals) != 1 || r.Goals[0].Progress.Done != 1 {
		t.Fatalf("%+v", r.Goals)
	}
	if r.Next == nil || len(r.Next.Ops) != 2 {
		t.Fatalf("want a carry-over and a goal slot: %+v", r.Next)
	}
	carry, slot := r.Next.Ops[0], r.Next.Ops[1]
	if carry.Type != OpUpdateTask || carry.Args["due"] != rfc(at(12, 9)) {
		t.Fatalf("carry: %+v", carry)
	}
	st, _ := time.Parse(time.RFC3339, slot.Args["start"].(string))
	if slot.Type != OpCreateEvent || st.Before(at(12, 0)) {
		t.Fatalf("goal slot must be next week: %+v", slot)
	}

	// Only work: the life goal and nothing else is left out.
	w, _ := sg.WeeklyReview(ctx, uid, now, store.SpaceWork, now)
	if w.TasksDone != 0 || len(w.Carried) != 1 || len(w.Goals) != 0 {
		t.Fatalf("work only: %+v", w)
	}
}
