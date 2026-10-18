package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liliang-cn/noted/internal/alarm"
)

var shanghai, _ = time.LoadLocation("Asia/Shanghai")

func sh(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, shanghai)
}

func intp(v int) *int { return &v }

func TestNotifyDefaults(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	start := sh(2026, 10, 20, 15, 0)

	for _, c := range []struct {
		name   string
		e      Event
		notify string
		lead   *int
	}{
		{"timed, nothing said", Event{Title: "开会", Start: start}, NotifyReminder, intp(10)},
		{"timed, a lead", Event{Title: "牙医", Start: start, RemindBefore: intp(30)}, NotifyReminder, intp(30)},
		{"all-day", Event{Title: "生日", Start: start, AllDay: true}, NotifyNone, nil},
		{"said none", Event{Title: "记一下", Start: start, Notify: NotifyNone}, NotifyNone, nil},
		{"said alarm", Event{Title: "赶飞机", Start: start, Notify: NotifyAlarm}, NotifyAlarm, nil},
		{"said notification, a lead", Event{Title: "交周报", Start: start, Notify: NotifyNotification, RemindBefore: intp(60)}, NotifyNotification, intp(60)},
	} {
		c.e.UserID = uid
		e, err := s.CreateEvent(ctx, c.e)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if e.Notify != c.notify || (c.lead == nil) != (e.RemindBefore == nil) || (c.lead != nil && *e.RemindBefore != *c.lead) {
			t.Fatalf("%s: notify %q lead %v", c.name, e.Notify, e.RemindBefore)
		}
	}
	if _, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "x", Start: start, Notify: "app"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown notify: %v", err)
	}

	// Alarms default to ringing; mode and notify both say how; mode reads back
	// as alarm only for an alarm, so older phones never ring anything else.
	a, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "起床", Enabled: true, Schedule: biweekly()})
	if err != nil || a.Notify != NotifyAlarm || a.Mode != alarm.ModeAlarm || a.Source != SourceAlarm {
		t.Fatalf("alarm: %+v %v", a, err)
	}
	a, err = s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "喝水", Mode: alarm.ModeReminder, Enabled: true, Schedule: biweekly()})
	if err != nil || a.Notify != NotifyReminder || a.Mode != alarm.ModeReminder {
		t.Fatalf("reminder alarm: %+v %v", a, err)
	}
	a, err = s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "看看", Mode: alarm.ModeAlarm, Notify: NotifyNotification, Enabled: true, Schedule: biweekly()})
	if err != nil || a.Notify != NotifyNotification || a.Mode != alarm.ModeReminder {
		t.Fatalf("notification alarm: %+v %v", a, err)
	}
	n := NotifyNone
	a, err = s.UpdateAlarm(ctx, uid, a.ID, AlarmPatch{Notify: &n})
	if err != nil || a.Notify != NotifyNone || a.Mode != alarm.ModeReminder || len(a.NextFires(time.Time{}, 3)) != 0 {
		t.Fatalf("silenced alarm: %+v %v", a, err)
	}

	// To-dos: told when they have a remind time; a notify without one is told at due.
	due := sh(2026, 10, 21, 9, 0)
	remind := due.Add(-time.Hour)
	tk, err := s.CreateTask(ctx, Task{UserID: uid, Title: "交房租", Due: &due, Remind: &remind})
	if err != nil || tk.Notify != NotifyReminder {
		t.Fatalf("task with remind: %+v %v", tk, err)
	}
	tk, err = s.CreateTask(ctx, Task{UserID: uid, Title: "看书", Due: &due})
	if err != nil || tk.Notify != NotifyNone || tk.Remind != nil {
		t.Fatalf("task with due only: %+v %v", tk, err)
	}
	tk, err = s.CreateTask(ctx, Task{UserID: uid, Title: "取快递", Due: &due, Notify: NotifyAlarm})
	if err != nil || tk.Notify != NotifyAlarm || tk.Remind == nil || !tk.Remind.Equal(due) {
		t.Fatalf("task told at due: %+v %v", tk, err)
	}
	if _, err := s.CreateTask(ctx, Task{UserID: uid, Title: "某天", Notify: NotifyReminder}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("told with no time: %v", err)
	}
	if _, err := s.CreateTask(ctx, Task{UserID: uid, Title: "某天"}); err != nil {
		t.Fatalf("plain to-do: %v", err)
	}
}

func TestNotifyFollowsTheLead(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	e, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "开会", Start: sh(2026, 10, 20, 15, 0), RemindBefore: intp(15)})
	if err != nil {
		t.Fatal(err)
	}
	var none *int
	e, err = s.UpdateEvent(ctx, uid, e.ID, EventPatch{RemindBefore: &none})
	if err != nil || e.Notify != NotifyNone {
		t.Fatalf("lead cleared: %+v %v", e, err)
	}
	five := intp(5)
	e, err = s.UpdateEvent(ctx, uid, e.ID, EventPatch{RemindBefore: &five})
	if err != nil || e.Notify != NotifyReminder || *e.RemindBefore != 5 {
		t.Fatalf("lead set: %+v %v", e, err)
	}
	alarmN := NotifyAlarm
	e, err = s.UpdateEvent(ctx, uid, e.ID, EventPatch{Notify: &alarmN, RemindBefore: &none})
	if err != nil || e.Notify != NotifyAlarm || e.RemindBefore != nil {
		t.Fatalf("notify given with the lead: %+v %v", e, err)
	}

	due := sh(2026, 10, 21, 9, 0)
	tk, err := s.CreateTask(ctx, Task{UserID: uid, Title: "看书", Due: &due})
	if err != nil {
		t.Fatal(err)
	}
	r := due.Add(-30 * time.Minute)
	rp := &r
	tk, err = s.UpdateTask(ctx, uid, tk.ID, TaskPatch{Remind: &rp})
	if err != nil || tk.Notify != NotifyReminder {
		t.Fatalf("remind set: %+v %v", tk, err)
	}
	var noTime *time.Time
	tk, err = s.UpdateTask(ctx, uid, tk.ID, TaskPatch{Remind: &noTime})
	if err != nil || tk.Notify != NotifyNone {
		t.Fatalf("remind cleared: %+v %v", tk, err)
	}
	// Told at its due time, it moves with the due time.
	tk, err = s.UpdateTask(ctx, uid, tk.ID, TaskPatch{Notify: &alarmN})
	if err != nil || tk.Remind == nil || !tk.Remind.Equal(due) {
		t.Fatalf("told at due: %+v %v", tk, err)
	}
	later := due.Add(2 * time.Hour)
	lp := &later
	tk, err = s.UpdateTask(ctx, uid, tk.ID, TaskPatch{Due: &lp})
	if err != nil || !tk.Remind.Equal(later) {
		t.Fatalf("due moved: %+v %v", tk, err)
	}
	item, err := s.GetEvent(ctx, uid, tk.ID)
	if err != nil || item.Notify != NotifyAlarm || item.RemindBefore == nil || *item.RemindBefore != 0 || !item.Start.Equal(later) {
		t.Fatalf("the to-do's schedule item: %+v %v", item, err)
	}
}

// Rows written before notify existed keep behaving as they did.
func TestNotifyMigration(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	silent, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "旧日程", Start: sh(2026, 10, 20, 15, 0), Notify: NotifyNone})
	if err != nil {
		t.Fatal(err)
	}
	told, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "旧提醒", Start: sh(2026, 10, 20, 16, 0), RemindBefore: intp(20)})
	if err != nil {
		t.Fatal(err)
	}
	due := sh(2026, 10, 21, 9, 0)
	remind := due.Add(-time.Hour)
	plain, err := s.CreateTask(ctx, Task{UserID: uid, Title: "旧待办", Due: &due})
	if err != nil {
		t.Fatal(err)
	}
	reminded, err := s.CreateTask(ctx, Task{UserID: uid, Title: "旧待办提醒", Due: &due, Remind: &remind})
	if err != nil {
		t.Fatal(err)
	}
	// What an older binary leaves: notify at the column default everywhere, an
	// alarm only in the alarms table, neither migration marked.
	sched, _ := alarm.MarshalSchedule(biweekly())
	for _, q := range []string{
		`UPDATE events SET notify = 'reminder', remind_before = NULL WHERE id = '` + silent.ID + `'`,
		`UPDATE events SET notify = 'app' WHERE id = '` + told.ID + `'`,
		`UPDATE tasks SET notify = 'reminder'`,
		`DELETE FROM migrations`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO alarms(id, user_id, label, mode, enabled, schedule, holiday_policy, region, note, project_id, space,
		original_text, created_ms, updated_ms) VALUES ('legacy-1', ?, '吃药', 'reminder', 1, ?, 'none', '', '', '', 'life', '', 1, 1)`, uid, sched); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateSchedule(ctx); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]string{silent.ID: NotifyNone, told.ID: NotifyReminder, plain.ID: NotifyNone, reminded.ID: NotifyReminder, "legacy-1": NotifyReminder} {
		e, err := s.GetEvent(ctx, uid, id)
		if err != nil || e.Notify != want {
			t.Fatalf("%s: notify %q, want %q (%v)", id, e.Notify, want, err)
		}
	}
	for id, want := range map[string]string{plain.ID: NotifyNone, reminded.ID: NotifyReminder} {
		tk, err := s.GetTask(ctx, uid, id)
		if err != nil || tk.Notify != want {
			t.Fatalf("task %s: notify %q, want %q (%v)", id, tk.Notify, want, err)
		}
	}
	// The default alarm list is what phones read before: the old alarm, as it was.
	as, _, err := s.ListAlarms(ctx, uid, AlarmFilter{})
	if err != nil || len(as) != 1 || as[0].ID != "legacy-1" || as[0].Mode != alarm.ModeReminder || as[0].Label != "吃药" {
		t.Fatalf("default list: %+v %v", as, err)
	}
	// A second run changes nothing.
	n := NotifyAlarm
	if _, err := s.UpdateEvent(ctx, uid, silent.ID, EventPatch{Notify: &n}); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateSchedule(ctx); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.GetEvent(ctx, uid, silent.ID); e.Notify != NotifyAlarm {
		t.Fatalf("rerun reset notify: %q", e.Notify)
	}

	// Rolling back writes the alarms back and unmarks both migrations.
	if err := s.RollbackSchedule(ctx); err != nil {
		t.Fatal(err)
	}
	var marks int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM migrations`).Scan(&marks); err != nil || marks != 0 {
		t.Fatalf("marks after rollback: %d %v", marks, err)
	}
}

func TestListFires(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)

	// 工作日 07:00 across 国庆 2026: 10-01..07 off, Saturday 10-10 worked (调休).
	wake, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "起床", Enabled: true, HolidayPolicy: alarm.PolicyWorkdayOnly, Region: "CN",
		Schedule: alarm.Schedule{Kind: alarm.KindRecurring, Anchor: "2026-09-01", Time: "07:00", TimeZone: "Asia/Shanghai",
			Recurrence: &alarm.Recurrence{Freq: alarm.FreqDaily, Interval: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	// A weekly meeting, skipping holidays, told 15 minutes before.
	meet, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "周会", Start: sh(2026, 9, 7, 10, 0), TimeZone: "Asia/Shanghai",
		RRule: "FREQ=WEEKLY;BYDAY=MO", RemindBefore: intp(15), HolidayPolicy: alarm.PolicySkipHoliday, Region: "CN"})
	if err != nil {
		t.Fatal(err)
	}
	due := sh(2026, 10, 9, 18, 0)
	remind := due.Add(-2 * time.Hour)
	rent, err := s.CreateTask(ctx, Task{UserID: uid, Title: "交房租", Due: &due, Remind: &remind, Notify: NotifyNotification})
	if err != nil {
		t.Fatal(err)
	}
	// Nobody is told about these.
	if _, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "国庆", Start: sh(2026, 10, 1, 0, 0), AllDay: true}); err != nil {
		t.Fatal(err)
	}
	offAt := sh(2026, 10, 2, 8, 0)
	off, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "关掉的", Enabled: false, Schedule: alarm.Schedule{Kind: alarm.KindOneShot,
		At: &offAt, TimeZone: "Asia/Shanghai"}})
	if err != nil || off.Enabled {
		t.Fatal(err)
	}
	doneDue := sh(2026, 10, 3, 9, 0)
	done, err := s.CreateTask(ctx, Task{UserID: uid, Title: "做完了", Due: &doneDue, Notify: NotifyReminder})
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err := s.UpdateTask(ctx, uid, done.ID, TaskPatch{Done: &yes}); err != nil {
		t.Fatal(err)
	}

	from, to := sh(2026, 9, 28, 0, 0), sh(2026, 10, 12, 0, 0)
	fs, version, err := s.ListFires(ctx, uid, from, to, FireFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range fs {
		got = append(got, f.Item.Title+" "+f.Item.Notify+" "+f.Fire.In(shanghai).Format("01-02 15:04"))
	}
	want := []string{
		"起床 alarm 09-28 07:00",
		"周会 reminder 09-28 09:45",
		"起床 alarm 09-29 07:00",
		"起床 alarm 09-30 07:00",
		// 10-01..07 are off: no wake-up, and Monday 10-05's meeting is skipped.
		"起床 alarm 10-08 07:00",
		"起床 alarm 10-09 07:00",
		"交房租 notification 10-09 16:00",
		"起床 alarm 10-10 07:00", // Saturday, worked instead (调休)
	}
	if len(got) != len(want) {
		t.Fatalf("fires:\n%v\nwant\n%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fire %d: %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
	if fs[1].Item.ID != meet.ID || !fs[1].At.Equal(sh(2026, 9, 28, 10, 0)) || fs[1].Item.Lead() != 15*time.Minute {
		t.Fatalf("meeting fire: %+v", fs[1])
	}
	if f := fs[6]; f.Item.ID != rent.ID || f.Item.Source != SourceTask || !f.At.Equal(due) {
		t.Fatalf("rent fire: %+v", f)
	}
	if _, v, _ := s.ListAlarms(ctx, uid, AlarmFilter{}); v != version {
		t.Fatalf("version %q, alarms' %q", version, v)
	}

	// One notify only.
	fs, _, err = s.ListFires(ctx, uid, from, to, FireFilter{Notify: NotifyAlarm})
	if err != nil || len(fs) != 6 || fs[0].Item.ID != wake.ID {
		t.Fatalf("alarms only: %d %v", len(fs), err)
	}
	if _, _, err := s.ListFires(ctx, uid, from, to, FireFilter{Notify: NotifyNone}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("none filter: %v", err)
	}
	if _, _, err := s.ListFires(ctx, uid, from, from.AddDate(0, 0, 40), FireFilter{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("long window: %v", err)
	}

	// Any change on the schedule moves the version.
	if _, err := s.UpdateTask(ctx, uid, rent.ID, TaskPatch{Notify: ptrString(NotifyNone)}); err != nil {
		t.Fatal(err)
	}
	if _, v, _ := s.ListFires(ctx, uid, from, to, FireFilter{}); v == version {
		t.Fatal("version did not move")
	}
}

func TestListAlarmsNotifyFilter(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	if _, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "起床", Enabled: true, Schedule: biweekly()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: "静音", Notify: NotifyNone, Enabled: true, Schedule: biweekly()}); err != nil {
		t.Fatal(err)
	}
	meet, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "开会", Start: sh(2026, 11, 2, 10, 0), Notify: NotifyNotification, RemindBefore: intp(10)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateEvent(ctx, Event{UserID: uid, Title: "生日", Start: sh(2026, 11, 3, 0, 0), AllDay: true}); err != nil {
		t.Fatal(err)
	}
	labels := func(f AlarmFilter) map[string]string {
		as, _, err := s.ListAlarms(ctx, uid, f)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, a := range as {
			out[a.Label] = a.Mode + "/" + a.Notify
		}
		return out
	}
	if got := labels(AlarmFilter{}); len(got) != 1 || got["起床"] != "alarm/alarm" {
		t.Fatalf("default: %v", got)
	}
	if got := labels(AlarmFilter{Notify: "all"}); len(got) != 2 || got["开会"] != "reminder/notification" {
		t.Fatalf("all: %v", got)
	}
	if got := labels(AlarmFilter{Notify: NotifyNone}); len(got) != 2 || got["静音"] == "" || got["生日"] == "" {
		t.Fatalf("none: %v", got)
	}
	a, err := s.GetAlarm(ctx, uid, meet.ID)
	if err != nil || a.Schedule.Kind != alarm.KindOneShot || a.RemindBefore != 10 ||
		a.NextFires(sh(2026, 11, 1, 0, 0), 1)[0].In(shanghai).Format("15:04") != "09:50" {
		t.Fatalf("event as alarm: %+v %v", a, err)
	}
}

// The server's reminder stream tells calendar events and to-dos as before,
// and alarms only when they asked for a plain notification.
func TestServerToldByNotify(t *testing.T) {
	ctx := context.Background()
	s, uid := newStore(t)
	now := sh(2026, 11, 2, 9, 0)
	soon := now.Add(5 * time.Minute)
	one := func(label, notify string) {
		if _, err := s.CreateAlarm(ctx, Alarm{UserID: uid, Label: label, Notify: notify, Enabled: true,
			Schedule: alarm.Schedule{Kind: alarm.KindOneShot, At: &now, TimeZone: "Asia/Shanghai"}}); err != nil {
			t.Fatal(err)
		}
	}
	one("响铃", NotifyAlarm)
	one("提醒", NotifyReminder)
	one("通知", NotifyNotification)
	for _, e := range []Event{
		{Title: "会议", Start: soon, RemindBefore: intp(10)},
		{Title: "会议闹钟", Start: soon, Notify: NotifyAlarm, RemindBefore: intp(10)},
		{Title: "不提醒", Start: soon, Notify: NotifyNone, RemindBefore: intp(10)},
	} {
		e.UserID = uid
		if _, err := s.CreateEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	rs, err := s.CollectDueReminders(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rs {
		got[r.Title] = r.Notify
	}
	if len(got) != 3 || got["通知"] != NotifyNotification || got["会议"] != NotifyReminder || got["会议闹钟"] != NotifyAlarm {
		t.Fatalf("told: %v", got)
	}
}

func ptrString(s string) *string { return &s }
