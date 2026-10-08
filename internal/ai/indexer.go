package ai

import (
	"context"
	"log/slog"
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

// Indexer keeps the semantic index in step with the notes table. It works
// from the database (notes whose index is missing or stale), not from an
// in-memory queue, so it survives restarts, catches up after AI is first
// enabled, and re-indexes everything after the index file is deleted.
type Indexer struct {
	Store  *store.Store
	Engine Engine

	wake chan struct{}
}

func NewIndexer(s *store.Store, e Engine) *Indexer {
	return &Indexer{Store: s, Engine: e, wake: make(chan struct{}, 1)}
}

// Wake asks the indexer to look for new work now. It never blocks.
func (ix *Indexer) Wake() {
	select {
	case ix.wake <- struct{}{}:
	default:
	}
}

// Run blocks until ctx is cancelled.
func (ix *Indexer) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		wait := ix.drain(ctx)
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ix.wake:
		case <-t.C:
		}
	}
}

// drain indexes until nothing is stale. It returns a non-zero backoff when an
// index call failed, so a down embedding endpoint is retried gently.
func (ix *Indexer) drain(ctx context.Context) time.Duration {
	for ctx.Err() == nil {
		notes, err := ix.Store.NotesNeedingIndex(ctx, 20)
		if err != nil {
			slog.Error("ai index: list stale notes", "err", err)
			return 30 * time.Second
		}
		if len(notes) == 0 {
			return 0
		}
		for _, n := range notes {
			if err := ix.Engine.IndexNote(ctx, n); err != nil {
				slog.Warn("ai index: note not indexed; will retry", "note", n.ID, "err", err)
				return 30 * time.Second
			}
			if err := ix.Store.MarkNoteIndexed(ctx, n.ID, n.Updated); err != nil {
				slog.Error("ai index: mark indexed", "note", n.ID, "err", err)
				return 30 * time.Second
			}
		}
	}
	return 0
}
