package service

import (
	"context"
	"log/slog"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/ai"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Notes struct {
	pb.UnimplementedNoteServiceServer
	Store *store.Store
	AI    ai.Engine // nil when AI is off
	// Changed is called after a note is written so the AI indexer can pick it
	// up. May be nil.
	Changed func()
}

func (s *Notes) changed() {
	if s.Changed != nil {
		s.Changed()
	}
}

func (s *Notes) CreateNote(ctx context.Context, req *pb.CreateNoteRequest) (*pb.Note, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetNote()
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "note is required")
	}
	n, err := s.Store.CreateNote(ctx, store.Note{
		UserID: u, Title: in.Title, Content: in.Content, Tags: in.Tags, Pinned: in.Pinned, Archived: in.Archived,
	})
	if err != nil {
		return nil, toStatus(err)
	}
	s.changed()
	return noteToPB(n), nil
}

func (s *Notes) GetNote(ctx context.Context, req *pb.GetNoteRequest) (*pb.Note, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	n, err := s.Store.GetNote(ctx, u, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return noteToPB(n), nil
}

func (s *Notes) UpdateNote(ctx context.Context, req *pb.UpdateNoteRequest) (*pb.Note, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetNote()
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "note.id is required")
	}
	paths, err := maskPaths(req.UpdateMask, "title", "content", "tags", "pinned", "archived")
	if err != nil {
		return nil, err
	}
	var p store.NotePatch
	if paths["title"] {
		p.Title = &in.Title
	}
	if paths["content"] {
		p.Content = &in.Content
	}
	if paths["tags"] {
		p.Tags = &in.Tags
	}
	if paths["pinned"] {
		p.Pinned = &in.Pinned
	}
	if paths["archived"] {
		p.Archived = &in.Archived
	}
	n, err := s.Store.UpdateNote(ctx, u, in.Id, p)
	if err != nil {
		return nil, toStatus(err)
	}
	s.changed()
	return noteToPB(n), nil
}

func (s *Notes) DeleteNote(ctx context.Context, req *pb.DeleteNoteRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteNote(ctx, u, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	if s.AI != nil {
		// Best effort: a stale index entry is harmless because search
		// results are re-checked against the store.
		go func() {
			if err := s.AI.RemoveNote(context.WithoutCancel(ctx), u, req.GetId()); err != nil {
				slog.Warn("remove note from ai index", "note", req.GetId(), "err", err)
			}
		}()
	}
	return &emptypb.Empty{}, nil
}

func (s *Notes) ListNotes(ctx context.Context, req *pb.ListNotesRequest) (*pb.ListNotesResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset, err := page(req.PageSize, req.PageToken)
	if err != nil {
		return nil, err
	}
	notes, err := s.Store.ListNotes(ctx, u, store.NoteFilter{
		Tag: req.Tag, PinnedOnly: req.PinnedOnly, IncludeArchived: req.IncludeArchived,
		Limit: limit + 1, Offset: offset,
	})
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.ListNotesResponse{NextPageToken: nextToken(offset, len(notes), limit)}
	for i, n := range notes {
		if i == limit {
			break
		}
		resp.Notes = append(resp.Notes, noteToPB(n))
	}
	return resp, nil
}

func (s *Notes) SearchNotes(ctx context.Context, req *pb.SearchNotesRequest) (*pb.SearchNotesResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)

	if req.Semantic && s.AI != nil && s.AI.Status().Semantic {
		if hits, err := s.semantic(ctx, u, req, limit); err == nil {
			return &pb.SearchNotesResponse{Hits: hits, Mode: "semantic"}, nil
		} else {
			slog.Warn("semantic search failed; falling back to full-text", "err", err)
		}
	}
	hits, err := s.Store.SearchNotes(ctx, u, req.Query, limit, req.IncludeArchived)
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &pb.SearchNotesResponse{Mode: "fulltext"}
	for _, h := range hits {
		resp.Hits = append(resp.Hits, &pb.NoteHit{Note: noteToPB(h.Note), Snippet: h.Snippet, Score: h.Score})
	}
	return resp, nil
}

func (s *Notes) semantic(ctx context.Context, userID string, req *pb.SearchNotesRequest, limit int) ([]*pb.NoteHit, error) {
	// Over-fetch: archived or deleted notes are filtered out below.
	matches, err := s.AI.Search(ctx, userID, req.Query, limit*2)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(matches))
	score := map[string]float64{}
	for i, m := range matches {
		ids[i] = m.NoteID
		score[m.NoteID] = m.Score
	}
	notes, err := s.Store.NotesByIDs(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	var hits []*pb.NoteHit
	for _, n := range notes {
		if n.Archived && !req.IncludeArchived {
			continue
		}
		hits = append(hits, &pb.NoteHit{Note: noteToPB(n), Snippet: store.Snippet(n.Content, req.Query, 80), Score: score[n.ID]})
		if len(hits) == limit {
			break
		}
	}
	return hits, nil
}
