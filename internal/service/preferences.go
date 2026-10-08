package service

import (
	"context"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Preferences keeps small per-user JSON documents (theme, layouts) so every
// device sees the same ones. It stores them verbatim and checks no entitlement.
type Preferences struct {
	pb.UnimplementedPreferenceServiceServer
	Store *store.Store
}

func prefToPB(p store.Preference) *pb.Preference {
	return &pb.Preference{Key: p.Key, Value: p.Value, Version: p.Version, UpdateTime: timestamppb.New(p.Updated)}
}

func (s *Preferences) GetPreference(ctx context.Context, req *pb.GetPreferenceRequest) (*pb.Preference, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	p, err := s.Store.GetPreference(ctx, u, req.GetKey())
	if err != nil {
		return nil, toStatus(err)
	}
	return prefToPB(p), nil
}

func (s *Preferences) SetPreference(ctx context.Context, req *pb.SetPreferenceRequest) (*pb.Preference, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	p, err := s.Store.SetPreference(ctx, u, req.Key, req.Value, req.IfVersion)
	if err != nil {
		return nil, toStatus(err)
	}
	return prefToPB(p), nil
}

func (s *Preferences) DeletePreference(ctx context.Context, req *pb.DeletePreferenceRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeletePreference(ctx, u, req.GetKey()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Preferences) ListPreferences(ctx context.Context, req *pb.ListPreferencesRequest) (*pb.ListPreferencesResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := s.Store.ListPreferences(ctx, u, req.Prefix)
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListPreferencesResponse{}
	for _, p := range ps {
		resp.Preferences = append(resp.Preferences, prefToPB(p))
	}
	return resp, nil
}
