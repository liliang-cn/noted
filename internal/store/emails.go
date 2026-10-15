package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrEmailTaken is returned when another user already signs in with the email.
var ErrEmailTaken = errors.New("email already registered")

// EmailLogin is what signing in by email needs: the user and the stored hash.
type EmailLogin struct {
	UserID       string
	PasswordHash string
}

// EmailLogin looks up an email (already normalized by the caller).
func (s *Store) EmailLogin(ctx context.Context, email string) (EmailLogin, error) {
	var l EmailLogin
	err := s.db.QueryRowContext(ctx, `SELECT user_id, password_hash FROM user_emails WHERE email = ?`, email).
		Scan(&l.UserID, &l.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return EmailLogin{}, ErrNotFound
	}
	return l, err
}

// UserEmail returns the user's sign-in email and password hash; ErrNotFound
// when none is set.
func (s *Store) UserEmail(ctx context.Context, userID string) (email, hash string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT email, password_hash FROM user_emails WHERE user_id = ?`, userID).Scan(&email, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return email, hash, err
}

// User returns a user by id.
func (s *Store) User(ctx context.Context, id string) (User, error) {
	var u User
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, created_ms FROM users WHERE id = ?`, id).Scan(&u.ID, &u.Name, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.Created = fromMS(created)
	return u, err
}

// SetUserEmail sets or replaces the user's sign-in email and password hash.
func (s *Store) SetUserEmail(ctx context.Context, userID, email, hash string) error {
	now := ms(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var owner string
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM user_emails WHERE email = ?`, email).Scan(&owner)
	switch {
	case err == nil && owner != userID:
		return ErrEmailTaken
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_emails(email, user_id, password_hash, created_ms, updated_ms) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET email = excluded.email, password_hash = excluded.password_hash, updated_ms = excluded.updated_ms`,
		email, userID, hash, now, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// CreateEmailUser makes a new user who signs in with email + password. The
// user is named after the email's local part, with a suffix when that name is
// taken.
func (s *Store) CreateEmailUser(ctx context.Context, email, hash string) (User, error) {
	base, _, _ := strings.Cut(email, "@")
	if base == "" {
		base = "user"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_emails WHERE email = ?`, email).Scan(&n); err != nil {
		return User{}, err
	}
	if n > 0 {
		return User{}, ErrEmailTaken
	}
	name := base
	for i := 0; ; i++ {
		var taken int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE name = ?`, name).Scan(&taken); err != nil {
			return User{}, err
		}
		if taken == 0 {
			break
		}
		if i >= 20 {
			return User{}, errors.New("cannot find a free user name")
		}
		b := make([]byte, 2)
		_, _ = rand.Read(b)
		name = base + "-" + hex.EncodeToString(b)
	}
	now := time.Now()
	u := User{ID: uuid.NewString(), Name: name, Created: fromMS(ms(now))}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id, name, created_ms) VALUES (?, ?, ?)`, u.ID, u.Name, ms(now)); err != nil {
		return User{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_emails(email, user_id, password_hash, created_ms, updated_ms) VALUES (?, ?, ?, ?, ?)`,
		email, u.ID, hash, ms(now), ms(now)); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	return u, tx.Commit()
}
