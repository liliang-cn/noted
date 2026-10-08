// Package server wires the gRPC services, auth and health into one grpc.Server.
package server

import (
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/ai"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/reminder"
	"github.com/liliang-cn/noted/internal/service"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

type Deps struct {
	Store      *store.Store
	Auth       *auth.Authenticator
	Hub        *reminder.Hub
	Engine     ai.Engine // nil: AI disabled
	Location   *time.Location
	Changed    func() // called after note writes; may be nil
	Reflection bool
}

func New(d Deps, opts ...grpc.ServerOption) *grpc.Server {
	opts = append(opts,
		grpc.ChainUnaryInterceptor(d.Auth.Unary()),
		grpc.ChainStreamInterceptor(d.Auth.Stream()),
		grpc.MaxRecvMsgSize(4<<20), // notes are capped at 1 MiB; leave headroom
	)
	g := grpc.NewServer(opts...)
	pb.RegisterNoteServiceServer(g, &service.Notes{Store: d.Store, AI: d.Engine, Changed: d.Changed})
	pb.RegisterCalendarServiceServer(g, &service.Calendar{Store: d.Store, Hub: d.Hub})
	pb.RegisterAIServiceServer(g, &service.AI{Store: d.Store, Engine: d.Engine, Location: d.Location, Changed: d.Changed})

	h := health.NewServer()
	healthpb.RegisterHealthServer(g, h)
	if d.Reflection {
		reflection.Register(g)
	}
	return g
}
