package server_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/ai"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/reminder"
	"github.com/liliang-cn/noted/internal/server"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type env struct {
	st    *store.Store
	hub   *reminder.Hub
	addr  string
	alice string // tokens
	bob   string
}

func start(t *testing.T, engine ai.Engine, authDisabled bool) *env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "noted.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a, err := auth.New(ctx, st, authDisabled)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{st: st, hub: reminder.NewHub()}
	for name, dst := range map[string]*string{"alice": &e.alice, "bob": &e.bob} {
		u, _ := st.EnsureUser(ctx, name)
		*dst, _ = st.CreateToken(ctx, u.ID, "test")
	}
	g := server.New(server.Deps{Store: st, Auth: a, Hub: e.hub, Engine: engine, Location: time.UTC, Reflection: true})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go g.Serve(lis)
	t.Cleanup(g.Stop)
	e.addr = lis.Addr().String()
	return e
}

// restart serves the same store and tokens again, now with an AI engine.
func restart(t *testing.T, old *env, engine ai.Engine) *env {
	t.Helper()
	a, err := auth.New(context.Background(), old.st, false)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{st: old.st, hub: old.hub, alice: old.alice, bob: old.bob}
	g := server.New(server.Deps{Store: old.st, Auth: a, Hub: old.hub, Engine: engine, Location: time.UTC})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go g.Serve(lis)
	t.Cleanup(g.Stop)
	e.addr = lis.Addr().String()
	return e
}

func (e *env) conn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	c, err := grpc.NewClient(e.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func as(token string) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Second, cancel) // a hung call fails the test instead of hanging it
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

func code(err error) codes.Code { return status.Code(err) }

func TestAuthRequired(t *testing.T) {
	e := start(t, nil, false)
	notes := pb.NewNoteServiceClient(e.conn(t))
	if _, err := notes.ListNotes(context.Background(), &pb.ListNotesRequest{}); code(err) != codes.Unauthenticated {
		t.Fatalf("no token: %v", err)
	}
	if _, err := notes.ListNotes(as("noted_bogus"), &pb.ListNotesRequest{}); code(err) != codes.Unauthenticated {
		t.Fatalf("bad token: %v", err)
	}
	if _, err := notes.ListNotes(as(e.alice), &pb.ListNotesRequest{}); err != nil {
		t.Fatalf("good token: %v", err)
	}
}

func TestAuthDisabledSingleUser(t *testing.T) {
	e := start(t, nil, true)
	notes := pb.NewNoteServiceClient(e.conn(t))
	if _, err := notes.CreateNote(context.Background(), &pb.CreateNoteRequest{Note: &pb.Note{Title: "x"}}); err != nil {
		t.Fatalf("auth-disabled create: %v", err)
	}
}

func TestNotesFlow(t *testing.T) {
	e := start(t, nil, false)
	c := pb.NewNoteServiceClient(e.conn(t))
	ctx := as(e.alice)

	n, err := c.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "Trip", Content: "book flights to Osaka", Tags: []string{"Travel"}}})
	if err != nil {
		t.Fatal(err)
	}
	if n.Id == "" || n.Tags[0] != "travel" {
		t.Fatalf("%+v", n)
	}
	// Update with a mask changes only the masked fields.
	up, err := c.UpdateNote(ctx, &pb.UpdateNoteRequest{
		Note: &pb.Note{Id: n.Id, Title: "ignored", Pinned: true}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"pinned"}}})
	if err != nil || !up.Pinned || up.Title != "Trip" {
		t.Fatalf("%+v %v", up, err)
	}
	if _, err := c.UpdateNote(ctx, &pb.UpdateNoteRequest{Note: &pb.Note{Id: n.Id}}); code(err) != codes.InvalidArgument {
		t.Fatalf("missing mask: %v", err)
	}
	if _, err := c.UpdateNote(ctx, &pb.UpdateNoteRequest{Note: &pb.Note{Id: n.Id},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"id"}}}); code(err) != codes.InvalidArgument {
		t.Fatalf("bad mask path: %v", err)
	}

	res, err := c.SearchNotes(ctx, &pb.SearchNotesRequest{Query: "Osaka", Semantic: true})
	if err != nil || len(res.Hits) != 1 || res.Mode != "fulltext" {
		t.Fatalf("search should fall back to fulltext without AI: %+v %v", res, err)
	}

	// Pagination.
	for i := 0; i < 5; i++ {
		c.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "n", Content: "c"}})
	}
	seen := 0
	tok := ""
	for pages := 0; pages < 10; pages++ {
		l, err := c.ListNotes(ctx, &pb.ListNotesRequest{PageSize: 2, PageToken: tok})
		if err != nil {
			t.Fatal(err)
		}
		seen += len(l.Notes)
		if tok = l.NextPageToken; tok == "" {
			break
		}
	}
	if seen != 6 {
		t.Fatalf("paged through %d notes, want 6", seen)
	}
	if _, err := c.ListNotes(ctx, &pb.ListNotesRequest{PageToken: "!!!"}); code(err) != codes.InvalidArgument {
		t.Fatalf("bad token: %v", err)
	}

	if _, err := c.DeleteNote(ctx, &pb.DeleteNoteRequest{Id: n.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetNote(ctx, &pb.GetNoteRequest{Id: n.Id}); code(err) != codes.NotFound {
		t.Fatalf("after delete: %v", err)
	}
}

func TestUsersCannotSeeEachOther(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	notes, cal := pb.NewNoteServiceClient(conn), pb.NewCalendarServiceClient(conn)

	n, _ := notes.CreateNote(as(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "private"}})
	ev, _ := cal.CreateEvent(as(e.alice), &pb.CreateEventRequest{Event: &pb.Event{Title: "private", StartTime: timestamppb.Now()}})
	tk, _ := cal.CreateTask(as(e.alice), &pb.CreateTaskRequest{Task: &pb.Task{Title: "private"}})

	if _, err := notes.GetNote(as(e.bob), &pb.GetNoteRequest{Id: n.Id}); code(err) != codes.NotFound {
		t.Errorf("note leaked: %v", err)
	}
	if _, err := cal.GetEvent(as(e.bob), &pb.GetEventRequest{Id: ev.Id}); code(err) != codes.NotFound {
		t.Errorf("event leaked: %v", err)
	}
	if _, err := cal.GetTask(as(e.bob), &pb.GetTaskRequest{Id: tk.Id}); code(err) != codes.NotFound {
		t.Errorf("task leaked: %v", err)
	}
	if _, err := cal.DeleteTask(as(e.bob), &pb.DeleteTaskRequest{Id: tk.Id}); code(err) != codes.NotFound {
		t.Errorf("bob deleted alice's task: %v", err)
	}
}

func TestCalendarFlow(t *testing.T) {
	e := start(t, nil, false)
	c := pb.NewCalendarServiceClient(e.conn(t))
	ctx := as(e.alice)
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	five := int32(5)

	ev, err := c.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{
		Title: "Standup", StartTime: timestamppb.New(start), Rrule: "FREQ=WEEKLY;BYDAY=MO,WE,FR", RemindBeforeMinutes: &five}})
	if err != nil {
		t.Fatal(err)
	}
	if ev.RemindBeforeMinutes == nil || *ev.RemindBeforeMinutes != 5 {
		t.Fatalf("reminder lost: %+v", ev)
	}
	l, err := c.ListEvents(ctx, &pb.ListEventsRequest{From: timestamppb.New(start), To: timestamppb.New(start.AddDate(0, 0, 14))})
	if err != nil || len(l.Occurrences) != 6 {
		t.Fatalf("want 6 standups in two weeks, got %d (%v)", len(l.Occurrences), err)
	}

	// Clearing the reminder through the mask.
	cleared, err := c.UpdateEvent(ctx, &pb.UpdateEventRequest{Event: &pb.Event{Id: ev.Id},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"remind_before_minutes"}}})
	if err != nil || cleared.RemindBeforeMinutes != nil {
		t.Fatalf("reminder not cleared: %+v %v", cleared, err)
	}
	if _, err := c.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{Title: "bad", StartTime: timestamppb.New(start), Rrule: "nope"}}); code(err) != codes.InvalidArgument {
		t.Fatalf("bad rrule: %v", err)
	}
	if _, err := c.ListEvents(ctx, &pb.ListEventsRequest{}); code(err) != codes.InvalidArgument {
		t.Fatalf("missing range: %v", err)
	}

	tk, err := c.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Pay rent", Priority: pb.Priority_PRIORITY_HIGH}})
	if err != nil {
		t.Fatal(err)
	}
	done, err := c.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: tk.Id, Completed: true},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"completed"}}})
	if err != nil || !done.Completed || done.CompleteTime == nil {
		t.Fatalf("%+v %v", done, err)
	}
	open, _ := c.ListTasks(ctx, &pb.ListTasksRequest{})
	all, _ := c.ListTasks(ctx, &pb.ListTasksRequest{Filter: pb.ListTasksRequest_FILTER_ALL})
	if len(open.Tasks) != 0 || len(all.Tasks) != 1 {
		t.Fatalf("open=%d all=%d", len(open.Tasks), len(all.Tasks))
	}
}

func TestWatchRemindersStreamsFiredReminders(t *testing.T) {
	e := start(t, nil, false)
	c := pb.NewCalendarServiceClient(e.conn(t))
	ctx, cancel := context.WithCancel(as(e.alice))
	defer cancel()

	// Alice has a task whose reminder is already due.
	past := timestamppb.New(time.Now().Add(-time.Minute))
	if _, err := c.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Call the bank", RemindTime: past}}); err != nil {
		t.Fatal(err)
	}
	stream, err := c.WatchReminders(ctx, &pb.WatchRemindersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan *pb.Reminder, 1)
	go func() {
		r, err := stream.Recv()
		if err == nil {
			got <- r
		}
	}()
	// A reminder fires once, so make sure the stream is subscribed before the tick.
	aliceID := func() string { u, _ := e.st.EnsureUser(context.Background(), "alice"); return u.ID }()
	for i := 0; e.hub.Subscribers(aliceID) == 0; i++ {
		if i > 200 {
			t.Fatal("stream never subscribed")
		}
		time.Sleep(25 * time.Millisecond)
	}
	(&reminder.Scheduler{Store: e.st, Hub: e.hub}).Tick(context.Background(), time.Now())
	select {
	case r := <-got:
		if r.Title != "Call the bank" || r.Kind != pb.Reminder_KIND_TASK {
			t.Fatalf("%+v", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reminder never streamed")
	}
	hist, err := c.ListReminders(ctx, &pb.ListRemindersRequest{})
	if err != nil || len(hist.Reminders) != 1 {
		t.Fatalf("history: %+v %v", hist, err)
	}
}

func TestAIDisabled(t *testing.T) {
	e := start(t, nil, false)
	c := pb.NewAIServiceClient(e.conn(t))
	st, err := c.GetStatus(as(e.alice), &pb.GetStatusRequest{})
	if err != nil || st.Enabled || st.Chat || st.SemanticSearch {
		t.Fatalf("%+v %v", st, err)
	}
	for name, call := range map[string]func() error{
		"ask": func() error { _, err := c.Ask(as(e.alice), &pb.AskRequest{Message: "hi"}); return err },
		"summary": func() error {
			_, err := c.SummarizeNote(as(e.alice), &pb.SummarizeNoteRequest{NoteId: "x"})
			return err
		},
		"tags":     func() error { _, err := c.SuggestTags(as(e.alice), &pb.SuggestTagsRequest{NoteId: "x"}); return err },
		"briefing": func() error { _, err := c.DailyBriefing(as(e.alice), &pb.DailyBriefingRequest{}); return err },
	} {
		if code(call()) != codes.FailedPrecondition {
			t.Errorf("%s: want FailedPrecondition, got %v", name, call())
		}
	}
}
