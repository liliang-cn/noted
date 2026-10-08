// Package recur expands the RFC 5545 RRULE subset that noted supports:
// FREQ=DAILY|WEEKLY|MONTHLY|YEARLY with INTERVAL, COUNT, UNTIL and (weekly
// only) BYDAY. Occurrences keep their wall-clock time in the event's zone, so
// a 09:00 meeting stays at 09:00 across daylight-saving changes.
package recur

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Rule struct {
	Freq     string
	Interval int
	Count    int       // 0: unbounded
	Until    time.Time // zero: unbounded
	ByDay    []time.Weekday
}

var weekdayCodes = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// Parse reads a rule such as "FREQ=WEEKLY;BYDAY=MO,WE;COUNT=10". A leading
// "RRULE:" is accepted. An empty string is not a rule.
func Parse(s string) (Rule, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "RRULE:"))
	if s == "" {
		return Rule{}, fmt.Errorf("empty rrule")
	}
	r := Rule{Interval: 1}
	for _, part := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return Rule{}, fmt.Errorf("rrule: bad part %q", part)
		}
		switch strings.ToUpper(k) {
		case "FREQ":
			r.Freq = strings.ToUpper(v)
			switch r.Freq {
			case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
			default:
				return Rule{}, fmt.Errorf("rrule: unsupported FREQ %q", v)
			}
		case "INTERVAL":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return Rule{}, fmt.Errorf("rrule: bad INTERVAL %q", v)
			}
			r.Interval = n
		case "COUNT":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return Rule{}, fmt.Errorf("rrule: bad COUNT %q", v)
			}
			r.Count = n
		case "UNTIL":
			t, err := parseUntil(v)
			if err != nil {
				return Rule{}, err
			}
			r.Until = t
		case "BYDAY":
			for _, d := range strings.Split(v, ",") {
				wd, ok := weekdayCodes[strings.ToUpper(strings.TrimSpace(d))]
				if !ok {
					return Rule{}, fmt.Errorf("rrule: bad BYDAY %q", d)
				}
				r.ByDay = append(r.ByDay, wd)
			}
		default:
			return Rule{}, fmt.Errorf("rrule: unsupported part %q", k)
		}
	}
	if r.Freq == "" {
		return Rule{}, fmt.Errorf("rrule: FREQ is required")
	}
	if r.Count > 0 && !r.Until.IsZero() {
		return Rule{}, fmt.Errorf("rrule: COUNT and UNTIL are mutually exclusive")
	}
	if len(r.ByDay) > 0 && r.Freq != "WEEKLY" {
		return Rule{}, fmt.Errorf("rrule: BYDAY is only supported with FREQ=WEEKLY")
	}
	return r, nil
}

func parseUntil(v string) (time.Time, error) {
	for _, layout := range []string{"20060102T150405Z", "20060102"} {
		if t, err := time.Parse(layout, v); err == nil {
			if layout == "20060102" {
				t = t.Add(24*time.Hour - time.Nanosecond) // the whole day counts
			}
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("rrule: bad UNTIL %q", v)
}

// maxPeriods bounds the scan so a hostile or absurd rule cannot spin forever.
const maxPeriods = 200000

// Occurrences returns the start times of every occurrence in [from, to), at
// most limit of them (limit <= 0: 1000). start is the first occurrence and
// carries the zone the rule is expanded in.
func (r Rule) Occurrences(start, from, to time.Time, limit int) []time.Time {
	if limit <= 0 {
		limit = 1000
	}
	loc := start.Location()
	var out []time.Time
	emitted := 0
	for k := 0; k < maxPeriods; k++ {
		periodStart, cands := r.period(start, loc, k)
		if !periodStart.Before(to) {
			break
		}
		for _, c := range cands {
			if c.Before(start) {
				continue
			}
			if !r.Until.IsZero() && c.After(r.Until) {
				return out
			}
			emitted++
			if r.Count > 0 && emitted > r.Count {
				return out
			}
			if !c.Before(from) && c.Before(to) {
				out = append(out, c)
				if len(out) >= limit {
					return out
				}
			}
		}
	}
	return out
}

// period returns the first instant of the k-th recurrence period and the
// occurrences inside it, in order.
func (r Rule) period(start time.Time, loc *time.Location, k int) (time.Time, []time.Time) {
	h, mi, s := start.Clock()
	ns := start.Nanosecond()
	at := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, h, mi, s, ns, loc)
	}
	y, m, d := start.Date()
	switch r.Freq {
	case "DAILY":
		t := at(y, m, d+k*r.Interval)
		return t, []time.Time{t}
	case "WEEKLY":
		if len(r.ByDay) == 0 {
			t := at(y, m, d+7*k*r.Interval)
			return t, []time.Time{t}
		}
		// Weeks start on Monday (RFC 5545 default WKST).
		back := (int(start.Weekday()) + 6) % 7
		monday := d - back + 7*k*r.Interval
		days := make([]int, 0, len(r.ByDay))
		for _, wd := range r.ByDay {
			days = append(days, (int(wd)+6)%7)
		}
		sort.Ints(days)
		var out []time.Time
		prev := -1
		for _, off := range days {
			if off == prev {
				continue
			}
			prev = off
			out = append(out, at(y, m, monday+off))
		}
		return at(y, m, monday), out
	case "MONTHLY":
		first := at(y, m+time.Month(k*r.Interval), 1)
		t := at(y, m+time.Month(k*r.Interval), d)
		if t.Day() != d { // e.g. the 31st in a 30-day month: skipped, per RFC
			return first, nil
		}
		return first, []time.Time{t}
	default: // YEARLY
		first := at(y+k*r.Interval, m, 1)
		t := at(y+k*r.Interval, m, d)
		if t.Day() != d || t.Month() != m { // Feb 29 in a common year
			return first, nil
		}
		return first, []time.Time{t}
	}
}
