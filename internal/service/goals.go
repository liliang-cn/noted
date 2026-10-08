package service

import (
	"context"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Goals struct {
	pb.UnimplementedGoalServiceServer
	Store *store.Store
}

func periodIn(p pb.GoalPeriod) string {
	switch p {
	case pb.GoalPeriod_GOAL_PERIOD_DAY:
		return "day"
	case pb.GoalPeriod_GOAL_PERIOD_WEEK:
		return "week"
	case pb.GoalPeriod_GOAL_PERIOD_MONTH:
		return "month"
	}
	return ""
}

func periodOut(p string) pb.GoalPeriod {
	switch p {
	case "day":
		return pb.GoalPeriod_GOAL_PERIOD_DAY
	case "week":
		return pb.GoalPeriod_GOAL_PERIOD_WEEK
	case "month":
		return pb.GoalPeriod_GOAL_PERIOD_MONTH
	}
	return pb.GoalPeriod_GOAL_PERIOD_UNSPECIFIED
}

func dayTotals(in []store.DayTotal) []*pb.DayTotal {
	out := make([]*pb.DayTotal, len(in))
	for i, d := range in {
		out[i] = &pb.DayTotal{Date: d.Date, Amount: d.Amount}
	}
	return out
}

func goalToPB(v store.GoalView) *pb.Goal {
	g, p := v.Goal, v.Progress
	out := &pb.Goal{
		Id: g.ID, Title: g.Title, Notes: g.Notes, Period: periodOut(g.Period), Target: g.Target, Unit: g.Unit,
		TimeZone: g.TimeZone, EventId: g.EventID, Archived: g.Archived, Space: spaceOut(g.Space),
		CounterUnit: g.CounterUnit, CounterTarget: g.CounterTarget,
		CreateTime: timestamppb.New(g.Created), UpdateTime: timestamppb.New(g.Updated),
		Progress: &pb.GoalProgress{
			PeriodStart: timestamppb.New(p.PeriodStart), PeriodEnd: timestamppb.New(p.PeriodEnd),
			Done: p.Done, Target: p.Target, Remaining: p.Remaining, Percent: p.Percent,
			Achieved: p.Achieved, Behind: p.Behind, Streak: int32(p.Streak),
			Days: dayTotals(p.Days), Recent: dayTotals(p.Recent),
			MilestonesDone: int32(p.MilestonesDone), MilestonesTotal: int32(p.MilestonesTotal),
			MilestonePercent: p.MilestonePercent, CounterDone: p.CounterDone, CounterPercent: p.CounterPercent,
		},
	}
	for _, m := range g.Milestones {
		out.Milestones = append(out.Milestones, &pb.Milestone{Id: m.ID, Title: m.Title, Done: m.Done, DoneTime: tsPtr(m.DoneAt)})
	}
	return out
}

func milestonesIn(in []*pb.Milestone) []store.Milestone {
	out := make([]store.Milestone, len(in))
	for i, m := range in {
		out[i] = store.Milestone{ID: m.Id, Title: m.Title}
	}
	return out
}

func (s *Goals) view(ctx context.Context, userID, id string) (*pb.Goal, error) {
	g, err := s.Store.GetGoal(ctx, userID, id)
	if err != nil {
		return nil, toStatus(err)
	}
	v, err := s.Store.ViewGoal(ctx, g, time.Now())
	if err != nil {
		return nil, toStatus(err)
	}
	return goalToPB(v), nil
}

func (s *Goals) CreateGoal(ctx context.Context, req *pb.CreateGoalRequest) (*pb.Goal, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetGoal()
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "goal is required")
	}
	g, err := s.Store.CreateGoal(ctx, store.Goal{
		UserID: u, Title: in.Title, Notes: in.Notes, Period: periodIn(in.Period), Target: in.Target, Unit: in.Unit,
		TimeZone: in.TimeZone, EventID: in.EventId, Archived: in.Archived, Space: spaceIn(in.Space),
		CounterUnit: in.CounterUnit, CounterTarget: in.CounterTarget, Milestones: milestonesIn(in.Milestones),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return s.view(ctx, u, g.ID)
}

func (s *Goals) GetGoal(ctx context.Context, req *pb.GetGoalRequest) (*pb.Goal, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, u, req.GetId())
}

func (s *Goals) UpdateGoal(ctx context.Context, req *pb.UpdateGoalRequest) (*pb.Goal, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetGoal()
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "goal.id is required")
	}
	paths, err := maskPaths(req.UpdateMask, "title", "notes", "period", "target", "unit", "time_zone", "event_id", "archived", "space", "counter_unit", "counter_target", "milestones")
	if err != nil {
		return nil, err
	}
	var p store.GoalPatch
	if paths["title"] {
		p.Title = &in.Title
	}
	if paths["notes"] {
		p.Notes = &in.Notes
	}
	if paths["period"] {
		per := periodIn(in.Period)
		p.Period = &per
	}
	if paths["target"] {
		p.Target = &in.Target
	}
	if paths["unit"] {
		p.Unit = &in.Unit
	}
	if paths["time_zone"] {
		p.TimeZone = &in.TimeZone
	}
	if paths["event_id"] {
		p.EventID = &in.EventId
	}
	if paths["archived"] {
		p.Archived = &in.Archived
	}
	if paths["space"] {
		sp := spaceIn(in.Space)
		if sp == "" {
			return nil, status.Error(codes.InvalidArgument, "space must be WORK or LIFE")
		}
		p.Space = &sp
	}
	if paths["counter_unit"] {
		p.CounterUnit = &in.CounterUnit
	}
	if paths["counter_target"] {
		p.CounterTarget = &in.CounterTarget
	}
	if paths["milestones"] {
		ms := milestonesIn(in.Milestones)
		p.Milestones = &ms
	}
	if _, err := s.Store.UpdateGoal(ctx, u, in.Id, p); err != nil {
		return nil, toStatus(err)
	}
	return s.view(ctx, u, in.Id)
}

func (s *Goals) DeleteGoal(ctx context.Context, req *pb.DeleteGoalRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteGoal(ctx, u, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Goals) ListGoals(ctx context.Context, req *pb.ListGoalsRequest) (*pb.ListGoalsResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	vs, err := s.Store.ListGoals(ctx, u, req.IncludeArchived, time.Now(), spaceIn(req.Space))
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListGoalsResponse{}
	for _, v := range vs {
		resp.Goals = append(resp.Goals, goalToPB(v))
	}
	return resp, nil
}

func checkInToPB(c store.CheckIn) *pb.CheckIn {
	return &pb.CheckIn{Id: c.ID, GoalId: c.GoalID, Time: timestamppb.New(c.Time), Amount: c.Amount, Count: c.Tally, Note: c.Note}
}

func (s *Goals) RecordCheckIn(ctx context.Context, req *pb.RecordCheckInRequest) (*pb.RecordCheckInResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	var at time.Time
	if req.Time != nil {
		at = req.Time.AsTime()
	}
	c, err := s.Store.RecordCheckIn(ctx, u, req.GoalId, req.Amount, req.Count, at, req.Note, time.Now())
	if err != nil {
		return nil, toStatus(err)
	}
	g, err := s.view(ctx, u, req.GoalId)
	if err != nil {
		return nil, err
	}
	return &pb.RecordCheckInResponse{CheckIn: checkInToPB(c), Goal: g}, nil
}

func (s *Goals) DeleteCheckIn(ctx context.Context, req *pb.DeleteCheckInRequest) (*pb.Goal, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	goalID, err := s.Store.DeleteCheckIn(ctx, u, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return s.view(ctx, u, goalID)
}

func (s *Goals) ListCheckIns(ctx context.Context, req *pb.ListCheckInsRequest) (*pb.ListCheckInsResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	cs, err := s.Store.ListCheckIns(ctx, u, req.GoalId, timePtr(req.From), timePtr(req.To), limit)
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListCheckInsResponse{}
	for _, c := range cs {
		resp.CheckIns = append(resp.CheckIns, checkInToPB(c))
	}
	return resp, nil
}

func (s *Goals) SetMilestoneDone(ctx context.Context, req *pb.SetMilestoneDoneRequest) (*pb.Goal, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.Store.SetMilestoneDone(ctx, u, req.GoalId, req.MilestoneId, req.Done); err != nil {
		return nil, toStatus(err)
	}
	return s.view(ctx, u, req.GoalId)
}
