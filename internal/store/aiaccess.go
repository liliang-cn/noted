package store

import (
	"context"
	"encoding/json"
	"errors"
)

// AIAccessKey is the reserved preference that records which spaces the
// assistant may read. It is written only through SetAIAccess, so turning a
// space off always comes with its side effects (purging the semantic index,
// forgetting conversations that quoted that space).
const AIAccessKey = "ai.access"

// AIAccess says which spaces' content may be sent to a language or embedding model.
type AIAccess struct {
	Work bool `json:"work"`
	Life bool `json:"life"`
	// Epoch increases every time access narrows. Assistant conversations are
	// keyed by it, so one that quoted content from a space since turned off is
	// never sent to the model again.
	Epoch int64 `json:"epoch"`
}

// Allows reports whether content of the given space may be sent to a model.
func (a AIAccess) Allows(space string) bool {
	switch space {
	case SpaceWork:
		return a.Work
	case SpaceLife:
		return a.Life
	}
	return false
}

// Scope turns a requested view into the filter the assistant must use:
// "" for everything, or one space. It reports false when the request asks for
// content the user has not allowed, or when nothing is allowed at all.
func (a AIAccess) Scope(requested string) (string, bool) {
	switch requested {
	case SpaceWork:
		return SpaceWork, a.Work
	case SpaceLife:
		return SpaceLife, a.Life
	}
	switch {
	case a.Work && a.Life:
		return "", true
	case a.Work:
		return SpaceWork, true
	case a.Life:
		return SpaceLife, true
	}
	return "", false
}

// GetAIAccess returns the user's setting; before they have set anything both spaces are allowed.
func (s *Store) GetAIAccess(ctx context.Context, userID string) (AIAccess, error) {
	p, err := s.GetPreference(ctx, userID, AIAccessKey)
	if errors.Is(err, ErrNotFound) {
		return AIAccess{Work: true, Life: true}, nil
	}
	if err != nil {
		return AIAccess{}, err
	}
	var a AIAccess
	if err := json.Unmarshal([]byte(p.Value), &a); err != nil {
		// A damaged value must fail closed, not open.
		return AIAccess{}, nil
	}
	return a, nil
}

// SetAIAccess records which spaces the assistant may read.
func (s *Store) SetAIAccess(ctx context.Context, userID string, work, life bool) (AIAccess, error) {
	old, err := s.GetAIAccess(ctx, userID)
	if err != nil {
		return AIAccess{}, err
	}
	a := AIAccess{Work: work, Life: life, Epoch: old.Epoch}
	if a != old {
		a.Epoch++
	}
	b, _ := json.Marshal(a)
	if _, err := s.setPreference(ctx, userID, AIAccessKey, string(b), nil); err != nil {
		return AIAccess{}, err
	}
	return a, nil
}

// ResetNoteIndexFor marks the user's notes in a space as needing (re-)indexing.
func (s *Store) ResetNoteIndexFor(ctx context.Context, userID, space string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notes SET ai_indexed_ms = NULL WHERE user_id = ? AND space = ?`, userID, space)
	return err
}

// NoteIDsInSpace lists the ids of the user's notes in a space.
func (s *Store) NoteIDsInSpace(ctx context.Context, userID, space string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM notes WHERE user_id = ? AND space = ?`, userID, space)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AIFeaturesKey is the reserved preference holding which assistant features are on.
const AIFeaturesKey = "ai.features"

// AIFeatures switches individual features on or off. All are on until the user says otherwise.
type AIFeatures struct {
	Briefing     bool `json:"briefing"`
	Suggestions  bool `json:"suggestions"`
	WeeklyReview bool `json:"weekly_review"`
	NoteTools    bool `json:"note_tools"` // summaries, suggested tags, to-dos found in a note
}

func allFeatures() AIFeatures {
	return AIFeatures{Briefing: true, Suggestions: true, WeeklyReview: true, NoteTools: true}
}

func (s *Store) GetAIFeatures(ctx context.Context, userID string) (AIFeatures, error) {
	p, err := s.GetPreference(ctx, userID, AIFeaturesKey)
	if errors.Is(err, ErrNotFound) {
		return allFeatures(), nil
	}
	if err != nil {
		return AIFeatures{}, err
	}
	f := allFeatures() // a field missing from an older value stays on
	if err := json.Unmarshal([]byte(p.Value), &f); err != nil {
		return allFeatures(), nil
	}
	return f, nil
}

func (s *Store) SetAIFeatures(ctx context.Context, userID string, f AIFeatures) (AIFeatures, error) {
	b, _ := json.Marshal(f)
	if _, err := s.setPreference(ctx, userID, AIFeaturesKey, string(b), nil); err != nil {
		return AIFeatures{}, err
	}
	return f, nil
}
