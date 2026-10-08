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

// CreateToken mints a bearer token for the user. The plaintext is returned
// once; only its SHA-256 is stored.
func (s *Store) CreateToken(ctx context.Context, userID, label string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := "noted_" + hex.EncodeToString(raw)
	_, err := s.db.ExecContext(ctx, `INSERT INTO tokens(hash, user_id, label, created_ms) VALUES (?, ?, ?, ?)`,
		hashToken(tok), userID, label, ms(time.Now()))
	if err != nil {
		return "", err
	}
	return tok, nil
}

// UserForToken resolves a bearer token to its user.
func (s *Store) UserForToken(ctx context.Context, tok string) (User, error) {
	var u User
	var created int64
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.name, u.created_ms FROM tokens t JOIN users u ON u.id = t.user_id WHERE t.hash = ?`,
		hashToken(tok)).Scan(&u.ID, &u.Name, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.Created = fromMS(created)
	return u, err
}
