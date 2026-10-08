package service

import (
	"context"
	"fmt"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/plan"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Suggestions turns the user's data into proposals and applies the ones they accept.
type Suggestions struct {
	pb.UnimplementedSuggestionServiceServer
	Store    *store.Store
	Location *time.Location
	Locale   plan.Locale
}

func (s *Suggestions) loc() *time.Location {
	if s.Location == nil {
		return time.UTC
	}
	return s.Location
}

func (s *Suggestions) suggester(loc *time.Location) plan.Suggester {
	return plan.Suggester{Store: s.Store, Loc: loc, Locale: s.Locale}
}

func (s *Suggestions) applier(loc *time.Location) plan.Applier {
	return plan.Applier{Store: s.Store, Loc: loc}
}

func proposalToPB(p store.Proposal) (*pb.Proposal, error) {
	ops, err := plan.DecodeOps(p.Ops)
	if err != nil {
		return nil, fmt.Errorf("stored proposal %s is damaged: %w", p.ID, err)
	}
	inputs, err := plan.DecodeInputs(p.Inputs)
	if err != nil {
		return nil, fmt.Errorf("stored proposal %s is damaged: %w", p.ID, err)
	}
	out := &pb.Proposal{
		Id: p.ID, Kind: p.Kind, Status: p.Status, Title: p.Title, Reason: p.Reason, Space: spaceOut(p.Space),
		Source: p.Source, CreateTime: timestamppb.New(p.Created), DecideTime: tsPtr(p.Decided),
	}
	for _, o := range ops {
		a, err := structpb.NewStruct(o.Args)
		if err != nil {
			return nil, err
		}
		out.Operations = append(out.Operations, &pb.Operation{Type: o.Type, Label: o.Label, Args: a})
	}
	for _, in := range inputs {
		out.Inputs = append(out.Inputs, &pb.ProposalInput{Name: in.Name, Label: in.Label, Type: in.Type, Required: in.Required})
	}
	return out, nil
}

func changeToPB(c store.Change) *pb.Change {
	n := 0
	if es, err := plan.DecodeEntries(c.Entries); err == nil {
		n = len(es)
	}
	return &pb.Change{Id: c.ID, ProposalId: c.ProposalID, Summary: c.Summary, Entries: int32(n),
		CreateTime: timestamppb.New(c.Created), Undone: c.Undone != nil, UndoTime: tsPtr(c.Undone)}
}

// saveDraft stores a draft as a pending proposal (deduplicated by fingerprint).
func saveDraft(ctx context.Context, st *store.Store, userID string, d plan.Draft, source string) (store.Proposal, bool, error) {
	return st.SaveProposal(ctx, store.Proposal{
		UserID: userID, Kind: d.Kind, Title: d.Title, Reason: d.Reason, Space: d.Space, Source: source,
		Fingerprint: d.Fingerprint, Ops: plan.EncodeOps(d.Ops), Inputs: plan.EncodeInputs(d.Inputs),
	})
}

func (s *Suggestions) ListProposals(ctx context.Context, req *pb.ListProposalsRequest) (*pb.ListProposalsResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	statusFilter := req.Status
	if statusFilter == "" {
		statusFilter = store.ProposalPending
	}
	feats, err := s.Store.GetAIFeatures(ctx, u)
	if err != nil {
		return nil, toStatus(err)
	}
	if !feats.Suggestions && statusFilter == store.ProposalPending {
		return &pb.ListProposalsResponse{}, nil // switched off: nothing is computed or shown
	}
	if statusFilter == store.ProposalPending && !req.NoRefresh {
		drafts, err := s.suggester(s.loc()).Rules(ctx, u, time.Now())
		if err != nil {
			return nil, toStatus(err)
		}
		keep := make([]string, 0, len(drafts))
		for _, d := range drafts {
			keep = append(keep, d.Fingerprint)
			if _, _, err := saveDraft(ctx, s.Store, u, d, plan.SourceRules); err != nil {
				return nil, toStatus(err)
			}
		}
		// A suggestion whose situation has passed is withdrawn rather than left to go stale.
		if err := s.Store.ExpirePending(ctx, u, plan.SourceRules, keep); err != nil {
			return nil, toStatus(err)
		}
	}
	ps, err := s.Store.ListProposals(ctx, u, statusFilter, spaceIn(req.Space))
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListProposalsResponse{}
	for _, p := range ps {
		out, err := proposalToPB(p)
		if err != nil {
			return nil, toStatus(err)
		}
		resp.Proposals = append(resp.Proposals, out)
	}
	return resp, nil
}

func (s *Suggestions) AcceptProposal(ctx context.Context, req *pb.AcceptProposalRequest) (*pb.AcceptProposalResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	p, err := s.Store.GetProposal(ctx, u, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return s.accept(ctx, u, p, req.Inputs, req.Selection)
}

// accept applies a proposal on behalf of the user. It claims the proposal first,
// so two devices cannot both apply it, and gives it back if applying fails.
func (s *Suggestions) accept(ctx context.Context, userID string, p store.Proposal, inputs map[string]string, sel *pb.OperationSelection) (*pb.AcceptProposalResponse, error) {
	ops, err := plan.DecodeOps(p.Ops)
	if err != nil {
		return nil, toStatus(err)
	}
	wanted, err := plan.DecodeInputs(p.Inputs)
	if err != nil {
		return nil, toStatus(err)
	}
	var selected map[int]bool
	if sel != nil {
		selected = map[int]bool{}
		for _, i := range sel.Indexes {
			if int(i) < 0 || int(i) >= len(ops) {
				return nil, status.Errorf(codes.InvalidArgument, "selection: there is no operation %d", i)
			}
			selected[int(i)] = true
		}
		if len(selected) == 0 {
			return nil, status.Error(codes.InvalidArgument, "selection is empty; dismiss the proposal instead")
		}
	}
	for _, in := range wanted {
		if in.Required && inputs[in.Name] == "" {
			return nil, status.Errorf(codes.InvalidArgument, "%s is required", in.Label)
		}
	}

	if err := s.Store.DecideProposal(ctx, userID, p.ID, store.ProposalAccepted); err != nil {
		return nil, toStatus(err)
	}
	entries, err := s.applier(s.loc()).Apply(ctx, userID, ops, selected, inputs)
	if err != nil {
		_ = s.Store.ReopenProposal(ctx, userID, p.ID)
		return nil, toStatus(err)
	}
	ch, err := s.Store.SaveChange(ctx, store.Change{UserID: userID, ProposalID: p.ID, Summary: p.Title, Entries: plan.EncodeEntries(entries)})
	if err != nil {
		return nil, toStatus(err)
	}
	done, err := s.Store.GetProposal(ctx, userID, p.ID)
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := proposalToPB(done)
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.AcceptProposalResponse{Proposal: out, Change: changeToPB(ch)}, nil
}

func (s *Suggestions) DismissProposal(ctx context.Context, req *pb.DismissProposalRequest) (*pb.Proposal, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DecideProposal(ctx, u, req.GetId(), store.ProposalDismissed); err != nil {
		return nil, toStatus(err)
	}
	p, err := s.Store.GetProposal(ctx, u, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := proposalToPB(p)
	if err != nil {
		return nil, toStatus(err)
	}
	return out, nil
}

func (s *Suggestions) zone(name string) (*time.Location, error) {
	if name == "" {
		return s.loc(), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unknown time_zone %q", name)
	}
	return loc, nil
}

func (s *Suggestions) ProposeSchedule(ctx context.Context, req *pb.ProposeScheduleRequest) (*pb.Proposal, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	loc, err := s.zone(req.TimeZone)
	if err != nil {
		return nil, err
	}
	d, err := s.suggester(loc).Schedule(ctx, u, req.TaskIds, time.Duration(req.DurationMinutes)*time.Minute, int(req.Days), time.Now())
	if err != nil {
		if err == plan.ErrNoSlot {
			return nil, status.Error(codes.FailedPrecondition, "there is not enough free time in that window")
		}
		return nil, toStatus(err)
	}
	p, _, err := saveDraft(ctx, s.Store, u, d, plan.SourceRules)
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := proposalToPB(p)
	if err != nil {
		return nil, toStatus(err)
	}
	return out, nil
}

func (s *Suggestions) GetWeeklyReview(ctx context.Context, req *pb.GetWeeklyReviewRequest) (*pb.WeeklyReview, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	loc, err := s.zone(req.TimeZone)
	if err != nil {
		return nil, err
	}
	feats, err := s.Store.GetAIFeatures(ctx, u)
	if err != nil {
		return nil, toStatus(err)
	}
	if !feats.WeeklyReview {
		return nil, errFeatureOff
	}
	now := time.Now()
	weekOf := now
	if req.WeekOf != nil {
		weekOf = req.WeekOf.AsTime()
	}
	r, err := s.suggester(loc).WeeklyReview(ctx, u, weekOf, spaceIn(req.Space), now)
	if err != nil {
		return nil, toStatus(err)
	}
	out := &pb.WeeklyReview{From: timestamppb.New(r.From), To: timestamppb.New(r.To),
		TasksDone: int32(r.TasksDone), TasksTotal: int32(r.TasksTotal)}
	for _, g := range r.Goals {
		out.Goals = append(out.Goals, goalToPB(g))
	}
	for _, t := range r.Carried {
		out.CarriedTasks = append(out.CarriedTasks, taskToPB(t))
	}
	if r.Next != nil {
		p, _, err := saveDraft(ctx, s.Store, u, *r.Next, plan.SourceRules)
		if err != nil {
			return nil, toStatus(err)
		}
		if p.Status == store.ProposalPending {
			if out.NextWeek, err = proposalToPB(p); err != nil {
				return nil, toStatus(err)
			}
		}
	}
	return out, nil
}

func (s *Suggestions) ListChanges(ctx context.Context, req *pb.ListChangesRequest) (*pb.ListChangesResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}
	cs, err := s.Store.ListChanges(ctx, u, min(limit, 200))
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListChangesResponse{}
	for _, c := range cs {
		resp.Changes = append(resp.Changes, changeToPB(c))
	}
	return resp, nil
}

func (s *Suggestions) UndoChange(ctx context.Context, req *pb.UndoChangeRequest) (*pb.Change, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.Store.GetChange(ctx, u, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	if c.Undone != nil {
		return nil, status.Error(codes.FailedPrecondition, "this change was already undone")
	}
	entries, err := plan.DecodeEntries(c.Entries)
	if err != nil {
		return nil, toStatus(err)
	}
	if err := s.Store.MarkChangeUndone(ctx, u, c.ID); err != nil {
		return nil, toStatus(err)
	}
	if err := s.applier(s.loc()).Undo(ctx, u, entries); err != nil {
		return nil, toStatus(err)
	}
	c, err = s.Store.GetChange(ctx, u, c.ID)
	if err != nil {
		return nil, toStatus(err)
	}
	return changeToPB(c), nil
}
