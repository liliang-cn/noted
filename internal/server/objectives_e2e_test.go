package server_test

import (
	"math"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestObjectiveCombinesDimensionsAndAMeasuredResult(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	obj, goals := pb.NewObjectiveServiceClient(conn), pb.NewGoalServiceClient(conn)

	now := time.Now()
	o, err := obj.CreateObjective(ctx, &pb.CreateObjectiveRequest{Objective: &pb.Objective{
		Title: "减肥", MetricName: "体重", MetricUnit: "kg", MetricStart: 75, MetricTarget: 68,
		StartTime: timestamppb.New(now.AddDate(0, 0, -30)), DueTime: timestamppb.New(now.AddDate(0, 0, 30)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if o.Progress.GoalsTotal != 0 || o.Progress.HasReading || o.Progress.Percent != 0 || !o.Progress.OnTrack {
		t.Fatalf("a new objective: %+v", o.Progress)
	}

	// Dimensions: swimming 2 a week, running 3 a week, light meals 10 a week, sugar-free days 7 a week.
	dims := map[string]float64{"游泳": 2, "跑步": 3, "轻食": 10, "控糖": 7}
	ids := map[string]string{}
	for title, target := range dims {
		g, err := goals.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: title, Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: target, Unit: "次", ObjectiveId: o.Id}})
		if err != nil {
			t.Fatal(title, err)
		}
		if g.ObjectiveId != o.Id {
			t.Fatalf("%s is not in the objective", title)
		}
		ids[title] = g.Id
	}
	// A goal outside the objective must not count towards it.
	if _, err := goals.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "德语", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 5}}); err != nil {
		t.Fatal(err)
	}
	// Swimming done, running half done, nothing else.
	check := func(title string, amount float64) {
		if _, err := goals.RecordCheckIn(ctx, &pb.RecordCheckInRequest{GoalId: ids[title], Amount: amount}); err != nil {
			t.Fatal(err)
		}
	}
	check("游泳", 2)
	check("跑步", 1.5)

	g, err := obj.GetObjective(ctx, &pb.GetObjectiveRequest{Id: o.Id})
	if err != nil {
		t.Fatal(err)
	}
	p := g.Progress
	if p.GoalsTotal != 4 || len(g.Goals) != 4 || p.GoalsAchieved != 1 {
		t.Fatalf("dimensions: %+v", p)
	}
	if want := (1 + 0.5) / 4; !near(p.GoalsPercent, want) || !near(p.Percent, want) {
		t.Fatalf("with no reading the headline is the dimensions' mean: goals %.3f percent %.3f, want %.3f", p.GoalsPercent, p.Percent, want)
	}

	// Readings: 75 -> 72.5 is 2.5 of 7 kg.
	for _, v := range []float64{75, 74, 72.5} {
		if _, err := obj.RecordMeasurement(ctx, &pb.RecordMeasurementRequest{ObjectiveId: o.Id, Value: v}); err != nil {
			t.Fatal(err)
		}
	}
	g, _ = obj.GetObjective(ctx, &pb.GetObjectiveRequest{Id: o.Id})
	p = g.Progress
	if !p.HasReading || !near(p.MetricCurrent, 72.5) || !near(p.MetricChange, -2.5) || !near(p.MetricPercent, 2.5/7) || !near(p.Percent, 2.5/7) {
		t.Fatalf("metric: %+v", p)
	}
	// Halfway through the time but only 36% of the way: behind the pace.
	if p.OnTrack {
		t.Fatalf("2.5 of 7 kg after half the time should be off track: %+v", p)
	}
	if p.DaysLeft == nil || *p.DaysLeft < 29 || *p.DaysLeft > 31 {
		t.Fatalf("days left: %v", p.DaysLeft)
	}

	// Reaching the target caps at 100%, and overshooting does not exceed it.
	if _, err := obj.RecordMeasurement(ctx, &pb.RecordMeasurementRequest{ObjectiveId: o.Id, Value: 66, Time: timestamppb.New(now.Add(time.Minute))}); err != nil {
		t.Fatal(err)
	}
	g, _ = obj.GetObjective(ctx, &pb.GetObjectiveRequest{Id: o.Id})
	if !near(g.Progress.MetricPercent, 1) || !g.Progress.OnTrack {
		t.Fatalf("past the target: %+v", g.Progress)
	}

	ms, err := obj.ListMeasurements(ctx, &pb.ListMeasurementsRequest{ObjectiveId: o.Id})
	if err != nil || len(ms.Measurements) != 4 {
		t.Fatalf("%v %v", ms, err)
	}
	for i := 1; i < len(ms.Measurements); i++ {
		if ms.Measurements[i].Time.AsTime().Before(ms.Measurements[i-1].Time.AsTime()) {
			t.Fatal("readings are not oldest first")
		}
	}
	two, _ := obj.ListMeasurements(ctx, &pb.ListMeasurementsRequest{ObjectiveId: o.Id, Limit: 2})
	if len(two.Measurements) != 2 || !near(two.Measurements[1].Value, 66) {
		t.Fatalf("limit keeps the newest: %+v", two.Measurements)
	}
	if _, err := obj.DeleteMeasurement(ctx, &pb.DeleteMeasurementRequest{Id: ms.Measurements[3].Id}); err != nil {
		t.Fatal(err)
	}
	g, _ = obj.GetObjective(ctx, &pb.GetObjectiveRequest{Id: o.Id})
	if !near(g.Progress.MetricCurrent, 72.5) {
		t.Fatalf("after deleting the newest reading: %+v", g.Progress)
	}

	// Lists carry the dimensions; another user sees none of it.
	l, _ := obj.ListObjectives(ctx, &pb.ListObjectivesRequest{})
	if len(l.Objectives) != 1 || len(l.Objectives[0].Goals) != 4 {
		t.Fatalf("list: %+v", l)
	}
	bl, _ := obj.ListObjectives(as(e.bob), &pb.ListObjectivesRequest{})
	if len(bl.Objectives) != 0 {
		t.Fatal("bob sees alice's objective")
	}
	if _, err := obj.GetObjective(as(e.bob), &pb.GetObjectiveRequest{Id: o.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob get: %v", err)
	}
	if _, err := obj.RecordMeasurement(as(e.bob), &pb.RecordMeasurementRequest{ObjectiveId: o.Id, Value: 1}); code(err) != codes.NotFound {
		t.Fatalf("bob record: %v", err)
	}
	if _, err := goals.CreateGoal(as(e.bob), &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "x", Period: pb.GoalPeriod_GOAL_PERIOD_DAY, Target: 1, ObjectiveId: o.Id}}); code(err) != codes.InvalidArgument {
		t.Fatalf("bob adds a goal to alice's objective: %v", err)
	}

	// Moving a goal out of the objective, then deleting the objective keeps the goals.
	if _, err := goals.UpdateGoal(ctx, &pb.UpdateGoalRequest{Goal: &pb.Goal{Id: ids["控糖"]}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"objective_id"}}}); err != nil {
		t.Fatal(err)
	}
	g, _ = obj.GetObjective(ctx, &pb.GetObjectiveRequest{Id: o.Id})
	if g.Progress.GoalsTotal != 3 {
		t.Fatalf("after moving one out: %d", g.Progress.GoalsTotal)
	}
	if _, err := obj.DeleteObjective(ctx, &pb.DeleteObjectiveRequest{Id: o.Id}); err != nil {
		t.Fatal(err)
	}
	gl, _ := goals.ListGoals(ctx, &pb.ListGoalsRequest{})
	if len(gl.Goals) != 5 {
		t.Fatalf("goals after deleting the objective: %d", len(gl.Goals))
	}
	for _, x := range gl.Goals {
		if x.ObjectiveId != "" {
			t.Fatalf("%s still points at a deleted objective", x.Title)
		}
	}
}

func TestObjectiveWithoutAMetricAndValidation(t *testing.T) {
	e := start(t, nil, false)
	ctx := as(e.alice)
	obj := pb.NewObjectiveServiceClient(e.conn(t))

	for name, o := range map[string]*pb.Objective{
		"no title":       {MetricUnit: "kg", MetricStart: 1, MetricTarget: 2},
		"same endpoints": {Title: "x", MetricUnit: "kg", MetricStart: 70, MetricTarget: 70},
		"due before":     {Title: "x", StartTime: timestamppb.New(time.Now()), DueTime: timestamppb.New(time.Now().Add(-time.Hour))},
	} {
		if _, err := obj.CreateObjective(ctx, &pb.CreateObjectiveRequest{Objective: o}); code(err) != codes.InvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}

	o, err := obj.CreateObjective(ctx, &pb.CreateObjectiveRequest{Objective: &pb.Objective{Title: "多读书"}})
	if err != nil {
		t.Fatal(err)
	}
	if o.Progress.HasMetric {
		t.Fatal("no unit means no metric")
	}
	if _, err := obj.RecordMeasurement(ctx, &pb.RecordMeasurementRequest{ObjectiveId: o.Id, Value: 1}); code(err) != codes.InvalidArgument {
		t.Fatalf("a reading for an objective with no metric: %v", err)
	}

	// A metric can be added later; a gaining objective (e.g. muscle mass) works upwards.
	o, err = obj.UpdateObjective(ctx, &pb.UpdateObjectiveRequest{
		Objective:  &pb.Objective{Id: o.Id, MetricName: "肌肉", MetricUnit: "kg", MetricStart: 30, MetricTarget: 35},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"metric_name", "metric_unit", "metric_start", "metric_target"}},
	})
	if err != nil || !o.Progress.HasMetric {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = obj.RecordMeasurement(ctx, &pb.RecordMeasurementRequest{ObjectiveId: o.Id, Value: 32})
	if err != nil || !near(o.Progress.MetricPercent, 0.4) {
		t.Fatalf("upward metric: %+v %v", o.Progress, err)
	}
	if _, err := obj.UpdateObjective(ctx, &pb.UpdateObjectiveRequest{Objective: &pb.Objective{Id: o.Id}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"id"}}}); code(err) != codes.InvalidArgument {
		t.Fatalf("masking a field that cannot change: %v", err)
	}
	// Archived objectives leave the default list.
	if _, err := obj.UpdateObjective(ctx, &pb.UpdateObjectiveRequest{Objective: &pb.Objective{Id: o.Id, Archived: true}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"archived"}}}); err != nil {
		t.Fatal(err)
	}
	l, _ := obj.ListObjectives(ctx, &pb.ListObjectivesRequest{})
	all, _ := obj.ListObjectives(ctx, &pb.ListObjectivesRequest{IncludeArchived: true})
	if len(l.Objectives) != 0 || len(all.Objectives) != 1 {
		t.Fatalf("archived: %d %d", len(l.Objectives), len(all.Objectives))
	}
}
