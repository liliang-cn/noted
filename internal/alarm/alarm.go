// Package alarm is the schedule of an alarm (闹钟) and the engine that turns it
// into the times it rings. noted keeps alarm definitions; the phone schedules
// the rings itself (iOS AlarmKit) with the same rules, so they are written
// down in docs/alarms.md and pinned by the shared fixtures in
// docs/alarm-fixtures.json, which this package's tests run and the Swift
// engine runs too.
//
// It is a port of SmartAlarm's ScheduleEngine (Scheduling/ScheduleEngine.swift)
// with its gaps closed: until and count apply to every frequency, nothing
// rings before the anchor day, weekly without byDay rings on the anchor's
// weekday instead of every day, monthly honours interval, weeks start on
// Monday whatever the locale, and minutely/hourly run in a fixed daily window
// from time to endTime instead of striding from "now".
package alarm

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liliang-cn/noted/internal/holiday"
)

// Schedule kinds.
const (
	KindOneShot   = "oneShot"
	KindRecurring = "recurring"
)

// Modes: how the phone rings it.
const (
	ModeAlarm    = "alarm"    // a system alarm: sounds through silent mode and Focus
	ModeReminder = "reminder" // a notification: respects silent mode and Focus
)

// Frequencies.
const (
	FreqMinutely = "minutely"
	FreqHourly   = "hourly"
	FreqDaily    = "daily"
	FreqWeekly   = "weekly"
	FreqMonthly  = "monthly"
)

// Holiday policies. Holidays and 调休 apply only in region CN; elsewhere
// skipHoliday rings as usual and the workday policies mean Monday to Friday.
const (
	PolicyNone            = "none"
	PolicySkipHoliday     = "skipHoliday"     // statutory days off are skipped
	PolicyWorkdayOnly     = "workdayOnly"     // working days only, 调休补班 included
	PolicyFollowStatutory = "followStatutory" // the same days as workdayOnly
)

const (
	// WindowDays is how far ahead occurrences are expanded: today and the
	// next WindowDays days, in the alarm's zone.
	WindowDays = 180
	// MaxOccurrences caps one expansion.
	MaxOccurrences = 200
	// DefaultEndTime ends a minutely or hourly day without its own endTime.
	DefaultEndTime = "22:00"
)

// Weekdays in the order they are written out.
var weekdays = []string{"MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN"}

// Recurrence is how a recurring schedule repeats.
type Recurrence struct {
	Freq       string   `json:"freq"`
	Interval   int      `json:"interval"`             // >= 1: every Interval days, weeks, months, minutes or hours
	ByDay      []string `json:"byDay,omitempty"`      // weekly: MON..SUN
	ByMonthDay int      `json:"byMonthDay,omitempty"` // monthly: 1-31; months without that day are skipped
	ByTime     []string `json:"byTime,omitempty"`     // daily: several HH:MM a day, instead of Schedule.Time
	Until      string   `json:"until,omitempty"`      // YYYY-MM-DD, the last day it may ring
	Count      int      `json:"count,omitempty"`      // rings in all, counted from the anchor
	EndTime    string   `json:"endTime,omitempty"`    // minutely/hourly: HH:MM, the day's last possible ring; default 22:00
}

// Schedule is when an alarm rings.
type Schedule struct {
	Kind       string      `json:"kind"`
	At         *time.Time  `json:"at,omitempty"`     // oneShot
	Anchor     string      `json:"anchor,omitempty"` // recurring: YYYY-MM-DD, the first day it may ring
	Time       string      `json:"time,omitempty"`   // recurring: HH:MM; minutely/hourly: the day's first ring
	TimeZone   string      `json:"timeZone"`
	Recurrence *Recurrence `json:"recurrence,omitempty"`
}

// Rule is everything that decides the rings.
type Rule struct {
	Schedule      Schedule `json:"schedule"`
	HolidayPolicy string   `json:"holidayPolicy"`
	Region        string   `json:"region"`
}

// ---- validation ----

func errf(format string, a ...any) error { return fmt.Errorf(format, a...) }

// ParseClock reads "HH:MM" (or "H:MM") and returns minutes after midnight.
func ParseClock(s string) (int, bool) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || len(m) != 2 || len(h) < 1 || len(h) > 2 {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

func clock(min int) string { return fmt.Sprintf("%02d:%02d", min/60, min%60) }

func normClock(field, s string) (string, error) {
	m, ok := ParseClock(s)
	if !ok {
		return "", errf("%s %q is not a time of day; write HH:MM, e.g. 07:30", field, s)
	}
	return clock(m), nil
}

func parseDate(field, s string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, errf("%s %q is not a date; write YYYY-MM-DD, e.g. 2026-10-14", field, s)
	}
	return d, nil
}

var dayNames = map[string]string{
	"MO": "MON", "MON": "MON", "MONDAY": "MON",
	"TU": "TUE", "TUE": "TUE", "TUESDAY": "TUE",
	"WE": "WED", "WED": "WED", "WEDNESDAY": "WED",
	"TH": "THU", "THU": "THU", "THURSDAY": "THU",
	"FR": "FRI", "FRI": "FRI", "FRIDAY": "FRI",
	"SA": "SAT", "SAT": "SAT", "SATURDAY": "SAT",
	"SU": "SUN", "SUN": "SUN", "SUNDAY": "SUN",
}

// dayName is the MON..SUN name of a weekday.
func dayName(w time.Weekday) string { return weekdays[(int(w)+6)%7] }

// Normalize checks a rule and writes it in its one canonical form: defaults
// filled in (zone, interval 1, a weekly rule's day, a monthly rule's day of
// the month, policy none), times as HH:MM, days in week order. tz is the zone
// for a schedule without one. Errors say in plain words what to change.
func Normalize(r *Rule, tz string) error {
	s := &r.Schedule
	if s.TimeZone == "" {
		s.TimeZone = tz
	}
	if s.TimeZone == "" {
		s.TimeZone = "UTC"
	}
	loc, err := time.LoadLocation(s.TimeZone)
	if err != nil {
		return errf("timeZone %q is not an IANA zone name such as Asia/Shanghai", s.TimeZone)
	}
	switch r.HolidayPolicy {
	case "":
		r.HolidayPolicy = PolicyNone
	case PolicyNone, PolicySkipHoliday, PolicyWorkdayOnly, PolicyFollowStatutory:
	default:
		return errf("holidayPolicy %q is unknown: use none, skipHoliday (节假日不响), workdayOnly (仅工作日，含调休补班) or followStatutory (按调休)", r.HolidayPolicy)
	}
	switch strings.ToUpper(strings.TrimSpace(r.Region)) {
	case "", "NONE":
		r.Region = ""
	case holiday.RegionCN:
		r.Region = holiday.RegionCN
	default:
		return errf("region %q has no holiday calendar: use CN (mainland China) or leave it empty", r.Region)
	}

	switch s.Kind {
	case KindOneShot:
		if s.Recurrence != nil {
			return errf("a oneShot alarm rings once and takes no recurrence; use kind recurring to repeat")
		}
		if s.At == nil {
			if s.Anchor == "" || s.Time == "" {
				return errf("a oneShot alarm needs at: when it rings, RFC 3339, e.g. 2026-10-14T07:00:00+08:00")
			}
			d, err := parseDate("anchor", s.Anchor)
			if err != nil {
				return err
			}
			m, ok := ParseClock(s.Time)
			if !ok {
				return errf("time %q is not a time of day; write HH:MM, e.g. 07:30", s.Time)
			}
			at := time.Date(d.Year(), d.Month(), d.Day(), m/60, m%60, 0, 0, loc)
			s.At = &at
		}
		at := s.At.In(loc).Truncate(time.Second)
		s.At, s.Anchor, s.Time = &at, "", ""
		return nil
	case KindRecurring:
	case "":
		return errf("schedule.kind is required: oneShot (rings once at a time) or recurring (repeats)")
	default:
		return errf("schedule.kind %q is unknown: use oneShot or recurring", s.Kind)
	}

	s.At = nil
	rc := s.Recurrence
	if rc == nil {
		return errf("a recurring alarm needs recurrence, e.g. {\"freq\":\"daily\"} or {\"freq\":\"weekly\",\"byDay\":[\"MON\"]}")
	}
	if s.Anchor == "" {
		return errf("a recurring alarm needs anchor: the first day it may ring, YYYY-MM-DD (usually today); every-N-days/weeks/months count from it")
	}
	anchor, err := parseDate("anchor", s.Anchor)
	if err != nil {
		return err
	}
	s.Anchor = anchor.Format("2006-01-02")
	switch rc.Freq {
	case FreqMinutely, FreqHourly, FreqDaily, FreqWeekly, FreqMonthly:
	case "":
		return errf("recurrence.freq is required: minutely, hourly, daily, weekly or monthly")
	case "yearly":
		return errf("yearly alarms are not supported; use monthly with interval 12 and byMonthDay")
	default:
		return errf("recurrence.freq %q is unknown: use minutely, hourly, daily, weekly or monthly", rc.Freq)
	}
	if rc.Interval == 0 {
		rc.Interval = 1
	}
	if rc.Interval < 1 || rc.Interval > 1000 {
		return errf("recurrence.interval must be 1-1000 (2 with weekly means every other week)")
	}
	if rc.Count < 0 || rc.Count > 10000 {
		return errf("recurrence.count must be 0 (no limit) to 10000")
	}
	if rc.Until != "" {
		u, err := parseDate("recurrence.until", rc.Until)
		if err != nil {
			return err
		}
		if u.Before(anchor) {
			return errf("recurrence.until %s is before the anchor %s", rc.Until, s.Anchor)
		}
		rc.Until = u.Format("2006-01-02")
	}

	if len(rc.ByDay) > 0 && rc.Freq != FreqWeekly {
		return errf("byDay only works with freq weekly (got %s); for workdays use holidayPolicy workdayOnly instead", rc.Freq)
	}
	if rc.ByMonthDay != 0 && rc.Freq != FreqMonthly {
		return errf("byMonthDay only works with freq monthly (got %s)", rc.Freq)
	}
	if len(rc.ByTime) > 0 && rc.Freq != FreqDaily {
		return errf("byTime (several times a day) only works with freq daily (got %s); for other rules make one alarm per time", rc.Freq)
	}
	if rc.EndTime != "" && rc.Freq != FreqMinutely && rc.Freq != FreqHourly {
		return errf("endTime only works with freq minutely or hourly (got %s)", rc.Freq)
	}

	if len(rc.ByTime) > 0 {
		if s.Time != "" {
			return errf("give time for one ring a day or byTime for several, not both")
		}
		seen := map[int]bool{}
		var mins []int
		for _, v := range rc.ByTime {
			m, ok := ParseClock(v)
			if !ok {
				return errf("byTime %q is not a time of day; write HH:MM, e.g. 08:00", v)
			}
			if !seen[m] {
				seen[m] = true
				mins = append(mins, m)
			}
		}
		if len(mins) > 48 {
			return errf("byTime takes at most 48 times a day")
		}
		sort.Ints(mins)
		rc.ByTime = make([]string, 0, len(mins))
		for _, m := range mins {
			rc.ByTime = append(rc.ByTime, clock(m))
		}
	} else {
		if s.Time == "" {
			if rc.Freq == FreqMinutely || rc.Freq == FreqHourly {
				return errf("a %s alarm needs time: its first ring each day, HH:MM (it then rings every %d %s until endTime, default %s)", rc.Freq, rc.Interval, unit(rc.Freq), DefaultEndTime)
			}
			return errf("a recurring alarm needs time: HH:MM in its time zone, e.g. 07:00 (or byTime with freq daily for several times a day)")
		}
		if s.Time, err = normClock("time", s.Time); err != nil {
			return err
		}
		rc.ByTime = nil
	}

	switch rc.Freq {
	case FreqWeekly:
		if len(rc.ByDay) == 0 {
			rc.ByDay = []string{dayName(anchor.Weekday())}
		}
		set := map[string]bool{}
		for _, d := range rc.ByDay {
			n, ok := dayNames[strings.ToUpper(strings.TrimSpace(d))]
			if !ok {
				return errf("byDay %q is not a weekday; use MON, TUE, WED, THU, FRI, SAT or SUN", d)
			}
			set[n] = true
		}
		rc.ByDay = make([]string, 0, len(set))
		for _, d := range weekdays {
			if set[d] {
				rc.ByDay = append(rc.ByDay, d)
			}
		}
	case FreqMonthly:
		if rc.ByMonthDay == 0 {
			rc.ByMonthDay = anchor.Day()
		}
		if rc.ByMonthDay < 1 || rc.ByMonthDay > 31 {
			return errf("byMonthDay must be 1-31 (months without that day are skipped)")
		}
	case FreqMinutely, FreqHourly:
		start, _ := ParseClock(s.Time)
		if rc.EndTime != "" {
			if rc.EndTime, err = normClock("endTime", rc.EndTime); err != nil {
				return err
			}
			if end, _ := ParseClock(rc.EndTime); end < start {
				return errf("endTime %s is before time %s; the window of a %s alarm stays within one day", rc.EndTime, s.Time, rc.Freq)
			}
		} else if end, _ := ParseClock(DefaultEndTime); end < start {
			return errf("time %s is after the default endTime %s; give an endTime later than time", s.Time, DefaultEndTime)
		}
		if step := rc.Interval * minutesPer(rc.Freq); step > 24*60 {
			return errf("every %d %s is longer than a day; use freq daily", rc.Interval, unit(rc.Freq))
		}
	}
	return nil
}

func unit(freq string) string {
	if freq == FreqHourly {
		return "hours"
	}
	return "minutes"
}

func minutesPer(freq string) int {
	if freq == FreqHourly {
		return 60
	}
	return 1
}

// ---- the engine ----

// day is a civil date, held at midnight UTC so day arithmetic is exact.
type day = time.Time

func civil(t time.Time) day { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }

func daysBetween(a, b day) int { return int(b.Sub(a).Hours() / 24) }

// monday is the Monday of d's week.
func monday(d day) day { return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7)) }

// Occurrences returns the times the rule rings after now, oldest first, at
// most limit of them (0 or more than MaxOccurrences: MaxOccurrences). cal is
// the holiday calendar of the rule's region; it is only read when the region
// is CN. The rule should be normalized; defaults are filled in here as well.
func Occurrences(r Rule, cal *holiday.Calendar, now time.Time, limit int) []time.Time {
	if limit <= 0 || limit > MaxOccurrences {
		limit = MaxOccurrences
	}
	s := r.Schedule
	loc, err := time.LoadLocation(s.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	out := []time.Time{}
	switch s.Kind {
	case KindOneShot:
		if s.At != nil && s.At.After(now) {
			out = append(out, s.At.In(loc))
		}
		return out
	case KindRecurring:
	default:
		return out
	}
	if s.Recurrence == nil {
		return out
	}
	rc := *s.Recurrence
	anchor, err := time.Parse("2006-01-02", s.Anchor)
	if err != nil {
		return out
	}
	interval := max(rc.Interval, 1)
	times := dayTimes(s, rc)
	if len(times) == 0 {
		return out
	}
	if r.Region != holiday.RegionCN {
		cal = nil
	}

	today := civil(now.In(loc))
	last := today.AddDate(0, 0, WindowDays)
	if rc.Until != "" {
		if u, err := time.Parse("2006-01-02", rc.Until); err == nil && u.Before(last) {
			last = u
		}
	}
	subDay := rc.Freq == FreqMinutely || rc.Freq == FreqHourly
	if subDay && rc.Until == "" && rc.Count == 0 && anchor.Before(last) {
		last = anchor // without until or count, a minutely/hourly alarm runs on its anchor day only
	}
	first := anchor
	if rc.Count == 0 && today.After(first) {
		first = today // with a count, rings before today still count, so start at the anchor
	}

	rung := 0
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		if !matches(d, anchor, rc, interval) || !passes(d, r.HolidayPolicy, r.Region, cal) {
			continue
		}
		for _, m := range times {
			fire := time.Date(d.Year(), d.Month(), d.Day(), m/60, m%60, 0, 0, loc)
			rung++
			if fire.After(now) {
				out = append(out, fire)
				if len(out) >= limit {
					return out
				}
			}
			if rc.Count > 0 && rung >= rc.Count {
				return out
			}
		}
	}
	return out
}

// dayTimes is when, in minutes after midnight, a matching day rings.
func dayTimes(s Schedule, rc Recurrence) []int {
	if rc.Freq == FreqMinutely || rc.Freq == FreqHourly {
		start, ok := ParseClock(s.Time)
		if !ok {
			return nil
		}
		end, ok := ParseClock(rc.EndTime)
		if !ok {
			end, _ = ParseClock(DefaultEndTime)
		}
		step := max(rc.Interval, 1) * minutesPer(rc.Freq)
		var out []int
		for m := start; m <= end; m += step {
			out = append(out, m)
		}
		return out
	}
	if rc.Freq == FreqDaily && len(rc.ByTime) > 0 {
		seen := map[int]bool{}
		var out []int
		for _, v := range rc.ByTime {
			if m, ok := ParseClock(v); ok && !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
		sort.Ints(out)
		return out
	}
	m, ok := ParseClock(s.Time)
	if !ok {
		return nil
	}
	return []int{m}
}

// matches reports whether the rule falls on day d (never before the anchor).
func matches(d, anchor day, rc Recurrence, interval int) bool {
	if d.Before(anchor) {
		return false
	}
	switch rc.Freq {
	case FreqMinutely, FreqHourly:
		return true
	case FreqDaily:
		return daysBetween(anchor, d)%interval == 0
	case FreqWeekly:
		byDay := rc.ByDay
		if len(byDay) == 0 {
			byDay = []string{dayName(anchor.Weekday())}
		}
		name := dayName(d.Weekday())
		hit := false
		for _, b := range byDay {
			if n := dayNames[strings.ToUpper(b)]; n == name {
				hit = true
			}
		}
		return hit && (daysBetween(monday(anchor), monday(d))/7)%interval == 0
	case FreqMonthly:
		md := rc.ByMonthDay
		if md == 0 {
			md = anchor.Day()
		}
		months := (d.Year()-anchor.Year())*12 + int(d.Month()) - int(anchor.Month())
		return d.Day() == md && months%interval == 0
	}
	return false
}

// passes applies the holiday policy to day d.
func passes(d day, policy, region string, cal *holiday.Calendar) bool {
	date := d.Format("2006-01-02")
	switch policy {
	case PolicySkipHoliday:
		if region != holiday.RegionCN {
			return true // no local holiday data: ring as usual
		}
		return !cal.IsHoliday(date)
	case PolicyWorkdayOnly, PolicyFollowStatutory:
		if region != holiday.RegionCN {
			return d.Weekday() != time.Saturday && d.Weekday() != time.Sunday
		}
		return cal.IsWorkday(date, d.Weekday())
	}
	return true
}

// Next is the next n rings of a rule after now, with its region's calendar.
func Next(r Rule, now time.Time, n int) []time.Time {
	return Occurrences(r, holiday.ForRegion(r.Region), now, n)
}

// ---- JSON ----

// MarshalSchedule is the schedule as stored.
func MarshalSchedule(s Schedule) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}

// UnmarshalSchedule reads a stored schedule.
func UnmarshalSchedule(v string) (Schedule, error) {
	var s Schedule
	err := json.Unmarshal([]byte(v), &s)
	return s, err
}
