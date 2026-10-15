package server_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/export"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTimeEntriesOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	tm, goals, projects := pb.NewTimeServiceClient(conn), pb.NewGoalServiceClient(conn), pb.NewProjectServiceClient(conn)

	study, err := goals.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "English", Period: pb.GoalPeriod_GOAL_PERIOD_DAY, Target: 60, Unit: "分钟", Space: pb.Space_SPACE_WORK}})
	if err != nil {
		t.Fatal(err)
	}
	swim, err := goals.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "Swim", Period: pb.GoalPeriod_GOAL_PERIOD_DAY, Target: 1}})
	if err != nil {
		t.Fatal(err)
	}
	app, err := projects.CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{Title: "App", Space: pb.Space_SPACE_WORK}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// Entries start a minute or two ago, so "today" holds them at any hour.
	at := func(d time.Duration) *timestamppb.Timestamp { return timestamppb.New(now.Add(-d)) }

	// A minutes goal gets the minutes; a count goal one session; the space comes from the goal.
	en, err := tm.CreateTimeEntry(ctx, &pb.CreateTimeEntryRequest{Entry: &pb.TimeEntry{Title: "英语学习", Minutes: 30, GoalId: study.Id, StartTime: at(time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	if en.CheckInId == "" || en.Space != pb.Space_SPACE_WORK || en.EndTime == nil || en.Running {
		t.Fatalf("%+v", en)
	}
	if _, err := tm.CreateTimeEntry(ctx, &pb.CreateTimeEntryRequest{Entry: &pb.TimeEntry{Title: "游泳", Minutes: 40, GoalId: swim.Id, StartTime: at(2 * time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	if g, _ := goals.GetGoal(ctx, &pb.GetGoalRequest{Id: study.Id}); g.Progress.Done != 30 {
		t.Fatalf("study done = %v", g.Progress.Done)
	}
	if g, _ := goals.GetGoal(ctx, &pb.GetGoalRequest{Id: swim.Id}); g.Progress.Done != 1 || !g.Progress.Achieved {
		t.Fatalf("swim done = %v", g.Progress.Done)
	}

	// The check-in follows the entry and cannot be undone on its own.
	cs, _ := goals.ListCheckIns(ctx, &pb.ListCheckInsRequest{GoalId: study.Id})
	if len(cs.CheckIns) != 1 || cs.CheckIns[0].TimeEntryId != en.Id {
		t.Fatalf("check-ins: %+v", cs.CheckIns)
	}
	if _, err := goals.DeleteCheckIn(ctx, &pb.DeleteCheckInRequest{Id: cs.CheckIns[0].Id}); code(err) != codes.FailedPrecondition {
		t.Fatalf("DeleteCheckIn on an entry's check-in: %v", err)
	}
	en, err = tm.UpdateTimeEntry(ctx, &pb.UpdateTimeEntryRequest{Entry: &pb.TimeEntry{Id: en.Id, Minutes: 45, ProjectId: app.Id},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"minutes", "project_id"}}})
	if err != nil || en.Minutes != 45 {
		t.Fatalf("%v %+v", err, en)
	}
	if g, _ := goals.GetGoal(ctx, &pb.GetGoalRequest{Id: study.Id}); g.Progress.Done != 45 {
		t.Fatalf("after editing: %v", g.Progress.Done)
	}
	if _, err := tm.UpdateTimeEntry(ctx, &pb.UpdateTimeEntryRequest{Entry: &pb.TimeEntry{Id: en.Id}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"running"}}}); code(err) != codes.InvalidArgument {
		t.Fatalf("a read-only field in the mask: %v", err)
	}

	// The project shows it, with the total.
	d, err := projects.GetProject(ctx, &pb.GetProjectRequest{Id: app.Id})
	if err != nil || len(d.TimeEntries) != 1 || d.Project.Progress.TimeMinutes != 45 {
		t.Fatalf("project detail: %v %+v", err, d.GetProject().GetProgress())
	}

	// Timer: one at a time; stopping makes the check-in.
	run, err := tm.StartTimer(ctx, &pb.StartTimerRequest{Title: "英语学习", GoalId: study.Id})
	if err != nil || !run.Running || run.EndTime != nil {
		t.Fatalf("%v %+v", err, run)
	}
	if _, err := tm.StartTimer(ctx, &pb.StartTimerRequest{Title: "游泳"}); code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), run.Id) {
		t.Fatalf("a second timer should name the running one: %v", err)
	}
	if r, _ := tm.GetRunningTimer(ctx, &pb.GetRunningTimerRequest{}); r.GetEntry().GetId() != run.Id {
		t.Fatalf("running timer: %+v", r)
	}
	if f, _ := pb.NewFocusServiceClient(conn).GetFocus(ctx, &pb.GetFocusRequest{}); f.GetRunningTimer().GetId() != run.Id {
		t.Fatal("the overview should show the running timer")
	}
	stopped, err := tm.StopTimer(ctx, &pb.StopTimerRequest{EndTime: timestamppb.New(time.Now().Add(20 * time.Minute))})
	if code(err) != codes.InvalidArgument {
		t.Fatalf("an end in the future: %v %+v", err, stopped)
	}
	stopped, err = tm.StopTimer(ctx, &pb.StopTimerRequest{})
	if err != nil || stopped.Running || stopped.Minutes != 1 || stopped.CheckInId == "" {
		t.Fatalf("%v %+v", err, stopped)
	}
	if g, _ := goals.GetGoal(ctx, &pb.GetGoalRequest{Id: study.Id}); g.Progress.Done != 46 {
		t.Fatalf("after the timer: %v", g.Progress.Done)
	}
	if _, err := tm.StopTimer(ctx, &pb.StopTimerRequest{}); code(err) != codes.FailedPrecondition {
		t.Fatalf("stop with none running: %v", err)
	}
	if r, _ := tm.GetRunningTimer(ctx, &pb.GetRunningTimerRequest{}); r.Entry != nil {
		t.Fatalf("no timer should run: %+v", r)
	}

	// Summary by activity, by goal; paging.
	sum, err := tm.SummarizeTime(ctx, &pb.SummarizeTimeRequest{})
	if err != nil || sum.TotalMinutes != 86 || sum.TotalCount != 3 || len(sum.Rows) != 2 || sum.GroupBy != pb.TimeGroupBy_TIME_GROUP_BY_TITLE {
		t.Fatalf("%v %+v", err, sum)
	}
	if r := sum.Rows[0]; r.Label != "英语学习" || r.Minutes != 46 || r.Count != 2 {
		t.Fatalf("top row: %+v", r)
	}
	byGoal, _ := tm.SummarizeTime(ctx, &pb.SummarizeTimeRequest{GroupBy: pb.TimeGroupBy_TIME_GROUP_BY_GOAL})
	if len(byGoal.Rows) != 2 || byGoal.Rows[0].Key != study.Id || byGoal.Rows[0].Label != "English" {
		t.Fatalf("by goal: %+v", byGoal.Rows)
	}
	if work, _ := tm.SummarizeTime(ctx, &pb.SummarizeTimeRequest{Space: pb.Space_SPACE_LIFE}); work.TotalMinutes != 40 {
		t.Fatalf("life only: %+v", work)
	}
	page1, _ := tm.ListTimeEntries(ctx, &pb.ListTimeEntriesRequest{PageSize: 2})
	if len(page1.Entries) != 2 || page1.NextPageToken == "" || page1.Entries[0].Id != stopped.Id {
		t.Fatalf("page 1: %+v", page1)
	}
	page2, _ := tm.ListTimeEntries(ctx, &pb.ListTimeEntriesRequest{PageSize: 2, PageToken: page1.NextPageToken})
	if len(page2.Entries) != 1 || page2.NextPageToken != "" {
		t.Fatalf("page 2: %+v", page2)
	}

	// Bob sees and touches nothing of it.
	bob := as(e.bob)
	if l, _ := tm.ListTimeEntries(bob, &pb.ListTimeEntriesRequest{}); len(l.Entries) != 0 {
		t.Fatalf("bob lists %d entries", len(l.Entries))
	}
	if _, err := tm.GetTimeEntry(bob, &pb.GetTimeEntryRequest{Id: en.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob read alice's entry: %v", err)
	}
	if _, err := tm.CreateTimeEntry(bob, &pb.CreateTimeEntryRequest{Entry: &pb.TimeEntry{Title: "x", Minutes: 5, GoalId: study.Id}}); code(err) != codes.InvalidArgument {
		t.Fatalf("bob logged time on alice's goal: %v", err)
	}
	if _, err := tm.StartTimer(bob, &pb.StartTimerRequest{Title: "own timer"}); err != nil {
		t.Fatalf("bob's own timer is independent of alice's: %v", err)
	}

	// Deleting the entry takes its check-in; deleting the project keeps the entries.
	if _, err := tm.DeleteTimeEntry(ctx, &pb.DeleteTimeEntryRequest{Id: stopped.Id}); err != nil {
		t.Fatal(err)
	}
	if g, _ := goals.GetGoal(ctx, &pb.GetGoalRequest{Id: study.Id}); g.Progress.Done != 45 {
		t.Fatalf("after deleting the timer's entry: %v", g.Progress.Done)
	}
	if _, err := projects.DeleteProject(ctx, &pb.DeleteProjectRequest{Id: app.Id}); err != nil {
		t.Fatal(err)
	}
	if got, err := tm.GetTimeEntry(ctx, &pb.GetTimeEntryRequest{Id: en.Id}); err != nil || got.ProjectId != "" {
		t.Fatalf("the entry should stay, outside the project: %v %+v", err, got)
	}
}

func TestTimeInWeeklyReviewAndExport(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	tm := pb.NewTimeServiceClient(conn)
	p, _ := pb.NewProjectServiceClient(conn).CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{Title: "菲律宾旅行"}})
	g, _ := pb.NewGoalServiceClient(conn).CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "运动", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 3, Unit: "次"}})
	for _, en := range []*pb.TimeEntry{
		{Title: "游泳", Minutes: 30, GoalId: g.Id, StartTime: timestamppb.New(time.Now().Add(-2 * time.Minute))},
		{Title: "订酒店", Minutes: 20, ProjectId: p.Id, Note: "=cmd", StartTime: timestamppb.New(time.Now().Add(-time.Minute))},
	} {
		if _, err := tm.CreateTimeEntry(ctx, &pb.CreateTimeEntryRequest{Entry: en}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tm.CreateTimeEntry(as(e.bob), &pb.CreateTimeEntryRequest{Entry: &pb.TimeEntry{Title: "bob secret", Minutes: 5}}); err != nil {
		t.Fatal(err)
	}

	r, err := pb.NewSuggestionServiceClient(conn).GetWeeklyReview(ctx, &pb.GetWeeklyReviewRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if r.TimeMinutes != 50 || len(r.TopActivities) != 2 || r.TopActivities[0].Label != "游泳" {
		t.Fatalf("weekly review time: %d %+v", r.TimeMinutes, r.TopActivities)
	}

	stream, err := pb.NewExportServiceClient(conn).Export(ctx, &pb.ExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for {
		c, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(c.Data)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		files[f.Name] = string(b)
	}
	var d export.Data
	if err := json.Unmarshal([]byte(files["noted.json"]), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Time) != 2 || d.Time[0].Title != "游泳" || d.Time[0].Goal != g.Id || d.Time[0].Minutes != 30 || d.Time[0].End == nil {
		t.Fatalf("time entries: %+v", d.Time)
	}
	if len(d.Goals) != 1 || len(d.Goals[0].CheckIns) != 1 || d.Goals[0].CheckIns[0].Entry != d.Time[0].ID {
		t.Fatalf("the goal's check-in should name its entry: %+v", d.Goals)
	}
	csv := files["time_entries.csv"]
	if !strings.HasPrefix(csv, "start,end,minutes,activity,project,goal,space,note\n") || !strings.Contains(csv, ",30,游泳,,运动,life,") ||
		!strings.Contains(csv, ",20,订酒店,菲律宾旅行,,life,'=cmd") || strings.Contains(csv, "bob secret") {
		t.Fatalf("time_entries.csv: %q", csv)
	}
}

func TestAssistantLogsTimeAndUndoTakesItBack(t *testing.T) {
	f := &fakeProvider{}
	var goalID string
	f.script = func(req map[string]any) (string, []map[string]any) {
		if _, has := toolResult(req); has {
			return "记下了。", nil
		}
		return "", []map[string]any{toolCall("c1", "log_time", map[string]any{"title": "英语学习", "minutes": 30, "goal_id": goalID})}
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	goals := pb.NewGoalServiceClient(conn)
	g, _ := goals.CreateGoal(as(e.alice), &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "英语", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 140, Unit: "分钟"}})
	goalID = g.Id
	resp, err := pb.NewAIServiceClient(conn).Ask(as(e.alice), &pb.AskRequest{Message: "英语学习 30 分钟", AutoApply: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Change == nil || !strings.Contains(resp.Proposal.Operations[0].Label, "英语学习 30") {
		t.Fatalf("the step should be applied and say what it does: %+v", resp.Proposal)
	}
	tm := pb.NewTimeServiceClient(conn)
	if l, _ := tm.ListTimeEntries(as(e.alice), &pb.ListTimeEntriesRequest{}); len(l.Entries) != 1 || l.Entries[0].GoalId != g.Id {
		t.Fatalf("entries: %+v", l)
	}
	if got, _ := goals.GetGoal(as(e.alice), &pb.GetGoalRequest{Id: g.Id}); got.Progress.Done != 30 {
		t.Fatalf("goal: %+v", got.Progress)
	}
	if _, err := pb.NewSuggestionServiceClient(conn).UndoChange(as(e.alice), &pb.UndoChangeRequest{Id: resp.Change.Id}); err != nil {
		t.Fatal(err)
	}
	if l, _ := tm.ListTimeEntries(as(e.alice), &pb.ListTimeEntriesRequest{}); len(l.Entries) != 0 {
		t.Fatalf("undo should remove the entry: %+v", l)
	}
	if got, _ := goals.GetGoal(as(e.alice), &pb.GetGoalRequest{Id: g.Id}); got.Progress.Done != 0 {
		t.Fatalf("and its minutes: %+v", got.Progress)
	}
}
