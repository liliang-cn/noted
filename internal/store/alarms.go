package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/liliang-cn/noted/internal/alarm"
)

// Alarm is the alarm-shaped view of a schedule item (闹钟): what the alarm
// API reads and writes, and what a phone schedules. Alarms live in the events
// table with notify alarm (or whatever notify the person chose) and an
// alarm-style Rule; a calendar event or a to-do with a notify is one too, its
// start+rrule written as a schedule.
type Alarm struct {
	ID            string
	UserID        string
	Label         string
	Mode          string // mirrors Notify: alarm, reminder, app (or none)
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

	Notify       string
	RemindBefore int    // minutes before each schedule time it rings; 0 for alarms proper
	Source       string // alarm, event or task
	Completed    bool
}

// Rule is what decides when the alarm rings.
func (a Alarm) Rule() alarm.Rule {
	return alarm.Rule{Schedule: a.Schedule, HolidayPolicy: a.HolidayPolicy, Region: a.Region}
}

// NextFires is the alarm's next n rings after now, the lead taken off; none
// when it is off, done, or silent.
func (a Alarm) NextFires(now time.Time, n int) []time.Time {
	if !a.Enabled || a.Completed || a.Notify == NotifyNone {
		return []time.Time{}
	}
	lead := time.Duration(a.RemindBefore) * time.Minute
	out := []time.Time{}
	for _, t := range alarm.Next(a.Rule(), now.Add(lead), n) {
		out = append(out, t.Add(-lead))
	}
	return out
}

type AlarmPatch struct {
	Label, Mode, HolidayPolicy, Region, Note, OriginalText *string
	Enabled                                                *bool
	Schedule                                               *alarm.Schedule // replaces the whole schedule
	ProjectID                                              *string
	Space                                                  *string
}

// AlarmFilter picks schedule items. Notify: "" the alarms proper (notify
// alarm, plus whatever the alarm API made); alarm, reminder or app that
// notify; "all" every item that notifies, what a phone schedules.
type AlarmFilter struct {
	Enabled   *bool
	Space     string
	ProjectID string
	Notify    string
}

const maxAlarmText = 2000

func modeToNotify(mode string) (string, error) {
	switch mode {
	case "":
		return NotifyAlarm, nil
	case alarm.ModeAlarm, alarm.ModeReminder, NotifyApp, NotifyNone:
		return mode, nil
	}
	return "", invalid("mode %q is unknown: use alarm (rings through silent mode) or reminder (a notification)", mode)
}

// toEvent is the schedule item an alarm definition writes.
func (a Alarm) toEvent() (Event, error) {
	label := strings.TrimSpace(a.Label)
	if label == "" {
		return Event{}, invalid("label is empty: say what the alarm is for, e.g. 起床")
	}
	notify, err := modeToNotify(a.Mode)
	if err != nil {
		return Event{}, err
	}
	if utf8Len(a.Note) > maxAlarmText || utf8Len(a.OriginalText) > maxAlarmText {
		return Event{}, invalid("note and original_text are at most %d characters", maxAlarmText)
	}
	sc := a.Schedule
	lead := a.RemindBefore
	return Event{UserID: a.UserID, Title: label, Description: strings.TrimSpace(a.Note), TimeZone: sc.TimeZone, Notify: notify, Rule: &sc,
		HolidayPolicy: a.HolidayPolicy, Region: a.Region, Disabled: !a.Enabled, ProjectID: a.ProjectID, Space: a.Space,
		OriginalText: a.OriginalText, Source: SourceAlarm, RemindBefore: &lead}, nil
}

func utf8Len(s string) int { return len([]rune(s)) }

// AlarmOf is the alarm-shaped view of a schedule item.
func AlarmOf(e Event) Alarm {
	a := Alarm{ID: e.ID, UserID: e.UserID, Label: e.Title, Mode: e.Notify, Enabled: !e.Disabled, Schedule: e.Schedule(),
		HolidayPolicy: e.HolidayPolicy, Region: e.Region, Note: e.Description, ProjectID: e.ProjectID, Space: e.Space,
		OriginalText: e.OriginalText, Created: e.Created, Updated: e.Updated, Notify: e.Notify, Source: e.Source, Completed: e.Completed}
	if e.RemindBefore != nil {
		a.RemindBefore = *e.RemindBefore
	}
	return a
}

// CreateAlarm saves a new alarm: a schedule item with the alarm's rule. Its
// zone defaults to UTC; callers fill in the server's.
func (s *Store) CreateAlarm(ctx context.Context, a Alarm) (Alarm, error) {
	e, err := a.toEvent()
	if err != nil {
		return Alarm{}, err
	}
	out, err := s.CreateEvent(ctx, e)
	if err != nil {
		return Alarm{}, err
	}
	return AlarmOf(out), nil
}

// GetAlarm reads any schedule item as an alarm.
func (s *Store) GetAlarm(ctx context.Context, userID, id string) (Alarm, error) {
	e, err := s.GetEvent(ctx, userID, id)
	if err != nil {
		return Alarm{}, err
	}
	return AlarmOf(e), nil
}

func (s *Store) UpdateAlarm(ctx context.Context, userID, id string, p AlarmPatch) (Alarm, error) {
	var ep EventPatch
	if p.Label != nil {
		l := strings.TrimSpace(*p.Label)
		if l == "" {
			return Alarm{}, invalid("label is empty: say what the alarm is for, e.g. 起床")
		}
		ep.Title = &l
	}
	if p.Mode != nil {
		n, err := modeToNotify(*p.Mode)
		if err != nil {
			return Alarm{}, err
		}
		ep.Notify = &n
	}
	if p.Note != nil {
		if utf8Len(*p.Note) > maxAlarmText {
			return Alarm{}, invalid("note and original_text are at most %d characters", maxAlarmText)
		}
		ep.Description = p.Note
	}
	if p.OriginalText != nil && utf8Len(*p.OriginalText) > maxAlarmText {
		return Alarm{}, invalid("note and original_text are at most %d characters", maxAlarmText)
	}
	ep.HolidayPolicy, ep.Region, ep.OriginalText = p.HolidayPolicy, p.Region, p.OriginalText
	if p.Enabled != nil {
		d := !*p.Enabled
		ep.Disabled = &d
	}
	ep.ProjectID, ep.Space = p.ProjectID, p.Space
	if p.Schedule != nil {
		sc := *p.Schedule
		ep.Rule = &sc
	}
	e, err := s.UpdateEvent(ctx, userID, id, ep)
	if err != nil {
		return Alarm{}, err
	}
	return AlarmOf(e), nil
}

func (s *Store) DeleteAlarm(ctx context.Context, userID, id string) error {
	return s.DeleteEvent(ctx, userID, id)
}

// ListAlarms returns the schedule items the filter picks as alarms, oldest
// first, and the version of the user's whole schedule, read together.
func (s *Store) ListAlarms(ctx context.Context, userID string, f AlarmFilter) ([]Alarm, string, error) {
	if err := checkSpaceFilter(f.Space); err != nil {
		return nil, "", err
	}
	switch f.Notify {
	case "", "all", NotifyAlarm, NotifyReminder, NotifyApp:
	default:
		return nil, "", invalid("notify filter %q is unknown: use alarm, reminder, app or all", f.Notify)
	}
	all, err := s.allSchedule(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	out := []Alarm{}
	for _, e := range all {
		if e.Completed {
			continue
		}
		switch f.Notify {
		case "":
			if e.Notify != NotifyAlarm && e.Source != SourceAlarm {
				continue
			}
		case "all":
			if e.Notify == NotifyNone || e.RemindBefore == nil {
				continue
			}
		default:
			if e.Notify != f.Notify || e.RemindBefore == nil {
				continue
			}
		}
		if (f.Enabled != nil && !e.Disabled != *f.Enabled) || (f.Space != "" && e.Space != f.Space) || (f.ProjectID != "" && e.ProjectID != f.ProjectID) {
			continue
		}
		out = append(out, AlarmOf(e))
	}
	return out, scheduleVersion(all), nil
}

// AlarmsVersion changes whenever any of the user's schedule items — alarm,
// event or a to-do's time — is created, changed or deleted (a project's
// deletion moving one out of it included).
func (s *Store) AlarmsVersion(ctx context.Context, userID string) (string, error) {
	all, err := s.allSchedule(ctx, userID)
	if err != nil {
		return "", err
	}
	return scheduleVersion(all), nil
}

// allSchedule is every schedule item of the user, oldest first.
func (s *Store) allSchedule(ctx context.Context, userID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventCols+` FROM events WHERE user_id = ? ORDER BY created_ms, rowid`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// scheduleVersion hashes every field of every item, so any write, delete or
// change made elsewhere (a project leaving, a to-do completed) moves it, with
// no counter to keep.
func scheduleVersion(all []Event) string {
	h := sha256.New()
	for _, e := range all {
		rule, _ := marshalRule(e.Rule)
		lead := "-"
		if e.RemindBefore != nil {
			lead = strconv.Itoa(*e.RemindBefore)
		}
		for _, v := range []string{e.ID, e.Title, e.Description, e.Location, strconv.FormatInt(ms(e.Start), 10), strconv.FormatInt(ms(e.End), 10),
			strconv.FormatBool(e.AllDay), e.TimeZone, e.RRule, lead, e.ProjectID, e.Space, e.Notify, rule, e.HolidayPolicy, e.Region,
			strconv.FormatBool(!e.Disabled), e.OriginalText, e.Source, e.TaskID, strconv.FormatBool(e.Completed), strconv.FormatInt(ms(e.Updated), 10)} {
			h.Write([]byte(v))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}
