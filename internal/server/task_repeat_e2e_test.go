package server_test

import (
	"context"
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/export"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func openTasks(t *testing.T, cal pb.CalendarServiceClient, ctx context.Context) []*pb.Task {
	t.Helper()
	r, err := cal.ListTasks(ctx, &pb.ListTasksRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return r.Tasks
}

func TestRepeatingTasks(t *testing.T) {
	e := start(t, nil, false)
	sh, _ := time.LoadLocation("Asia/Shanghai")
	e.st.SetLocation(sh)
	cal := pb.NewCalendarServiceClient(e.conn(t))
	ctx := as(e.alice)

	due := time.Date(2026, 10, 12, 9, 0, 0, 0, sh)
	task, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{
		Title: "背单词", DueTime: timestamppb.New(due), RemindTime: timestamppb.New(due.Add(-10 * time.Minute)),
		Priority: pb.Priority_PRIORITY_HIGH, Tags: []string{"英语"}, Space: pb.Space_SPACE_WORK, Rrule: "FREQ=DAILY"}})
	if err != nil {
		t.Fatal(err)
	}
	if task.Rrule != "FREQ=DAILY" {
		t.Fatalf("rrule: %q", task.Rrule)
	}

	// Completing returns the completed task; the next one shows up in the list.
	done, err := cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: task.Id, Completed: true},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"completed"}}})
	if err != nil {
		t.Fatal(err)
	}
	if done.Id != task.Id || !done.Completed || !done.DueTime.AsTime().Equal(due) {
		t.Fatalf("completing returns the completed task: %+v", done)
	}
	open := openTasks(t, cal, ctx)
	if len(open) != 1 {
		t.Fatalf("open: %+v", open)
	}
	next := open[0]
	nextDue := time.Date(2026, 10, 13, 9, 0, 0, 0, sh)
	if next.Id == task.Id || next.Title != "背单词" || next.Rrule != "FREQ=DAILY" || next.Priority != pb.Priority_PRIORITY_HIGH ||
		next.Space != pb.Space_SPACE_WORK || len(next.Tags) != 1 || !next.DueTime.AsTime().Equal(nextDue) ||
		!next.RemindTime.AsTime().Equal(nextDue.Add(-10*time.Minute)) {
		t.Fatalf("next: %+v", next)
	}

	// Reopening the first one leaves the next one alone.
	if _, err := cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: task.Id, Completed: false},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"completed"}}}); err != nil {
		t.Fatal(err)
	}
	if open := openTasks(t, cal, ctx); len(open) != 2 {
		t.Fatalf("after reopening: %+v", open)
	}

	// The mask takes rrule; LogTaskWork reaching 100% completes and repeats.
	got, err := cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: next.Id, Rrule: "FREQ=WEEKLY;BYDAY=TU,TH;COUNT=2"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"rrule"}}})
	if err != nil || got.Rrule != "FREQ=WEEKLY;BYDAY=TU,TH;COUNT=2" {
		t.Fatalf("update rrule: %v %+v", err, got)
	}
	worked, err := cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: next.Id, Minutes: 20, Progress: proto.Int32(100)})
	if err != nil || !worked.Completed || worked.Id != next.Id || worked.WorkSessions != 1 {
		t.Fatalf("log 100%%: %v %+v", err, worked)
	}
	var third *pb.Task
	for _, o := range openTasks(t, cal, ctx) {
		if o.Id != task.Id {
			third = o
		}
	}
	if third == nil || !third.DueTime.AsTime().Equal(time.Date(2026, 10, 15, 9, 0, 0, 0, sh)) || third.WorkSessions != 0 || third.Progress != 0 {
		t.Fatalf("after work: %+v", third)
	}
	if _, err := cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: third.Id, Completed: true},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"completed"}}}); err != nil {
		t.Fatal(err)
	}
	if open := openTasks(t, cal, ctx); len(open) != 1 || open[0].Id != task.Id {
		t.Fatalf("COUNT=2 is over, only the reopened first task is left: %+v", open)
	}

	// Invalid rules, and a rule without a due time.
	for _, tk := range []*pb.Task{
		{Title: "x", DueTime: timestamppb.New(due), Rrule: "FREQ=SECONDLY"},
		{Title: "x", DueTime: timestamppb.New(due), Rrule: "FREQ=DAILY;COUNT=0"},
		{Title: "x", Rrule: "FREQ=DAILY"},
	} {
		if _, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: tk}); code(err) != codes.InvalidArgument {
			t.Errorf("create %q: %v", tk.Rrule, err)
		}
	}
	if _, err := cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: task.Id},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"due_time"}}}); code(err) != codes.InvalidArgument {
		t.Errorf("clearing the due of a repeating task: %v", err)
	}
	plain, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "plain"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: plain.Id, Rrule: "FREQ=DAILY"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"rrule"}}}); code(err) != codes.InvalidArgument {
		t.Errorf("a rule on an undated task: %v", err)
	}

	// Export: rrule in the JSON, RRULE (with COUNT counted down) on the open VTODO.
	d, err := export.Collect(context.Background(), e.st, userID(t, e, "alice"), "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rules := 0
	for _, x := range d.Tasks {
		if x.RRule != "" {
			rules++
		}
	}
	if rules != 3 {
		t.Fatalf("tasks with rrule in the export: %d", rules)
	}
	ics := export.ICS(d)
	if strings.Count(ics, "RRULE:") != 1 || !strings.Contains(ics, "RRULE:FREQ=DAILY\r\n") || !strings.Contains(ics, "DTSTART:20261012T010000Z\r\n") {
		t.Fatalf("calendar.ics: only the open task repeats:\n%s", ics)
	}
}

func userID(t *testing.T, e *env, name string) string {
	t.Helper()
	u, err := e.st.EnsureUser(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestICSCountsDownARepeatingTask(t *testing.T) {
	due := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	d := &export.Data{ExportedAt: due, Tasks: []export.Task{
		{ID: "a", Title: "a", Due: &due, RRule: "FREQ=DAILY;COUNT=5", Occurrence: 3},
		{ID: "b", Title: "b", Due: &due, RRule: "FREQ=DAILY;COUNT=5", Occurrence: 2, Done: true},
	}}
	ics := export.ICS(d)
	if strings.Count(ics, "RRULE:") != 1 || !strings.Contains(ics, "RRULE:FREQ=DAILY;COUNT=3\r\n") {
		t.Fatalf("%s", ics)
	}
}

func TestMCPRepeatingTask(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)

	out, isErr := call(t, cs, "create_task", map[string]any{"title": "Water plants", "due": "2030-11-01T09:00:00Z", "rrule": "FREQ=WEEKLY;BYDAY=FR"})
	if isErr || !has(`"rrule":\s*"FREQ=WEEKLY;BYDAY=FR"`, out) {
		t.Fatalf("create_task: %v %s", isErr, out)
	}
	id := firstID(out)
	if out, _ := call(t, cs, "get_task", map[string]any{"id": id}); !has(`"rrule":\s*"FREQ=WEEKLY;BYDAY=FR"`, out) {
		t.Fatalf("get_task: %s", out)
	}
	if out, isErr := call(t, cs, "complete_task", map[string]any{"id": id}); isErr || !has(`"completed":\s*true`, out) || !strings.Contains(out, id) {
		t.Fatalf("complete_task: %v %s", isErr, out)
	}
	out, _ = call(t, cs, "list_tasks", map[string]any{})
	if !strings.Contains(out, "2030-11-08T09:00:00Z") || !has(`"rrule":\s*"FREQ=WEEKLY;BYDAY=FR"`, out) || strings.Contains(out, id) {
		t.Fatalf("list_tasks shows the next one: %s", out)
	}
	if _, isErr := call(t, cs, "create_task", map[string]any{"title": "x", "rrule": "FREQ=DAILY"}); !isErr {
		t.Fatal("a rule without due should be a tool error")
	}
	if _, isErr := call(t, cs, "create_task", map[string]any{"title": "x", "due": "2030-11-01T09:00:00Z", "rrule": "FREQ=MINUTELY"}); !isErr {
		t.Fatal("a bad rule should be a tool error")
	}
	if out, isErr := call(t, cs, "update_task", map[string]any{"id": id, "rrule": ""}); isErr || has(`"rrule"`, out) {
		t.Fatalf("update_task clearing rrule: %v %s", isErr, out)
	}
}
