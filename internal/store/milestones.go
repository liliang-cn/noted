package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// A Milestone is a step on the way to a goal or an objective. A list of them
// keeps its order: the first is the first step.
type Milestone struct {
	ID       string
	Title    string
	Done     bool
	DoneAt   *time.Time
	Due      *time.Time // optional: when it should be done by
	Criteria string     // optional: what counts as done
}

// Overdue reports whether the milestone is undone and past its due time.
func (m Milestone) Overdue(now time.Time) bool {
	return !m.Done && m.Due != nil && now.After(*m.Due)
}

const (
	maxMilestones = 50
	maxCriteria   = 1000
)

func normalizeMilestones(ms []Milestone) error {
	if len(ms) > maxMilestones {
		return invalid("at most %d milestones", maxMilestones)
	}
	for i := range ms {
		m := &ms[i]
		m.Title = strings.TrimSpace(m.Title)
		if m.Title == "" || utf8.RuneCountInString(m.Title) > 200 {
			return invalid("a milestone title must be 1-200 characters")
		}
		m.Criteria = strings.TrimSpace(m.Criteria)
		if utf8.RuneCountInString(m.Criteria) > maxCriteria {
			return invalid("a milestone's criteria is at most %d characters", maxCriteria)
		}
	}
	return nil
}

// newMilestones gives every entry a fresh id and clears its done state, for a
// list made with a new goal or objective.
func newMilestones(ms []Milestone) {
	for i := range ms {
		ms[i].ID, ms[i].Done, ms[i].DoneAt = uuid.NewString(), false, nil
	}
}

// mergeMilestones is the replacement list when a client sends a new one:
// entries that carry an existing id keep that id and their done state and take
// the new title, due time and criteria; the rest are new.
func mergeMilestones(old, in []Milestone) []Milestone {
	existing := make(map[string]Milestone, len(old))
	for _, m := range old {
		existing[m.ID] = m
	}
	next := make([]Milestone, 0, len(in))
	for _, m := range in {
		if prev, ok := existing[m.ID]; ok && m.ID != "" {
			prev.Title, prev.Due, prev.Criteria = m.Title, m.Due, m.Criteria
			next = append(next, prev)
			delete(existing, m.ID) // an id given twice is new the second time
		} else {
			next = append(next, Milestone{ID: uuid.NewString(), Title: m.Title, Due: m.Due, Criteria: m.Criteria})
		}
	}
	return next
}

// milestoneTable is where one kind of owner keeps its milestones; position is
// the place in the list.
type milestoneTable struct{ table, owner string }

var (
	goalMilestones      = milestoneTable{"goal_milestones", "goal_id"}
	objectiveMilestones = milestoneTable{"objective_milestones", "objective_id"}
)

// write replaces the owner's milestones with ms.
func (t milestoneTable) write(ctx context.Context, tx *sql.Tx, ownerID string, ms []Milestone) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+t.table+` WHERE `+t.owner+` = ?`, ownerID); err != nil {
		return err
	}
	for i, m := range ms {
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+t.table+`(id, `+t.owner+`, position, title, done_ms, due_ms, criteria) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			m.ID, ownerID, i, m.Title, nullMS(m.DoneAt), nullMS(m.Due), m.Criteria); err != nil {
			return err
		}
	}
	return nil
}

// load returns the milestones of each owner, in order.
func (t milestoneTable) load(ctx context.Context, db *sql.DB, ownerIDs []string) (map[string][]Milestone, error) {
	out := map[string][]Milestone{}
	if len(ownerIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(ownerIDs))
	for i, id := range ownerIDs {
		args[i] = id
	}
	rows, err := db.QueryContext(ctx, `SELECT `+t.owner+`, id, title, done_ms, due_ms, criteria FROM `+t.table+`
		WHERE `+t.owner+` IN (?`+strings.Repeat(",?", len(ownerIDs)-1)+`) ORDER BY `+t.owner+`, position`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var owner string
		var m Milestone
		var done, due sql.NullInt64
		if err := rows.Scan(&owner, &m.ID, &m.Title, &done, &due, &m.Criteria); err != nil {
			return nil, err
		}
		m.DoneAt, m.Done, m.Due = ptrTime(done), done.Valid, ptrTime(due)
		out[owner] = append(out[owner], m)
	}
	return out, rows.Err()
}

// setDone ticks or unticks one milestone of the owner. Ticking one that is
// already done keeps its done time.
func (t milestoneTable) setDone(ctx context.Context, db *sql.DB, ownerID, milestoneID string, done bool, now time.Time) error {
	q := `UPDATE ` + t.table + ` SET done_ms = NULL WHERE id = ? AND ` + t.owner + ` = ?`
	args := []any{milestoneID, ownerID}
	if done {
		q = `UPDATE ` + t.table + ` SET done_ms = coalesce(done_ms, ?) WHERE id = ? AND ` + t.owner + ` = ?`
		args = append([]any{ms(now)}, args...)
	}
	res, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
