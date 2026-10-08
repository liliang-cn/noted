package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/ai"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AI exposes the optional assistant. With a nil Engine every method except
// GetStatus answers FAILED_PRECONDITION, so clients can feature-detect.
type AI struct {
	pb.UnimplementedAIServiceServer
	Store    *store.Store
	Engine   ai.Engine
	Location *time.Location // default zone for briefings
	Changed  func()
}

var errDisabled = status.Error(codes.FailedPrecondition, "AI is not enabled on this server")

func (s *AI) need(ctx context.Context) (store.User, error) {
	u, ok := auth.User(ctx)
	if !ok {
		return store.User{}, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if s.Engine == nil || !s.Engine.Status().Chat {
		return store.User{}, errDisabled
	}
	return u, nil
}

func (s *AI) GetStatus(context.Context, *pb.GetStatusRequest) (*pb.GetStatusResponse, error) {
	if s.Engine == nil {
		return &pb.GetStatusResponse{}, nil
	}
	st := s.Engine.Status()
	return &pb.GetStatusResponse{Enabled: true, Chat: st.Chat, SemanticSearch: st.Semantic, Model: st.Model}, nil
}

func (s *AI) Ask(ctx context.Context, req *pb.AskRequest) (*pb.AskResponse, error) {
	u, err := s.need(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Message) == "" {
		return nil, status.Error(codes.InvalidArgument, "message is empty")
	}
	r, err := s.Engine.Ask(ctx, u, req.SessionId, req.Message)
	if err != nil {
		return nil, s.aiError("ask", err)
	}
	if s.Changed != nil {
		s.Changed() // the assistant may have written notes
	}
	return &pb.AskResponse{Reply: r.Text, SessionId: r.SessionID, ToolsUsed: r.ToolsUsed}, nil
}

func (s *AI) SummarizeNote(ctx context.Context, req *pb.SummarizeNoteRequest) (*pb.SummarizeNoteResponse, error) {
	u, err := s.need(ctx)
	if err != nil {
		return nil, err
	}
	n, err := s.Store.GetNote(ctx, u.ID, req.NoteId)
	if err != nil {
		return nil, toStatus(err)
	}
	text, err := s.Engine.Summarize(ctx, n)
	if err != nil {
		return nil, s.aiError("summarize", err)
	}
	return &pb.SummarizeNoteResponse{Summary: text}, nil
}

func (s *AI) SuggestTags(ctx context.Context, req *pb.SuggestTagsRequest) (*pb.SuggestTagsResponse, error) {
	u, err := s.need(ctx)
	if err != nil {
		return nil, err
	}
	n, err := s.Store.GetNote(ctx, u.ID, req.NoteId)
	if err != nil {
		return nil, toStatus(err)
	}
	tags, err := s.Engine.SuggestTags(ctx, n)
	if err != nil {
		return nil, s.aiError("suggest tags", err)
	}
	if req.Apply && len(tags) > 0 {
		merged := append(append([]string{}, n.Tags...), tags...)
		if _, err := s.Store.UpdateNote(ctx, u.ID, n.ID, store.NotePatch{Tags: &merged}); err != nil {
			return nil, toStatus(err)
		}
		if s.Changed != nil {
			s.Changed()
		}
	}
	return &pb.SuggestTagsResponse{Tags: tags}, nil
}

func (s *AI) DailyBriefing(ctx context.Context, req *pb.DailyBriefingRequest) (*pb.DailyBriefingResponse, error) {
	u, err := s.need(ctx)
	if err != nil {
		return nil, err
	}
	loc := s.Location
	if req.TimeZone != "" {
		if loc, err = time.LoadLocation(req.TimeZone); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "unknown time_zone %q", req.TimeZone)
		}
	}
	day := time.Now()
	if req.Day != nil {
		day = req.Day.AsTime()
	}
	text, err := s.Engine.Briefing(ctx, u, day, loc)
	if err != nil {
		return nil, s.aiError("briefing", err)
	}
	return &pb.DailyBriefingResponse{Briefing: text}, nil
}

// aiError hides upstream provider details (which can echo credentials or
// request bodies) from clients and keeps them in the server log.
func (s *AI) aiError(op string, err error) error {
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	if ctxErr := toStatus(err); status.Code(ctxErr) != codes.Internal {
		return ctxErr
	}
	slog.Error("ai request failed", "op", op, "err", err)
	return status.Errorf(codes.Unavailable, "AI request failed (%s); check the server log", op)
}
