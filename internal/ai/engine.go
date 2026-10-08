// Package ai is the optional intelligence layer. Everything here sits behind
// the Engine interface; when AI is disabled the server simply holds a nil
// Engine and the notes/calendar paths never touch this package.
package ai

import (
	"context"
	"errors"
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

var (
	// ErrNoSemantic is returned by Search when no embedding model is configured.
	ErrNoSemantic = errors.New("semantic search needs an embedding model")
)

type Status struct {
	Chat     bool
	Semantic bool
	Model    string
}

type Match struct {
	NoteID string
	Score  float64
}

type Reply struct {
	Text      string
	SessionID string
	ToolsUsed []string
}

type Engine interface {
	Status() Status

	// IndexNote adds or replaces a note in the semantic index.
	IndexNote(ctx context.Context, n store.Note) error
	RemoveNote(ctx context.Context, userID, noteID string) error
	// Search ranks the user's indexed notes against query.
	Search(ctx context.Context, userID, query string, limit int) ([]Match, error)

	// Ask runs the assistant, which can read and write the user's notes,
	// events and tasks through tools.
	Ask(ctx context.Context, user store.User, sessionID, message string) (Reply, error)
	Summarize(ctx context.Context, n store.Note) (string, error)
	SuggestTags(ctx context.Context, n store.Note) ([]string, error)
	Briefing(ctx context.Context, user store.User, day time.Time, loc *time.Location) (string, error)

	Close() error
}
