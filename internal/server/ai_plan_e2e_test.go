package server_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// answerJSON makes the fake model reply to the one-shot prompts with a fixed JSON object.
func answerJSON(marker, body string) func(map[string]any) (string, []map[string]any) {
	return func(req map[string]any) (string, []map[string]any) {
		b, _ := json.Marshal(req)
		if strings.Contains(string(b), marker) {
			return body, nil
		}
		return "ok", nil
	}
}

func TestPlanFromTextAsksForTheMissingStartDate(t *testing.T) {
	f := &fakeProvider{script: answerJSON("break it into ONE project", `{"project":"菲律宾旅行","start":null,"due":null,"space":"life",
		"tasks":[{"title":"办理签证","days_before_start":30},{"title":"订机票","days_before_start":60},{"title":"准备酒店"}]}`)}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	ai, sug := pb.NewAIServiceClient(conn), pb.NewSuggestionServiceClient(conn)
	ctx := as(e.alice)

	p, err := ai.PlanFromText(ctx, &pb.PlanFromTextRequest{Text: "下个月去菲律宾,要办签证、订机票和酒店"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "plan" || p.Source != "ai" || p.Status != "pending" || len(p.Operations) != 4 {
		t.Fatalf("%+v", p)
	}
	if len(p.Inputs) != 1 || p.Inputs[0].Name != "start" || !p.Inputs[0].Required || p.Inputs[0].Type != "date" {
		t.Fatalf("a start date the user did not give must be asked for, not guessed: %+v", p.Inputs)
	}
	// Nothing exists yet.
	if l, _ := pb.NewProjectServiceClient(conn).ListProjects(ctx, &pb.ListProjectsRequest{}); len(l.Projects) != 0 {
		t.Fatal("planning created a project")
	}
	// Accepting without the date is refused, and leaves the proposal pending.
	if _, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: p.Id}); code(err) != codes.InvalidArgument {
		t.Fatalf("missing required input: %v", err)
	}
	acc, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: p.Id, Inputs: map[string]string{"start": "2030-11-20"}})
	if err != nil {
		t.Fatal(err)
	}
	if acc.Change.Entries != 4 {
		t.Fatalf("%+v", acc.Change)
	}

	projects := pb.NewProjectServiceClient(conn)
	l, _ := projects.ListProjects(ctx, &pb.ListProjectsRequest{})
	if len(l.Projects) != 1 || l.Projects[0].Space != pb.Space_SPACE_LIFE || !l.Projects[0].Pinned ||
		l.Projects[0].StartTime.AsTime().Format("2006-01-02") != "2030-11-20" {
		t.Fatalf("%+v", l.Projects)
	}
	d, _ := projects.GetProject(ctx, &pb.GetProjectRequest{Id: l.Projects[0].Id})
	due := map[string]string{}
	for _, tk := range d.Tasks {
		if tk.DueTime != nil {
			due[tk.Title] = tk.DueTime.AsTime().Format("2006-01-02 15")
		} else {
			due[tk.Title] = ""
		}
	}
	if due["办理签证"] != "2030-10-21 09" || due["订机票"] != "2030-09-21 09" || due["准备酒店"] != "" {
		t.Fatalf("task dates should be counted back from the departure: %v", due)
	}

	// Undo removes the lot.
	if _, err := sug.UndoChange(ctx, &pb.UndoChangeRequest{Id: acc.Change.Id}); err != nil {
		t.Fatal(err)
	}
	if l, _ := projects.ListProjects(ctx, &pb.ListProjectsRequest{}); len(l.Projects) != 0 {
		t.Fatal("undo left the project behind")
	}
	if ts, _ := pb.NewCalendarServiceClient(conn).ListTasks(ctx, &pb.ListTasksRequest{Filter: pb.ListTasksRequest_FILTER_ALL}); len(ts.Tasks) != 0 {
		t.Fatalf("undo left %d tasks behind", len(ts.Tasks))
	}
}

func TestPlanFromTextWithAStatedDateNeedsNoInput(t *testing.T) {
	f := &fakeProvider{script: answerJSON("break it into ONE project", `{"project":"Launch","start":"2030-12-01","due":null,
		"tasks":[{"title":"Write notes","days_before_start":7},{"title":"Book venue","due":"2030-11-01"}]}`)}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	p, err := pb.NewAIServiceClient(conn).PlanFromText(as(e.alice), &pb.PlanFromTextRequest{Text: "launch on Dec 1", Space: pb.Space_SPACE_WORK})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Inputs) != 0 || p.Space != pb.Space_SPACE_WORK {
		t.Fatalf("%+v", p)
	}
	if _, err := pb.NewSuggestionServiceClient(conn).AcceptProposal(as(e.alice), &pb.AcceptProposalRequest{Id: p.Id}); err != nil {
		t.Fatal(err)
	}
	ts, _ := pb.NewCalendarServiceClient(conn).ListTasks(as(e.alice), &pb.ListTasksRequest{})
	got := map[string]string{}
	for _, tk := range ts.Tasks {
		got[tk.Title] = tk.DueTime.AsTime().Format("2006-01-02")
		if tk.Space != pb.Space_SPACE_WORK {
			t.Fatalf("%s should be work", tk.Title)
		}
	}
	if got["Write notes"] != "2030-11-24" || got["Book venue"] != "2030-11-01" {
		t.Fatalf("%v", got)
	}
}

func TestPlanFromTextErrors(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "I can't do that.", nil }}
	e, _ := startWithAI(t, f, false)
	ai := pb.NewAIServiceClient(e.conn(t))
	_, err := ai.PlanFromText(as(e.alice), &pb.PlanFromTextRequest{Text: "plan something"})
	if code(err) != codes.Unavailable {
		t.Fatalf("a model that does not return JSON should be a clean failure: %v", err)
	}
	if _, err := ai.PlanFromText(as(e.alice), &pb.PlanFromTextRequest{Text: "  "}); code(err) != codes.InvalidArgument {
		t.Fatalf("empty text: %v", err)
	}
	// With AI off the RPCs answer FAILED_PRECONDITION.
	off := start(t, nil, false)
	c := pb.NewAIServiceClient(off.conn(t))
	if _, err := c.PlanFromText(as(off.alice), &pb.PlanFromTextRequest{Text: "x"}); code(err) != codes.FailedPrecondition {
		t.Fatalf("AI off: %v", err)
	}
	if _, err := c.ExtractTasks(as(off.alice), &pb.ExtractTasksRequest{NoteId: "x"}); code(err) != codes.FailedPrecondition {
		t.Fatalf("AI off: %v", err)
	}
}

func TestExtractTasksFromANote(t *testing.T) {
	f := &fakeProvider{script: answerJSON("still has to do", `{"tasks":[{"title":"订大阪机票","due":"2030-10-31"},{"title":"订酒店"}]}`)}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	proj, _ := pb.NewProjectServiceClient(conn).CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{Title: "Japan", Space: pb.Space_SPACE_LIFE}})
	n, _ := pb.NewNoteServiceClient(conn).CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{
		Title: "日本行程", Content: "订大阪机票(10月31日前)。订酒店。", ProjectId: proj.Id}})

	p, err := pb.NewAIServiceClient(conn).ExtractTasks(ctx, &pb.ExtractTasksRequest{NoteId: n.Id})
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "extract" || len(p.Operations) != 2 {
		t.Fatalf("%+v", p)
	}
	sug := pb.NewSuggestionServiceClient(conn)
	// Take only the second one.
	if _, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: p.Id, Selection: &pb.OperationSelection{Indexes: []int32{1}}}); err != nil {
		t.Fatal(err)
	}
	ts, _ := pb.NewCalendarServiceClient(conn).ListTasks(ctx, &pb.ListTasksRequest{ProjectId: proj.Id})
	if len(ts.Tasks) != 1 || ts.Tasks[0].Title != "订酒店" || ts.Tasks[0].Space != pb.Space_SPACE_LIFE {
		t.Fatalf("the task should join the note's project: %+v", ts.Tasks)
	}

	// Someone else's note, and a note with nothing to do.
	if _, err := pb.NewAIServiceClient(conn).ExtractTasks(as(e.bob), &pb.ExtractTasksRequest{NoteId: n.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob read alice's note: %v", err)
	}
}

func TestExtractTasksWithNothingFound(t *testing.T) {
	f := &fakeProvider{script: answerJSON("still has to do", `{"tasks":[]}`)}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	n, _ := pb.NewNoteServiceClient(conn).CreateNote(as(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "Poem", Content: "Roses are red"}})
	if _, err := pb.NewAIServiceClient(conn).ExtractTasks(as(e.alice), &pb.ExtractTasksRequest{NoteId: n.Id}); code(err) != codes.FailedPrecondition {
		t.Fatalf("%v", err)
	}
}

// ---- the assistant stages, never writes ----

func TestAssistantRejectsBadArgumentsBeforeStaging(t *testing.T) {
	f := &fakeProvider{}
	var seen string
	f.script = func(req map[string]any) (string, []map[string]any) {
		if content, has := toolResult(req); has {
			seen = content
			return "那个时间不对。", nil
		}
		return "", []map[string]any{toolCall("c1", "create_event", map[string]any{
			"title": "Broken", "start": "2030-01-01T10:00:00Z", "end": "2030-01-01T09:00:00Z"})}
	}
	e, _ := startWithAI(t, f, false)
	resp, err := pb.NewAIServiceClient(e.conn(t)).Ask(as(e.alice), &pb.AskRequest{Message: "make a broken event"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen, `"ok":false`) || !strings.Contains(seen, "end_time is before") {
		t.Fatalf("the model should see why it failed: %s", seen)
	}
	if resp.Proposal != nil {
		t.Fatalf("a rejected call must not leave a proposal behind: %+v", resp.Proposal)
	}
}

func TestAssistantRescheduleIsStagedAndUndoable(t *testing.T) {
	f := &fakeProvider{}
	var eventID string
	f.script = func(req map[string]any) (string, []map[string]any) {
		if _, has := toolResult(req); has {
			return "已改到 16:00。", nil
		}
		return "", []map[string]any{toolCall("c1", "reschedule_event", map[string]any{"id": eventID, "start": "2030-05-01T16:00:00Z"})}
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	cal := pb.NewCalendarServiceClient(conn)
	start := time.Date(2030, 5, 1, 10, 0, 0, 0, time.UTC)
	ev, _ := cal.CreateEvent(as(e.alice), &pb.CreateEventRequest{Event: &pb.Event{Title: "Dentist", StartTime: timestamppb.New(start), EndTime: timestamppb.New(start.Add(time.Hour))}})
	eventID = ev.Id

	resp, err := pb.NewAIServiceClient(conn).Ask(as(e.alice), &pb.AskRequest{Message: "把牙医改到下午四点", AutoApply: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := cal.GetEvent(as(e.alice), &pb.GetEventRequest{Id: ev.Id})
	if got.StartTime.AsTime().Hour() != 16 || got.EndTime.AsTime().Hour() != 17 {
		t.Fatalf("moved to %v-%v, want 16:00-17:00 (duration kept)", got.StartTime.AsTime(), got.EndTime.AsTime())
	}
	if _, err := pb.NewSuggestionServiceClient(conn).UndoChange(as(e.alice), &pb.UndoChangeRequest{Id: resp.Change.Id}); err != nil {
		t.Fatal(err)
	}
	got, _ = cal.GetEvent(as(e.alice), &pb.GetEventRequest{Id: ev.Id})
	if !got.StartTime.AsTime().Equal(start) {
		t.Fatalf("undo did not restore the time: %v", got.StartTime.AsTime())
	}
	// Another user's event cannot be staged for rescheduling at all.
	eventID = ev.Id
	other, err := pb.NewAIServiceClient(conn).Ask(as(e.bob), &pb.AskRequest{Message: "move it", AutoApply: true})
	if err != nil {
		t.Fatal(err)
	}
	if other.Proposal != nil {
		t.Fatalf("bob's assistant staged a change to alice's event: %+v", other.Proposal)
	}
}

// A model that reasons before it answers can spend its whole token budget
// thinking and return nothing. That must be an error, not an empty summary.
func TestAnEmptyModelAnswerIsAnErrorNotAnEmptySummary(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "", nil }}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	n, _ := pb.NewNoteServiceClient(conn).CreateNote(as(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "t", Content: "some text"}})
	ai := pb.NewAIServiceClient(conn)
	if r, err := ai.SummarizeNote(as(e.alice), &pb.SummarizeNoteRequest{NoteId: n.Id}); code(err) != codes.Unavailable {
		t.Fatalf("an empty answer was returned as a summary: %+v %v", r, err)
	}
	if _, err := ai.DailyBriefing(as(e.alice), &pb.DailyBriefingRequest{}); code(err) != codes.Unavailable {
		t.Fatalf("an empty briefing: %v", err)
	}
}
