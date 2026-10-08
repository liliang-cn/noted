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

type Projects struct {
	pb.UnimplementedProjectServiceServer
	Store    *store.Store
	Location *time.Location // zone that "days left" is counted in
}

func (s *Projects) loc() *time.Location {
	if s.Location == nil {
		return time.UTC
	}
	return s.Location
}

func projectToPB(v store.ProjectView) *pb.Project {
	p, pr := v.Project, v.Progress
	out := &pb.Project{
		Id: p.ID, Title: p.Title, Notes: p.Notes, Pinned: p.Pinned, Archived: p.Archived,
		StartTime: tsPtr(p.Start), DueTime: tsPtr(p.Due), Space: spaceOut(p.Space),
		CreateTime: timestamppb.New(p.Created), UpdateTime: timestamppb.New(p.Updated),
		Progress: &pb.ProjectProgress{
			TasksTotal: int32(pr.TasksTotal), TasksDone: int32(pr.TasksDone), TasksOverdue: int32(pr.TasksOverdue),
			Percent: pr.Percent, EventsUpcoming: int32(pr.EventsUpcoming), NotesCount: int32(pr.NotesCount),
			NextTime: tsPtr(pr.Next), NextTitle: pr.NextTitle,
		},
	}
	if pr.DaysLeft != nil {
		d := int32(*pr.DaysLeft)
		out.Progress.DaysLeft = &d
	}
	return out
}

func (s *Projects) CreateProject(ctx context.Context, req *pb.CreateProjectRequest) (*pb.Project, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetProject()
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "project is required")
	}
	p, err := s.Store.CreateProject(ctx, store.Project{
		UserID: u, Title: in.Title, Notes: in.Notes, Pinned: in.Pinned, Archived: in.Archived,
		Start: timePtr(in.StartTime), Due: timePtr(in.DueTime), Space: spaceIn(in.Space),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	v, err := s.Store.ViewOf(ctx, p, time.Now(), s.loc())
	if err != nil {
		return nil, toStatus(err)
	}
	return projectToPB(v), nil
}

func (s *Projects) GetProject(ctx context.Context, req *pb.GetProjectRequest) (*pb.ProjectDetail, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	d, err := s.Store.GetProject(ctx, u, req.GetId(), time.Now(), s.loc())
	if err != nil {
		return nil, toStatus(err)
	}
	out := &pb.ProjectDetail{Project: projectToPB(d.View)}
	for _, t := range d.Tasks {
		out.Tasks = append(out.Tasks, taskToPB(t))
	}
	for _, e := range d.Events {
		out.Events = append(out.Events, eventToPB(e))
	}
	for _, n := range d.Notes {
		out.Notes = append(out.Notes, noteToPB(n))
	}
	return out, nil
}

func (s *Projects) UpdateProject(ctx context.Context, req *pb.UpdateProjectRequest) (*pb.Project, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetProject()
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "project.id is required")
	}
	paths, err := maskPaths(req.UpdateMask, "title", "notes", "pinned", "archived", "start_time", "due_time", "space")
	if err != nil {
		return nil, err
	}
	var p store.ProjectPatch
	if paths["title"] {
		p.Title = &in.Title
	}
	if paths["notes"] {
		p.Notes = &in.Notes
	}
	if paths["pinned"] {
		p.Pinned = &in.Pinned
	}
	if paths["archived"] {
		p.Archived = &in.Archived
	}
	if paths["start_time"] {
		t := timePtr(in.StartTime)
		p.Start = &t
	}
	if paths["due_time"] {
		t := timePtr(in.DueTime)
		p.Due = &t
	}
	if paths["space"] {
		sp := spaceIn(in.Space)
		if sp == "" {
			return nil, status.Error(codes.InvalidArgument, "space must be WORK or LIFE")
		}
		p.Space = &sp
	}
	v, err := s.Store.UpdateProject(ctx, u, in.Id, p, time.Now(), s.loc())
	if err != nil {
		return nil, toStatus(err)
	}
	return projectToPB(v), nil
}

func (s *Projects) DeleteProject(ctx context.Context, req *pb.DeleteProjectRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteProject(ctx, u, req.GetId(), req.DeleteItems); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Projects) ListProjects(ctx context.Context, req *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	vs, err := s.Store.ListProjects(ctx, u, store.ProjectFilter{
		IncludeArchived: req.IncludeArchived, PinnedOnly: req.PinnedOnly, Space: spaceIn(req.Space),
	}, time.Now(), s.loc())
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListProjectsResponse{}
	for _, v := range vs {
		resp.Projects = append(resp.Projects, projectToPB(v))
	}
	return resp, nil
}
