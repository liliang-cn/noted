package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "noted.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	u, err := s.EnsureUser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	return s, u.ID
}

func TestNoteCRUDAndTags(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	n, err := s.CreateNote(ctx, Note{UserID: uid, Title: "Groceries", Content: "milk, eggs", Tags: []string{"#Home", "home", " errands "}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNote(ctx, uid, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "errands" || got.Tags[1] != "home" {
		t.Fatalf("tags = %v", got.Tags)
	}
	title := "Shopping"
	tags := []string{"shop"}
	up, err := s.UpdateNote(ctx, uid, n.ID, NotePatch{Title: &title, Tags: &tags})
	if err != nil || up.Title != "Shopping" || len(up.Tags) != 1 {
		t.Fatalf("update: %+v %v", up, err)
	}
	if err := s.DeleteNote(ctx, uid, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNote(ctx, uid, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.DeleteNote(ctx, uid, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestNotesAreIsolatedPerUser(t *testing.T) {
	ctx := context.Background()
	s, alice := newStore(t)
	bob, _ := s.EnsureUser(ctx, "bob")
	n, _ := s.CreateNote(ctx, Note{UserID: alice, Title: "secret plan", Content: "x"})
	if _, err := s.GetNote(ctx, bob.ID, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob read alice's note: %v", err)
	}
	if err := s.DeleteNote(ctx, bob.ID, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob deleted alice's note: %v", err)
	}
	hits, err := s.SearchNotes(ctx, bob.ID, "secret", 10, false, "")
	if err != nil || len(hits) != 0 {
		t.Fatalf("bob search: %v %v", hits, err)
	}
}

func TestSearchFullTextAndCJK(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	s.CreateNote(ctx, Note{UserID: uid, Title: "Kubernetes upgrade", Content: "plan the cluster upgrade for next week"})
	s.CreateNote(ctx, Note{UserID: uid, Title: "周会纪要", Content: "讨论了季度报告和预算安排"})
	s.CreateNote(ctx, Note{UserID: uid, Title: "Recipe", Content: "pasta"})

	for _, tc := range []struct {
		q    string
		want int
	}{
		{"cluster", 1},
		{"CLUSTER upgrade", 1}, // case-insensitive, AND of terms
		{"季度报告", 1},            // CJK via trigram
		{"预算", 1},              // two characters: substring fallback
		{"pa", 1},              // short ASCII: substring fallback
		{"nonexistent", 0},
		{`"quoted" OR (weird`, 0}, // operators in user input must not break the query
	} {
		hits, err := s.SearchNotes(ctx, uid, tc.q, 10, false, "")
		if err != nil {
			t.Fatalf("%q: %v", tc.q, err)
		}
		if len(hits) != tc.want {
			t.Errorf("%q: got %d hits, want %d", tc.q, len(hits), tc.want)
		}
	}
	hits, _ := s.SearchNotes(ctx, uid, "cluster", 10, false, "")
	if hits[0].Snippet == "" {
		t.Error("empty snippet")
	}
}

func TestArchivedHiddenFromListAndSearch(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	n, _ := s.CreateNote(ctx, Note{UserID: uid, Title: "old stuff", Content: "legacy content"})
	yes := true
	s.UpdateNote(ctx, uid, n.ID, NotePatch{Archived: &yes})
	if l, _ := s.ListNotes(ctx, uid, NoteFilter{Limit: 10}); len(l) != 0 {
		t.Fatal("archived note listed")
	}
	if l, _ := s.ListNotes(ctx, uid, NoteFilter{Limit: 10, IncludeArchived: true}); len(l) != 1 {
		t.Fatal("archived note missing with include_archived")
	}
	if h, _ := s.SearchNotes(ctx, uid, "legacy", 10, false, ""); len(h) != 0 {
		t.Fatal("archived note searchable")
	}
}

func TestIndexQueue(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	n, _ := s.CreateNote(ctx, Note{UserID: uid, Title: "a", Content: "b"})
	q, _ := s.NotesNeedingIndex(ctx, 10)
	if len(q) != 1 {
		t.Fatalf("queue = %d", len(q))
	}
	s.MarkNoteIndexed(ctx, n.ID, q[0].Updated)
	if q, _ = s.NotesNeedingIndex(ctx, 10); len(q) != 0 {
		t.Fatal("indexed note still queued")
	}
	time.Sleep(2 * time.Millisecond)
	c := "changed"
	s.UpdateNote(ctx, uid, n.ID, NotePatch{Content: &c})
	if q, _ = s.NotesNeedingIndex(ctx, 10); len(q) != 1 {
		t.Fatal("edited note not re-queued")
	}
}

func TestEventValidationAndRange(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	start := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	if _, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "", Start: start}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty title: %v", err)
	}
	if _, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "x", Start: start, End: start.Add(-time.Hour)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("end before start: %v", err)
	}
	if _, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "x", Start: start, RRule: "FREQ=HOURLY"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad rrule: %v", err)
	}
	if _, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "x", Start: start, TimeZone: "Mars/Base"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad zone: %v", err)
	}

	one, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "Dentist", Start: start})
	if err != nil || !one.End.Equal(start.Add(time.Hour)) {
		t.Fatalf("default end: %+v %v", one, err)
	}
	s.CreateEvent(ctx, Event{UserID: uid, Title: "Standup", Start: start, End: start.Add(15 * time.Minute), RRule: "FREQ=DAILY;COUNT=10"})

	occ, err := s.ListEvents(ctx, uid, time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(occ) != 4 { // dentist + standup on 4th, 5th, 6th
		t.Fatalf("got %d occurrences", len(occ))
	}
	if _, err := s.ListEvents(ctx, uid, start, start.AddDate(5, 0, 0), ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("huge range: %v", err)
	}
}

func TestEventOverlappingWindowStart(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	start := time.Date(2026, 5, 4, 22, 0, 0, 0, time.UTC)
	s.CreateEvent(ctx, Event{UserID: uid, Title: "Night shift", Start: start, End: start.Add(8 * time.Hour)})
	occ, _ := s.ListEvents(ctx, uid, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC), "")
	if len(occ) != 1 {
		t.Fatalf("event spanning midnight missing from next day: %d", len(occ))
	}
}

func TestAllDayEventSnapsToLocalMidnight(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	e, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "Holiday", AllDay: true, TimeZone: "Asia/Shanghai",
		Start: time.Date(2026, 10, 1, 15, 30, 0, 0, time.UTC)}) // 23:30 local on Oct 1
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC); !e.Start.Equal(want) {
		t.Fatalf("start = %v, want %v", e.Start, want)
	}
	if e.End.Sub(e.Start) != 24*time.Hour {
		t.Fatalf("duration = %v", e.End.Sub(e.Start))
	}
}

func TestUpdateEventMoveKeepsDuration(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	start := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	e, _ := s.CreateEvent(ctx, Event{UserID: uid, Title: "Long", Start: start, End: start.Add(3 * time.Hour)})
	ns := start.Add(24 * time.Hour)
	up, err := s.UpdateEvent(ctx, uid, e.ID, EventPatch{Start: &ns})
	if err != nil || up.End.Sub(up.Start) != 3*time.Hour {
		t.Fatalf("%+v %v", up, err)
	}
}

func TestTaskLifecycle(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	due := time.Now().Add(24 * time.Hour).UTC()
	a, _ := s.CreateTask(ctx, Task{UserID: uid, Title: "later", Tags: []string{"x"}})
	b, _ := s.CreateTask(ctx, Task{UserID: uid, Title: "soon", Due: &due, Priority: 3})
	open, _ := s.ListTasks(ctx, uid, TaskFilter{Limit: 10})
	if len(open) != 2 || open[0].ID != b.ID {
		t.Fatalf("dated task should sort first: %+v", open)
	}
	yes := true
	done, err := s.UpdateTask(ctx, uid, a.ID, TaskPatch{Done: &yes})
	if err != nil || !done.Done || done.DoneAt == nil {
		t.Fatalf("%+v %v", done, err)
	}
	if o, _ := s.ListTasks(ctx, uid, TaskFilter{Limit: 10}); len(o) != 1 {
		t.Fatal("completed task still open")
	}
	if o, _ := s.ListTasks(ctx, uid, TaskFilter{Limit: 10, State: "done"}); len(o) != 1 {
		t.Fatal("completed task missing from done list")
	}
	if o, _ := s.ListTasks(ctx, uid, TaskFilter{Limit: 10, State: "all", Tag: "x"}); len(o) != 1 {
		t.Fatal("tag filter")
	}
	var none *time.Time
	cleared, _ := s.UpdateTask(ctx, uid, b.ID, TaskPatch{Due: &none})
	if cleared.Due != nil {
		t.Fatal("due not cleared")
	}
}

func TestEventReminderFiresOncePerOccurrence(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	start := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	ten := 10
	s.CreateEvent(ctx, Event{UserID: uid, Title: "Daily sync", Start: start, RRule: "FREQ=DAILY", RemindBefore: &ten})

	// 09:00 the same day: too early for tomorrow's, today's already started.
	if got, _ := s.CollectDueReminders(ctx, start.Add(time.Minute)); len(got) != 0 {
		t.Fatalf("fired after start: %v", got)
	}
	// 08:55 next day: inside the window for the 5th.
	now := start.Add(24*time.Hour - 5*time.Minute)
	got, err := s.CollectDueReminders(ctx, now)
	if err != nil || len(got) != 1 {
		t.Fatalf("want 1 reminder, got %v %v", got, err)
	}
	if want := start.Add(24 * time.Hour); !got[0].Due.Equal(want) {
		t.Fatalf("due = %v, want %v", got[0].Due, want)
	}
	if again, _ := s.CollectDueReminders(ctx, now.Add(time.Minute)); len(again) != 0 {
		t.Fatal("fired twice for the same occurrence")
	}
	// ...and again for the following day.
	if next, _ := s.CollectDueReminders(ctx, now.Add(24*time.Hour)); len(next) != 1 {
		t.Fatal("did not fire for the next occurrence")
	}
	if l, _ := s.ListReminders(ctx, uid, start, 10, ""); len(l) != 2 {
		t.Fatalf("history = %d", len(l))
	}
}

func TestTaskReminderRearmsWhenRescheduled(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	now := time.Now().UTC()
	r := now.Add(-time.Minute)
	task, _ := s.CreateTask(ctx, Task{UserID: uid, Title: "call mum", Remind: &r})
	if got, _ := s.CollectDueReminders(ctx, now); len(got) != 1 {
		t.Fatal("task reminder did not fire")
	}
	if got, _ := s.CollectDueReminders(ctx, now); len(got) != 0 {
		t.Fatal("task reminder fired twice")
	}
	r2 := now.Add(-30 * time.Second)
	rp := &r2
	s.UpdateTask(ctx, uid, task.ID, TaskPatch{Remind: &rp})
	if got, _ := s.CollectDueReminders(ctx, now); len(got) != 1 {
		t.Fatal("rescheduled reminder did not re-arm")
	}
}

func TestTokens(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	tok, err := s.CreateToken(ctx, uid, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.UserForToken(ctx, tok)
	if err != nil || u.ID != uid {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err := s.UserForToken(ctx, tok+"x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad token accepted: %v", err)
	}
}
