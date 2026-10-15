package alarm

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liliang-cn/noted/internal/holiday"
)

type fixtures struct {
	WindowDays     int                      `json:"windowDays"`
	MaxOccurrences int                      `json:"maxOccurrences"`
	DefaultEndTime string                   `json:"defaultEndTime"`
	Holidays       map[string][]holiday.Day `json:"holidays"`
	Cases          []struct {
		Name  string `json:"name"`
		Now   string `json:"now"`
		Alarm struct {
			Rule
			Enabled *bool `json:"enabled"`
		} `json:"alarm"`
		Limit    int      `json:"limit"`
		Expected []string `json:"expected"`
		Total    int      `json:"total"`
	} `json:"cases"`
}

func loadFixtures(t *testing.T) fixtures {
	t.Helper()
	raw, err := os.ReadFile("../../docs/alarm-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixtures
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestFixtures runs every shared case (docs/alarm-fixtures.json), the ones the
// phone's engine runs too.
func TestFixtures(t *testing.T) {
	f := loadFixtures(t)
	if f.WindowDays != WindowDays || f.MaxOccurrences != MaxOccurrences || f.DefaultEndTime != DefaultEndTime {
		t.Fatalf("fixture constants %d/%d/%s differ from the engine's", f.WindowDays, f.MaxOccurrences, f.DefaultEndTime)
	}
	cal := holiday.New(f.Holidays["CN"])
	if len(f.Cases) < 30 {
		t.Fatalf("only %d cases", len(f.Cases))
	}
	for _, c := range f.Cases {
		t.Run(c.Name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, c.Now)
			if err != nil {
				t.Fatal(err)
			}
			// The cases are written canonically: normalizing changes nothing.
			r := c.Alarm.Rule
			canon := r
			if canon.Schedule.Recurrence != nil {
				rc := *canon.Schedule.Recurrence
				rc.ByDay = append([]string(nil), rc.ByDay...)
				rc.ByTime = append([]string(nil), rc.ByTime...)
				canon.Schedule.Recurrence = &rc
			}
			if err := Normalize(&canon, ""); err != nil {
				t.Fatalf("normalize: %v", err)
			}
			a, _ := json.Marshal(r)
			b, _ := json.Marshal(canon)
			if strings.ReplaceAll(string(a), `"byTime":[],`, "") != strings.ReplaceAll(string(b), `"byTime":[],`, "") {
				t.Fatalf("not canonical:\n got %s\nwant %s", a, b)
			}

			run := func(limit int) []time.Time {
				if c.Alarm.Enabled != nil && !*c.Alarm.Enabled {
					return nil
				}
				return Occurrences(r, cal, now, limit)
			}
			got := run(c.Limit)
			if len(got) != len(c.Expected) {
				t.Fatalf("got %d rings %v, want %v", len(got), fmtTimes(got), c.Expected)
			}
			for i, w := range c.Expected {
				wt, err := time.Parse(time.RFC3339, w)
				if err != nil {
					t.Fatal(err)
				}
				if !got[i].Equal(wt) {
					t.Fatalf("ring %d: got %s, want %s (all: %v)", i, got[i].Format(time.RFC3339), w, fmtTimes(got))
				}
			}
			if all := run(0); len(all) != c.Total {
				t.Fatalf("total: got %d, want %d (last %v)", len(all), c.Total, fmtTimes(all[max(len(all)-3, 0):]))
			}
		})
	}
}

func fmtTimes(ts []time.Time) []string {
	out := make([]string, 0, len(ts))
	for _, v := range ts {
		out = append(out, v.Format(time.RFC3339))
	}
	return out
}

// The embedded data is what the fixtures carry for 2026.
func TestEmbeddedHolidaysMatchFixtures(t *testing.T) {
	f := loadFixtures(t)
	got := holiday.ForRegion("CN").Year(2026)
	want := f.Holidays["CN"]
	if len(got) != len(want) || len(got) == 0 {
		t.Fatalf("2026: %d embedded days, %d in the fixtures", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("day %d: embedded %v, fixtures %v", i, got[i], want[i])
		}
	}
	if y := holiday.ForRegion("CN").Years(); len(y) < 2 || y[0] != 2025 || y[1] != 2026 {
		t.Fatalf("years %v", y)
	}
	if holiday.ForRegion("") != nil || holiday.ForRegion("US") != nil {
		t.Fatal("only CN has a calendar")
	}
}

func TestNormalize(t *testing.T) {
	at := time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC)
	good := []struct {
		name string
		in   Rule
		want string // the normalized schedule as JSON
	}{
		{"defaults", Rule{Schedule: Schedule{Kind: KindRecurring, Anchor: "2026-10-14", Time: "7:05", Recurrence: &Recurrence{Freq: FreqWeekly}}},
			`{"kind":"recurring","anchor":"2026-10-14","time":"07:05","timeZone":"Asia/Shanghai","recurrence":{"freq":"weekly","interval":1,"byDay":["WED"]}}`},
		{"weekdays sorted and deduplicated", Rule{Schedule: Schedule{Kind: KindRecurring, Anchor: "2026-10-14", Time: "15:15", Recurrence: &Recurrence{Freq: FreqWeekly, Interval: 2, ByDay: []string{"thu", "MO", "Monday"}}}},
			`{"kind":"recurring","anchor":"2026-10-14","time":"15:15","timeZone":"Asia/Shanghai","recurrence":{"freq":"weekly","interval":2,"byDay":["MON","THU"]}}`},
		{"monthly day from the anchor", Rule{Schedule: Schedule{Kind: KindRecurring, Anchor: "2026-10-15", Time: "09:00", Recurrence: &Recurrence{Freq: FreqMonthly}}},
			`{"kind":"recurring","anchor":"2026-10-15","time":"09:00","timeZone":"Asia/Shanghai","recurrence":{"freq":"monthly","interval":1,"byMonthDay":15}}`},
		{"byTime sorted", Rule{Schedule: Schedule{Kind: KindRecurring, Anchor: "2026-10-15", Recurrence: &Recurrence{Freq: FreqDaily, ByTime: []string{"18:00", "8:00", "12:00", "08:00"}}}},
			`{"kind":"recurring","anchor":"2026-10-15","timeZone":"Asia/Shanghai","recurrence":{"freq":"daily","interval":1,"byTime":["08:00","12:00","18:00"]}}`},
		{"oneShot from anchor and time", Rule{Schedule: Schedule{Kind: KindOneShot, Anchor: "2026-10-14", Time: "07:00"}},
			`{"kind":"oneShot","at":"2026-10-14T07:00:00+08:00","timeZone":"Asia/Shanghai"}`},
		{"oneShot in its zone", Rule{Schedule: Schedule{Kind: KindOneShot, At: &at, TimeZone: "Asia/Tokyo"}},
			`{"kind":"oneShot","at":"2026-10-14T16:00:00+09:00","timeZone":"Asia/Tokyo"}`},
	}
	for _, c := range good {
		r := c.in
		if err := Normalize(&r, "Asia/Shanghai"); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if b, _ := json.Marshal(r.Schedule); string(b) != c.want {
			t.Fatalf("%s:\n got %s\nwant %s", c.name, b, c.want)
		}
		if r.HolidayPolicy != PolicyNone || r.Region != "" {
			t.Fatalf("%s: policy %q region %q", c.name, r.HolidayPolicy, r.Region)
		}
	}

	daily := func(mut func(*Rule)) Rule {
		r := Rule{Schedule: Schedule{Kind: KindRecurring, Anchor: "2026-10-14", Time: "07:00", Recurrence: &Recurrence{Freq: FreqDaily}}}
		mut(&r)
		return r
	}
	bad := []struct {
		r    Rule
		want string
	}{
		{Rule{}, "schedule.kind is required"},
		{Rule{Schedule: Schedule{Kind: "daily"}}, `schedule.kind "daily" is unknown`},
		{Rule{Schedule: Schedule{Kind: KindOneShot}}, "needs at"},
		{daily(func(r *Rule) {
			r.Schedule.Recurrence.ByTime = []string{"08:00"}
			r.Schedule.Time = ""
			r.Schedule.Recurrence.Freq = FreqWeekly
		}), "byTime (several times a day) only works with freq daily (got weekly)"},
		{daily(func(r *Rule) { r.Schedule.Recurrence.ByTime = []string{"08:00"} }), "not both"},
		{daily(func(r *Rule) { r.Schedule.Recurrence.ByDay = []string{"MON"} }), "byDay only works with freq weekly"},
		{daily(func(r *Rule) { r.Schedule.Recurrence.ByMonthDay = 3 }), "byMonthDay only works with freq monthly"},
		{daily(func(r *Rule) { r.Schedule.Recurrence.EndTime = "18:00" }), "endTime only works with freq minutely or hourly"},
		{daily(func(r *Rule) { r.Schedule.Anchor = "" }), "needs anchor"},
		{daily(func(r *Rule) { r.Schedule.Anchor = "tomorrow" }), `anchor "tomorrow" is not a date`},
		{daily(func(r *Rule) { r.Schedule.Time = "25:00" }), `time "25:00" is not a time of day`},
		{daily(func(r *Rule) { r.Schedule.Time = "" }), "needs time"},
		{daily(func(r *Rule) { r.Schedule.Recurrence = nil }), "needs recurrence"},
		{daily(func(r *Rule) { r.Schedule.Recurrence.Freq = "yearly" }), "yearly alarms are not supported"},
		{daily(func(r *Rule) { r.Schedule.Recurrence.Interval = -1 }), "interval must be"},
		{daily(func(r *Rule) { r.Schedule.Recurrence.Until = "2026-10-01" }), "is before the anchor"},
		{daily(func(r *Rule) { r.Schedule.TimeZone = "Mars/Base" }), "not an IANA zone"},
		{daily(func(r *Rule) { r.HolidayPolicy = "holidays" }), `holidayPolicy "holidays" is unknown`},
		{daily(func(r *Rule) { r.Region = "US" }), `region "US" has no holiday calendar`},
		{daily(func(r *Rule) { r.Schedule.Kind = KindOneShot }), "takes no recurrence"},
		{daily(func(r *Rule) { r.Schedule.Recurrence.Freq = FreqHourly; r.Schedule.Time = "22:30" }), "after the default endTime 22:00"},
		{daily(func(r *Rule) {
			r.Schedule.Recurrence.Freq = FreqMinutely
			r.Schedule.Recurrence.EndTime = "08:00"
			r.Schedule.Time = "09:00"
		}), "endTime 08:00 is before time 09:00"},
		{daily(func(r *Rule) {
			r.Schedule.Recurrence.Freq = FreqWeekly
			r.Schedule.Recurrence.ByDay = []string{"周一"}
		}), "is not a weekday"},
	}
	for _, c := range bad {
		r := c.r
		err := Normalize(&r, "Asia/Shanghai")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("want an error with %q, got %v", c.want, err)
		}
	}
}

func TestDescribe(t *testing.T) {
	r := Rule{Schedule: Schedule{Kind: KindRecurring, Anchor: "2026-10-12", Time: "15:15", Recurrence: &Recurrence{Freq: FreqWeekly, Interval: 2, ByDay: []string{"MON"}}},
		HolidayPolicy: PolicyWorkdayOnly, Region: "CN"}
	if err := Normalize(&r, "Asia/Shanghai"); err != nil {
		t.Fatal(err)
	}
	for lang, want := range map[string]string{"zh": "每隔一周的周一 15:15", "en": "every other week on Mon at 15:15"} {
		if got := Describe(r.Schedule, lang); got != want {
			t.Fatalf("%s: got %q, want %q", lang, got, want)
		}
	}
	if got := DescribePolicy(r.HolidayPolicy, r.Region, "zh"); got != "仅工作日（按法定节假日和调休补班）" {
		t.Fatalf("policy: %q", got)
	}
	r.Schedule.Recurrence = &Recurrence{Freq: FreqDaily, ByTime: []string{"08:00", "12:00"}, Until: "2026-12-31"}
	r.Schedule.Time = ""
	if got := Describe(r.Schedule, "zh"); got != "每天 08:00、12:00，到 2026-12-31 为止" {
		t.Fatalf("daily: %q", got)
	}
	r.Schedule.Recurrence = &Recurrence{Freq: FreqMinutely, Interval: 30}
	r.Schedule.Time = "09:00"
	if got := Describe(r.Schedule, "zh"); got != "每 30 分钟（09:00–22:00），仅 2026-10-12 当天" {
		t.Fatalf("minutely: %q", got)
	}
}
