package server_test

import (
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTaskWorkedOverSeveralSittings(t *testing.T) {
	e := start(t, nil, false)
	cal := pb.NewCalendarServiceClient(e.conn(t))
	ctx := as(e.alice)

	task, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "写年度总结"}})
	if err != nil {
		t.Fatal(err)
	}
	if task.Progress != 0 || task.WorkedMinutes != 0 || task.WorkSessions != 0 || task.LastWorkedTime != nil {
		t.Fatalf("a new task has no work: %+v", task)
	}

	// Yesterday: 45 minutes, about 30% done. Today: 30 more, no estimate.
	yesterday := time.Now().Add(-24 * time.Hour).Truncate(time.Millisecond)
	today := time.Now().Truncate(time.Millisecond)
	got, err := cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: task.Id, Time: timestamppb.New(yesterday), Minutes: 45, Progress: proto.Int32(30), Note: "提纲"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Progress != 30 || got.WorkedMinutes != 45 || got.WorkSessions != 1 || !got.LastWorkedTime.AsTime().Equal(yesterday) {
		t.Fatalf("after one sitting: %+v", got)
	}
	got, err = cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: task.Id, Time: timestamppb.New(today), Minutes: 30})
	if err != nil {
		t.Fatal(err)
	}
	if got.Progress != 30 || got.WorkedMinutes != 75 || got.WorkSessions != 2 || !got.LastWorkedTime.AsTime().Equal(today) {
		t.Fatalf("a sitting without progress keeps the last one: %+v", got)
	}
	if got.Completed {
		t.Fatal("work alone does not complete the task")
	}

	// A later estimate wins; one logged for earlier does not override it.
	if got, err = cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: task.Id, Time: timestamppb.New(today.Add(time.Minute)), Progress: proto.Int32(60)}); err != nil {
		t.Fatal(err)
	}
	if got.Progress != 60 {
		t.Fatalf("latest progress: %d", got.Progress)
	}
	backfill, err := cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: task.Id, Time: timestamppb.New(yesterday.Add(-time.Hour)), Minutes: 10, Progress: proto.Int32(10)})
	if err != nil {
		t.Fatal(err)
	}
	if backfill.Progress != 60 || backfill.WorkedMinutes != 85 || backfill.WorkSessions != 4 || !backfill.LastWorkedTime.AsTime().Equal(today.Add(time.Minute)) {
		t.Fatalf("a back-filled sitting: %+v", backfill)
	}
	// Progress 0 is a real estimate, not "not given".
	if got, err = cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: task.Id, Time: timestamppb.New(today.Add(2 * time.Minute)), Progress: proto.Int32(0), Note: "推倒重来"}); err != nil {
		t.Fatal(err)
	}
	if got.Progress != 0 || got.WorkSessions != 5 {
		t.Fatalf("progress 0: %+v", got)
	}

	list, err := cal.ListTaskWork(ctx, &pb.ListTaskWorkRequest{TaskId: task.Id})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 5 {
		t.Fatalf("entries: %d", len(list.Entries))
	}
	for i := 1; i < len(list.Entries); i++ {
		if list.Entries[i].Time.AsTime().After(list.Entries[i-1].Time.AsTime()) {
			t.Fatal("entries are not newest first")
		}
	}
	newest, oldest := list.Entries[0], list.Entries[4]
	if newest.Progress != 0 || newest.Note != "推倒重来" || newest.TaskId != task.Id {
		t.Fatalf("newest: %+v", newest)
	}
	if oldest.Minutes != 10 || oldest.Progress != 10 {
		t.Fatalf("oldest: %+v", oldest)
	}
	if list.Entries[3].Note != "提纲" || list.Entries[2].Progress != -1 {
		t.Fatalf("a sitting without progress reports -1: %+v", list.Entries[2])
	}

	// Deleting a sitting recomputes the totals: progress falls back to 60.
	got, err = cal.DeleteTaskWork(ctx, &pb.DeleteTaskWorkRequest{Id: newest.Id})
	if err != nil {
		t.Fatal(err)
	}
	if got.Progress != 60 || got.WorkedMinutes != 85 || got.WorkSessions != 4 || !got.LastWorkedTime.AsTime().Equal(today.Add(time.Minute)) {
		t.Fatalf("after deleting the newest: %+v", got)
	}
	if _, err := cal.DeleteTaskWork(ctx, &pb.DeleteTaskWorkRequest{Id: newest.Id}); code(err) != codes.NotFound {
		t.Fatalf("deleting twice: %v", err)
	}
	// GetTask reports the same totals.
	if g, err := cal.GetTask(ctx, &pb.GetTaskRequest{Id: task.Id}); err != nil || g.WorkedMinutes != 85 || g.WorkSessions != 4 || g.Progress != 60 {
		t.Fatalf("get: %+v %v", g, err)
	}
}

func TestTaskWorkAtFullProgressCompletesTheTask(t *testing.T) {
	e := start(t, nil, false)
	cal := pb.NewCalendarServiceClient(e.conn(t))
	ctx := as(e.alice)

	task, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "装修预算"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: task.Id, Minutes: 20, Progress: proto.Int32(50)}); err != nil {
		t.Fatal(err)
	}
	done, err := cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: task.Id, Minutes: 40, Progress: proto.Int32(100)})
	if err != nil {
		t.Fatal(err)
	}
	if !done.Completed || done.CompleteTime == nil || done.Progress != 100 || done.WorkedMinutes != 60 {
		t.Fatalf("progress 100: %+v", done)
	}
	open, err := cal.ListTasks(ctx, &pb.ListTasksRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(open.Tasks) != 0 {
		t.Fatalf("the task is still open: %v", open.Tasks)
	}

	// Removing the finishing sitting recomputes the totals but leaves the task done.
	list, err := cal.ListTaskWork(ctx, &pb.ListTaskWorkRequest{TaskId: task.Id})
	if err != nil {
		t.Fatal(err)
	}
	got, err := cal.DeleteTaskWork(ctx, &pb.DeleteTaskWorkRequest{Id: list.Entries[0].Id})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Completed || got.Progress != 50 || got.WorkedMinutes != 20 || got.WorkSessions != 1 {
		t.Fatalf("after deleting the finishing sitting: %+v", got)
	}
}

func TestTaskWorkValidation(t *testing.T) {
	e := start(t, nil, false)
	cal := pb.NewCalendarServiceClient(e.conn(t))
	ctx := as(e.alice)

	task, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "读论文"}})
	if err != nil {
		t.Fatal(err)
	}
	for name, req := range map[string]*pb.LogTaskWorkRequest{
		"nothing":           {TaskId: task.Id},
		"blank note":        {TaskId: task.Id, Note: "   "},
		"negative minutes":  {TaskId: task.Id, Minutes: -5},
		"more than a day":   {TaskId: task.Id, Minutes: 24*60 + 1},
		"negative progress": {TaskId: task.Id, Minutes: 10, Progress: proto.Int32(-1)},
		"progress over 100": {TaskId: task.Id, Progress: proto.Int32(101)},
	} {
		if _, err := cal.LogTaskWork(ctx, req); code(err) != codes.InvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The edges are fine: a whole day, or just a note.
	if _, err := cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: task.Id, Minutes: 24 * 60}); err != nil {
		t.Fatal(err)
	}
	if _, err := cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: task.Id, Note: "第三节没看懂"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: "nope", Minutes: 5}); code(err) != codes.NotFound {
		t.Fatalf("unknown task: %v", err)
	}
	if _, err := cal.ListTaskWork(ctx, &pb.ListTaskWorkRequest{TaskId: "nope"}); code(err) != codes.NotFound {
		t.Fatalf("listing an unknown task: %v", err)
	}
	got, err := cal.GetTask(ctx, &pb.GetTaskRequest{Id: task.Id})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkSessions != 2 || got.WorkedMinutes != 24*60 || got.Progress != 0 {
		t.Fatalf("rejected sittings left a trace: %+v", got)
	}
}

func TestTaskWorkIsPerUserAndGoesWithTheTask(t *testing.T) {
	e := start(t, nil, false)
	cal := pb.NewCalendarServiceClient(e.conn(t))
	alice, bob := as(e.alice), as(e.bob)

	task, err := cal.CreateTask(alice, &pb.CreateTaskRequest{Task: &pb.Task{Title: "整理照片"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cal.LogTaskWork(alice, &pb.LogTaskWorkRequest{TaskId: task.Id, Minutes: 25, Progress: proto.Int32(20)}); err != nil {
		t.Fatal(err)
	}
	list, err := cal.ListTaskWork(alice, &pb.ListTaskWorkRequest{TaskId: task.Id})
	if err != nil || len(list.Entries) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	entry := list.Entries[0]

	if _, err := cal.LogTaskWork(bob, &pb.LogTaskWorkRequest{TaskId: task.Id, Minutes: 10}); code(err) != codes.NotFound {
		t.Fatalf("bob logs on alice's task: %v", err)
	}
	if _, err := cal.ListTaskWork(bob, &pb.ListTaskWorkRequest{TaskId: task.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob lists alice's sittings: %v", err)
	}
	if _, err := cal.DeleteTaskWork(bob, &pb.DeleteTaskWorkRequest{Id: entry.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob deletes alice's sitting: %v", err)
	}
	got, err := cal.GetTask(alice, &pb.GetTaskRequest{Id: task.Id})
	if err != nil || got.WorkSessions != 1 || got.WorkedMinutes != 25 {
		t.Fatalf("bob changed alice's task: %+v %v", got, err)
	}

	// Deleting the task takes its sittings with it.
	if _, err := cal.DeleteTask(alice, &pb.DeleteTaskRequest{Id: task.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := cal.ListTaskWork(alice, &pb.ListTaskWorkRequest{TaskId: task.Id}); code(err) != codes.NotFound {
		t.Fatalf("listing a deleted task: %v", err)
	}
	if _, err := cal.DeleteTaskWork(alice, &pb.DeleteTaskWorkRequest{Id: entry.Id}); code(err) != codes.NotFound {
		t.Fatalf("a deleted task's sitting is still there: %v", err)
	}
}
