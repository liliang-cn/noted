package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/ai"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/plan"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AI exposes the optional assistant. With a nil Engine every method except
// GetStatus answers FAILED_PRECONDITION, so clients can feature-detect.
type AI struct {
	pb.UnimplementedAIServiceServer
	Store    *store.Store
	Engine   ai.Engine
	Location *time.Location // default zone for briefings
	Locale   plan.Locale
	Sugg     *Suggestions // accepts what the assistant prepares
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

// errOff names the space the user has not let the assistant read.
func errOff(space string) error {
	switch space {
	case store.SpaceWork:
		return status.Error(codes.FailedPrecondition, "AI is turned off for work content")
	case store.SpaceLife:
		return status.Error(codes.FailedPrecondition, "AI is turned off for life content")
	}
	return status.Error(codes.FailedPrecondition, "AI is turned off for all of your content")
}

// scope turns the view the app asked for into what the assistant may look at.
// This is where "AI may not read my work content" is enforced: everything the
// assistant reads goes through the filter returned here.
func (s *AI) scope(ctx context.Context, userID, requested string) (string, store.AIAccess, error) {
	acc, err := s.Store.GetAIAccess(ctx, userID)
	if err != nil {
		return "", acc, toStatus(err)
	}
	space, ok := acc.Scope(requested)
	if !ok {
		return "", acc, errOff(requested)
	}
	return space, acc, nil
}

// allowNote refuses to run a model over a note from a space that is off.
func (s *AI) allowNote(ctx context.Context, userID string, n store.Note) error {
	acc, err := s.Store.GetAIAccess(ctx, userID)
	if err != nil {
		return toStatus(err)
	}
	if !acc.Allows(n.Space) {
		return errOff(n.Space)
	}
	return nil
}

func (s *AI) Ask(ctx context.Context, req *pb.AskRequest) (*pb.AskResponse, error) {
	u, err := s.need(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Message) == "" {
		return nil, status.Error(codes.InvalidArgument, "message is empty")
	}
	space, acc, err := s.scope(ctx, u.ID, spaceIn(req.Space))
	if err != nil {
		return nil, err
	}
	// A conversation belongs to one epoch of the access setting. Once a space
	// is turned off, earlier conversations (which may quote it) are never replayed.
	sid := req.SessionId
	if sid == "" {
		sid = uuid.NewString()
	}
	prefix := fmt.Sprintf("e%d.", acc.Epoch)
	r, err := s.Engine.Ask(ctx, u, prefix+sid, req.Message, space)
	if err != nil {
		return nil, s.aiError("ask", err)
	}
	r.SessionID = strings.TrimPrefix(r.SessionID, prefix)
	resp := &pb.AskResponse{Reply: r.Text, SessionId: r.SessionID, ToolsUsed: r.ToolsUsed}
	for _, ref := range r.Refs {
		out := &pb.Reference{Kind: ref.Kind, Id: ref.ID, Title: ref.Title}
		if ref.Time != nil {
			out.Time = timestamppb.New(*ref.Time)
		}
		resp.References = append(resp.References, out)
	}
	if len(r.Ops) > 0 {
		p, _, err := saveDraft(ctx, s.Store, u.ID, plan.BuildAsk(r.Ops, spaceIn(req.Space), s.Locale), plan.SourceAI)
		if err != nil {
			return nil, toStatus(err)
		}
		if req.AutoApply {
			acc, err := s.Sugg.accept(ctx, u.ID, p, nil, nil)
			if err != nil {
				return nil, err
			}
			resp.Proposal, resp.Change = acc.Proposal, acc.Change
			if s.Changed != nil {
				s.Changed() // the assistant may have written notes
			}
		} else if resp.Proposal, err = proposalToPB(p); err != nil {
			return nil, toStatus(err)
		}
	}
	return resp, nil
}

var errFeatureOff = status.Error(codes.FailedPrecondition, "this feature is turned off in the AI settings")

// feature returns an error when a feature the user switched off is called.
func (s *AI) feature(ctx context.Context, userID string, on func(store.AIFeatures) bool) error {
	f, err := s.Store.GetAIFeatures(ctx, userID)
	if err != nil {
		return toStatus(err)
	}
	if !on(f) {
		return errFeatureOff
	}
	return nil
}

func (s *AI) SummarizeNote(ctx context.Context, req *pb.SummarizeNoteRequest) (*pb.SummarizeNoteResponse, error) {
	u, err := s.need(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.feature(ctx, u.ID, func(f store.AIFeatures) bool { return f.NoteTools }); err != nil {
		return nil, err
	}
	n, err := s.Store.GetNote(ctx, u.ID, req.NoteId)
	if err != nil {
		return nil, toStatus(err)
	}
	if err := s.allowNote(ctx, u.ID, n); err != nil {
		return nil, err
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
	if err := s.feature(ctx, u.ID, func(f store.AIFeatures) bool { return f.NoteTools }); err != nil {
		return nil, err
	}
	n, err := s.Store.GetNote(ctx, u.ID, req.NoteId)
	if err != nil {
		return nil, toStatus(err)
	}
	if err := s.allowNote(ctx, u.ID, n); err != nil {
		return nil, err
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
	if err := s.feature(ctx, u.ID, func(f store.AIFeatures) bool { return f.Briefing }); err != nil {
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
	space, _, err := s.scope(ctx, u.ID, spaceIn(req.Space))
	if err != nil {
		return nil, err
	}
	text, err := s.Engine.Briefing(ctx, u, day, loc, space)
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

func (s *AI) PlanFromText(ctx context.Context, req *pb.PlanFromTextRequest) (*pb.Proposal, error) {
	u, err := s.need(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Text) == "" {
		return nil, status.Error(codes.InvalidArgument, "text is empty")
	}
	spec, err := s.Engine.Plan(ctx, u, req.Text, spaceIn(req.Space))
	if err != nil {
		return nil, s.aiError("plan", err)
	}
	p, _, err := saveDraft(ctx, s.Store, u.ID, plan.BuildPlan(spec, s.Locale), plan.SourceAI)
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := proposalToPB(p)
	if err != nil {
		return nil, toStatus(err)
	}
	return out, nil
}

func (s *AI) ExtractTasks(ctx context.Context, req *pb.ExtractTasksRequest) (*pb.Proposal, error) {
	u, err := s.need(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.feature(ctx, u.ID, func(f store.AIFeatures) bool { return f.NoteTools }); err != nil {
		return nil, err
	}
	n, err := s.Store.GetNote(ctx, u.ID, req.NoteId)
	if err != nil {
		return nil, toStatus(err)
	}
	if err := s.allowNote(ctx, u.ID, n); err != nil {
		return nil, err
	}
	tasks, err := s.Engine.Extract(ctx, u, n)
	if err != nil {
		return nil, s.aiError("extract", err)
	}
	if len(tasks) == 0 {
		return nil, status.Error(codes.FailedPrecondition, "no to-dos found in this note")
	}
	p, _, err := saveDraft(ctx, s.Store, u.ID, plan.BuildExtract(tasks, n.ProjectID, n.Space, s.Locale), plan.SourceAI)
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := proposalToPB(p)
	if err != nil {
		return nil, toStatus(err)
	}
	return out, nil
}

func accessToPB(a store.AIAccess) *pb.AIAccess {
	return &pb.AIAccess{AllowWork: a.Work, AllowLife: a.Life}
}

func (s *AI) GetAIAccess(ctx context.Context, _ *pb.GetAIAccessRequest) (*pb.AIAccess, error) {
	u, ok := auth.User(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	a, err := s.Store.GetAIAccess(ctx, u.ID)
	if err != nil {
		return nil, toStatus(err)
	}
	return accessToPB(a), nil
}

// SetAIAccess records which spaces the assistant may read and applies what
// follows from it: a space turned off is removed from the semantic index and
// stops being indexed; one turned back on is indexed again.
func (s *AI) SetAIAccess(ctx context.Context, req *pb.SetAIAccessRequest) (*pb.AIAccess, error) {
	u, ok := auth.User(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	before, err := s.Store.GetAIAccess(ctx, u.ID)
	if err != nil {
		return nil, toStatus(err)
	}
	after, err := s.Store.SetAIAccess(ctx, u.ID, req.AllowWork, req.AllowLife)
	if err != nil {
		return nil, toStatus(err)
	}
	for _, sp := range []string{store.SpaceWork, store.SpaceLife} {
		switch {
		case before.Allows(sp) && !after.Allows(sp):
			s.purge(ctx, u.ID, sp)
		case !before.Allows(sp) && after.Allows(sp):
			if err := s.Store.ResetNoteIndexFor(ctx, u.ID, sp); err != nil {
				return nil, toStatus(err)
			}
			if s.Changed != nil {
				s.Changed()
			}
		}
	}
	return accessToPB(after), nil
}

// purge removes a space's notes from the semantic index. The permission is
// already off, so nothing reads them meanwhile; this makes the removal real.
func (s *AI) purge(ctx context.Context, userID, space string) {
	if s.Engine == nil {
		return
	}
	ids, err := s.Store.NoteIDsInSpace(ctx, userID, space)
	if err != nil {
		slog.Error("ai access: list notes to purge", "err", err)
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		for _, id := range ids {
			if err := s.Engine.RemoveNote(bg, userID, id); err != nil {
				slog.Warn("ai access: remove note from index", "note", id, "err", err)
			}
		}
	}()
}

func featuresToPB(f store.AIFeatures) *pb.AIFeatures {
	return &pb.AIFeatures{DailyBriefing: f.Briefing, Suggestions: f.Suggestions, WeeklyReview: f.WeeklyReview, NoteTools: f.NoteTools}
}

func (s *AI) GetAIFeatures(ctx context.Context, _ *pb.GetAIFeaturesRequest) (*pb.AIFeatures, error) {
	u, ok := auth.User(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	f, err := s.Store.GetAIFeatures(ctx, u.ID)
	if err != nil {
		return nil, toStatus(err)
	}
	return featuresToPB(f), nil
}

func (s *AI) SetAIFeatures(ctx context.Context, req *pb.SetAIFeaturesRequest) (*pb.AIFeatures, error) {
	u, ok := auth.User(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	f, err := s.Store.SetAIFeatures(ctx, u.ID, store.AIFeatures{
		Briefing: req.DailyBriefing, Suggestions: req.Suggestions, WeeklyReview: req.WeeklyReview, NoteTools: req.NoteTools})
	if err != nil {
		return nil, toStatus(err)
	}
	return featuresToPB(f), nil
}
