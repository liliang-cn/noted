package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// The ledger of the one-way sync into a CortexDB brain. Its target names the
// brain (endpoint and collection); items sent to one brain say nothing about
// another, so a run for a new target starts from an empty ledger.

// CortexLedger returns what has been sent for the user to target: item key ->
// hash of the item as sent. It is empty when the user's last run went
// elsewhere, or there was none.
func (s *Store) CortexLedger(ctx context.Context, userID, target string) (map[string]string, error) {
	var cur string
	err := s.db.QueryRowContext(ctx, `SELECT target FROM cortex_sync_state WHERE user_id = ?`, userID).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && cur != target) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT item_key, hash FROM cortex_sync_items WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var k, h string
		if err := rows.Scan(&k, &h); err != nil {
			return nil, err
		}
		out[k] = h
	}
	return out, rows.Err()
}

// CortexBegin records that a run for the user sends to target, dropping the
// ledger of a different target.
func (s *Store) CortexBegin(ctx context.Context, userID, target string, now time.Time) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var cur string
		err := tx.QueryRowContext(ctx, `SELECT target FROM cortex_sync_state WHERE user_id = ?`, userID).Scan(&cur)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if cur != target {
			if _, err := tx.ExecContext(ctx, `DELETE FROM cortex_sync_items WHERE user_id = ?`, userID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO cortex_sync_state(user_id, target, run_ms) VALUES (?, ?, ?)
			ON CONFLICT(user_id) DO UPDATE SET target = excluded.target, run_ms = excluded.run_ms`, userID, target, ms(now))
		return err
	})
}

// CortexMark records that an item was sent with this hash.
func (s *Store) CortexMark(ctx context.Context, userID, key, hash string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO cortex_sync_items(user_id, item_key, hash, synced_ms) VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, item_key) DO UPDATE SET hash = excluded.hash, synced_ms = excluded.synced_ms`, userID, key, hash, ms(at))
	return err
}

// CortexForget removes an item from the ledger once its delete was sent.
func (s *Store) CortexForget(ctx context.Context, userID, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cortex_sync_items WHERE user_id = ? AND item_key = ?`, userID, key)
	return err
}
