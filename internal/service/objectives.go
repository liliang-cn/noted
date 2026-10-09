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

type Objectives struct {
	pb.UnimplementedObjectiveServiceServer
	Store *store.Store
}

func objectiveToPB(v store.ObjectiveView) *pb.Objective {
	o, p := v.Objective, v.Progress
	out := &pb.Objective{
		Id: o.ID, Title: o.Title, Notes: o.Notes, Space: spaceOut(o.Space), StartTime: tsPtr(o.Start), DueTime: tsPtr(o.Due),
		MetricName: o.MetricName, MetricUnit: o.MetricUnit, MetricStart: o.MetricStart, MetricTarget: o.MetricTarget,
		Archived: o.Archived, CreateTime: timestamppb.New(o.Created), UpdateTime: timestamppb.New(o.Updated),
		Progress: &pb.ObjectiveProgress{
			GoalsTotal: int32(p.GoalsTotal), GoalsAchieved: int32(p.GoalsAchieved), GoalsPercent: p.GoalsPercent, Behind: p.Behind,
			HasMetric: p.HasMetric, HasReading: p.HasReading, MetricCurrent: p.MetricCurrent, MetricChange: p.MetricChange,
			MetricPercent: p.MetricPercent, LastReadingTime: tsPtr(p.LastReading), Percent: p.Percent, OnTrack: p.OnTrack,
		},
	}
	if p.DaysLeft != nil {
		d := int32(*p.DaysLeft)
		out.Progress.DaysLeft = &d
	}
	for _, g := range v.Goals {
		out.Goals = append(out.Goals, goalToPB(g))
	}
	return out
}

func (s *Objectives) view(ctx context.Context, userID, id string) (*pb.Objective, error) {
	v, err := s.Store.ViewObjective(ctx, userID, id, time.Now())
	if err != nil {
		return nil, toStatus(err)
	}
	return objectiveToPB(v), nil
}

func (s *Objectives) CreateObjective(ctx context.Context, req *pb.CreateObjectiveRequest) (*pb.Objective, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetObjective()
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "objective is required")
	}
	o, err := s.Store.CreateObjective(ctx, store.Objective{
		UserID: u, Title: in.Title, Notes: in.Notes, Space: spaceIn(in.Space), Start: timePtr(in.StartTime), Due: timePtr(in.DueTime),
		MetricName: in.MetricName, MetricUnit: in.MetricUnit, MetricStart: in.MetricStart, MetricTarget: in.MetricTarget, Archived: in.Archived,
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return s.view(ctx, u, o.ID)
}

func (s *Objectives) GetObjective(ctx context.Context, req *pb.GetObjectiveRequest) (*pb.Objective, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, u, req.GetId())
}

func (s *Objectives) UpdateObjective(ctx context.Context, req *pb.UpdateObjectiveRequest) (*pb.Objective, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetObjective()
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "objective.id is required")
	}
	paths, err := maskPaths(req.UpdateMask, "title", "notes", "space", "start_time", "due_time", "metric_name", "metric_unit", "metric_start", "metric_target", "archived")
	if err != nil {
		return nil, err
	}
	var p store.ObjectivePatch
	if paths["title"] {
		p.Title = &in.Title
	}
	if paths["notes"] {
		p.Notes = &in.Notes
	}
	if paths["space"] {
		sp := spaceIn(in.Space)
		if sp == "" {
			return nil, status.Error(codes.InvalidArgument, "space must be WORK or LIFE")
		}
		p.Space = &sp
	}
	if paths["start_time"] {
		t := timePtr(in.StartTime)
		p.Start = &t
	}
	if paths["due_time"] {
		t := timePtr(in.DueTime)
		p.Due = &t
	}
	if paths["metric_name"] {
		p.MetricName = &in.MetricName
	}
	if paths["metric_unit"] {
		p.MetricUnit = &in.MetricUnit
	}
	if paths["metric_start"] {
		p.MetricStart = &in.MetricStart
	}
	if paths["metric_target"] {
		p.MetricTarget = &in.MetricTarget
	}
	if paths["archived"] {
		p.Archived = &in.Archived
	}
	if _, err := s.Store.UpdateObjective(ctx, u, in.Id, p); err != nil {
		return nil, toStatus(err)
	}
	return s.view(ctx, u, in.Id)
}

func (s *Objectives) DeleteObjective(ctx context.Context, req *pb.DeleteObjectiveRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteObjective(ctx, u, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Objectives) ListObjectives(ctx context.Context, req *pb.ListObjectivesRequest) (*pb.ListObjectivesResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	vs, err := s.Store.ListObjectives(ctx, u, req.IncludeArchived, time.Now(), spaceIn(req.Space))
	if err != nil {
		return nil, toStatus(err)
	}
	out := &pb.ListObjectivesResponse{}
	for _, v := range vs {
		out.Objectives = append(out.Objectives, objectiveToPB(v))
	}
	return out, nil
}

func (s *Objectives) RecordMeasurement(ctx context.Context, req *pb.RecordMeasurementRequest) (*pb.Objective, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	var at time.Time
	if t := timePtr(req.Time); t != nil {
		at = *t
	}
	if _, err := s.Store.RecordMeasurement(ctx, u, req.GetObjectiveId(), at, req.GetValue(), req.GetNote()); err != nil {
		return nil, toStatus(err)
	}
	return s.view(ctx, u, req.GetObjectiveId())
}

func (s *Objectives) DeleteMeasurement(ctx context.Context, req *pb.DeleteMeasurementRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteMeasurement(ctx, u, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Objectives) ListMeasurements(ctx context.Context, req *pb.ListMeasurementsRequest) (*pb.ListMeasurementsResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 365
	}
	limit = min(limit, 5000)
	ms, err := s.Store.ListMeasurements(ctx, u, req.GetObjectiveId(), timePtr(req.From), timePtr(req.To), limit)
	if err != nil {
		return nil, toStatus(err)
	}
	out := &pb.ListMeasurementsResponse{}
	for _, m := range ms {
		out.Measurements = append(out.Measurements, &pb.Measurement{Id: m.ID, ObjectiveId: m.ObjectiveID, Time: timestamppb.New(m.Time), Value: m.Value, Note: m.Note})
	}
	return out, nil
}
