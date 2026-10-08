package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	maxPrefBytes = 64 << 10
	maxPrefKeys  = 100
)

var prefKeyRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type Preference struct {
	Key     string
	Value   string
	Version int64
	Updated time.Time
}

func checkPrefKey(key string) error {
	if !prefKeyRE.MatchString(key) {
		return invalid("key must be 1-64 characters of a-z, 0-9, '.', '_' or '-', starting with a letter or digit")
	}
	return nil
}

func (s *Store) GetPreference(ctx context.Context, userID, key string) (Preference, error) {
	var p Preference
	var updated int64
	err := s.db.QueryRowContext(ctx, `SELECT key, value, version, updated_ms FROM preferences WHERE user_id = ? AND key = ?`,
		userID, key).Scan(&p.Key, &p.Value, &p.Version, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Preference{}, ErrNotFound
	}
	p.Updated = fromMS(updated)
	return p, err
}

// SetPreference stores a JSON document. With ifVersion set the write is a
// compare-and-swap: 0 means the key must not exist, otherwise the stored
// version must match, else ErrConflict.
func (s *Store) SetPreference(ctx context.Context, userID, key, value string, ifVersion *int64) (Preference, error) {
	if key == AIAccessKey || key == AIFeaturesKey {
		return Preference{}, invalid("%q is managed through the AIService", key)
	}
	return s.setPreference(ctx, userID, key, value, ifVersion)
}

// setPreference is SetPreference without the reserved-key guard, for the store's own use.
func (s *Store) setPreference(ctx context.Context, userID, key, value string, ifVersion *int64) (Preference, error) {
	if err := checkPrefKey(key); err != nil {
		return Preference{}, err
	}
	if len(value) > maxPrefBytes {
		return Preference{}, invalid("value is larger than %d bytes", maxPrefBytes)
	}
	if !json.Valid([]byte(value)) {
		return Preference{}, invalid("value is not valid JSON")
	}
	var out Preference
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var cur int64
		err := tx.QueryRowContext(ctx, `SELECT version FROM preferences WHERE user_id = ? AND key = ?`, userID, key).Scan(&cur)
		exists := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if ifVersion != nil && *ifVersion != cur { // cur is 0 when the key does not exist
			return ErrConflict
		}
		if !exists {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM preferences WHERE user_id = ?`, userID).Scan(&n); err != nil {
				return err
			}
			if n >= maxPrefKeys {
				return invalid("at most %d preferences", maxPrefKeys)
			}
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		out = Preference{Key: key, Value: value, Version: cur + 1, Updated: now}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO preferences(user_id, key, value, version, updated_ms) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, version = excluded.version, updated_ms = excluded.updated_ms`,
			userID, key, value, out.Version, ms(now))
		return err
	})
	return out, err
}

func (s *Store) DeletePreference(ctx context.Context, userID, key string) error {
	if key == AIAccessKey || key == AIFeaturesKey {
		return invalid("%q is managed through the AIService", key)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM preferences WHERE user_id = ? AND key = ?`, userID, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListPreferences(ctx context.Context, userID, prefix string) ([]Preference, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value, version, updated_ms FROM preferences
		WHERE user_id = ? AND key LIKE ? ESCAPE '\' ORDER BY key`, userID, likeEscape(strings.ToLower(prefix))+"%")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Preference
	for rows.Next() {
		var p Preference
		var updated int64
		if err := rows.Scan(&p.Key, &p.Value, &p.Version, &updated); err != nil {
			return nil, err
		}
		p.Updated = fromMS(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}
