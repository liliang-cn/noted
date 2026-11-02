package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// A Compass sits above objectives and goals: one per user per space. Every
// save appends a revision, so it can change over time without losing history.
type Compass struct {
	UserID      string
	Space       string
	AntiVision  string   // the life I refuse
	Vision      string   // what I am building toward
	Identity    string   // "我是那种……的人"
	Constraints []string // what I will not sacrifice
	Version     int      // 0: never saved
	Updated     time.Time
}

// CompassPatch changes some fields of the current compass; nil keeps a field.
type CompassPatch struct {
	AntiVision, Vision, Identity *string
	Constraints                  *[]string
}

const (
	maxCompassText        = 500
	maxCompassConstraints = 20
)

func (s *Store) migrateCompass(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS compass_revisions (
	user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	space       TEXT NOT NULL,
	version     INTEGER NOT NULL,
	anti_vision TEXT NOT NULL DEFAULT '',
	vision      TEXT NOT NULL DEFAULT '',
	identity    TEXT NOT NULL DEFAULT '',
	constraints TEXT NOT NULL DEFAULT '[]',
	created_ms  INTEGER NOT NULL,
	PRIMARY KEY (user_id, space, version)
)`)
	return err
}

func compassSpace(space string) (string, error) {
	switch space {
	case "":
		return SpaceLife, nil
	case SpaceLife, SpaceWork:
		return space, nil
	}
	return "", invalid("space must be work or life")
}

func (c *Compass) normalize() error {
	for _, f := range []struct {
		name string
		v    *string
	}{{"anti_vision", &c.AntiVision}, {"vision", &c.Vision}, {"identity", &c.Identity}} {
		*f.v = strings.TrimSpace(*f.v)
		if utf8.RuneCountInString(*f.v) > maxCompassText {
			return invalid("%s is longer than %d characters", f.name, maxCompassText)
		}
	}
	var cs []string
	for _, x := range c.Constraints {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		if utf8.RuneCountInString(x) > maxCompassText {
			return invalid("a constraint is longer than %d characters", maxCompassText)
		}
		cs = append(cs, x)
	}
	if len(cs) > maxCompassConstraints {
		return invalid("at most %d constraints", maxCompassConstraints)
	}
	c.Constraints = cs
	return nil
}

func (c Compass) sameText(o Compass) bool {
	if c.AntiVision != o.AntiVision || c.Vision != o.Vision || c.Identity != o.Identity || len(c.Constraints) != len(o.Constraints) {
		return false
	}
	for i := range c.Constraints {
		if c.Constraints[i] != o.Constraints[i] {
			return false
		}
	}
	return true
}

const compassCols = `space, version, anti_vision, vision, identity, constraints, created_ms`

func scanCompass(userID string, r scanner) (Compass, error) {
	c := Compass{UserID: userID}
	var cons string
	var created int64
	if err := r.Scan(&c.Space, &c.Version, &c.AntiVision, &c.Vision, &c.Identity, &cons, &created); err != nil {
		return Compass{}, err
	}
	_ = json.Unmarshal([]byte(cons), &c.Constraints)
	c.Updated = fromMS(created)
	return c, nil
}

// GetCompass returns the current compass of a space ("" is life); an empty one
// with Version 0 when none was saved.
func (s *Store) GetCompass(ctx context.Context, userID, space string) (Compass, error) {
	space, err := compassSpace(space)
	if err != nil {
		return Compass{}, err
	}
	c, err := scanCompass(userID, s.db.QueryRowContext(ctx, `SELECT `+compassCols+` FROM compass_revisions
		WHERE user_id = ? AND space = ? ORDER BY version DESC LIMIT 1`, userID, space))
	if errors.Is(err, sql.ErrNoRows) {
		return Compass{UserID: userID, Space: space}, nil
	}
	return c, err
}

// SetCompass applies p to the current compass and appends the result as a new
// revision. When nothing changes the current compass is returned as it is.
func (s *Store) SetCompass(ctx context.Context, userID, space string, p CompassPatch, now time.Time) (Compass, error) {
	space, err := compassSpace(space)
	if err != nil {
		return Compass{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Compass{}, err
	}
	defer func() { _ = tx.Rollback() }()
	cur, err := scanCompass(userID, tx.QueryRowContext(ctx, `SELECT `+compassCols+` FROM compass_revisions
		WHERE user_id = ? AND space = ? ORDER BY version DESC LIMIT 1`, userID, space))
	if errors.Is(err, sql.ErrNoRows) {
		cur, err = Compass{UserID: userID, Space: space}, nil
	}
	if err != nil {
		return Compass{}, err
	}
	next := cur
	if p.AntiVision != nil {
		next.AntiVision = *p.AntiVision
	}
	if p.Vision != nil {
		next.Vision = *p.Vision
	}
	if p.Identity != nil {
		next.Identity = *p.Identity
	}
	if p.Constraints != nil {
		next.Constraints = append([]string(nil), *p.Constraints...)
	}
	if err := next.normalize(); err != nil {
		return Compass{}, err
	}
	if cur.Version > 0 && next.sameText(cur) {
		return cur, nil
	}
	if cur.Version == 0 && next.sameText(cur) {
		return cur, invalid("the compass is empty")
	}
	next.Version, next.Updated = cur.Version+1, now.UTC()
	cons, _ := json.Marshal(append([]string{}, next.Constraints...))
	if _, err := tx.ExecContext(ctx, `INSERT INTO compass_revisions (user_id, space, version, anti_vision, vision, identity, constraints, created_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, userID, space, next.Version, next.AntiVision, next.Vision, next.Identity, string(cons), ms(now)); err != nil {
		return Compass{}, err
	}
	next.Updated = fromMS(ms(now))
	return next, tx.Commit()
}

// ListCompassRevisions returns the saved revisions of a space, newest first.
func (s *Store) ListCompassRevisions(ctx context.Context, userID, space string, limit int) ([]Compass, error) {
	space, err := compassSpace(space)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 500)
	rows, err := s.db.QueryContext(ctx, `SELECT `+compassCols+` FROM compass_revisions WHERE user_id = ? AND space = ?
		ORDER BY version DESC LIMIT ?`, userID, space, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Compass
	for rows.Next() {
		c, err := scanCompass(userID, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
