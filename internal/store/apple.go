package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// UserForApple returns the user an Apple ID (the identity token's sub) is linked to.
func (s *Store) UserForApple(ctx context.Context, sub string) (User, error) {
	var u User
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.name, u.created_ms FROM apple_ids a JOIN users u ON u.id = a.user_id WHERE a.sub = ?`, sub).
		Scan(&u.ID, &u.Name, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.Created = fromMS(created)
	return u, nil
}

// LinkApple ties an Apple ID to a user. A user has at most one, and an Apple ID
// belongs to at most one user; relinking the same pair is a no-op.
func (s *Store) LinkApple(ctx context.Context, userID, sub string) error {
	if sub == "" {
		return invalid("apple id is empty")
	}
	if owner, err := s.UserForApple(ctx, sub); err == nil {
		if owner.ID == userID {
			return nil
		}
		return invalid("this Apple ID is already linked to another user")
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	var have int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM apple_ids WHERE user_id = ?`, userID).Scan(&have); err != nil {
		return err
	}
	if have > 0 {
		return invalid("this user already has an Apple ID linked; unlink it first")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO apple_ids(sub, user_id, created_ms) VALUES (?, ?, ?)`, sub, userID, ms(time.Now()))
	return err
}

func (s *Store) UnlinkApple(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM apple_ids WHERE user_id = ?`, userID)
	return err
}

func (s *Store) AppleLinked(ctx context.Context, userID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM apple_ids WHERE user_id = ?`, userID).Scan(&n)
	return n > 0, err
}
