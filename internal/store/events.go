package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/liliang-cn/noted/internal/alarm"
	"github.com/liliang-cn/noted/internal/recur"
)

// How a schedule item tells the person when its time comes.
const (
	NotifyAlarm    = "alarm"    // the phone rings until stopped by hand, through silent mode
	NotifyReminder = "reminder" // a system notification that can be swiped away (the default)
	NotifyApp      = "app"      // shown inside the app only
	NotifyNone     = "none"
)

// Where a schedule item came from.
const (
	SourceEvent = "event"
	SourceAlarm = "alarm" // the alarm API, or the alarms table before it was folded in
	SourceTask  = "task"  // the schedule of a to-do with a time; its id is the task's
)

// Event is one schedule item (日程): a calendar event, an alarm (a rule
// instead of start+rrule) or the time of a to-do. Notify says how the person
// is told, RemindBefore how far ahead; without RemindBefore nothing is
// scheduled anywhere, whatever Notify says.
type Event struct {
	ID           string
	UserID       string
	Title        string
	Description  string
	Location     string
	Start        time.Time
	End          time.Time
	AllDay       bool
	TimeZone     string
	RRule        string
	RemindBefore *int // minutes; nil: no reminder. 0 is at the start, not unset
	ProjectID    string
	Space        string
	Created      time.Time
	Updated      time.Time

	Notify string
	// Rule is an alarm-style schedule (oneShot at / recurring anchor+time+
	// recurrence) in place of Start+RRule. Start then mirrors its first ring.
	Rule          *alarm.Schedule
	HolidayPolicy string // none, skipHoliday, workdayOnly, followStatutory; applies to Rule and RRule alike
	Region        string // CN or ""
	Disabled      bool   // paused: no occurrences, nothing told; the alarm API's enabled false
	OriginalText  string
	// Read-only.
	Source    string
	TaskID    string // Source task: the to-do, which is also this item's id
	Completed bool   // Source task: mirrors the to-do
}

type EventPatch struct {
	Title, Description, Location *string
	Start, End                   *time.Time
	AllDay                       *bool
	TimeZone, RRule              *string
	RemindBefore                 **int // non-nil: set; *RemindBefore == nil clears
	ProjectID                    *string
	Space                        *string
	Notify                       *string
	Rule                         *alarm.Schedule // replaces the whole rule; a schedule with Kind "" drops it (Start+RRule rule again)
	HolidayPolicy, Region        *string
	Disabled                     *bool
	OriginalText                 *string
}

type Occurrence struct {
	Event Event
	Start time.Time
	End   time.Time
}

// Schedule is a window of the calendar: every occurrence in it and, when
// asked, the open to-dos that have no time yet.
type Schedule struct {
	Occurrences []Occurrence
	Unscheduled []Task
}

const (
	maxRemindMinutes = 28 * 24 * 60
	maxRangeDays     = 366
	maxEventText     = 2000
)

func checkNotify(v *string) error {
	switch *v {
	case "":
		*v = NotifyReminder
	case NotifyAlarm, NotifyReminder, NotifyApp, NotifyNone:
	default:
		return invalid("notify %q is unknown: use alarm (rings until stopped), reminder (a notification), app (in the app only) or none", *v)
	}
	return nil
}

// Normalize validates the event and fills defaults (zone, end time, notify,
// policy; the first ring of a rule as the start).
func (e *Event) Normalize() error {
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" {
		return invalid("title is empty")
	}
	if utf8.RuneCountInString(e.Title) > maxTitle {
		return invalid("title is longer than %d characters", maxTitle)
	}
	e.OriginalText = strings.TrimSpace(e.OriginalText)
	if utf8.RuneCountInString(e.OriginalText) > maxEventText {
		return invalid("original_text is at most %d characters", maxEventText)
	}
	if err := checkNotify(&e.Notify); err != nil {
		return err
	}
	switch e.Source {
	case "":
		e.Source = SourceEvent
	case SourceEvent, SourceAlarm, SourceTask:
	default:
		return invalid("source %q is unknown", e.Source)
	}
	if e.RemindBefore != nil && (*e.RemindBefore < 0 || *e.RemindBefore > maxRemindMinutes) {
		return invalid("remind_before_minutes must be between 0 and %d", maxRemindMinutes)
	}
	if e.Rule != nil {
		if e.RRule != "" {
			return invalid("give either an alarm-style schedule or an rrule, not both")
		}
		if e.AllDay {
			return invalid("an alarm-style schedule rings at a time; it cannot be all-day")
		}
		r := alarm.Rule{Schedule: *e.Rule, HolidayPolicy: e.HolidayPolicy, Region: e.Region}
		if err := alarm.Normalize(&r, e.TimeZone); err != nil {
			return invalid("%s", err.Error())
		}
		e.Rule, e.HolidayPolicy, e.Region = &r.Schedule, r.HolidayPolicy, r.Region
		e.TimeZone = r.Schedule.TimeZone
		first := alarm.FirstRing(r.Schedule)
		if first.IsZero() {
			return invalid("the schedule has no first ring")
		}
		e.Start, e.End = first.UTC(), first.UTC()
		return nil
	}
	if err := alarm.NormalizePolicy(&e.HolidayPolicy, &e.Region); err != nil {
		return invalid("%s", err.Error())
	}
	if e.Start.IsZero() {
		return invalid("start_time is required")
	}
	if e.TimeZone == "" {
		e.TimeZone = "UTC"
	}
	loc, err := time.LoadLocation(e.TimeZone)
	if err != nil {
		return invalid("unknown time_zone %q", e.TimeZone)
	}
	if e.AllDay {
		y, m, d := e.Start.In(loc).Date()
		e.Start = time.Date(y, m, d, 0, 0, 0, 0, loc).UTC()
		if e.End.IsZero() || !e.End.After(e.Start) {
			e.End = time.Date(y, m, d+1, 0, 0, 0, 0, loc).UTC()
		}
	} else if e.End.IsZero() {
		e.End = e.Start.Add(time.Hour)
	}
	if e.End.Before(e.Start) {
		return invalid("end_time is before start_time")
	}
	if e.RRule != "" {
		if _, err := recur.Parse(e.RRule); err != nil {
			return invalid("%v", err)
		}
	}
	return nil
}

func (e Event) location() *time.Location {
	loc, err := time.LoadLocation(e.TimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Schedule is the item's time as an alarm-style schedule: its Rule, or its
// Start+RRule written as one (so a phone expands every item the same way).
func (e Event) Schedule() alarm.Schedule {
	if e.Rule != nil {
		return *e.Rule
	}
	return alarm.FromRRule(e.Start, e.TimeZone, e.RRule)
}

// Notifies reports whether the item is told to the person at all: on, not
// done, a notify other than none, and a reminder time.
func (e Event) Notifies() bool {
	return !e.Disabled && !e.Completed && e.Notify != NotifyNone && e.RemindBefore != nil
}

// Occurrences returns the instances of e that overlap [from, to), the holiday
// policy applied; none when it is paused.
func (e Event) Occurrences(from, to time.Time, limit int) []Occurrence {
	if e.Disabled {
		return nil
	}
	if e.Rule != nil {
		var out []Occurrence
		for _, t := range alarm.Next(alarm.Rule{Schedule: *e.Rule, HolidayPolicy: e.HolidayPolicy, Region: e.Region}, from.Add(-time.Nanosecond), limit) {
			if !t.Before(to) {
				break
			}
			out = append(out, Occurrence{Event: e, Start: t.UTC(), End: t.UTC()})
		}
		return out
	}
	dur := e.End.Sub(e.Start)
	loc := e.location()
	if e.RRule == "" {
		if (e.Start.Before(to) && e.End.After(from)) || (dur == 0 && !e.Start.Before(from) && e.Start.Before(to)) {
			if alarm.DayAllowed(e.Start.In(loc), e.HolidayPolicy, e.Region) {
				return []Occurrence{{Event: e, Start: e.Start, End: e.End}}
			}
		}
		return nil
	}
	rule, err := recur.Parse(e.RRule)
	if err != nil {
		return nil // validated on write; a corrupt row just yields nothing
	}
	var out []Occurrence
	for _, s := range rule.Occurrences(e.Start.In(loc), from.Add(-dur), to, limit) {
		end := s.Add(dur)
		if (end.After(from) || dur == 0) && alarm.DayAllowed(s.In(loc), e.HolidayPolicy, e.Region) {
			out = append(out, Occurrence{Event: e, Start: s.UTC(), End: end.UTC()})
		}
	}
	return out
}

func (s *Store) CreateEvent(ctx context.Context, e Event) (Event, error) {
	if e.Source == SourceTask || e.TaskID != "" {
		return Event{}, invalid("a to-do's schedule is made by giving the to-do a due time")
	}
	if e.Rule != nil && e.TimeZone == "" {
		e.TimeZone = e.Rule.TimeZone
	}
	if err := e.Normalize(); err != nil {
		return Event{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	e.ID, e.Created, e.Updated = uuid.NewString(), now, now
	if err := s.checkProject(ctx, e.UserID, e.ProjectID); err != nil {
		return Event{}, err
	}
	var err error
	if e.Space, err = s.resolveSpace(ctx, e.UserID, e.Space, e.ProjectID); err != nil {
		return Event{}, err
	}
	rule, err := marshalRule(e.Rule)
	if err != nil {
		return Event{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO events(id, user_id, title, description, location, start_ms, end_ms, all_day, tz, rrule,
		                   remind_before, project_id, space, created_ms, updated_ms,
		                   notify, rule, holiday_policy, region, enabled, original_text, source, task_id, completed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', 0)`,
		e.ID, e.UserID, e.Title, e.Description, e.Location, ms(e.Start), ms(e.End), b2i(e.AllDay), e.TimeZone, e.RRule,
		nullInt(e.RemindBefore), e.ProjectID, e.Space, ms(e.Created), ms(e.Updated),
		e.Notify, rule, e.HolidayPolicy, e.Region, b2i(!e.Disabled), e.OriginalText, e.Source)
	return e, err
}

func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func marshalRule(r *alarm.Schedule) (string, error) {
	if r == nil {
		return "", nil
	}
	return alarm.MarshalSchedule(*r)
}

const eventCols = `id, user_id, title, description, location, start_ms, end_ms, all_day, tz, rrule, remind_before, project_id, space, created_ms, updated_ms,
	notify, rule, holiday_policy, region, enabled, original_text, source, task_id, completed`

func scanEvent(r scanner, extra ...any) (Event, error) {
	var e Event
	var start, end, created, updated int64
	var allDay, enabled, completed int
	var remind sql.NullInt64
	var rule string
	dst := []any{&e.ID, &e.UserID, &e.Title, &e.Description, &e.Location, &start, &end, &allDay,
		&e.TimeZone, &e.RRule, &remind, &e.ProjectID, &e.Space, &created, &updated,
		&e.Notify, &rule, &e.HolidayPolicy, &e.Region, &enabled, &e.OriginalText, &e.Source, &e.TaskID, &completed}
	if err := r.Scan(append(dst, extra...)...); err != nil {
		return Event{}, err
	}
	e.Start, e.End, e.AllDay = fromMS(start), fromMS(end), allDay != 0
	e.Disabled, e.Completed = enabled == 0, completed != 0
	e.Created, e.Updated = fromMS(created), fromMS(updated)
	if remind.Valid {
		v := int(remind.Int64)
		e.RemindBefore = &v
	}
	if rule != "" {
		sc, err := alarm.UnmarshalSchedule(rule)
		if err != nil {
			return Event{}, err
		}
		e.Rule = &sc
	}
	return e, nil
}

func (s *Store) GetEvent(ctx context.Context, userID, id string) (Event, error) {
	e, err := scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return e, err
}

func (s *Store) UpdateEvent(ctx context.Context, userID, id string, p EventPatch) (Event, error) {
	e, err := s.GetEvent(ctx, userID, id)
	if err != nil {
		return Event{}, err
	}
	if e.Source == SourceTask {
		return s.updateTaskSchedule(ctx, userID, e, p)
	}
	oldStart, oldRule, oldRemind := e.Start, e.Rule, nullInt(e.RemindBefore)
	dur := e.End.Sub(e.Start)
	if p.Title != nil {
		e.Title = *p.Title
	}
	if p.Description != nil {
		e.Description = *p.Description
	}
	if p.Location != nil {
		e.Location = *p.Location
	}
	if p.AllDay != nil {
		e.AllDay = *p.AllDay
	}
	if p.TimeZone != nil {
		e.TimeZone = *p.TimeZone
	}
	if p.RRule != nil {
		e.RRule = *p.RRule
		if e.RRule != "" {
			e.Rule = nil
		}
	}
	if p.RemindBefore != nil {
		e.RemindBefore = *p.RemindBefore
	}
	if p.Notify != nil {
		e.Notify = *p.Notify
	}
	if p.HolidayPolicy != nil {
		e.HolidayPolicy = *p.HolidayPolicy
	}
	if p.Region != nil {
		e.Region = *p.Region
	}
	if p.Disabled != nil {
		e.Disabled = *p.Disabled
	}
	if p.OriginalText != nil {
		e.OriginalText = *p.OriginalText
	}
	if p.Rule != nil {
		if p.Rule.Kind == "" {
			e.Rule = nil
		} else {
			r := *p.Rule
			if r.TimeZone == "" {
				r.TimeZone = e.TimeZone // a new rule without a zone keeps the old one
			}
			e.Rule, e.RRule = &r, ""
		}
	}
	if p.ProjectID != nil {
		e.ProjectID = *p.ProjectID
		if err := s.checkProject(ctx, userID, e.ProjectID); err != nil {
			return Event{}, err
		}
	}
	if p.Space != nil {
		if *p.Space != SpaceWork && *p.Space != SpaceLife {
			return Event{}, invalid("space must be work or life")
		}
		e.Space = *p.Space
	}
	if p.Start != nil {
		e.Start = *p.Start
		if p.End == nil { // moving the start keeps the duration
			e.End = e.Start.Add(dur)
		}
		if p.Rule == nil && e.Rule != nil {
			// A rule's time is the rule's: moving the start moves a one-shot, and is refused for a repeating rule.
			if e.Rule.Kind != alarm.KindOneShot {
				return Event{}, invalid("this item repeats by an alarm-style schedule: change its schedule, not start_time")
			}
			at := e.Start
			e.Rule = &alarm.Schedule{Kind: alarm.KindOneShot, At: &at, TimeZone: e.Rule.TimeZone}
		}
	}
	if p.End != nil {
		e.End = *p.End
	}
	if err := e.Normalize(); err != nil {
		return Event{}, err
	}
	rule, err := marshalRule(e.Rule)
	if err != nil {
		return Event{}, err
	}
	oldRuleJSON, _ := marshalRule(oldRule)
	e.Updated = time.Now().UTC().Truncate(time.Millisecond)
	// Rescheduling, a new rule or a new lead re-arms the reminder.
	rearm := !e.Start.Equal(oldStart) || rule != oldRuleJSON || nullInt(e.RemindBefore) != oldRemind
	_, err = s.db.ExecContext(ctx, `
		UPDATE events SET title = ?, description = ?, location = ?, start_ms = ?, end_ms = ?, all_day = ?, tz = ?,
		                  rrule = ?, remind_before = ?, project_id = ?, space = ?, updated_ms = ?,
		                  notify = ?, rule = ?, holiday_policy = ?, region = ?, enabled = ?, original_text = ?,
		                  reminded_for_ms = CASE WHEN ? THEN NULL ELSE reminded_for_ms END
		WHERE id = ? AND user_id = ?`,
		e.Title, e.Description, e.Location, ms(e.Start), ms(e.End), b2i(e.AllDay), e.TimeZone,
		e.RRule, nullInt(e.RemindBefore), e.ProjectID, e.Space, ms(e.Updated),
		e.Notify, rule, e.HolidayPolicy, e.Region, b2i(!e.Disabled), e.OriginalText, b2i(rearm), id, userID)
	return e, err
}

// updateTaskSchedule applies a patch on a to-do's schedule to the to-do: its
// title, time, lead, notify, project and space. The rest belongs to the to-do
// itself (update_task) or makes no sense for one.
func (s *Store) updateTaskSchedule(ctx context.Context, userID string, e Event, p EventPatch) (Event, error) {
	if p.Location != nil || p.AllDay != nil || p.TimeZone != nil || p.RRule != nil || p.Rule != nil ||
		p.HolidayPolicy != nil || p.Region != nil || p.Disabled != nil || p.OriginalText != nil || p.End != nil {
		return Event{}, invalid("this is the schedule of a to-do: only title, description, start_time, remind_before_minutes, notify, project_id and space can be changed here; the rest through the to-do (update_task)")
	}
	t, err := s.GetTask(ctx, userID, e.TaskID)
	if err != nil {
		return Event{}, err
	}
	var tp TaskPatch
	tp.Title, tp.Notes, tp.Notify, tp.ProjectID, tp.Space = p.Title, p.Description, p.Notify, p.ProjectID, p.Space
	lead := e.RemindBefore
	if p.RemindBefore != nil {
		lead = *p.RemindBefore
	}
	start := e.Start
	if p.Start != nil {
		start = *p.Start
	}
	if p.Start != nil || p.RemindBefore != nil {
		if t.Due != nil || p.Start != nil {
			due := start
			tp.Due = ptrPtr(&due)
		}
		var remind *time.Time
		if lead != nil {
			r := start.Add(-time.Duration(*lead) * time.Minute)
			remind = &r
		}
		tp.Remind = ptrPtr(remind)
	}
	if _, err := s.UpdateTask(ctx, userID, e.TaskID, tp); err != nil {
		return Event{}, err
	}
	return s.GetEvent(ctx, userID, e.ID)
}

func ptrPtr(t *time.Time) **time.Time { return &t }

func (s *Store) DeleteEvent(ctx context.Context, userID, id string) error {
	e, err := s.GetEvent(ctx, userID, id)
	if err != nil {
		return err
	}
	if e.Source == SourceTask {
		return s.DeleteTask(ctx, userID, e.TaskID)
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM events WHERE id = ? AND user_id = ?`, id, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		// A goal scheduled by this event keeps going, just unscheduled.
		_, err = tx.ExecContext(ctx, `UPDATE goals SET event_id = '' WHERE event_id = ? AND user_id = ?`, id, userID)
		return err
	})
}

// ListEvents returns every occurrence of the events and alarms overlapping
// [from, to), ordered by start; to-dos are left out (see ListSchedule).
func (s *Store) ListEvents(ctx context.Context, userID string, from, to time.Time, space string) ([]Occurrence, error) {
	sc, err := s.ListSchedule(ctx, userID, from, to, space, false)
	if err != nil {
		return nil, err
	}
	return sc.Occurrences, nil
}

// ListSchedule returns every occurrence overlapping [from, to), ordered by
// start, and with tasks the schedules of to-dos in the window (done ones too)
// plus the open to-dos that have no time at all, as Unscheduled.
func (s *Store) ListSchedule(ctx context.Context, userID string, from, to time.Time, space string, tasks bool) (Schedule, error) {
	if err := checkSpaceFilter(space); err != nil {
		return Schedule{}, err
	}
	if !to.After(from) {
		return Schedule{}, invalid("to must be after from")
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		return Schedule{}, invalid("range is longer than %d days", maxRangeDays)
	}
	// Recurring items can start long before the window, so they are always
	// candidates; one-offs are pre-filtered in SQL.
	q := `SELECT ` + eventCols + ` FROM events
		WHERE user_id = ? AND (rule <> '' OR (start_ms < ? AND (rrule <> '' OR end_ms > ? OR (end_ms = start_ms AND start_ms >= ?))))`
	args := []any{userID, ms(to), ms(from), ms(from)}
	if !tasks {
		q += ` AND task_id = ''`
	}
	q, args = spaceClause(q, args, "space", space)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return Schedule{}, err
	}
	defer func() { _ = rows.Close() }()
	out := Schedule{Occurrences: []Occurrence{}}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return Schedule{}, err
		}
		out.Occurrences = append(out.Occurrences, e.Occurrences(from, to, 1000)...)
	}
	if err := rows.Err(); err != nil {
		return Schedule{}, err
	}
	sort.SliceStable(out.Occurrences, func(i, j int) bool { return out.Occurrences[i].Start.Before(out.Occurrences[j].Start) })
	if tasks {
		out.Unscheduled, err = s.unscheduledTasks(ctx, userID, space)
		if err != nil {
			return Schedule{}, err
		}
	}
	return out, nil
}

// AllEvents returns every calendar event of the user as stored, recurring ones
// once, ordered by start. Alarms and to-dos' schedules are not among them:
// they are listed as alarms and tasks.
func (s *Store) AllEvents(ctx context.Context, userID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventCols+` FROM events WHERE user_id = ? AND source = ? ORDER BY start_ms, id`, userID, SourceEvent)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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
