package server_test

import (
	"context"
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/ai"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const secret = "SECRET-WORK-CONTENT"

func TestAIAccessDefaultsAndReservedKey(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	c := pb.NewAIServiceClient(conn)
	ctx := as(e.alice)

	a, err := c.GetAIAccess(ctx, &pb.GetAIAccessRequest{})
	if err != nil || !a.AllowWork || !a.AllowLife {
		t.Fatalf("both spaces are allowed until the user says otherwise: %+v %v", a, err)
	}
	a, err = c.SetAIAccess(ctx, &pb.SetAIAccessRequest{AllowWork: false, AllowLife: true})
	if err != nil || a.AllowWork || !a.AllowLife {
		t.Fatalf("%+v %v", a, err)
	}
	if got, _ := c.GetAIAccess(ctx, &pb.GetAIAccessRequest{}); got.AllowWork {
		t.Fatal("setting did not stick")
	}
	// It is per user.
	if b, _ := c.GetAIAccess(as(e.bob), &pb.GetAIAccessRequest{}); !b.AllowWork {
		t.Fatal("alice's setting changed bob's")
	}
	// The generic preference API cannot be used to get around the side effects.
	prefs := pb.NewPreferenceServiceClient(conn)
	if _, err := prefs.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ai.access", Value: `{"work":true,"life":true}`}); code(err) != codes.InvalidArgument {
		t.Fatalf("writing the reserved key: %v", err)
	}
	if _, err := prefs.DeletePreference(ctx, &pb.DeletePreferenceRequest{Key: "ai.access"}); code(err) != codes.InvalidArgument {
		t.Fatalf("deleting the reserved key: %v", err)
	}
	if got, _ := c.GetAIAccess(ctx, &pb.GetAIAccessRequest{}); got.AllowWork {
		t.Fatal("the reserved key was bypassed")
	}
}

func TestAssistantCannotReadASpaceThatIsOff(t *testing.T) {
	f := &fakeProvider{}
	var noteID string
	step := 0
	f.script = func(req map[string]any) (string, []map[string]any) {
		if _, has := toolResult(req); !has {
			return "", []map[string]any{
				toolCall("c1", "list_tasks", map[string]any{}),
				toolCall("c2", "get_note", map[string]any{"id": noteID}),
				toolCall("c3", "search_notes", map[string]any{"query": "SECRET"}),
				toolCall("c4", "list_projects", map[string]any{}),
			}
		}
		step++
		return "done", nil
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	cal, notes := pb.NewCalendarServiceClient(conn), pb.NewNoteServiceClient(conn)
	cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "LIFE-TASK-VISIBLE", Space: pb.Space_SPACE_LIFE}})
	cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "task " + secret, Space: pb.Space_SPACE_WORK}})
	pb.NewProjectServiceClient(conn).CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{Title: "project " + secret, Space: pb.Space_SPACE_WORK}})
	n, _ := notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "note", Content: secret, Space: pb.Space_SPACE_WORK}})
	noteID = n.Id

	ai := pb.NewAIServiceClient(conn)
	if _, err := ai.SetAIAccess(ctx, &pb.SetAIAccessRequest{AllowWork: false, AllowLife: true}); err != nil {
		t.Fatal(err)
	}

	// Asking about work directly is refused outright, before any model call.
	f.mu.Lock()
	before := len(f.chatReqs)
	f.mu.Unlock()
	if _, err := ai.Ask(ctx, &pb.AskRequest{Message: "what's on my work plate?", Space: pb.Space_SPACE_WORK}); code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "work") {
		t.Fatalf("work mode with work off: %v", err)
	}
	f.mu.Lock()
	if len(f.chatReqs) != before {
		t.Fatal("a refused request still reached the model")
	}
	f.mu.Unlock()

	// In combined mode the assistant runs, but sees only life.
	if _, err := ai.Ask(ctx, &pb.AskRequest{Message: "what do I have to do?"}); err != nil {
		t.Fatal(err)
	}
	sent := f.everythingSentToModels()
	if strings.Contains(sent, secret) {
		t.Fatalf("work content reached the model:\n%s", sent)
	}
	if !strings.Contains(sent, "LIFE-TASK-VISIBLE") {
		t.Fatal("the allowed space should still be visible to the assistant")
	}
	// Reading the work note by its id is no way round it.
	if !strings.Contains(sent, "not found") {
		t.Fatalf("get_note on a hidden note should look like not found:\n%s", sent)
	}

	// Turning it back on restores access.
	ai.SetAIAccess(ctx, &pb.SetAIAccessRequest{AllowWork: true, AllowLife: true})
	step = 0
	if _, err := ai.Ask(ctx, &pb.AskRequest{Message: "now everything", Space: pb.Space_SPACE_WORK}); err != nil {
		t.Fatalf("work mode after allowing it again: %v", err)
	}
	if !strings.Contains(f.everythingSentToModels(), secret) {
		t.Fatal("after allowing it again the assistant should see work content")
	}
}

func TestEverythingOffRefusesEveryAIFeature(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "ok", nil }}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	n, _ := pb.NewNoteServiceClient(conn).CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "t", Content: secret, Space: pb.Space_SPACE_WORK}})
	ai := pb.NewAIServiceClient(conn)
	ai.SetAIAccess(ctx, &pb.SetAIAccessRequest{})

	for name, call := range map[string]func() error{
		"ask":      func() error { _, err := ai.Ask(ctx, &pb.AskRequest{Message: "hi"}); return err },
		"briefing": func() error { _, err := ai.DailyBriefing(ctx, &pb.DailyBriefingRequest{}); return err },
		"summary":  func() error { _, err := ai.SummarizeNote(ctx, &pb.SummarizeNoteRequest{NoteId: n.Id}); return err },
		"tags":     func() error { _, err := ai.SuggestTags(ctx, &pb.SuggestTagsRequest{NoteId: n.Id}); return err },
		"extract":  func() error { _, err := ai.ExtractTasks(ctx, &pb.ExtractTasksRequest{NoteId: n.Id}); return err },
	} {
		if err := call(); code(err) != codes.FailedPrecondition {
			t.Errorf("%s: %v", name, err)
		}
	}
	if strings.Contains(f.everythingSentToModels(), secret) {
		t.Fatal("content reached the model although everything is off")
	}
}

func TestSummaryOfAHiddenNoteIsRefusedButOthersWork(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "summary text", nil }}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	notes, ai := pb.NewNoteServiceClient(conn), pb.NewAIServiceClient(conn)
	work, _ := notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "w", Content: secret, Space: pb.Space_SPACE_WORK}})
	life, _ := notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "l", Content: "dinner ideas", Space: pb.Space_SPACE_LIFE}})
	ai.SetAIAccess(ctx, &pb.SetAIAccessRequest{AllowWork: false, AllowLife: true})

	if _, err := ai.SummarizeNote(ctx, &pb.SummarizeNoteRequest{NoteId: work.Id}); code(err) != codes.FailedPrecondition {
		t.Fatalf("work note: %v", err)
	}
	if r, err := ai.SummarizeNote(ctx, &pb.SummarizeNoteRequest{NoteId: life.Id}); err != nil || r.Summary != "summary text" {
		t.Fatalf("life note: %+v %v", r, err)
	}
	if strings.Contains(f.everythingSentToModels(), secret) {
		t.Fatal("the work note was sent to the model")
	}
	// Moving the note into the open space is the user's call, and then it works.
	notes.UpdateNote(ctx, &pb.UpdateNoteRequest{Note: &pb.Note{Id: work.Id, Space: pb.Space_SPACE_LIFE}, UpdateMask: mask("space")})
	if _, err := ai.SummarizeNote(ctx, &pb.SummarizeNoteRequest{NoteId: work.Id}); err != nil {
		t.Fatalf("after moving it: %v", err)
	}
}

func TestBriefingLeavesOutASpaceThatIsOff(t *testing.T) {
	f := &fakeProvider{}
	var prompt string
	f.script = func(req map[string]any) (string, []map[string]any) {
		b, _ := jsonString(req)
		if strings.Contains(b, "morning briefing") {
			prompt = b
			return "briefing", nil
		}
		return "ok", nil
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	day := time.Date(2031, 3, 3, 0, 0, 0, 0, time.UTC)
	cal := pb.NewCalendarServiceClient(conn)
	cal.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{Title: "Dentist", StartTime: timestamppb.New(day.Add(9 * time.Hour)), Space: pb.Space_SPACE_LIFE}})
	cal.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{Title: "Board " + secret, StartTime: timestamppb.New(day.Add(10 * time.Hour)), Space: pb.Space_SPACE_WORK}})
	pb.NewGoalServiceClient(conn).CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "Goal " + secret, Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 3, Space: pb.Space_SPACE_WORK}})
	ai := pb.NewAIServiceClient(conn)
	ai.SetAIAccess(ctx, &pb.SetAIAccessRequest{AllowWork: false, AllowLife: true})

	if _, err := ai.DailyBriefing(ctx, &pb.DailyBriefingRequest{Day: timestamppb.New(day.Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Dentist") || strings.Contains(prompt, secret) {
		t.Fatalf("the briefing should cover life only:\n%s", prompt)
	}
}

func TestOldConversationsAreDroppedWhenAccessNarrows(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "ok", nil }}
	e, _ := startWithAI(t, f, false)
	ai := pb.NewAIServiceClient(e.conn(t))
	ctx := as(e.alice)

	first, err := ai.Ask(ctx, &pb.AskRequest{Message: "remember: mango-" + secret})
	if err != nil {
		t.Fatal(err)
	}
	sid := first.SessionId
	if strings.Contains(sid, "e0.") || sid == "" {
		t.Fatalf("the session id given to clients is plain: %q", sid)
	}
	// Same conversation continues while nothing has changed.
	f.mu.Lock()
	f.chatReqs = nil
	f.mu.Unlock()
	ai.Ask(ctx, &pb.AskRequest{Message: "and?", SessionId: sid})
	if !strings.Contains(f.everythingSentToModels(), "mango-") {
		t.Fatal("a conversation should keep its history while access is unchanged")
	}

	// Narrow access; the old conversation can no longer be replayed to the model.
	ai.SetAIAccess(ctx, &pb.SetAIAccessRequest{AllowWork: false, AllowLife: true})
	f.mu.Lock()
	f.chatReqs = nil
	f.mu.Unlock()
	again, err := ai.Ask(ctx, &pb.AskRequest{Message: "what did I say before?", SessionId: sid})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.everythingSentToModels(), "mango-") {
		t.Fatalf("an old conversation was replayed after access narrowed:\n%s", f.everythingSentToModels())
	}
	if again.SessionId != sid {
		t.Fatalf("the client keeps using the same session id: %q vs %q", again.SessionId, sid)
	}
}

func TestSemanticIndexHonoursTheAccessSetting(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "ok", nil }}
	e, eng := startWithAI(t, f, true)
	conn := e.conn(t)
	ctx := as(e.alice)
	notes, aic := pb.NewNoteServiceClient(conn), pb.NewAIServiceClient(conn)
	notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "Roadmap", Content: "osaka " + secret, Space: pb.Space_SPACE_WORK}})
	notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "Trip", Content: "osaka flights", Space: pb.Space_SPACE_LIFE}})
	u, _ := e.st.EnsureUser(context.Background(), "alice")

	aic.SetAIAccess(ctx, &pb.SetAIAccessRequest{AllowWork: false, AllowLife: true})
	ix := ai.NewIndexer(e.st, eng)
	run, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ix.Run(run)
	ix.Wake()

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitFor("the allowed note to be indexed", func() bool {
		ms, _ := eng.Search(context.Background(), u.ID, "osaka", 10)
		return len(ms) == 1
	})
	time.Sleep(300 * time.Millisecond) // let the indexer finish its pass
	if strings.Contains(strings.Join(f.embedded, "\n"), secret) {
		t.Fatal("work content was sent to the embedding endpoint while work was off")
	}
	// Even if something stale were in the index, it is never shown as a result.
	res, _ := notes.SearchNotes(ctx, &pb.SearchNotesRequest{Query: "osaka", Semantic: true})
	for _, h := range res.Hits {
		if strings.Contains(h.Note.Content, secret) && res.Mode == "semantic" {
			t.Fatal("a work note came back from semantic search with work off")
		}
	}

	// Turn work on again: its notes are queued for indexing.
	aic.SetAIAccess(ctx, &pb.SetAIAccessRequest{AllowWork: true, AllowLife: true})
	stale, _ := e.st.NotesNeedingIndex(context.Background(), 10)
	queued := false
	for _, n := range stale {
		queued = queued || strings.Contains(n.Content, secret)
	}
	if !queued {
		t.Fatal("re-allowing a space should queue its notes for indexing")
	}
	ix.Wake()
	waitFor("the work note to be indexed once allowed", func() bool {
		ms, _ := eng.Search(context.Background(), u.ID, "osaka", 10)
		return len(ms) == 2
	})

	// Turning it off again removes it from the index.
	aic.SetAIAccess(ctx, &pb.SetAIAccessRequest{AllowWork: false, AllowLife: true})
	waitFor("the work note to be purged", func() bool {
		ms, _ := eng.Search(context.Background(), u.ID, "osaka", 10)
		return len(ms) == 1
	})
}
