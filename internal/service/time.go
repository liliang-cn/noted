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

// Time is time tracking: entries of time spent, a running timer, and totals.
type Time struct {
	pb.UnimplementedTimeServiceServer
	Store    *store.Store
	Location *time.Location // zone for days in summaries when the request names none
}

func (s *Time) loc() *time.Location {
	if s.Location == nil {
		return time.UTC
	}
	return s.Location
}

// timeEntryToPB converts an entry; a running timer shows the minutes it has run so far.
func timeEntryToPB(e store.TimeEntry, now time.Time) *pb.TimeEntry {
	minutes := e.Minutes
	if e.Running() {
		minutes = max(int(now.Sub(e.Start)/time.Minute), 0)
	}
	return &pb.TimeEntry{
		Id: e.ID, Title: e.Title, Minutes: int32(minutes), StartTime: timestamppb.New(e.Start), EndTime: tsPtr(e.End),
		Note: e.Note, ProjectId: e.ProjectID, GoalId: e.GoalID, Space: spaceOut(e.Space), Running: e.Running(),
		CheckInId: e.CheckInID, CreateTime: timestamppb.New(e.Created), UpdateTime: timestamppb.New(e.Updated), Timed: e.Timed,
	}
}

func timeRowToPB(r store.TimeRow) *pb.TimeSummaryRow {
	return &pb.TimeSummaryRow{Key: r.Key, Label: r.Label, Minutes: int32(r.Minutes), Count: int32(r.Count)}
}

func groupByIn(g pb.TimeGroupBy) string {
	switch g {
	case pb.TimeGroupBy_TIME_GROUP_BY_PROJECT:
		return store.TimeByProject
	case pb.TimeGroupBy_TIME_GROUP_BY_GOAL:
		return store.TimeByGoal
	case pb.TimeGroupBy_TIME_GROUP_BY_DAY:
		return store.TimeByDay
	}
	return store.TimeByTitle
}

func (s *Time) CreateTimeEntry(ctx context.Context, req *pb.CreateTimeEntryRequest) (*pb.TimeEntry, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetEntry()
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "entry is required")
	}
	e := store.TimeEntry{UserID: u, Title: in.Title, Minutes: int(in.Minutes), Note: in.Note,
		ProjectID: in.ProjectId, GoalID: in.GoalId, Space: spaceIn(in.Space), End: timePtr(in.EndTime)}
	if in.StartTime != nil {
		e.Start = in.StartTime.AsTime()
	}
	now := time.Now()
	out, err := s.Store.CreateTimeEntry(ctx, e, now)
	if err != nil {
		return nil, toStatus(err)
	}
	return timeEntryToPB(out, now), nil
}

func (s *Time) GetTimeEntry(ctx context.Context, req *pb.GetTimeEntryRequest) (*pb.TimeEntry, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	e, err := s.Store.GetTimeEntry(ctx, u, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return timeEntryToPB(e, time.Now()), nil
}

func (s *Time) UpdateTimeEntry(ctx context.Context, req *pb.UpdateTimeEntryRequest) (*pb.TimeEntry, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetEntry()
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "entry.id is required")
	}
	paths, err := maskPaths(req.UpdateMask, "title", "minutes", "start_time", "end_time", "note", "project_id", "goal_id", "space")
	if err != nil {
		return nil, err
	}
	var p store.TimeEntryPatch
	if paths["title"] {
		p.Title = &in.Title
	}
	if paths["note"] {
		p.Note = &in.Note
	}
	if paths["minutes"] {
		m := int(in.Minutes)
		p.Minutes = &m
	}
	if paths["start_time"] {
		if in.StartTime == nil {
			return nil, status.Error(codes.InvalidArgument, "start_time cannot be cleared")
		}
		t := in.StartTime.AsTime()
		p.Start = &t
	}
	if paths["end_time"] {
		if in.EndTime == nil {
			return nil, status.Error(codes.InvalidArgument, "end_time cannot be cleared; start a timer instead")
		}
		t := in.EndTime.AsTime()
		p.End = &t
	}
	if paths["project_id"] {
		p.ProjectID = &in.ProjectId
	}
	if paths["goal_id"] {
		p.GoalID = &in.GoalId
	}
	if paths["space"] {
		sp := spaceIn(in.Space)
		if sp == "" {
			return nil, status.Error(codes.InvalidArgument, "space must be WORK or LIFE")
		}
		p.Space = &sp
	}
	now := time.Now()
	e, err := s.Store.UpdateTimeEntry(ctx, u, in.Id, p, now)
	if err != nil {
		return nil, toStatus(err)
	}
	return timeEntryToPB(e, now), nil
}

func (s *Time) DeleteTimeEntry(ctx context.Context, req *pb.DeleteTimeEntryRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteTimeEntry(ctx, u, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Time) ListTimeEntries(ctx context.Context, req *pb.ListTimeEntriesRequest) (*pb.ListTimeEntriesResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset, err := page(req.PageSize, req.PageToken)
	if err != nil {
		return nil, err
	}
	es, err := s.Store.ListTimeEntries(ctx, u, store.TimeFilter{
		From: timePtr(req.From), To: timePtr(req.To), ProjectID: req.ProjectId, GoalID: req.GoalId, Title: req.Title,
		Space: spaceIn(req.Space), Limit: limit + 1, Offset: offset,
	})
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListTimeEntriesResponse{NextPageToken: nextToken(offset, len(es), limit)}
	now := time.Now()
	for i, e := range es {
		if i == limit {
			break
		}
		resp.Entries = append(resp.Entries, timeEntryToPB(e, now))
	}
	return resp, nil
}

func (s *Time) StartTimer(ctx context.Context, req *pb.StartTimerRequest) (*pb.TimeEntry, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	e := store.TimeEntry{UserID: u, Title: req.Title, Note: req.Note, ProjectID: req.ProjectId, GoalID: req.GoalId, Space: spaceIn(req.Space)}
	if req.StartTime != nil {
		e.Start = req.StartTime.AsTime()
	}
	now := time.Now()
	out, err := s.Store.StartTimer(ctx, e, now)
	if err != nil {
		return nil, toStatus(err)
	}
	return timeEntryToPB(out, now), nil
}

func (s *Time) StopTimer(ctx context.Context, req *pb.StopTimerRequest) (*pb.TimeEntry, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	e, err := s.Store.StopTimer(ctx, u, timePtr(req.EndTime), now)
	if err != nil {
		return nil, toStatus(err)
	}
	return timeEntryToPB(e, now), nil
}

func (s *Time) GetRunningTimer(ctx context.Context, _ *pb.GetRunningTimerRequest) (*pb.GetRunningTimerResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	e, ok, err := s.Store.RunningTimer(ctx, u)
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.GetRunningTimerResponse{}
	if ok {
		resp.Entry = timeEntryToPB(e, time.Now())
	}
	return resp, nil
}

func (s *Time) SummarizeTime(ctx context.Context, req *pb.SummarizeTimeRequest) (*pb.TimeSummary, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	loc := s.loc()
	if req.TimeZone != "" {
		if loc, err = time.LoadLocation(req.TimeZone); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "unknown time_zone %q", req.TimeZone)
		}
	}
	to := time.Now()
	if req.To != nil {
		to = req.To.AsTime()
	}
	from := to.AddDate(0, 0, -7)
	if req.From != nil {
		from = req.From.AsTime()
	}
	if !from.Before(to) {
		return nil, status.Error(codes.InvalidArgument, "from must be before to")
	}
	by := groupByIn(req.GroupBy)
	sum, err := s.Store.SummarizeTime(ctx, u, store.TimeFilter{From: &from, To: &to, Space: spaceIn(req.Space),
		ProjectID: req.ProjectId, GoalID: req.GoalId}, by, loc)
	if err != nil {
		return nil, toStatus(err)
	}
	g := req.GroupBy
	if g == pb.TimeGroupBy_TIME_GROUP_BY_UNSPECIFIED {
		g = pb.TimeGroupBy_TIME_GROUP_BY_TITLE
	}
	out := &pb.TimeSummary{From: timestamppb.New(from), To: timestamppb.New(to), GroupBy: g,
		TotalMinutes: int32(sum.Minutes), TotalCount: int32(sum.Count)}
	for _, r := range sum.Rows {
		out.Rows = append(out.Rows, timeRowToPB(r))
	}
	return out, nil
}
