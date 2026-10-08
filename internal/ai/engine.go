// Package ai is the optional intelligence layer. Everything here sits behind
// the Engine interface; when AI is disabled the server simply holds a nil
// Engine and the notes/calendar paths never touch this package.
package ai

import (
	"context"
	"errors"
	"time"

	"github.com/liliang-cn/noted/internal/plan"
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

// Ref is something the assistant read during a reply.
type Ref struct {
	Kind  string // event, task, note, project, goal
	ID    string
	Title string
	Time  *time.Time
}

type Reply struct {
	Text      string
	SessionID string
	ToolsUsed []string
	// Ops are the changes the assistant wants to make. It has made none of them:
	// they are staged, and take effect only when the user accepts them.
	Ops []plan.Op
	// Refs are what the assistant looked at that its reply mentions.
	Refs []Ref
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
	// space is the app's current mode ("work", "life" or "" for both). It scopes what
	// the assistant reads and is the default for what it creates.
	Ask(ctx context.Context, user store.User, sessionID, message, space string) (Reply, error)
	// Plan breaks a request like "next month I'm going to X, I need A, B and C" into a
	// project with tasks. Dates are kept only when the user stated them.
	Plan(ctx context.Context, user store.User, text, space string) (plan.PlanSpec, error)
	// Extract finds the to-dos written inside a note.
	Extract(ctx context.Context, user store.User, n store.Note) ([]plan.ExtractSpec, error)
	Summarize(ctx context.Context, n store.Note) (string, error)
	SuggestTags(ctx context.Context, n store.Note) ([]string, error)
	Briefing(ctx context.Context, user store.User, day time.Time, loc *time.Location, space string) (string, error)

	Close() error
}
