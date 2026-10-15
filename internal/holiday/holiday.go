// Package holiday is the statutory holiday calendar alarms follow: the days
// off (法定节假日) and the weekend days worked instead (调休补班), per region.
// Only mainland China ("CN") has data for now.
//
// The data is embedded from data/<year>.json, copied unchanged from
// github.com/NateScarlet/holiday-cn (each file's $id and papers name its source
// and the State Council notice it follows), the same files the SmartAlarm
// server shipped. To add a year, drop its file from that repository into
// data/; nothing else changes. A day the data does not list is an ordinary
// day: Monday to Friday work, Saturday and Sunday off.
package holiday

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed data/*.json
var dataFS embed.FS

// RegionCN is mainland China, the one region with a calendar.
const RegionCN = "CN"

// Kinds of listed days.
const (
	KindHoliday = "holiday" // a statutory day off, 调休 rest days included
	KindWorkday = "workday" // a weekend day worked instead (调休补班)
)

// Day is one listed day.
type Day struct {
	Date string `json:"date"` // YYYY-MM-DD, local to the region
	Kind string `json:"kind"` // holiday or workday
	Name string `json:"name"` // e.g. 国庆节
}

// Calendar answers what kind of day a date is in one region. A nil Calendar
// knows no holidays: weekdays work, weekends rest.
type Calendar struct {
	days  map[string]Day
	years []int
}

// New builds a calendar from listed days; later days with the same date win.
func New(days []Day) *Calendar {
	c := &Calendar{days: map[string]Day{}}
	seen := map[int]bool{}
	for _, d := range days {
		c.days[d.Date] = d
		if y, ok := yearOf(d.Date); ok && !seen[y] {
			seen[y] = true
			c.years = append(c.years, y)
		}
	}
	sort.Ints(c.years)
	return c
}

func yearOf(date string) (int, bool) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0, false
	}
	return t.Year(), true
}

// IsHoliday reports whether date (YYYY-MM-DD) is a listed day off.
func (c *Calendar) IsHoliday(date string) bool {
	if c == nil {
		return false
	}
	return c.days[date].Kind == KindHoliday
}

// IsWorkday reports whether date is a working day: a listed workday, or a
// Monday to Friday that is not a listed day off.
func (c *Calendar) IsWorkday(date string, weekday time.Weekday) bool {
	if c != nil {
		if d, ok := c.days[date]; ok {
			return d.Kind == KindWorkday
		}
	}
	return weekday != time.Saturday && weekday != time.Sunday
}

// Lookup returns the listed day for date, if any.
func (c *Calendar) Lookup(date string) (Day, bool) {
	if c == nil {
		return Day{}, false
	}
	d, ok := c.days[date]
	return d, ok
}

// Year lists the year's days by date; empty when the year has no data.
func (c *Calendar) Year(year int) []Day {
	out := []Day{}
	if c == nil {
		return out
	}
	prefix := fmt.Sprintf("%04d-", year)
	for date, d := range c.days {
		if strings.HasPrefix(date, prefix) {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// Years lists the years with data, oldest first.
func (c *Calendar) Years() []int {
	if c == nil {
		return nil
	}
	return append([]int(nil), c.years...)
}

// Version changes whenever the data does: clients keep a copy of the days and
// fetch again when it moves.
func (c *Calendar) Version() string {
	parts := []string{"holiday-cn"}
	for _, y := range c.Years() {
		parts = append(parts, fmt.Sprint(y))
	}
	return strings.Join(parts, ".")
}

// yearFile is the holiday-cn schema.
type yearFile struct {
	Year int `json:"year"`
	Days []struct {
		Name     string `json:"name"`
		Date     string `json:"date"`
		IsOffDay bool   `json:"isOffDay"`
	} `json:"days"`
}

var cn = sync.OnceValue(func() *Calendar {
	entries, err := dataFS.ReadDir("data")
	if err != nil {
		panic(fmt.Sprintf("holiday: %v", err))
	}
	var days []Day
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := dataFS.ReadFile("data/" + e.Name())
		if err != nil {
			panic(fmt.Sprintf("holiday: %s: %v", e.Name(), err))
		}
		var y yearFile
		if err := json.Unmarshal(raw, &y); err != nil || y.Year == 0 {
			panic(fmt.Sprintf("holiday: %s is not a holiday-cn year file: %v", e.Name(), err))
		}
		for _, d := range y.Days {
			kind := KindWorkday
			if d.IsOffDay {
				kind = KindHoliday
			}
			days = append(days, Day{Date: d.Date, Kind: kind, Name: d.Name})
		}
	}
	return New(days)
})

// ForRegion is the region's calendar; nil (no holidays) for any region but CN.
func ForRegion(region string) *Calendar {
	if region == RegionCN {
		return cn()
	}
	return nil
}
