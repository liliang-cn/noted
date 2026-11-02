package store

import (
	"context"
	"time"
)

// CompletedTasks returns the tasks finished in [from, to), oldest first.
func (s *Store) CompletedTasks(ctx context.Context, userID string, from, to time.Time, space string) ([]Task, error) {
	if err := checkSpaceFilter(space); err != nil {
		return nil, err
	}
	q := `SELECT ` + taskCols + ` FROM tasks t WHERE user_id = ? AND completed = 1 AND complete_ms >= ? AND complete_ms < ?`
	args := []any{userID, ms(from), ms(to)}
	q, args = spaceClause(q, args, "space", space)
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY complete_ms, id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
