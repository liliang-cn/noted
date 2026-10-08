package service

import (
	"context"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Focus struct {
	pb.UnimplementedFocusServiceServer
	Store    *store.Store
	Location *time.Location // default zone when the request names none
}

func horizonIn(h pb.Horizon) string {
	switch h {
	case pb.Horizon_HORIZON_UPCOMING:
		return store.HorizonUpcoming
	case pb.Horizon_HORIZON_WEEK:
		return store.HorizonWeek
	case pb.Horizon_HORIZON_MONTH:
		return store.HorizonMonth
	}
	return store.HorizonToday
}

func horizonOut(h string) pb.Horizon {
	switch h {
	case store.HorizonUpcoming:
		return pb.Horizon_HORIZON_UPCOMING
	case store.HorizonWeek:
		return pb.Horizon_HORIZON_WEEK
	case store.HorizonMonth:
		return pb.Horizon_HORIZON_MONTH
	}
	return pb.Horizon_HORIZON_TODAY
}

func (s *Focus) GetFocus(ctx context.Context, req *pb.GetFocusRequest) (*pb.GetFocusResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	loc := s.Location
	if loc == nil {
		loc = time.UTC
	}
	if req.TimeZone != "" {
		if loc, err = time.LoadLocation(req.TimeZone); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "unknown time_zone %q", req.TimeZone)
		}
	}
	f, err := s.Store.Focus(ctx, u, horizonIn(req.Horizon), spaceIn(req.Space), loc, time.Now())
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.GetFocusResponse{
		Horizon: horizonOut(f.Horizon), Space: req.Space,
		From: timestamppb.New(f.From), To: timestamppb.New(f.To),
	}
	for _, it := range f.Items {
		out := &pb.FocusItem{
			SortTime: timestamppb.New(it.SortTime), Overdue: it.Overdue,
			ProjectId: it.ProjectID, ProjectTitle: it.ProjectTitle,
		}
		switch {
		case it.Event != nil:
			out.Item = &pb.FocusItem_Event{Event: &pb.Occurrence{
				Event: eventToPB(it.Event.Event), StartTime: timestamppb.New(it.Event.Start), EndTime: timestamppb.New(it.Event.End),
			}}
		case it.Task != nil:
			out.Item = &pb.FocusItem_Task{Task: taskToPB(*it.Task)}
		}
		resp.Items = append(resp.Items, out)
	}
	for _, g := range f.Goals {
		resp.Goals = append(resp.Goals, goalToPB(g))
	}
	for _, p := range f.Projects {
		resp.PinnedProjects = append(resp.PinnedProjects, projectToPB(p))
	}
	return resp, nil
}
