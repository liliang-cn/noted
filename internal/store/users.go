package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID      string
	Name    string
	Created time.Time
}

// EnsureUser returns the user with that name, creating it when missing.
func (s *Store) EnsureUser(ctx context.Context, name string) (User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return User{}, invalid("user name is empty")
	}
	var u User
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, created_ms FROM users WHERE name = ?`, name).Scan(&u.ID, &u.Name, &created)
	if err == nil {
		u.Created = fromMS(created)
		return u, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}
	u = User{ID: uuid.NewString(), Name: name, Created: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO users(id, name, created_ms) VALUES (?, ?, ?)`, u.ID, u.Name, ms(u.Created))
	return u, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_ms FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []User
	for rows.Next() {
		var u User
		var created int64
		if err := rows.Scan(&u.ID, &u.Name, &created); err != nil {
			return nil, err
		}
		u.Created = fromMS(created)
		out = append(out, u)
	}
	return out, rows.Err()
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// Token describes a bearer token without its secret.
type Token struct {
	ID       string // stable public id: the first 12 hex chars of the hash
	Label    string
	Created  time.Time
	LastUsed time.Time // zero: never used
}

const tokenIDLen = 12

// TokenID is the public id of a plaintext token.
func TokenID(tok string) string { return hashToken(tok)[:tokenIDLen] }

// lastUsedEvery throttles last-used writes: a token in steady use is written at
// most once per interval.
const lastUsedEvery = time.Minute

// CreateToken mints a bearer token for the user. The plaintext is returned
// once; only its SHA-256 is stored.
func (s *Store) CreateToken(ctx context.Context, userID, label string) (string, error) {
	tok, _, err := s.NewToken(ctx, userID, label)
	return tok, err
}

// NewToken is CreateToken that also describes the new token.
func (s *Store) NewToken(ctx context.Context, userID, label string) (string, Token, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", Token{}, err
	}
	tok := "noted_" + hex.EncodeToString(raw)
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO tokens(hash, user_id, label, created_ms) VALUES (?, ?, ?, ?)`,
		hashToken(tok), userID, label, ms(now))
	if err != nil {
		return "", Token{}, err
	}
	return tok, Token{ID: TokenID(tok), Label: label, Created: fromMS(ms(now))}, nil
}

// UserForToken resolves a bearer token to its user.
func (s *Store) UserForToken(ctx context.Context, tok string) (User, error) {
	u, _, err := s.Authenticate(ctx, tok)
	return u, err
}

// Authenticate resolves a bearer token to its user and the token's id, and
// records when it was last used (at most once a minute per token).
func (s *Store) Authenticate(ctx context.Context, tok string) (User, string, error) {
	var u User
	var created int64
	var last sql.NullInt64
	h := hashToken(tok)
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.name, u.created_ms, t.last_used_ms FROM tokens t JOIN users u ON u.id = t.user_id WHERE t.hash = ?`,
		h).Scan(&u.ID, &u.Name, &created, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", err
	}
	u.Created = fromMS(created)
	now := time.Now()
	if !last.Valid || now.Sub(fromMS(last.Int64)) >= lastUsedEvery {
		// Best effort: a failed bookkeeping write must not fail the call.
		_, _ = s.db.ExecContext(ctx, `UPDATE tokens SET last_used_ms = ? WHERE hash = ?`, ms(now), h)
	}
	return u, h[:tokenIDLen], nil
}

// ListTokens returns the user's tokens, newest first.
func (s *Store) ListTokens(ctx context.Context, userID string) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT hash, label, created_ms, last_used_ms FROM tokens WHERE user_id = ? ORDER BY created_ms DESC, hash`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Token
	for rows.Next() {
		var t Token
		var hash string
		var created int64
		var last sql.NullInt64
		if err := rows.Scan(&hash, &t.Label, &created, &last); err != nil {
			return nil, err
		}
		t.ID = hash[:tokenIDLen]
		t.Created = fromMS(created)
		if last.Valid {
			t.LastUsed = fromMS(last.Int64)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeToken deletes one of the user's tokens by id. Another user's token id
// is reported as not found.
func (s *Store) RevokeToken(ctx context.Context, userID, id string) error {
	id = strings.ToLower(strings.TrimSpace(id))
	if len(id) != tokenIDLen {
		return ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM tokens WHERE user_id = ? AND substr(hash, 1, ?) = ?`, userID, tokenIDLen, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UserName returns the name of a user id.
func (s *Store) UserName(ctx context.Context, id string) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return name, err
}

// SetUserPro grants or withdraws Noted Pro for a user by name, for people who got it
// outside the App Store (the owner's own accounts, gifts).
func (s *Store) SetUserPro(ctx context.Context, name string, on bool) error {
	v := 0
	if on {
		v = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET pro = ? WHERE name = ?`, v, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UserPro(ctx context.Context, userID string) (bool, error) {
	var v int
	err := s.db.QueryRowContext(ctx, `SELECT pro FROM users WHERE id = ?`, userID).Scan(&v)
	return v != 0, err
}
