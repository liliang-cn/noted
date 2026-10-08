package service

import (
	"context"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/reminder"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Calendar struct {
	pb.UnimplementedCalendarServiceServer
	Store *store.Store
	Hub   *reminder.Hub
}

// ---- events ----

func (s *Calendar) CreateEvent(ctx context.Context, req *pb.CreateEventRequest) (*pb.Event, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetEvent() == nil {
		return nil, status.Error(codes.InvalidArgument, "event is required")
	}
	e := eventFromPB(req.Event)
	e.UserID = u
	out, err := s.Store.CreateEvent(ctx, e)
	if err != nil {
		return nil, toStatus(err)
	}
	return eventToPB(out), nil
}

func (s *Calendar) GetEvent(ctx context.Context, req *pb.GetEventRequest) (*pb.Event, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	e, err := s.Store.GetEvent(ctx, u, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return eventToPB(e), nil
}

func (s *Calendar) UpdateEvent(ctx context.Context, req *pb.UpdateEventRequest) (*pb.Event, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetEvent()
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "event.id is required")
	}
	paths, err := maskPaths(req.UpdateMask, "title", "description", "location", "start_time", "end_time",
		"all_day", "time_zone", "rrule", "remind_before_minutes")
	if err != nil {
		return nil, err
	}
	e := eventFromPB(in)
	var p store.EventPatch
	if paths["title"] {
		p.Title = &e.Title
	}
	if paths["description"] {
		p.Description = &e.Description
	}
	if paths["location"] {
		p.Location = &e.Location
	}
	if paths["start_time"] {
		if in.StartTime == nil {
			return nil, status.Error(codes.InvalidArgument, "start_time cannot be cleared")
		}
		p.Start = &e.Start
	}
	if paths["end_time"] {
		if in.EndTime == nil {
			return nil, status.Error(codes.InvalidArgument, "end_time cannot be cleared")
		}
		p.End = &e.End
	}
	if paths["all_day"] {
		p.AllDay = &e.AllDay
	}
	if paths["time_zone"] {
		p.TimeZone = &e.TimeZone
	}
	if paths["rrule"] {
		p.RRule = &e.RRule
	}
	if paths["remind_before_minutes"] {
		p.RemindBefore = &e.RemindBefore // nil clears the reminder
	}
	out, err := s.Store.UpdateEvent(ctx, u, in.Id, p)
	if err != nil {
		return nil, toStatus(err)
	}
	return eventToPB(out), nil
}

func (s *Calendar) DeleteEvent(ctx context.Context, req *pb.DeleteEventRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteEvent(ctx, u, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Calendar) ListEvents(ctx context.Context, req *pb.ListEventsRequest) (*pb.ListEventsResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if req.From == nil || req.To == nil {
		return nil, status.Error(codes.InvalidArgument, "from and to are required")
	}
	occ, err := s.Store.ListEvents(ctx, u, req.From.AsTime(), req.To.AsTime())
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListEventsResponse{}
	for _, o := range occ {
		resp.Occurrences = append(resp.Occurrences, &pb.Occurrence{
			Event: eventToPB(o.Event), StartTime: timestamppb.New(o.Start), EndTime: timestamppb.New(o.End),
		})
	}
	return resp, nil
}

// ---- tasks ----

func (s *Calendar) CreateTask(ctx context.Context, req *pb.CreateTaskRequest) (*pb.Task, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetTask()
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "task is required")
	}
	t, err := s.Store.CreateTask(ctx, store.Task{
		UserID: u, Title: in.Title, Notes: in.Notes, Due: timePtr(in.DueTime), Priority: int(in.Priority),
		Done: in.Completed, Remind: timePtr(in.RemindTime), Tags: in.Tags,
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return taskToPB(t), nil
}

func (s *Calendar) GetTask(ctx context.Context, req *pb.GetTaskRequest) (*pb.Task, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.Store.GetTask(ctx, u, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return taskToPB(t), nil
}

func (s *Calendar) UpdateTask(ctx context.Context, req *pb.UpdateTaskRequest) (*pb.Task, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetTask()
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task.id is required")
	}
	paths, err := maskPaths(req.UpdateMask, "title", "notes", "due_time", "priority", "completed", "remind_time", "tags")
	if err != nil {
		return nil, err
	}
	var p store.TaskPatch
	if paths["title"] {
		p.Title = &in.Title
	}
	if paths["notes"] {
		p.Notes = &in.Notes
	}
	if paths["due_time"] {
		due := timePtr(in.DueTime)
		p.Due = &due
	}
	if paths["priority"] {
		pr := int(in.Priority)
		p.Priority = &pr
	}
	if paths["completed"] {
		p.Done = &in.Completed
	}
	if paths["remind_time"] {
		r := timePtr(in.RemindTime)
		p.Remind = &r
	}
	if paths["tags"] {
		p.Tags = &in.Tags
	}
	t, err := s.Store.UpdateTask(ctx, u, in.Id, p)
	if err != nil {
		return nil, toStatus(err)
	}
	return taskToPB(t), nil
}

func (s *Calendar) DeleteTask(ctx context.Context, req *pb.DeleteTaskRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteTask(ctx, u, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Calendar) ListTasks(ctx context.Context, req *pb.ListTasksRequest) (*pb.ListTasksResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset, err := page(req.PageSize, req.PageToken)
	if err != nil {
		return nil, err
	}
	state := "open"
	switch req.Filter {
	case pb.ListTasksRequest_FILTER_COMPLETED:
		state = "done"
	case pb.ListTasksRequest_FILTER_ALL:
		state = "all"
	}
	tasks, err := s.Store.ListTasks(ctx, u, store.TaskFilter{
		State: state, Tag: req.Tag, DueBefore: timePtr(req.DueBefore), Limit: limit + 1, Offset: offset,
	})
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListTasksResponse{NextPageToken: nextToken(offset, len(tasks), limit)}
	for i, t := range tasks {
		if i == limit {
			break
		}
		resp.Tasks = append(resp.Tasks, taskToPB(t))
	}
	return resp, nil
}

// ---- reminders ----

func (s *Calendar) WatchReminders(_ *pb.WatchRemindersRequest, stream pb.CalendarService_WatchRemindersServer) error {
	u, err := uid(stream.Context())
	if err != nil {
		return err
	}
	ch, cancel := s.Hub.Subscribe(u)
	defer cancel()
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case r := <-ch:
			if err := stream.Send(reminderToPB(r)); err != nil {
				return err
			}
		}
	}
}

func (s *Calendar) ListReminders(ctx context.Context, req *pb.ListRemindersRequest) (*pb.ListRemindersResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-24 * time.Hour)
	if req.Since != nil {
		since = req.Since.AsTime()
	}
	limit := int(req.Limit)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rs, err := s.Store.ListReminders(ctx, u, since, limit)
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListRemindersResponse{}
	for _, r := range rs {
		resp.Reminders = append(resp.Reminders, reminderToPB(r))
	}
	return resp, nil
}
