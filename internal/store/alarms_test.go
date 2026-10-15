package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/liliang-cn/noted/internal/alarm"
)

func biweekly() alarm.Schedule {
	return alarm.Schedule{Kind: alarm.KindRecurring, Anchor: "2026-10-12", Time: "15:15", TimeZone: "Asia/Shanghai",
		Recurrence: &alarm.Recurrence{Freq: alarm.FreqWeekly, Interval: 2, ByDay: []string{"mo"}}}
}

func TestAlarmCRUD(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	a, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: " 接三宝 ", Enabled: true, Schedule: biweekly(), HolidayPolicy: alarm.PolicyWorkdayOnly,
		Region: "cn", OriginalText: "每隔一周的周一 15:15 接三宝"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Label != "接三宝" || a.Mode != alarm.ModeAlarm || a.Space != SpaceLife || a.Region != "CN" ||
		a.Schedule.Recurrence.ByDay[0] != "MON" || !a.Enabled {
		t.Fatalf("created %+v", a)
	}
	got, err := s.GetAlarm(ctx, uid, a.ID)
	if err != nil || got.Label != a.Label || got.Schedule.Time != "15:15" || got.Schedule.Recurrence.Interval != 2 || got.HolidayPolicy != alarm.PolicyWorkdayOnly {
		t.Fatalf("get: %+v %v", got, err)
	}
	if next := got.NextFires(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), 2); len(next) != 2 || next[0].Format(time.RFC3339) != "2026-10-26T15:15:00+08:00" {
		t.Fatalf("next fires %v", next)
	}

	// A new schedule replaces the old one and keeps its zone when it names none.
	off, mode := false, alarm.ModeReminder
	sc := alarm.Schedule{Kind: alarm.KindOneShot, Anchor: "2026-10-14", Time: "07:00"}
	up, err := s.UpdateAlarm(ctx, uid, a.ID, AlarmPatch{Enabled: &off, Mode: &mode, Schedule: &sc})
	if err != nil {
		t.Fatal(err)
	}
	if up.Enabled || up.Mode != alarm.ModeReminder || up.Schedule.Kind != alarm.KindOneShot || up.Schedule.Recurrence != nil ||
		up.Schedule.At.Format(time.RFC3339) != "2026-10-14T07:00:00+08:00" || len(up.NextFires(time.Time{}, 5)) != 0 {
		t.Fatalf("updated %+v", up)
	}

	// Validation speaks plainly.
	bad := biweekly()
	bad.Recurrence.ByTime = []string{"08:00"}
	bad.Time = ""
	if _, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "x", Schedule: bad}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "only works with freq daily") {
		t.Fatalf("byTime with weekly: %v", err)
	}
	if _, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "", Schedule: biweekly()}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty label: %v", err)
	}
	if _, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "x", Mode: "loud", Schedule: biweekly()}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad mode: %v", err)
	}
	if _, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "x", ProjectID: "nope", Schedule: biweekly()}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown project: %v", err)
	}

	// Another user sees nothing.
	bob, _ := s.EnsureUser(ctx, "bob")
	if _, err := s.GetAlarm(ctx, bob.ID, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob reads alice's alarm: %v", err)
	}
	if err := s.DeleteAlarm(ctx, bob.ID, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob deletes alice's alarm: %v", err)
	}
	if err := s.DeleteAlarm(ctx, uid, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAlarm(ctx, uid, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestAlarmListFiltersAndVersion(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	v0, err := s.AlarmsVersion(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(ctx, Project{UserID: uid, Title: "School", Space: SpaceWork})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "接三宝", Enabled: true, ProjectID: p.ID, Schedule: biweekly()})
	if err != nil {
		t.Fatal(err)
	}
	if a.Space != SpaceWork {
		t.Fatalf("space %q: want the project's", a.Space)
	}
	b, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "起床", Enabled: false, Schedule: biweekly()})
	if err != nil {
		t.Fatal(err)
	}
	all, v1, err := s.ListAlarms(ctx, uid, AlarmFilter{})
	if err != nil || len(all) != 2 || all[0].ID != a.ID || v1 == v0 {
		t.Fatalf("list: %d %v %q", len(all), err, v1)
	}
	on := true
	for _, c := range []struct {
		f    AlarmFilter
		want string
	}{{AlarmFilter{Enabled: &on}, a.ID}, {AlarmFilter{Space: SpaceLife}, b.ID}, {AlarmFilter{ProjectID: p.ID}, a.ID}} {
		got, v, err := s.ListAlarms(ctx, uid, c.f)
		if err != nil || len(got) != 1 || got[0].ID != c.want || v != v1 {
			t.Fatalf("filter %+v: %v %v %q", c.f, got, err, v)
		}
	}
	if v, _ := s.AlarmsVersion(ctx, uid); v != v1 {
		t.Fatalf("version moved without a change: %q %q", v, v1)
	}

	// Every kind of change moves the version.
	label := "接三宝放学"
	if _, err := s.UpdateAlarm(ctx, uid, a.ID, AlarmPatch{Label: &label}); err != nil {
		t.Fatal(err)
	}
	v2, _ := s.AlarmsVersion(ctx, uid)
	if err := s.DeleteProject(ctx, uid, p.ID, true); err != nil {
		t.Fatal(err)
	}
	kept, err := s.GetAlarm(ctx, uid, a.ID)
	if err != nil || kept.ProjectID != "" {
		t.Fatalf("the alarm should leave the deleted project: %+v %v", kept, err)
	}
	v3, _ := s.AlarmsVersion(ctx, uid)
	if err := s.DeleteAlarm(ctx, uid, b.ID); err != nil {
		t.Fatal(err)
	}
	v4, _ := s.AlarmsVersion(ctx, uid)
	seen := map[string]bool{}
	for _, v := range []string{v0, v1, v2, v3, v4} {
		if seen[v] {
			t.Fatalf("a version repeated: %q in %v", v, []string{v0, v1, v2, v3, v4})
		}
		seen[v] = true
	}
	// Another user's changes do not move it.
	bob, _ := s.EnsureUser(ctx, "bob")
	if _, err := s.CreateAlarm(ctx, Alarm{UserID: bob.ID, Label: "x", Enabled: true, Schedule: biweekly()}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.AlarmsVersion(ctx, uid); v != v4 {
		t.Fatal("bob's alarm moved alice's version")
	}
}
