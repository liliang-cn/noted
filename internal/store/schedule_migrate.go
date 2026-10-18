package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/liliang-cn/noted/internal/alarm"
)

// migrationAlarms marks the one-time copy of the alarms table into events.
const migrationAlarms = "alarms_to_events"

// ScheduleStatus is what the schedule migration has done so far.
type ScheduleStatus struct {
	AlarmsMigrated bool // the alarms table has been copied into events
	LegacyAlarms   int  // rows still in the alarms table (kept for a rollback)
	Alarms         int  // schedule items that came from alarms
	TaskSchedules  int  // schedule items that are a to-do's time
	Events         int  // plain calendar events
}

// migrateSchedule folds alarms and to-dos' times into the events table:
// once, every row of the alarms table becomes a schedule item with the same
// id (the table itself stays, untouched, so an older binary still runs on
// its data); every start, the schedule of each to-do with a time is written
// or brought up to date, and items of to-dos that lost their time or were
// deleted are removed. Both are idempotent.
func (s *Store) migrateSchedule(ctx context.Context) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := migrateAlarms(ctx, tx); err != nil {
			return err
		}
		return syncAllTaskSchedules(ctx, tx, s.location().String())
	})
}

func applied(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM migrations WHERE name = ?`, name).Scan(&n)
	return n > 0, err
}

func migrateAlarms(ctx context.Context, tx *sql.Tx) error {
	done, err := applied(ctx, tx, migrationAlarms)
	if err != nil || done {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, user_id, label, mode, enabled, schedule, holiday_policy, region, note, project_id, space, original_text,
		created_ms, updated_ms FROM alarms ORDER BY created_ms, rowid`)
	if err != nil {
		return err
	}
	type row struct {
		id, user, label, mode, schedule, policy, region, note, project, space, text string
		enabled                                                                     int
		created, updated                                                            int64
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.user, &r.label, &r.mode, &r.enabled, &r.schedule, &r.policy, &r.region, &r.note, &r.project, &r.space, &r.text,
			&r.created, &r.updated); err != nil {
			_ = rows.Close()
			return err
		}
		all = append(all, r)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range all {
		sc, err := alarm.UnmarshalSchedule(r.schedule)
		if err != nil {
			return err
		}
		first := alarm.FirstRing(sc)
		notify := r.mode
		if notify == "" {
			notify = NotifyAlarm
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO events(id, user_id, title, description, location, start_ms, end_ms, all_day, tz, rrule,
				remind_before, project_id, space, created_ms, updated_ms, notify, rule, holiday_policy, region, enabled, original_text, source, task_id, completed)
			VALUES (?, ?, ?, ?, '', ?, ?, 0, ?, '', 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'alarm', '', 0)`,
			r.id, r.user, r.label, r.note, ms(first), ms(first), sc.TimeZone, r.project, r.space, r.created, r.updated,
			notify, r.schedule, r.policy, r.region, r.enabled, r.text); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO migrations(name, applied_ms) VALUES (?, ?)`, migrationAlarms, ms(time.Now()))
	return err
}

// syncAllTaskSchedules writes the schedule item of every to-do with a time
// and drops the items whose to-do has none (or is gone). "Already told" is
// carried over from the tasks table the first time an item is made.
func syncAllTaskSchedules(ctx context.Context, tx *sql.Tx, tz string) error {
	if _, err := tx.ExecContext(ctx, taskScheduleUpsert("", true), tz); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM events WHERE source = 'task'
		AND NOT EXISTS (SELECT 1 FROM tasks WHERE id = events.task_id AND (due_ms IS NOT NULL OR remind_ms IS NOT NULL))`)
	return err
}

// ScheduleStatus reports where the schedule migration stands.
func (s *Store) ScheduleStatus(ctx context.Context) (ScheduleStatus, error) {
	var st ScheduleStatus
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		if st.AlarmsMigrated, err = applied(ctx, tx, migrationAlarms); err != nil {
			return err
		}
		for col, dst := range map[string]*int{"alarm": &st.Alarms, "task": &st.TaskSchedules, "event": &st.Events} {
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE source = ?`, col).Scan(dst); err != nil {
				return err
			}
		}
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM alarms`).Scan(&st.LegacyAlarms)
	})
	return st, err
}

// MigrateSchedule runs the schedule migration again by hand (it also runs at
// every start). It re-copies the alarms table (rows already present are left
// as they are) and re-syncs the to-dos' schedules.
func (s *Store) MigrateSchedule(ctx context.Context) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM migrations WHERE name = ?`, migrationAlarms); err != nil {
			return err
		}
		if err := migrateAlarms(ctx, tx); err != nil {
			return err
		}
		return syncAllTaskSchedules(ctx, tx, s.location().String())
	})
}

// RollbackSchedule undoes the migration so an older binary finds its data:
// every alarm (schedule items from the alarm API, changes since the migration
// included) is written back to the alarms table, the alarm and to-do items
// leave the events table, and the migration is unmarked so a newer binary
// folds them in again when it next starts. Plain events keep their notify
// and other new columns, which an older binary ignores.
func (s *Store) RollbackSchedule(ctx context.Context) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+eventCols+` FROM events WHERE source = 'alarm'`)
		if err != nil {
			return err
		}
		var alarms []Event
		for rows.Next() {
			e, err := scanEvent(rows)
			if err != nil {
				_ = rows.Close()
				return err
			}
			alarms = append(alarms, e)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, e := range alarms {
			sched, err := alarm.MarshalSchedule(e.Schedule())
			if err != nil {
				return err
			}
			mode := e.Notify
			if mode != alarm.ModeAlarm {
				mode = alarm.ModeReminder // the old table knows alarm and reminder only
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO alarms(id, user_id, label, mode, enabled, schedule, holiday_policy, region, note, project_id, space,
					original_text, created_ms, updated_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET label = excluded.label, mode = excluded.mode, enabled = excluded.enabled, schedule = excluded.schedule,
					holiday_policy = excluded.holiday_policy, region = excluded.region, note = excluded.note, project_id = excluded.project_id,
					space = excluded.space, original_text = excluded.original_text, updated_ms = excluded.updated_ms`,
				e.ID, e.UserID, e.Title, mode, b2i(!e.Disabled), sched, e.HolidayPolicy, e.Region, e.Description, e.ProjectID, e.Space,
				e.OriginalText, ms(e.Created), ms(e.Updated)); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE source IN ('alarm', 'task')`); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM migrations WHERE name = ?`, migrationAlarms)
		return err
	})
}

