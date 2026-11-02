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

type Compass struct {
	pb.UnimplementedCompassServiceServer
	Store *store.Store
}

func compassToPB(c store.Compass) *pb.Compass {
	out := &pb.Compass{Space: spaceOut(c.Space), AntiVision: c.AntiVision, Vision: c.Vision, Identity: c.Identity,
		Constraints: c.Constraints, Version: int32(c.Version)}
	if c.Version > 0 {
		out.UpdateTime = timestamppb.New(c.Updated)
	}
	return out
}

func (s *Compass) GetCompass(ctx context.Context, req *pb.GetCompassRequest) (*pb.Compass, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.Store.GetCompass(ctx, u, spaceIn(req.Space))
	if err != nil {
		return nil, toStatus(err)
	}
	return compassToPB(c), nil
}

func (s *Compass) SetCompass(ctx context.Context, req *pb.SetCompassRequest) (*pb.Compass, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetCompass()
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "compass is required")
	}
	paths := req.GetUpdateMask().GetPaths()
	if len(paths) == 0 {
		paths = []string{"anti_vision", "vision", "identity", "constraints"}
	}
	var p store.CompassPatch
	for _, f := range paths {
		switch f {
		case "anti_vision":
			p.AntiVision = &in.AntiVision
		case "vision":
			p.Vision = &in.Vision
		case "identity":
			p.Identity = &in.Identity
		case "constraints":
			cs := in.Constraints
			p.Constraints = &cs
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unknown field %q in update_mask", f)
		}
	}
	c, err := s.Store.SetCompass(ctx, u, spaceIn(in.Space), p, time.Now())
	if err != nil {
		return nil, toStatus(err)
	}
	return compassToPB(c), nil
}

func (s *Compass) ListCompassRevisions(ctx context.Context, req *pb.ListCompassRevisionsRequest) (*pb.ListCompassRevisionsResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	cs, err := s.Store.ListCompassRevisions(ctx, u, spaceIn(req.Space), int(req.Limit))
	if err != nil {
		return nil, toStatus(err)
	}
	out := &pb.ListCompassRevisionsResponse{}
	for _, c := range cs {
		out.Revisions = append(out.Revisions, compassToPB(c))
	}
	return out, nil
}
