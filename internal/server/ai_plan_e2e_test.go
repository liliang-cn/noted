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

// A plan with no task counted from the start asks for the start date but does not
// need it: leaving it empty must still be accepted, and gives a project without one.
// A task dated without a time of day is due at 09:00, not at midnight.
func TestPlanFromTextOptionalStartCanBeLeftEmpty(t *testing.T) {
	f := &fakeProvider{script: answerJSON("break it into ONE project", `{"project":"离线同步上线","start":null,"due":"2030-10-30","space":"work",
		"tasks":[{"title":"写技术方案"},{"title":"发公告","due":"2030-10-28"}]}`)}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	p, err := pb.NewAIServiceClient(conn).PlanFromText(ctx, &pb.PlanFromTextRequest{Text: "10月30号上线,要写技术方案,上线前两天发公告"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Inputs) != 1 || p.Inputs[0].Required || p.Inputs[0].Label != "开始日期" {
		t.Fatalf("the start date should be offered, optional, with a neutral label: %+v", p.Inputs)
	}
	if _, err := pb.NewSuggestionServiceClient(conn).AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: p.Id}); err != nil {
		t.Fatalf("an optional input left empty must not block accepting: %v", err)
	}
	l, _ := pb.NewProjectServiceClient(conn).ListProjects(ctx, &pb.ListProjectsRequest{})
	if len(l.Projects) != 1 || l.Projects[0].StartTime != nil || l.Projects[0].DueTime.AsTime().Format("2006-01-02") != "2030-10-30" {
		t.Fatalf("%+v", l.Projects)
	}
	ts, _ := pb.NewCalendarServiceClient(conn).ListTasks(ctx, &pb.ListTasksRequest{})
	for _, tk := range ts.Tasks {
		if tk.Title == "发公告" && tk.DueTime.AsTime().Format("2006-01-02 15:04") != "2030-10-28 09:00" {
			t.Fatalf("a date-only due should be 09:00 that day: %v", tk.DueTime.AsTime())
		}
	}
}

// "Remind me" on a task becomes a reminder on the task once accepted, and the
// proposal of a reply asked from the combined mode takes the space its steps share.
func TestAssistantTaskReminder(t *testing.T) {
	f := &fakeProvider{}
	f.script = func(req map[string]any) (string, []map[string]any) {
		if _, has := toolResult(req); has {
			return "已准备好。", nil
		}
		return "", []map[string]any{toolCall("c1", "create_task", map[string]any{
			"title": "跟进合同", "due": "2030-05-02T14:00:00Z", "remind_at": "2030-05-02T13:30:00Z", "space": "work"})}
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	resp, err := pb.NewAIServiceClient(conn).Ask(as(e.alice), &pb.AskRequest{Message: "明天下午两点跟进合同,提前半小时提醒我"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Proposal == nil || resp.Proposal.Space != pb.Space_SPACE_WORK {
		t.Fatalf("the proposal should be in the space of its only step: %+v", resp.Proposal)
	}
	if _, err := pb.NewSuggestionServiceClient(conn).AcceptProposal(as(e.alice), &pb.AcceptProposalRequest{Id: resp.Proposal.Id}); err != nil {
		t.Fatal(err)
	}
	ts, _ := pb.NewCalendarServiceClient(conn).ListTasks(as(e.alice), &pb.ListTasksRequest{})
	if len(ts.Tasks) != 1 || ts.Tasks[0].RemindTime == nil || ts.Tasks[0].RemindTime.AsTime().Format("15:04") != "13:30" {
		t.Fatalf("the task should carry the reminder: %+v", ts.Tasks)
	}
}

// turnScript makes the fake model stage calls(turn) once per Ask and then
// answer. It tells the turns of one conversation apart by the "turn-N" marker
// in each message, since the session's history keeps earlier tool results.
func turnScript(calls func(turn string) []map[string]any) func(map[string]any) (string, []map[string]any) {
	return func(req map[string]any) (string, []map[string]any) {
		msgs, _ := req["messages"].([]any)
		turn := ""
		for _, m := range msgs {
			mm, _ := m.(map[string]any)
			c, _ := mm["content"].(string)
			if mm["role"] == "user" && strings.Contains(c, "turn-") {
				turn = c[strings.Index(c, "turn-"):]
			}
		}
		first := -1
		for i, m := range msgs {
			mm, _ := m.(map[string]any)
			c, _ := mm["content"].(string)
			if first < 0 && mm["role"] == "user" && turn != "" && strings.Contains(c, turn) {
				first = i
			}
			if first >= 0 && i > first && mm["role"] == "tool" {
				return "已准备好。", nil
			}
		}
		return "", calls(turn)
	}
}

// A follow-up in the same conversation that proposes again replaces the
// earlier, still pending proposal: accepting both must not create the project
// twice. Other conversations' proposals are left alone.
func TestFollowUpAskSupersedesEarlierProposal(t *testing.T) {
	f := &fakeProvider{script: turnScript(func(turn string) []map[string]any {
		return []map[string]any{toolCall("c1", "create_project", map[string]any{"title": "东京之行 " + turn, "space": "life"})}
	})}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	aic, sug := pb.NewAIServiceClient(conn), pb.NewSuggestionServiceClient(conn)

	other, err := aic.Ask(ctx, &pb.AskRequest{Message: "另一个会话 turn-0"})
	if err != nil || other.Proposal == nil {
		t.Fatalf("%v %+v", err, other)
	}
	first, err := aic.Ask(ctx, &pb.AskRequest{Message: "下个月去东京 turn-1"})
	if err != nil || first.Proposal == nil {
		t.Fatalf("%v %+v", err, first)
	}
	second, err := aic.Ask(ctx, &pb.AskRequest{Message: "再加上签证 turn-2", SessionId: first.SessionId})
	if err != nil || second.Proposal == nil {
		t.Fatalf("%v %+v", err, second)
	}
	if second.Proposal.Id == first.Proposal.Id {
		t.Fatal("the follow-up should make a new proposal")
	}

	pending, _ := sug.ListProposals(ctx, &pb.ListProposalsRequest{NoRefresh: true})
	ids := map[string]bool{}
	for _, p := range pending.Proposals {
		ids[p.Id] = true
	}
	if ids[first.Proposal.Id] || !ids[second.Proposal.Id] || !ids[other.Proposal.Id] {
		t.Fatalf("pending should be the follow-up and the other conversation's, got %v", ids)
	}
	gone, _ := sug.ListProposals(ctx, &pb.ListProposalsRequest{Status: "superseded", NoRefresh: true})
	if len(gone.Proposals) != 1 || gone.Proposals[0].Id != first.Proposal.Id {
		t.Fatalf("the earlier proposal should be superseded: %+v", gone.Proposals)
	}

	_, err = sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: first.Proposal.Id})
	if code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "superseded") {
		t.Fatalf("accepting a superseded proposal: %v", err)
	}
	if _, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: second.Proposal.Id}); err != nil {
		t.Fatal(err)
	}
	l, _ := pb.NewProjectServiceClient(conn).ListProjects(ctx, &pb.ListProjectsRequest{})
	if len(l.Projects) != 1 || !strings.Contains(l.Projects[0].Title, "turn-2") {
		t.Fatalf("only the follow-up's project should exist: %+v", l.Projects)
	}
	if _, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: other.Proposal.Id}); err != nil {
		t.Fatalf("another conversation's proposal must stay acceptable: %v", err)
	}
}

// A proposal whose steps span work and life belongs to both spaces: it is not
// labelled life, and each mode lists it. One whose steps share a space keeps it.
func TestMixedSpaceProposalIsListedInBothModes(t *testing.T) {
	f := &fakeProvider{script: turnScript(func(turn string) []map[string]any {
		if strings.Contains(turn, "turn-mixed") {
			return []map[string]any{
				toolCall("c1", "create_task", map[string]any{"title": "写周报", "space": "work"}),
				toolCall("c2", "create_task", map[string]any{"title": "买菜", "space": "life"}),
			}
		}
		return []map[string]any{toolCall("c1", "create_task", map[string]any{"title": "开会", "space": "work"})}
	})}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	aic, sug := pb.NewAIServiceClient(conn), pb.NewSuggestionServiceClient(conn)

	mixed, err := aic.Ask(ctx, &pb.AskRequest{Message: "两件事 turn-mixed", Space: pb.Space_SPACE_LIFE})
	if err != nil || mixed.Proposal == nil {
		t.Fatalf("%v %+v", err, mixed)
	}
	if mixed.Proposal.Space != pb.Space_SPACE_UNSPECIFIED {
		t.Fatalf("a proposal spanning work and life should be in both, got %v", mixed.Proposal.Space)
	}
	work, err := aic.Ask(ctx, &pb.AskRequest{Message: "开会 turn-work"})
	if err != nil || work.Proposal == nil {
		t.Fatalf("%v %+v", err, work)
	}
	if work.Proposal.Space != pb.Space_SPACE_WORK {
		t.Fatalf("a proposal whose steps are all work stays work, got %v", work.Proposal.Space)
	}

	// Last week's slipped tasks, one in each space: the carry-over spans both.
	cal := pb.NewCalendarServiceClient(conn)
	lastWeek := time.Now().AddDate(0, 0, -7)
	due := timestamppb.New(lastWeek)
	cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Work slipped", DueTime: due, Space: pb.Space_SPACE_WORK}})
	cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Life slipped", DueTime: due, Space: pb.Space_SPACE_LIFE}})
	r, err := sug.GetWeeklyReview(ctx, &pb.GetWeeklyReviewRequest{WeekOf: due, TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if r.NextWeek == nil || r.NextWeek.Space != pb.Space_SPACE_UNSPECIFIED {
		t.Fatalf("the review's plan spans both spaces: %+v", r.NextWeek)
	}

	in := func(space pb.Space) map[string]bool {
		l, err := sug.ListProposals(ctx, &pb.ListProposalsRequest{Space: space, NoRefresh: true})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, p := range l.Proposals {
			out[p.Id] = true
		}
		return out
	}
	w, l := in(pb.Space_SPACE_WORK), in(pb.Space_SPACE_LIFE)
	for _, id := range []string{mixed.Proposal.Id, r.NextWeek.Id} {
		if !w[id] || !l[id] {
			t.Fatalf("a mixed proposal must show in both modes: work=%v life=%v", w, l)
		}
	}
	if !w[work.Proposal.Id] || l[work.Proposal.Id] {
		t.Fatalf("a work proposal shows in work only: work=%v life=%v", w, l)
	}
}
