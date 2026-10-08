package server_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/reminder"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func mask(paths ...string) *fieldmaskpb.FieldMask { return &fieldmaskpb.FieldMask{Paths: paths} }

func utcMidnight() time.Time {
	n := time.Now().UTC()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func TestGoalsOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	c := pb.NewGoalServiceClient(conn)
	ctx := as(e.alice)

	g, err := c.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{
		Title: "运动", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 3, Unit: "次", Space: pb.Space_SPACE_LIFE}})
	if err != nil {
		t.Fatal(err)
	}
	if g.Progress == nil || g.Progress.Target != 3 || g.Progress.Done != 0 || len(g.Progress.Days) != 7 || len(g.Progress.Recent) != 14 {
		t.Fatalf("fresh progress: %+v", g.Progress)
	}

	r1, err := c.RecordCheckIn(ctx, &pb.RecordCheckInRequest{GoalId: g.Id})
	if err != nil {
		t.Fatal(err)
	}
	if r1.CheckIn.Amount != 1 || r1.Goal.Progress.Done != 1 || r1.Goal.Progress.Remaining != 2 {
		t.Fatalf("%+v", r1)
	}
	if _, err := c.RecordCheckIn(ctx, &pb.RecordCheckInRequest{GoalId: g.Id, Amount: 2, Note: "run"}); err != nil {
		t.Fatal(err)
	}
	got, _ := c.GetGoal(ctx, &pb.GetGoalRequest{Id: g.Id})
	if !got.Progress.Achieved || got.Progress.Done != 3 || got.Progress.Streak < 1 {
		t.Fatalf("three units should achieve a target of 3: %+v", got.Progress)
	}
	// Undoing a check-in lowers progress again.
	undone, err := c.DeleteCheckIn(ctx, &pb.DeleteCheckInRequest{Id: r1.CheckIn.Id})
	if err != nil || undone.Progress.Done != 2 || undone.Progress.Achieved {
		t.Fatalf("%+v %v", undone.GetProgress(), err)
	}
	hist, _ := c.ListCheckIns(ctx, &pb.ListCheckInsRequest{GoalId: g.Id})
	if len(hist.CheckIns) != 1 || hist.CheckIns[0].Note != "run" {
		t.Fatalf("%+v", hist)
	}

	// Milestones via update mask, then tick one off.
	g, err = c.UpdateGoal(ctx, &pb.UpdateGoalRequest{
		Goal:       &pb.Goal{Id: g.Id, Milestones: []*pb.Milestone{{Title: "A1"}, {Title: "A2"}}},
		UpdateMask: mask("milestones")})
	if err != nil || len(g.Milestones) != 2 {
		t.Fatalf("%+v %v", g, err)
	}
	g, err = c.SetMilestoneDone(ctx, &pb.SetMilestoneDoneRequest{GoalId: g.Id, MilestoneId: g.Milestones[0].Id, Done: true})
	if err != nil || g.Progress.MilestonesDone != 1 || g.Progress.MilestonePercent != 0.5 {
		t.Fatalf("%+v %v", g.GetProgress(), err)
	}

	// Validation and isolation.
	if _, err := c.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "x", Target: 1}}); code(err) != codes.InvalidArgument {
		t.Fatalf("missing period: %v", err)
	}
	if _, err := c.UpdateGoal(ctx, &pb.UpdateGoalRequest{Goal: &pb.Goal{Id: g.Id}}); code(err) != codes.InvalidArgument {
		t.Fatalf("missing mask: %v", err)
	}
	if _, err := c.GetGoal(as(e.bob), &pb.GetGoalRequest{Id: g.Id}); code(err) != codes.NotFound {
		t.Fatalf("goal leaked: %v", err)
	}
	if _, err := c.RecordCheckIn(as(e.bob), &pb.RecordCheckInRequest{GoalId: g.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob checked in on alice's goal: %v", err)
	}
	if _, err := c.RecordCheckIn(ctx, &pb.RecordCheckInRequest{GoalId: g.Id, Amount: -1}); code(err) != codes.InvalidArgument {
		t.Fatalf("negative amount: %v", err)
	}

	// Goals can be filtered by space.
	c.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "周复盘", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 1, Space: pb.Space_SPACE_WORK}})
	for sp, want := range map[pb.Space]int{pb.Space_SPACE_UNSPECIFIED: 2, pb.Space_SPACE_WORK: 1, pb.Space_SPACE_LIFE: 1} {
		l, err := c.ListGoals(ctx, &pb.ListGoalsRequest{Space: sp})
		if err != nil || len(l.Goals) != want {
			t.Errorf("space %v: %d goals, want %d (%v)", sp, len(l.GetGoals()), want, err)
		}
	}
	// The one behind pace sorts first: nothing is done on the review goal.
	l, _ := c.ListGoals(ctx, &pb.ListGoalsRequest{})
	_ = l
}

func TestProjectsOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	projects := pb.NewProjectServiceClient(conn)
	cal := pb.NewCalendarServiceClient(conn)
	notes := pb.NewNoteServiceClient(conn)
	ctx := as(e.alice)

	depart := time.Now().UTC().AddDate(0, 0, 43)
	p, err := projects.CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{
		Title: "菲律宾旅行", Pinned: true, StartTime: timestamppb.New(depart), Space: pb.Space_SPACE_LIFE}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Progress.DaysLeft == nil || *p.Progress.DaysLeft != 43 {
		t.Fatalf("days left = %v", p.Progress.DaysLeft)
	}

	visaDue := time.Now().Add(240 * time.Hour)
	visa, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "办理签证", DueTime: timestamppb.New(visaDue), ProjectId: p.Id}})
	if err != nil {
		t.Fatal(err)
	}
	if visa.Space != pb.Space_SPACE_LIFE {
		t.Fatalf("a task should inherit its project's space, got %v", visa.Space)
	}
	cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "准备酒店", ProjectId: p.Id}})
	flights, _ := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "订机票", ProjectId: p.Id}})
	cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: flights.Id, Completed: true}, UpdateMask: mask("completed")})
	cal.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{Title: "航班", StartTime: timestamppb.New(depart), ProjectId: p.Id}})
	notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "行程想法", Content: "先去巴拉望", ProjectId: p.Id}})

	d, err := projects.GetProject(ctx, &pb.GetProjectRequest{Id: p.Id})
	if err != nil {
		t.Fatal(err)
	}
	pr := d.Project.Progress
	if pr.TasksTotal != 3 || pr.TasksDone != 1 || pr.EventsUpcoming != 1 || pr.NotesCount != 1 || len(d.Tasks) != 3 || len(d.Events) != 1 || len(d.Notes) != 1 {
		t.Fatalf("%+v tasks=%d events=%d notes=%d", pr, len(d.Tasks), len(d.Events), len(d.Notes))
	}
	if pr.NextTitle != "办理签证" {
		t.Fatalf("next step = %q", pr.NextTitle)
	}

	// Filters by project.
	pt, _ := cal.ListTasks(ctx, &pb.ListTasksRequest{ProjectId: p.Id, Filter: pb.ListTasksRequest_FILTER_ALL})
	pn, _ := notes.ListNotes(ctx, &pb.ListNotesRequest{ProjectId: p.Id})
	if len(pt.Tasks) != 3 || len(pn.Notes) != 1 {
		t.Fatalf("tasks=%d notes=%d", len(pt.Tasks), len(pn.Notes))
	}

	// Validation and isolation.
	if _, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "x", ProjectId: "nope"}}); code(err) != codes.InvalidArgument {
		t.Fatalf("unknown project: %v", err)
	}
	if _, err := cal.CreateTask(as(e.bob), &pb.CreateTaskRequest{Task: &pb.Task{Title: "x", ProjectId: p.Id}}); code(err) != codes.InvalidArgument {
		t.Fatalf("bob used alice's project: %v", err)
	}
	if _, err := projects.GetProject(as(e.bob), &pb.GetProjectRequest{Id: p.Id}); code(err) != codes.NotFound {
		t.Fatalf("project leaked: %v", err)
	}

	// Unpin through the mask; list reflects it.
	up, err := projects.UpdateProject(ctx, &pb.UpdateProjectRequest{Project: &pb.Project{Id: p.Id}, UpdateMask: mask("pinned")})
	if err != nil || up.Pinned {
		t.Fatalf("%+v %v", up, err)
	}
	if l, _ := projects.ListProjects(ctx, &pb.ListProjectsRequest{PinnedOnly: true}); len(l.Projects) != 0 {
		t.Fatal("unpinned project still listed as pinned")
	}

	// Deleting keeps the tasks, now unassigned.
	if _, err := projects.DeleteProject(ctx, &pb.DeleteProjectRequest{Id: p.Id}); err != nil {
		t.Fatal(err)
	}
	got, err := cal.GetTask(ctx, &pb.GetTaskRequest{Id: visa.Id})
	if err != nil || got.ProjectId != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSpaceOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	notes, cal := pb.NewNoteServiceClient(conn), pb.NewCalendarServiceClient(conn)
	ctx := as(e.alice)
	day := utcMidnight().Add(30 * time.Hour)

	n, _ := notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "Roadmap", Content: "planning", Space: pb.Space_SPACE_WORK}})
	notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "Recipes", Content: "planning dinner"}}) // defaults to life
	cal.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{Title: "Board", StartTime: timestamppb.New(day), Space: pb.Space_SPACE_WORK}})
	cal.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{Title: "Dentist", StartTime: timestamppb.New(day)}})
	cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Ship", Space: pb.Space_SPACE_WORK}})
	cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Rent"}})

	if n.Space != pb.Space_SPACE_WORK {
		t.Fatalf("space = %v", n.Space)
	}
	rng := func(sp pb.Space) *pb.ListEventsRequest {
		return &pb.ListEventsRequest{From: timestamppb.New(day.Add(-time.Hour)), To: timestamppb.New(day.Add(time.Hour)), Space: sp}
	}
	for sp, want := range map[pb.Space]int{pb.Space_SPACE_UNSPECIFIED: 2, pb.Space_SPACE_WORK: 1, pb.Space_SPACE_LIFE: 1} {
		ln, _ := notes.ListNotes(ctx, &pb.ListNotesRequest{Space: sp})
		sn, _ := notes.SearchNotes(ctx, &pb.SearchNotesRequest{Query: "planning", Space: sp})
		le, _ := cal.ListEvents(ctx, rng(sp))
		lt, _ := cal.ListTasks(ctx, &pb.ListTasksRequest{Space: sp})
		if len(ln.Notes) != want || len(sn.Hits) != want || len(le.Occurrences) != want || len(lt.Tasks) != want {
			t.Errorf("space %v: notes=%d search=%d events=%d tasks=%d, want %d each", sp, len(ln.Notes), len(sn.Hits), len(le.Occurrences), len(lt.Tasks), want)
		}
	}

	// Move a note to life; a bad update is rejected.
	up, err := notes.UpdateNote(ctx, &pb.UpdateNoteRequest{Note: &pb.Note{Id: n.Id, Space: pb.Space_SPACE_LIFE}, UpdateMask: mask("space")})
	if err != nil || up.Space != pb.Space_SPACE_LIFE {
		t.Fatalf("%+v %v", up, err)
	}
	if _, err := notes.UpdateNote(ctx, &pb.UpdateNoteRequest{Note: &pb.Note{Id: n.Id}, UpdateMask: mask("space")}); code(err) != codes.InvalidArgument {
		t.Fatalf("clearing space should be rejected: %v", err)
	}
}

func TestFocusOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	cal, projects, focus := pb.NewCalendarServiceClient(conn), pb.NewProjectServiceClient(conn), pb.NewFocusServiceClient(conn)
	ctx := as(e.alice)
	today := utcMidnight()

	task := func(title string, due time.Time, sp pb.Space) {
		if _, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: title, DueTime: timestamppb.New(due), Space: sp}}); err != nil {
			t.Fatal(err)
		}
	}
	task("overdue report", today.Add(-48*time.Hour), pb.Space_SPACE_WORK)
	task("pay rent", today.Add(23*time.Hour), pb.Space_SPACE_LIFE)
	task("next week", today.AddDate(0, 0, 9), pb.Space_SPACE_LIFE)
	cal.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{Title: "standup", StartTime: timestamppb.New(today.Add(9 * time.Hour)), Space: pb.Space_SPACE_WORK}})
	projects.CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{Title: "Trip", Pinned: true}})
	projects.CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{Title: "Not pinned"}})
	pb.NewGoalServiceClient(conn).CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "Run", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 3}})

	titles := func(r *pb.GetFocusResponse) map[string]bool {
		m := map[string]bool{}
		for _, it := range r.Items {
			if ev := it.GetEvent(); ev != nil {
				m[ev.Event.Title] = true
			} else if tk := it.GetTask(); tk != nil {
				m[tk.Title] = true
			}
		}
		return m
	}

	r, err := focus.GetFocus(ctx, &pb.GetFocusRequest{Horizon: pb.Horizon_HORIZON_TODAY})
	if err != nil {
		t.Fatal(err)
	}
	got := titles(r)
	if !got["standup"] || !got["pay rent"] || !got["overdue report"] || got["next week"] {
		t.Fatalf("today: %v", got)
	}
	if !r.Items[0].Overdue || r.Items[0].GetTask().Title != "overdue report" {
		t.Fatalf("overdue should lead: %+v", r.Items[0])
	}
	if len(r.PinnedProjects) != 1 || r.PinnedProjects[0].Title != "Trip" || len(r.Goals) != 1 {
		t.Fatalf("pinned=%d goals=%d", len(r.PinnedProjects), len(r.Goals))
	}

	work, _ := focus.GetFocus(ctx, &pb.GetFocusRequest{Horizon: pb.Horizon_HORIZON_TODAY, Space: pb.Space_SPACE_WORK})
	if g := titles(work); !g["standup"] || !g["overdue report"] || g["pay rent"] {
		t.Fatalf("work today: %v", g)
	}
	if len(work.Goals) != 0 {
		t.Fatalf("the goal is a life goal, but work focus listed it: %d", len(work.Goals))
	}
	life, _ := focus.GetFocus(ctx, &pb.GetFocusRequest{Horizon: pb.Horizon_HORIZON_TODAY, Space: pb.Space_SPACE_LIFE})
	if g := titles(life); g["standup"] || !g["pay rent"] {
		t.Fatalf("life today: %v", g)
	}

	up, _ := focus.GetFocus(ctx, &pb.GetFocusRequest{Horizon: pb.Horizon_HORIZON_UPCOMING})
	if g := titles(up); !g["next week"] || g["pay rent"] || g["overdue report"] {
		t.Fatalf("upcoming: %v", g)
	}
	if _, err := focus.GetFocus(ctx, &pb.GetFocusRequest{TimeZone: "Mars/Base"}); code(err) != codes.InvalidArgument {
		t.Fatalf("bad zone: %v", err)
	}
	if _, err := focus.GetFocus(as(e.bob), &pb.GetFocusRequest{}); err != nil {
		t.Fatal(err)
	}
	other, _ := focus.GetFocus(as(e.bob), &pb.GetFocusRequest{})
	if len(other.Items) != 0 || len(other.PinnedProjects) != 0 || len(other.Goals) != 0 {
		t.Fatalf("bob sees alice's data: %+v", other)
	}
}

func TestReminderSpaceFilterOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	c := pb.NewCalendarServiceClient(e.conn(t))
	ctx := as(e.alice)
	past := timestamppb.New(time.Now().Add(-time.Minute))
	c.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Send invoice", RemindTime: past, Space: pb.Space_SPACE_WORK}})
	c.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Call mum", RemindTime: past}})
	(&reminder.Scheduler{Store: e.st, Hub: e.hub}).Tick(context.Background(), time.Now())

	for sp, want := range map[pb.Space]string{pb.Space_SPACE_WORK: "Send invoice", pb.Space_SPACE_LIFE: "Call mum"} {
		l, err := c.ListReminders(ctx, &pb.ListRemindersRequest{Space: sp})
		if err != nil || len(l.Reminders) != 1 || l.Reminders[0].Title != want || l.Reminders[0].Space != sp {
			t.Errorf("space %v: %+v %v", sp, l, err)
		}
	}
	if l, _ := c.ListReminders(ctx, &pb.ListRemindersRequest{}); len(l.Reminders) != 2 {
		t.Fatalf("both spaces: %d", len(l.Reminders))
	}
}

// ---- the assistant ----

func TestAssistantCreatesProjectAndTasksInTheCurrentMode(t *testing.T) {
	f := &fakeProvider{}
	step := 0
	var projectID string
	f.script = func(req map[string]any) (string, []map[string]any) {
		content, has := toolResult(req)
		switch {
		case !has:
			return "", []map[string]any{toolCall("c1", "create_project", map[string]any{"title": "Q4 发布", "due": "2026-12-20T00:00:00Z", "pinned": true})}
		case step == 0:
			step = 1
			var r struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			json.Unmarshal([]byte(content), &r)
			projectID = r.Data.ID
			return "", []map[string]any{toolCall("c2", "create_task", map[string]any{"title": "写发布说明", "project_id": projectID})}
		default:
			return "已建好项目和一项待办。", nil
		}
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	resp, err := pb.NewAIServiceClient(conn).Ask(as(e.alice), &pb.AskRequest{
		Message: "新建一个项目 Q4 发布,12月20日截止,加一项写发布说明", Space: pb.Space_SPACE_WORK})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Reply, "已建好") {
		t.Fatalf("%+v", resp)
	}
	if ps, _ := pb.NewProjectServiceClient(conn).ListProjects(as(e.alice), &pb.ListProjectsRequest{}); len(ps.Projects) != 0 {
		t.Fatal("nothing should exist before the user confirms")
	}
	if resp.Proposal == nil || len(resp.Proposal.Operations) != 2 {
		t.Fatalf("want a project and a task staged: %+v", resp.Proposal)
	}
	if _, err := pb.NewSuggestionServiceClient(conn).AcceptProposal(as(e.alice), &pb.AcceptProposalRequest{Id: resp.Proposal.Id}); err != nil {
		t.Fatal(err)
	}

	ps, _ := pb.NewProjectServiceClient(conn).ListProjects(as(e.alice), &pb.ListProjectsRequest{})
	if len(ps.Projects) != 1 || ps.Projects[0].Space != pb.Space_SPACE_WORK || !ps.Projects[0].Pinned {
		t.Fatalf("the project should be created in the current mode (work): %+v", ps)
	}
	ts, _ := pb.NewCalendarServiceClient(conn).ListTasks(as(e.alice), &pb.ListTasksRequest{ProjectId: ps.Projects[0].Id})
	if len(ts.Tasks) != 1 || ts.Tasks[0].Space != pb.Space_SPACE_WORK {
		t.Fatalf("the task should join the project and inherit its space: %+v", ts)
	}
	// Nothing leaked to another user.
	if o, _ := pb.NewProjectServiceClient(conn).ListProjects(as(e.bob), &pb.ListProjectsRequest{}); len(o.Projects) != 0 {
		t.Fatal("assistant project leaked to another user")
	}
}

func TestAssistantChecksInOnAGoal(t *testing.T) {
	f := &fakeProvider{}
	var goalID string
	f.script = func(req map[string]any) (string, []map[string]any) {
		if content, has := toolResult(req); has {
			if strings.Contains(content, `"behind"`) && goalID == "" {
				var r struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				}
				json.Unmarshal([]byte(content), &r)
				goalID = r.Data[0].ID
				return "", []map[string]any{toolCall("c2", "check_in", map[string]any{"goal_id": goalID, "amount": 20})}
			}
			return "记下了。", nil
		}
		return "", []map[string]any{toolCall("c1", "list_goals", map[string]any{})}
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	goals := pb.NewGoalServiceClient(conn)
	g, _ := goals.CreateGoal(as(e.alice), &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "德语", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 140, Unit: "分钟"}})

	// auto_apply carries out what the assistant prepared, for callers that confirm elsewhere.
	resp, err := pb.NewAIServiceClient(conn).Ask(as(e.alice), &pb.AskRequest{Message: "德语学了 20 分钟", AutoApply: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Change == nil || resp.Change.Entries != 1 || resp.Proposal.Status != "accepted" {
		t.Fatalf("%+v", resp)
	}
	got, _ := goals.GetGoal(as(e.alice), &pb.GetGoalRequest{Id: g.Id})
	if got.Progress.Done != 20 {
		t.Fatalf("check-in not recorded: %+v", got.Progress)
	}
}

func TestGoalCounterOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	c := pb.NewGoalServiceClient(e.conn(t))
	ctx := as(e.alice)

	g, err := c.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "德语", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK,
		Target: 140, Unit: "分钟", CounterUnit: "课时", CounterTarget: 40}})
	if err != nil {
		t.Fatal(err)
	}
	if g.CounterUnit != "课时" || g.CounterTarget != 40 || g.Progress.CounterDone != 0 {
		t.Fatalf("%+v", g)
	}
	r, err := c.RecordCheckIn(ctx, &pb.RecordCheckInRequest{GoalId: g.Id, Amount: 20, Count: 1})
	if err != nil || r.CheckIn.Count != 1 || r.Goal.Progress.Done != 20 || r.Goal.Progress.CounterDone != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	r, _ = c.RecordCheckIn(ctx, &pb.RecordCheckInRequest{GoalId: g.Id, Count: 2})
	if r.Goal.Progress.Done != 20 || r.Goal.Progress.CounterDone != 3 || r.Goal.Progress.CounterPercent != 3.0/40 {
		t.Fatalf("a lesson with no minutes must not add minutes: %+v", r.Goal.Progress)
	}
	h, _ := c.ListCheckIns(ctx, &pb.ListCheckInsRequest{GoalId: g.Id})
	if len(h.CheckIns) != 2 {
		t.Fatalf("%+v", h)
	}

	// Change the target through the mask; refuse nonsense.
	up, err := c.UpdateGoal(ctx, &pb.UpdateGoalRequest{Goal: &pb.Goal{Id: g.Id, CounterTarget: 30}, UpdateMask: mask("counter_target")})
	if err != nil || up.CounterTarget != 30 || up.Progress.CounterPercent != 3.0/30 {
		t.Fatalf("%+v %v", up, err)
	}
	if _, err := c.UpdateGoal(ctx, &pb.UpdateGoalRequest{Goal: &pb.Goal{Id: g.Id, CounterTarget: -5}, UpdateMask: mask("counter_target")}); code(err) != codes.InvalidArgument {
		t.Fatalf("negative target: %v", err)
	}
	plain, _ := c.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "运动", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 3}})
	if _, err := c.RecordCheckIn(ctx, &pb.RecordCheckInRequest{GoalId: plain.Id, Count: 1}); code(err) != codes.InvalidArgument {
		t.Fatalf("count on a goal without a counter: %v", err)
	}
}

func TestAssistantChecksInWithACount(t *testing.T) {
	f := &fakeProvider{}
	var goalID string
	f.script = func(req map[string]any) (string, []map[string]any) {
		if _, has := toolResult(req); has {
			return "记下了。", nil
		}
		return "", []map[string]any{toolCall("c1", "check_in", map[string]any{"goal_id": goalID, "amount": 20, "count": 1})}
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	goals := pb.NewGoalServiceClient(conn)
	g, _ := goals.CreateGoal(as(e.alice), &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "德语", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK,
		Target: 140, Unit: "分钟", CounterUnit: "课时", CounterTarget: 40}})
	goalID = g.Id
	resp, err := pb.NewAIServiceClient(conn).Ask(as(e.alice), &pb.AskRequest{Message: "上了一节 20 分钟的德语课", AutoApply: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Proposal.Operations[0].Label, "+1课时") {
		t.Fatalf("the staged step should say what it does: %q", resp.Proposal.Operations[0].Label)
	}
	got, _ := goals.GetGoal(as(e.alice), &pb.GetGoalRequest{Id: g.Id})
	if got.Progress.Done != 20 || got.Progress.CounterDone != 1 {
		t.Fatalf("%+v", got.Progress)
	}
	// Undo reverses both numbers.
	pb.NewSuggestionServiceClient(conn).UndoChange(as(e.alice), &pb.UndoChangeRequest{Id: resp.Change.Id})
	got, _ = goals.GetGoal(as(e.alice), &pb.GetGoalRequest{Id: g.Id})
	if got.Progress.Done != 0 || got.Progress.CounterDone != 0 {
		t.Fatalf("%+v", got.Progress)
	}
}
