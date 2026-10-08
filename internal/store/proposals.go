package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Proposal statuses.
const (
	ProposalPending   = "pending"
	ProposalAccepted  = "accepted"
	ProposalDismissed = "dismissed"
)

// Proposal is a set of changes somebody (a rule, the assistant) suggests but
// has not made. Nothing in it touches the user's data until it is accepted.
// Ops and Inputs are JSON owned by the plan package; the store keeps them verbatim.
type Proposal struct {
	ID          string
	UserID      string
	Kind        string
	Status      string
	Title       string
	Reason      string
	Space       string
	Source      string // "rules" or "ai"
	Fingerprint string // identifies what the proposal is about, so it is not made twice
	Ops         json.RawMessage
	Inputs      json.RawMessage
	Created     time.Time
	Decided     *time.Time
}

// Change records what accepting a proposal did, with what is needed to undo it.
type Change struct {
	ID         string
	UserID     string
	ProposalID string
	Summary    string
	Entries    json.RawMessage
	Created    time.Time
	Undone     *time.Time
}

const proposalCols = `id, user_id, kind, status, title, reason, space, source, fingerprint, ops, inputs, created_ms, decided_ms`

func scanProposal(r scanner) (Proposal, error) {
	var p Proposal
	var ops, inputs string
	var created int64
	var decided sql.NullInt64
	if err := r.Scan(&p.ID, &p.UserID, &p.Kind, &p.Status, &p.Title, &p.Reason, &p.Space, &p.Source, &p.Fingerprint,
		&ops, &inputs, &created, &decided); err != nil {
		return Proposal{}, err
	}
	p.Ops, p.Inputs = json.RawMessage(ops), json.RawMessage(inputs)
	p.Created, p.Decided = fromMS(created), ptrTime(decided)
	return p, nil
}

// SaveProposal stores a new pending proposal. If the user already has one with
// the same fingerprint (pending, accepted or dismissed) nothing is stored and
// created is false: a dismissed suggestion stays dismissed.
func (s *Store) SaveProposal(ctx context.Context, p Proposal) (saved Proposal, created bool, err error) {
	if p.Fingerprint != "" {
		old, err := scanProposal(s.db.QueryRowContext(ctx, `SELECT `+proposalCols+` FROM proposals WHERE user_id = ? AND fingerprint = ?`, p.UserID, p.Fingerprint))
		if err == nil {
			return old, false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Proposal{}, false, err
		}
	}
	if p.Space == "" {
		p.Space = SpaceLife
	}
	if len(p.Ops) == 0 {
		p.Ops = json.RawMessage("[]")
	}
	if len(p.Inputs) == 0 {
		p.Inputs = json.RawMessage("[]")
	}
	p.ID, p.Status, p.Created = uuid.NewString(), ProposalPending, time.Now().UTC().Truncate(time.Millisecond)
	_, err = s.db.ExecContext(ctx, `INSERT INTO proposals(`+proposalCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		p.ID, p.UserID, p.Kind, p.Status, p.Title, p.Reason, p.Space, p.Source, p.Fingerprint, string(p.Ops), string(p.Inputs), ms(p.Created))
	return p, err == nil, err
}

func (s *Store) GetProposal(ctx context.Context, userID, id string) (Proposal, error) {
	p, err := scanProposal(s.db.QueryRowContext(ctx, `SELECT `+proposalCols+` FROM proposals WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, ErrNotFound
	}
	return p, err
}

// ListProposals returns the user's proposals with the given status (empty: pending), newest first.
func (s *Store) ListProposals(ctx context.Context, userID, status, space string) ([]Proposal, error) {
	if status == "" {
		status = ProposalPending
	}
	if err := checkSpaceFilter(space); err != nil {
		return nil, err
	}
	q := `SELECT ` + proposalCols + ` FROM proposals WHERE user_id = ? AND status = ?`
	args := []any{userID, status}
	q, args = spaceClause(q, args, "space", space)
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_ms DESC, id LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DecideProposal moves a pending proposal to accepted or dismissed. It reports
// ErrConflict if the proposal was already decided, so two devices cannot both
// accept it.
func (s *Store) DecideProposal(ctx context.Context, userID, id, status string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE proposals SET status = ?, decided_ms = ? WHERE id = ? AND user_id = ? AND status = ?`,
		status, ms(time.Now()), id, userID, ProposalPending)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetProposal(ctx, userID, id); err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

// ReopenProposal puts an accepted proposal back to pending (used when applying it failed).
func (s *Store) ReopenProposal(ctx context.Context, userID, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE proposals SET status = ?, decided_ms = NULL WHERE id = ? AND user_id = ?`, ProposalPending, id, userID)
	return err
}

// ExpirePending deletes pending proposals of the given source whose fingerprint
// is not in keep: the situation that prompted them has passed.
func (s *Store) ExpirePending(ctx context.Context, userID, source string, keep []string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, fingerprint FROM proposals WHERE user_id = ? AND source = ? AND status = ?`, userID, source, ProposalPending)
	if err != nil {
		return err
	}
	keepSet := map[string]bool{}
	for _, k := range keep {
		keepSet[k] = true
	}
	var drop []string
	for rows.Next() {
		var id, fp string
		if err := rows.Scan(&id, &fp); err != nil {
			_ = rows.Close()
			return err
		}
		if !keepSet[fp] {
			drop = append(drop, id)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range drop {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM proposals WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveChange(ctx context.Context, c Change) (Change, error) {
	c.ID, c.Created = uuid.NewString(), time.Now().UTC().Truncate(time.Millisecond)
	_, err := s.db.ExecContext(ctx, `INSERT INTO changes(id, user_id, proposal_id, summary, entries, created_ms, undone_ms) VALUES (?, ?, ?, ?, ?, ?, NULL)`,
		c.ID, c.UserID, c.ProposalID, c.Summary, string(c.Entries), ms(c.Created))
	return c, err
}

func scanChange(r scanner) (Change, error) {
	var c Change
	var entries string
	var created int64
	var undone sql.NullInt64
	if err := r.Scan(&c.ID, &c.UserID, &c.ProposalID, &c.Summary, &entries, &created, &undone); err != nil {
		return Change{}, err
	}
	c.Entries, c.Created, c.Undone = json.RawMessage(entries), fromMS(created), ptrTime(undone)
	return c, nil
}

func (s *Store) GetChange(ctx context.Context, userID, id string) (Change, error) {
	c, err := scanChange(s.db.QueryRowContext(ctx, `SELECT id, user_id, proposal_id, summary, entries, created_ms, undone_ms FROM changes WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Change{}, ErrNotFound
	}
	return c, err
}

func (s *Store) ListChanges(ctx context.Context, userID string, limit int) ([]Change, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, proposal_id, summary, entries, created_ms, undone_ms FROM changes
		WHERE user_id = ? ORDER BY created_ms DESC, id LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Change
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkChangeUndone flags a change as undone. ErrConflict if it already was.
func (s *Store) MarkChangeUndone(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE changes SET undone_ms = ? WHERE id = ? AND user_id = ? AND undone_ms IS NULL`, ms(time.Now()), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetChange(ctx, userID, id); err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

// Validate checks a task without storing it (the same rules as CreateTask).
func (t Task) Validate() error { return t.normalize() }

// Validate checks a project without storing it.
func (p Project) Validate() error { return p.normalize() }

// Validate checks an event without storing it.
func (e Event) Validate() error { return e.Normalize() }

// Validate checks a note without storing it (the same rules as CreateNote).
func (n Note) Validate() error {
	tags, err := NormalizeTags(n.Tags)
	if err != nil {
		return err
	}
	n.Tags = tags
	return checkNote(n)
}
