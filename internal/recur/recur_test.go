package recur

import (
	"testing"
	"time"
)

func mustRule(t *testing.T, s string) Rule {
	t.Helper()
	r, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return r
}

func days(ts []time.Time) []string {
	out := make([]string, len(ts))
	for i, x := range ts {
		out[i] = x.Format("01-02")
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDailyCountAndWindow(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, loc)
	r := mustRule(t, "FREQ=DAILY;COUNT=5")
	got := days(r.Occurrences(start, time.Date(2026, 1, 2, 0, 0, 0, 0, loc), time.Date(2026, 2, 1, 0, 0, 0, 0, loc), 0))
	if want := []string{"01-02", "01-03", "01-04", "01-05"}; !eq(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWeeklyByDay(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 1, 5, 10, 0, 0, 0, loc) // a Monday
	r := mustRule(t, "FREQ=WEEKLY;BYDAY=MO,WE;COUNT=5")
	got := days(r.Occurrences(start, start, start.AddDate(0, 3, 0), 0))
	if want := []string{"01-05", "01-07", "01-12", "01-14", "01-19"}; !eq(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWeeklyByDayStartMidWeek(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 1, 7, 10, 0, 0, 0, loc) // Wednesday: the Monday of that week precedes start
	r := mustRule(t, "FREQ=WEEKLY;BYDAY=MO,WE;COUNT=3")
	got := days(r.Occurrences(start, start, start.AddDate(0, 3, 0), 0))
	if want := []string{"01-07", "01-12", "01-14"}; !eq(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestMonthlySkipsShortMonths(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 1, 31, 8, 0, 0, 0, loc)
	r := mustRule(t, "FREQ=MONTHLY;COUNT=3")
	got := days(r.Occurrences(start, start, start.AddDate(1, 0, 0), 0))
	if want := []string{"01-31", "03-31", "05-31"}; !eq(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestYearlyLeapDay(t *testing.T) {
	loc := time.UTC
	start := time.Date(2024, 2, 29, 8, 0, 0, 0, loc)
	r := mustRule(t, "FREQ=YEARLY;COUNT=2")
	got := r.Occurrences(start, start, start.AddDate(10, 0, 0), 0)
	if len(got) != 2 || got[1].Year() != 2028 {
		t.Fatalf("got %v", got)
	}
}

func TestUntilIsInclusiveDay(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, loc)
	r := mustRule(t, "FREQ=DAILY;UNTIL=20260103")
	got := days(r.Occurrences(start, start, start.AddDate(0, 1, 0), 0))
	if want := []string{"01-01", "01-02", "01-03"}; !eq(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWallClockSurvivesDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	start := time.Date(2026, 3, 7, 9, 0, 0, 0, ny) // DST starts 2026-03-08
	r := mustRule(t, "FREQ=DAILY;COUNT=3")
	for _, o := range r.Occurrences(start, start, start.AddDate(0, 0, 10), 0) {
		if o.Hour() != 9 {
			t.Fatalf("occurrence drifted to %v", o)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{"", "FREQ=HOURLY", "FREQ=DAILY;COUNT=0", "FREQ=DAILY;COUNT=2;UNTIL=20260101",
		"FREQ=MONTHLY;BYDAY=MO", "COUNT=3", "FREQ=DAILY;BYSETPOS=1", "FREQ=WEEKLY;BYDAY=XX"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) should fail", s)
		}
	}
}

func TestLimit(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	r := mustRule(t, "FREQ=DAILY")
	if got := r.Occurrences(start, start, start.AddDate(10, 0, 0), 7); len(got) != 7 {
		t.Fatalf("limit not applied: %d", len(got))
	}
}
