package plan

import (
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

type busy struct{ start, end time.Time }

// busyFrom turns event occurrences into blocks of time. All-day events do not
// block: a birthday should not stop you from exercising.
func busyFrom(occ []store.Occurrence) []busy {
	var out []busy
	for _, o := range occ {
		if o.Event.AllDay {
			continue
		}
		out = append(out, busy{o.Start, o.End})
	}
	return out
}

func roundUp(t time.Time, step time.Duration) time.Time {
	r := t.Truncate(step)
	if r.Before(t) {
		r = r.Add(step)
	}
	return r
}

// freeSlot finds the first stretch of dur, at least from `from`, that fits
// inside [startHour, endHour) of some day in the next `days` days (local time
// in loc) without touching any busy block.
func freeSlot(blocks []busy, loc *time.Location, from time.Time, days int, dur time.Duration, startHour, endHour int) (time.Time, bool) {
	from = from.In(loc)
	day0 := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	for d := 0; d < days; d++ {
		day := day0.AddDate(0, 0, d)
		open := time.Date(day.Year(), day.Month(), day.Day(), startHour, 0, 0, 0, loc)
		closeAt := time.Date(day.Year(), day.Month(), day.Day(), endHour, 0, 0, 0, loc)
		cur := open
		if from.After(cur) {
			cur = roundUp(from, 30*time.Minute)
		}
		for !cur.Add(dur).After(closeAt) {
			var hit *busy
			for i := range blocks {
				if blocks[i].start.Before(cur.Add(dur)) && blocks[i].end.After(cur) {
					hit = &blocks[i]
					break
				}
			}
			if hit == nil {
				return cur, true
			}
			cur = roundUp(hit.end.In(loc), 30*time.Minute)
		}
	}
	return time.Time{}, false
}
