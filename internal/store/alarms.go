package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/liliang-cn/noted/internal/alarm"
)

// Alarm is an alarm definition (闹钟). noted keeps it; the phone rings it.
type Alarm struct {
	ID            string
	UserID        string
	Label         string
	Mode          string // alarm or reminder
	Enabled       bool
	Schedule      alarm.Schedule
	HolidayPolicy string
	Region        string // CN or ""
	Note          string
	ProjectID     string
	Space         string
	OriginalText  string
	Created       time.Time
	Updated       time.Time
}

// Rule is what decides when the alarm rings.
func (a Alarm) Rule() alarm.Rule {
	return alarm.Rule{Schedule: a.Schedule, HolidayPolicy: a.HolidayPolicy, Region: a.Region}
}

// NextFires is the alarm's next n rings after now; none when it is off.
func (a Alarm) NextFires(now time.Time, n int) []time.Time {
	if !a.Enabled {
		return []time.Time{}
	}
	return alarm.Next(a.Rule(), now, n)
}

type AlarmPatch struct {
	Label, Mode, HolidayPolicy, Region, Note, OriginalText *string
	Enabled                                                *bool
	Schedule                                               *alarm.Schedule // replaces the whole schedule
	ProjectID                                              *string
	Space                                                  *string
}

type AlarmFilter struct {
	Enabled   *bool
	Space     string
	ProjectID string
}

const (
	maxAlarms    = 1000
	maxAlarmText = 2000
)

// normalize checks an alarm and puts its rule in canonical form.
func (a *Alarm) normalize() error {
	a.Label = strings.TrimSpace(a.Label)
	if a.Label == "" {
		return invalid("label is empty: say what the alarm is for, e.g. 起床")
	}
	if utf8.RuneCountInString(a.Label) > maxTitle {
		return invalid("label is longer than %d characters", maxTitle)
	}
	switch a.Mode {
	case "":
		a.Mode = alarm.ModeAlarm
	case alarm.ModeAlarm, alarm.ModeReminder:
	default:
		return invalid("mode %q is unknown: use alarm (rings through silent mode) or reminder (a notification)", a.Mode)
	}
	a.Note = strings.TrimSpace(a.Note)
	a.OriginalText = strings.TrimSpace(a.OriginalText)
	if utf8.RuneCountInString(a.Note) > maxAlarmText || utf8.RuneCountInString(a.OriginalText) > maxAlarmText {
		return invalid("note and original_text are at most %d characters", maxAlarmText)
	}
	r := a.Rule()
	if err := alarm.Normalize(&r, "UTC"); err != nil {
		return invalid("%s", err.Error())
	}
	a.Schedule, a.HolidayPolicy, a.Region = r.Schedule, r.HolidayPolicy, r.Region
	return nil
}

const alarmCols = `id, user_id, label, mode, enabled, schedule, holiday_policy, region, note, project_id, space, original_text, created_ms, updated_ms`

func scanAlarm(r scanner) (Alarm, error) {
	var a Alarm
	var enabled int
	var sched string
	var created, updated int64
	if err := r.Scan(&a.ID, &a.UserID, &a.Label, &a.Mode, &enabled, &sched, &a.HolidayPolicy, &a.Region, &a.Note, &a.ProjectID,
		&a.Space, &a.OriginalText, &created, &updated); err != nil {
		return Alarm{}, err
	}
	s, err := alarm.UnmarshalSchedule(sched)
	if err != nil {
		return Alarm{}, err
	}
	a.Schedule, a.Enabled = s, enabled != 0
	a.Created, a.Updated = fromMS(created), fromMS(updated)
	return a, nil
}

// CreateAlarm saves a new alarm. Its zone defaults to UTC; callers fill in
// the server's.
func (s *Store) CreateAlarm(ctx context.Context, a Alarm) (Alarm, error) {
	if err := a.normalize(); err != nil {
		return Alarm{}, err
	}
	if err := s.checkProject(ctx, a.UserID, a.ProjectID); err != nil {
		return Alarm{}, err
	}
	sp, err := s.resolveSpace(ctx, a.UserID, a.Space, a.ProjectID)
	if err != nil {
		return Alarm{}, err
	}
	a.Space = sp
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alarms WHERE user_id = ?`, a.UserID).Scan(&n); err != nil {
		return Alarm{}, err
	}
	if n >= maxAlarms {
		return Alarm{}, invalid("at most %d alarms; delete some first", maxAlarms)
	}
	sched, err := alarm.MarshalSchedule(a.Schedule)
	if err != nil {
		return Alarm{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.ID, a.Created, a.Updated = uuid.NewString(), now, now
	_, err = s.db.ExecContext(ctx, `INSERT INTO alarms(`+alarmCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.UserID, a.Label, a.Mode, b2i(a.Enabled), sched, a.HolidayPolicy, a.Region, a.Note, a.ProjectID, a.Space, a.OriginalText,
		ms(a.Created), ms(a.Updated))
	if err != nil {
		return Alarm{}, err
	}
	return a, nil
}

func (s *Store) GetAlarm(ctx context.Context, userID, id string) (Alarm, error) {
	a, err := scanAlarm(s.db.QueryRowContext(ctx, `SELECT `+alarmCols+` FROM alarms WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Alarm{}, ErrNotFound
	}
	return a, err
}

func (s *Store) UpdateAlarm(ctx context.Context, userID, id string, p AlarmPatch) (Alarm, error) {
	a, err := s.GetAlarm(ctx, userID, id)
	if err != nil {
		return Alarm{}, err
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&a.Label, p.Label)
	set(&a.Mode, p.Mode)
	set(&a.HolidayPolicy, p.HolidayPolicy)
	set(&a.Region, p.Region)
	set(&a.Note, p.Note)
	set(&a.OriginalText, p.OriginalText)
	if p.Enabled != nil {
		a.Enabled = *p.Enabled
	}
	if p.Schedule != nil {
		tz := a.Schedule.TimeZone
		a.Schedule = *p.Schedule
		if a.Schedule.TimeZone == "" {
			a.Schedule.TimeZone = tz // a new schedule without a zone keeps the old one
		}
	}
	if p.ProjectID != nil {
		if err := s.checkProject(ctx, userID, *p.ProjectID); err != nil {
			return Alarm{}, err
		}
		a.ProjectID = *p.ProjectID
	}
	if p.Space != nil {
		if *p.Space != SpaceWork && *p.Space != SpaceLife {
			return Alarm{}, invalid("space must be work or life")
		}
		a.Space = *p.Space
	}
	if err := a.normalize(); err != nil {
		return Alarm{}, err
	}
	sched, err := alarm.MarshalSchedule(a.Schedule)
	if err != nil {
		return Alarm{}, err
	}
	a.Updated = time.Now().UTC().Truncate(time.Millisecond)
	_, err = s.db.ExecContext(ctx, `
		UPDATE alarms SET label = ?, mode = ?, enabled = ?, schedule = ?, holiday_policy = ?, region = ?, note = ?, project_id = ?,
			space = ?, original_text = ?, updated_ms = ?
		WHERE id = ? AND user_id = ?`,
		a.Label, a.Mode, b2i(a.Enabled), sched, a.HolidayPolicy, a.Region, a.Note, a.ProjectID, a.Space, a.OriginalText, ms(a.Updated),
		id, userID)
	if err != nil {
		return Alarm{}, err
	}
	return a, nil
}

func (s *Store) DeleteAlarm(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM alarms WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAlarms returns the alarms the filter picks, oldest first, and the
// version of all the user's alarms, read together.
func (s *Store) ListAlarms(ctx context.Context, userID string, f AlarmFilter) ([]Alarm, string, error) {
	if err := checkSpaceFilter(f.Space); err != nil {
		return nil, "", err
	}
	all, err := s.allAlarms(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	out := []Alarm{}
	for _, a := range all {
		if (f.Enabled != nil && a.Enabled != *f.Enabled) || (f.Space != "" && a.Space != f.Space) || (f.ProjectID != "" && a.ProjectID != f.ProjectID) {
			continue
		}
		out = append(out, a)
	}
	return out, alarmsVersion(all), nil
}

// AlarmsVersion changes whenever any of the user's alarms is created, changed
// or deleted (a project's deletion moving one out of it included).
func (s *Store) AlarmsVersion(ctx context.Context, userID string) (string, error) {
	all, err := s.allAlarms(ctx, userID)
	if err != nil {
		return "", err
	}
	return alarmsVersion(all), nil
}

func (s *Store) allAlarms(ctx context.Context, userID string) ([]Alarm, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+alarmCols+` FROM alarms WHERE user_id = ? ORDER BY created_ms, rowid`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alarm
	for rows.Next() {
		a, err := scanAlarm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// alarmsVersion hashes every field of every alarm, so any write, delete or
// change made elsewhere (a project leaving) moves it, with no counter to keep.
func alarmsVersion(all []Alarm) string {
	h := sha256.New()
	for _, a := range all {
		sched, _ := alarm.MarshalSchedule(a.Schedule)
		for _, v := range []string{a.ID, a.Label, a.Mode, strconv.FormatBool(a.Enabled), sched, a.HolidayPolicy, a.Region, a.Note,
			a.ProjectID, a.Space, a.OriginalText, strconv.FormatInt(ms(a.Updated), 10)} {
			h.Write([]byte(v))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}
