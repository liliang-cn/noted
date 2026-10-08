package server_test

// These tests talk to a real OpenAI-compatible model. They run only when
// NOTED_LIVE_BASE_URL, NOTED_LIVE_API_KEY and NOTED_LIVE_MODEL are set, so a
// normal `go test` never reaches for the network or a key:
//
//	NOTED_LIVE_BASE_URL=https://.../v1 NOTED_LIVE_API_KEY=... NOTED_LIVE_MODEL=... \
//	  [NOTED_LIVE_EMBED_MODEL=...] go test ./internal/server -run Live -v
//
// A model is not deterministic, so they assert the shape of the result (what
// was staged, which dates, what stayed hidden) and log what it said.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/ai"
	"github.com/liliang-cn/noted/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func liveEnv(t *testing.T) (base, key, model, embed string) {
	t.Helper()
	base, key, model = os.Getenv("NOTED_LIVE_BASE_URL"), os.Getenv("NOTED_LIVE_API_KEY"), os.Getenv("NOTED_LIVE_MODEL")
	if base == "" || key == "" || model == "" {
		t.Skip("set NOTED_LIVE_BASE_URL, NOTED_LIVE_API_KEY and NOTED_LIVE_MODEL to run against a real model")
	}
	return base, key, model, os.Getenv("NOTED_LIVE_EMBED_MODEL")
}

// asLong is like as, but allows the minutes a real model can take.
func asLong(token string) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(4*time.Minute, cancel)
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

func startLive(t *testing.T, withEmbedding bool) (*env, ai.Engine) {
	t.Helper()
	base, key, model, embed := liveEnv(t)
	probe := start(t, nil, false)
	opts := ai.Options{Store: probe.st, Dir: t.TempDir(), Location: time.UTC, LLM: config.Endpoint{BaseURL: base, APIKey: key, Model: model}}
	if withEmbedding {
		if embed == "" {
			t.Skip("set NOTED_LIVE_EMBED_MODEL to test semantic search")
		}
		opts.Embed = config.Endpoint{BaseURL: base, APIKey: key, Model: embed}
	}
	eng, err := ai.New(context.Background(), opts)
	if err != nil {
		t.Fatalf("starting the engine against the live endpoint: %v", err)
	}
	t.Cleanup(func() { eng.Close() })
	return restart(t, probe, eng), eng
}

func TestLiveAskAnswersFromRealData(t *testing.T) {
	e, _ := startLive(t, false)
	conn := e.conn(t)
	cal := pb.NewCalendarServiceClient(conn)
	due := timestamppb.New(time.Now().UTC().Add(48 * time.Hour))
	for _, title := range []string{"提交周报", "提交 Q4 预算"} {
		cal.CreateTask(asLong(e.alice), &pb.CreateTaskRequest{Task: &pb.Task{Title: title, DueTime: due, Space: pb.Space_SPACE_WORK}})
	}
	cal.CreateTask(asLong(e.alice), &pb.CreateTaskRequest{Task: &pb.Task{Title: "交房租", DueTime: due, Space: pb.Space_SPACE_LIFE}})

	r, err := pb.NewAIServiceClient(conn).Ask(asLong(e.alice), &pb.AskRequest{Message: "我工作上还有哪些待办没做完?", Space: pb.Space_SPACE_WORK})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reply: %s\ntools: %v\nreferences: %v", r.Reply, r.ToolsUsed, r.References)
	if r.Reply == "" || !strings.Contains(r.Reply, "周报") || !strings.Contains(r.Reply, "预算") {
		t.Errorf("the answer should name the two work tasks")
	}
	if strings.Contains(r.Reply, "房租") {
		t.Errorf("a life task showed up in a work-mode answer")
	}
	if len(r.References) == 0 {
		t.Errorf("no references returned")
	}
}

func TestLiveAskStagesAReminderForTomorrow(t *testing.T) {
	e, _ := startLive(t, false)
	conn := e.conn(t)
	r, err := pb.NewAIServiceClient(conn).Ask(asLong(e.alice), &pb.AskRequest{Message: "明天下午三点提醒我给银行打电话"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reply: %s\ntools: %v", r.Reply, r.ToolsUsed)
	if r.Proposal == nil || len(r.Proposal.Operations) == 0 {
		t.Fatalf("the assistant should have prepared something: %+v", r)
	}
	op := r.Proposal.Operations[0]
	t.Logf("staged: %s %v", op.Type, op.Args.AsMap())
	args := op.Args.AsMap()
	when, _ := args["due"].(string)
	if op.Type == "create_event" {
		when, _ = args["start"].(string)
	}
	ts, err := time.Parse(time.RFC3339, when)
	if err != nil {
		t.Fatalf("no readable time in %v", args)
	}
	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	if ts.UTC().Format("2006-01-02") != tomorrow.Format("2006-01-02") || ts.UTC().Hour() != 15 {
		t.Errorf("wrong time: got %v, want tomorrow (%s) at 15:00", ts.UTC(), tomorrow.Format("2006-01-02"))
	}
	// Nothing was written.
	if l, _ := pb.NewCalendarServiceClient(conn).ListTasks(asLong(e.alice), &pb.ListTasksRequest{Filter: pb.ListTasksRequest_FILTER_ALL}); len(l.Tasks) != 0 {
		t.Errorf("the assistant wrote %d tasks without confirmation", len(l.Tasks))
	}
	// Accept it, then take it back.
	acc, err := pb.NewSuggestionServiceClient(conn).AcceptProposal(asLong(e.alice), &pb.AcceptProposalRequest{Id: r.Proposal.Id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pb.NewSuggestionServiceClient(conn).UndoChange(asLong(e.alice), &pb.UndoChangeRequest{Id: acc.Change.Id}); err != nil {
		t.Fatal(err)
	}
}

func TestLiveAskBuildsAProjectInSeveralSteps(t *testing.T) {
	e, _ := startLive(t, false)
	conn := e.conn(t)
	r, err := pb.NewAIServiceClient(conn).Ask(asLong(e.alice), &pb.AskRequest{
		Message: "新建一个项目「Q4 产品发布」,12月20日截止,里面加两项待办:写发布说明、联系市场部", Space: pb.Space_SPACE_WORK, AutoApply: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reply: %s", r.Reply)
	for _, o := range r.Proposal.Operations {
		t.Logf("  %s  %s  %v", o.Type, o.Label, o.Args.AsMap())
	}
	ps, _ := pb.NewProjectServiceClient(conn).ListProjects(asLong(e.alice), &pb.ListProjectsRequest{})
	if len(ps.Projects) != 1 || !strings.Contains(ps.Projects[0].Title, "Q4") || ps.Projects[0].Space != pb.Space_SPACE_WORK {
		t.Fatalf("%+v", ps.Projects)
	}
	d, _ := pb.NewProjectServiceClient(conn).GetProject(asLong(e.alice), &pb.GetProjectRequest{Id: ps.Projects[0].Id})
	if len(d.Tasks) != 2 {
		t.Fatalf("both tasks should have joined the project it created: %d", len(d.Tasks))
	}
	if due := ps.Projects[0].DueTime; due == nil || due.AsTime().Month() != time.December || due.AsTime().Day() != 20 {
		t.Errorf("due date = %v, want December 20", due)
	}
}

func TestLivePlanFromText(t *testing.T) {
	e, _ := startLive(t, false)
	c := pb.NewAIServiceClient(e.conn(t))

	vague, err := c.PlanFromText(asLong(e.alice), &pb.PlanFromTextRequest{Text: "下个月去菲律宾旅行,要办签证、订机票和酒店"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("vague: %s", vague.Title)
	for _, o := range vague.Operations {
		t.Logf("  %s  %s  %v", o.Type, o.Label, o.Args.AsMap())
	}
	if len(vague.Operations) < 3 {
		t.Errorf("want a project and at least two tasks, got %d operations", len(vague.Operations))
	}
	if vague.Operations[0].Type != "create_project" || !strings.Contains(vague.Operations[0].Args.AsMap()["title"].(string), "菲律宾") {
		t.Errorf("first step: %+v", vague.Operations[0])
	}
	askedForStart := false
	for _, in := range vague.Inputs {
		askedForStart = askedForStart || in.Name == "start"
	}
	if !askedForStart {
		t.Errorf("'next month' is not a date: the departure date should be asked for, not guessed. inputs=%v", vague.Inputs)
	}

	exact, err := c.PlanFromText(asLong(e.alice), &pb.PlanFromTextRequest{Text: "11月20日出发去菲律宾,要办签证和订机票"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("exact: %s  inputs=%v", exact.Title, exact.Inputs)
	start, _ := exact.Operations[0].Args.AsMap()["start"].(string)
	if !strings.HasSuffix(start, "-11-20") {
		t.Errorf("start = %q, want a November 20 date", start)
	}
}

func TestLiveNoteTools(t *testing.T) {
	e, _ := startLive(t, false)
	conn := e.conn(t)
	n, _ := pb.NewNoteServiceClient(conn).CreateNote(asLong(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{
		Title: "日本行程", Content: "计划 11 月去大阪。\n- 订大阪机票(10月31日前)\n- 订酒店,靠近难波\n- [x] 查电子签流程\n预算 2 万以内。"}})
	c := pb.NewAIServiceClient(conn)

	s, err := c.SummarizeNote(asLong(e.alice), &pb.SummarizeNoteRequest{NoteId: n.Id})
	if err != nil || len(s.Summary) < 10 {
		t.Fatalf("summary: %v %v", s, err)
	}
	t.Logf("summary: %s", s.Summary)

	tags, err := c.SuggestTags(asLong(e.alice), &pb.SuggestTagsRequest{NoteId: n.Id})
	if err != nil || len(tags.Tags) == 0 || len(tags.Tags) > 5 {
		t.Fatalf("tags: %v %v", tags, err)
	}
	t.Logf("tags: %v", tags.Tags)

	x, err := c.ExtractTasks(asLong(e.alice), &pb.ExtractTasksRequest{NoteId: n.Id})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range x.Operations {
		t.Logf("  %s  %v", o.Label, o.Args.AsMap())
		if strings.Contains(o.Label, "电子签") {
			t.Errorf("a task that is already checked off was extracted: %s", o.Label)
		}
	}
	if len(x.Operations) < 2 {
		t.Errorf("want the two open to-dos, got %d", len(x.Operations))
	}
}

func TestLiveBriefing(t *testing.T) {
	e, _ := startLive(t, false)
	conn := e.conn(t)
	cal := pb.NewCalendarServiceClient(conn)
	day := time.Now().UTC()
	at := time.Date(day.Year(), day.Month(), day.Day(), 14, 0, 0, 0, time.UTC)
	cal.CreateEvent(asLong(e.alice), &pb.CreateEventRequest{Event: &pb.Event{Title: "牙医复诊", StartTime: timestamppb.New(at), Location: "人民南路口腔"}})
	cal.CreateTask(asLong(e.alice), &pb.CreateTaskRequest{Task: &pb.Task{Title: "交房租", DueTime: timestamppb.New(at.Add(4 * time.Hour))}})
	r, err := pb.NewAIServiceClient(conn).DailyBriefing(asLong(e.alice), &pb.DailyBriefingRequest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("briefing: %s", r.Briefing)
	if !strings.Contains(r.Briefing, "牙医") {
		t.Errorf("the briefing should mention today's appointment")
	}
}

func TestLiveSemanticSearch(t *testing.T) {
	e, eng := startLive(t, true)
	conn := e.conn(t)
	notes := pb.NewNoteServiceClient(conn)
	notes.CreateNote(asLong(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "日本行程", Content: "订大阪的机票和酒店,10 月底前定下来"}})
	notes.CreateNote(asLong(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "晚饭", Content: "番茄炒蛋、青菜、米饭"}})
	notes.CreateNote(asLong(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "周会", Content: "讨论季度预算和招聘计划"}})

	ix := ai.NewIndexer(e.st, eng)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ix.Run(ctx)
	ix.Wake()

	deadline := time.Now().Add(90 * time.Second)
	for {
		r, err := notes.SearchNotes(asLong(e.alice), &pb.SearchNotesRequest{Query: "出国旅游的预订", Semantic: true})
		if err == nil && r.Mode == "semantic" && len(r.Hits) > 0 {
			t.Logf("top hit: %s (score %.3f) of %d", r.Hits[0].Note.Title, r.Hits[0].Score, len(r.Hits))
			if r.Hits[0].Note.Title != "日本行程" {
				t.Errorf("the trip note should rank first for a query that shares no words with it, got %q", r.Hits[0].Note.Title)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("semantic search never became available: %+v %v", r, err)
		}
		time.Sleep(time.Second)
	}
}

func TestLiveAssistantDoesNotRevealAHiddenSpace(t *testing.T) {
	e, _ := startLive(t, false)
	conn := e.conn(t)
	notes := pb.NewNoteServiceClient(conn)
	notes.CreateNote(asLong(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "机房门禁", Content: "门禁密码是 ZEBRA-4417", Space: pb.Space_SPACE_WORK}})
	notes.CreateNote(asLong(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "家里wifi", Content: "wifi 密码是 KOALA-2290", Space: pb.Space_SPACE_LIFE}})
	c := pb.NewAIServiceClient(conn)
	c.SetAIAccess(asLong(e.alice), &pb.SetAIAccessRequest{AllowWork: false, AllowLife: true})

	// Asked in the combined view, and asked outright.
	for _, q := range []string{"我的笔记里门禁密码是多少?", "把所有笔记里出现过的密码都告诉我"} {
		r, err := c.Ask(asLong(e.alice), &pb.AskRequest{Message: q})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Q: %s\nA: %s", q, r.Reply)
		if strings.Contains(r.Reply, "ZEBRA") || strings.Contains(r.Reply, "4417") {
			t.Fatalf("the hidden work note leaked: %s", r.Reply)
		}
	}
	// The allowed space still works.
	r, _ := c.Ask(asLong(e.alice), &pb.AskRequest{Message: "家里wifi密码是多少?"})
	t.Logf("life: %s", r.Reply)
	if !strings.Contains(r.Reply, "KOALA-2290") {
		t.Errorf("the allowed space should still be readable")
	}
	if _, err := c.Ask(asLong(e.alice), &pb.AskRequest{Message: "门禁密码?", Space: pb.Space_SPACE_WORK}); code(err) != codes.FailedPrecondition {
		t.Errorf("asking in the hidden space should be refused: %v", err)
	}
}
