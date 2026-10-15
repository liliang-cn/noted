package server_test

import (
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestObjectiveMilestonesOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	obj, goals := pb.NewObjectiveServiceClient(e.conn(t)), pb.NewGoalServiceClient(e.conn(t))
	ctx := as(e.alice)

	due := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	o, err := obj.CreateObjective(ctx, &pb.CreateObjectiveRequest{Objective: &pb.Objective{Title: "Deutsch B1", Milestones: []*pb.Milestone{
		{Title: "A1", DueTime: timestamppb.New(due), Criteria: "Goethe A1 passed"},
		{Title: "A2"},
		{Title: "B1", Done: true}, // done is not taken from the client
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Milestones) != 3 || o.Milestones[0].Criteria != "Goethe A1 passed" || !o.Milestones[0].DueTime.AsTime().Equal(due) || o.Milestones[2].Done {
		t.Fatalf("created: %+v", o.Milestones)
	}
	p := o.Progress
	if p.MilestonesTotal != 3 || p.MilestonesDone != 0 || p.MilestonesOverdue != 1 || !p.Behind || p.OnTrack || p.NextMilestone.GetTitle() != "A1" {
		t.Fatalf("progress: %+v", p)
	}

	// A dimension at 100% does not lead while there are milestones.
	g, err := goals.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "study", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 1, ObjectiveId: o.Id}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goals.RecordCheckIn(ctx, &pb.RecordCheckInRequest{GoalId: g.Id}); err != nil {
		t.Fatal(err)
	}
	o, err = obj.SetObjectiveMilestoneDone(ctx, &pb.SetObjectiveMilestoneDoneRequest{ObjectiveId: o.Id, MilestoneId: o.Milestones[0].Id, Done: true})
	if err != nil {
		t.Fatal(err)
	}
	p = o.Progress
	if !o.Milestones[0].Done || o.Milestones[0].DoneTime == nil || p.MilestonesDone != 1 || !near(p.MilestonePercent, 1.0/3) ||
		!near(p.Percent, 1.0/3) || !near(p.GoalsPercent, 1) || p.Behind || !p.OnTrack || p.NextMilestone.GetTitle() != "A2" {
		t.Fatalf("after ticking A1: %+v", p)
	}

	// Replace the list: keep A1 (done) by id, drop A2, rename B1, add C1.
	a1, b1 := o.Milestones[0].Id, o.Milestones[2].Id
	o, err = obj.UpdateObjective(ctx, &pb.UpdateObjectiveRequest{
		Objective:  &pb.Objective{Id: o.Id, Milestones: []*pb.Milestone{{Id: a1, Title: "A1"}, {Id: b1, Title: "B1 exam"}, {Title: "C1"}}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"milestones"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Milestones) != 3 || o.Milestones[0].Id != a1 || !o.Milestones[0].Done || o.Milestones[1].Id != b1 ||
		o.Milestones[1].Title != "B1 exam" || o.Milestones[2].Title != "C1" || o.Milestones[2].Done {
		t.Fatalf("replaced: %+v", o.Milestones)
	}

	if _, err := obj.SetObjectiveMilestoneDone(ctx, &pb.SetObjectiveMilestoneDoneRequest{ObjectiveId: o.Id, MilestoneId: "nope", Done: true}); code(err) != codes.NotFound {
		t.Fatalf("unknown milestone: %v", err)
	}
	if _, err := obj.SetObjectiveMilestoneDone(as(e.bob), &pb.SetObjectiveMilestoneDoneRequest{ObjectiveId: o.Id, MilestoneId: a1, Done: false}); code(err) != codes.NotFound {
		t.Fatalf("bob: %v", err)
	}
	if _, err := obj.SetObjectiveMilestoneDone(ctx, &pb.SetObjectiveMilestoneDoneRequest{ObjectiveId: o.Id}); code(err) != codes.InvalidArgument {
		t.Fatalf("no milestone id: %v", err)
	}

	// The list carries them too.
	l, err := obj.ListObjectives(ctx, &pb.ListObjectivesRequest{})
	if err != nil || len(l.Objectives) != 1 || len(l.Objectives[0].Milestones) != 3 || l.Objectives[0].Progress.MilestonesDone != 1 {
		t.Fatalf("list: %v %+v", err, l)
	}
}

func TestMCPObjectiveMilestones(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)

	_, sc, isErr := callS(t, cs, "create_objective", map[string]any{"title": "Philippines trip", "milestones": []any{
		map[string]any{"title": "Visa", "due": "2020-01-01T00:00:00Z", "criteria": "visa in passport"},
		map[string]any{"title": "Flights"},
	}})
	if isErr {
		t.Fatalf("create_objective: %v", sc)
	}
	ms := sc["milestones"].([]any)
	visa := ms[0].(map[string]any)
	if len(ms) != 2 || visa["criteria"] != "visa in passport" || visa["dueTime"] == nil {
		t.Fatalf("milestones: %v", ms)
	}
	p := sc["progress"].(map[string]any)
	if p["milestonesTotal"] != 2.0 || p["milestonesOverdue"] != 1.0 || p["onTrack"] != false || p["behind"] != true {
		t.Fatalf("progress: %v", p)
	}
	if next := p["nextMilestone"].(map[string]any); next["title"] != "Visa" {
		t.Fatalf("next: %v", next)
	}

	_, sc, isErr = callS(t, cs, "set_objective_milestone_done", map[string]any{"objective_id": sc["id"], "milestone_id": visa["id"], "done": true})
	if isErr {
		t.Fatalf("set_objective_milestone_done: %v", sc)
	}
	p = sc["progress"].(map[string]any)
	if p["milestonesDone"] != 1.0 || !near(p["percent"].(float64), 0.5) || p["onTrack"] != true || p["nextMilestone"].(map[string]any)["title"] != "Flights" {
		t.Fatalf("after ticking: %v", p)
	}

	// update_objective replaces the list; the visa keeps its done state by id.
	id := sc["id"]
	_, sc, isErr = callS(t, cs, "update_objective", map[string]any{"id": id, "milestones": []any{
		map[string]any{"id": visa["id"], "title": "Visa"},
		map[string]any{"title": "Hotel", "criteria": "booked and paid"},
	}})
	if isErr {
		t.Fatalf("update_objective: %v", sc)
	}
	ms = sc["milestones"].([]any)
	if len(ms) != 2 || ms[0].(map[string]any)["done"] != true || ms[1].(map[string]any)["title"] != "Hotel" || ms[1].(map[string]any)["done"] != false {
		t.Fatalf("replaced: %v", ms)
	}
	if out, isErr := call(t, cs, "set_objective_milestone_done", map[string]any{"objective_id": id, "milestone_id": "nope", "done": true}); !isErr {
		t.Fatalf("an unknown milestone should be a tool error: %s", out)
	}
	if out, isErr := call(t, cs, "create_objective", map[string]any{"title": "x", "milestones": []any{map[string]any{"title": "a", "due": "someday"}}}); !isErr {
		t.Fatalf("a bad due should be a tool error: %s", out)
	}

	// Goal milestones are unchanged.
	gout, _ := call(t, cs, "create_goal", map[string]any{"title": "German", "period": "week", "target": 3, "milestones": []any{"A1", "A2"}})
	gid := firstID(gout)
	_, gsc, _ := callS(t, cs, "get_goal", map[string]any{"id": gid})
	mid := gsc["milestones"].([]any)[0].(map[string]any)["id"]
	if _, gsc, isErr = callS(t, cs, "set_milestone_done", map[string]any{"goal_id": gid, "milestone_id": mid, "done": true}); isErr ||
		gsc["progress"].(map[string]any)["milestonesDone"] != 1.0 {
		t.Fatalf("set_milestone_done: %v", gsc)
	}
}
