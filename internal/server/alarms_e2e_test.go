package server_test

import (
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAlarmsOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	al, hol := pb.NewAlarmServiceClient(conn), pb.NewHolidayServiceClient(conn)
	ctx := as(e.alice)
	sh, _ := time.LoadLocation("Asia/Shanghai")
	today := time.Now().In(sh).Format("2006-01-02")

	if _, err := al.ListAlarms(as("nope"), &pb.ListAlarmsRequest{}); code(err) != codes.Unauthenticated {
		t.Fatalf("no token: %v", err)
	}
	v0, err := al.GetAlarmsVersion(ctx, &pb.GetAlarmsVersionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	// Defaults: on, mode alarm, life, the CN calendar for a Shanghai alarm.
	a, err := al.CreateAlarm(ctx, &pb.CreateAlarmRequest{Alarm: &pb.Alarm{Label: "接三宝", HolidayPolicy: "workdayOnly",
		Schedule: &pb.AlarmSchedule{Kind: "recurring", Anchor: today, Time: "15:15", TimeZone: "Asia/Shanghai",
			Recurrence: &pb.AlarmRecurrence{Freq: "weekly", Interval: 2, ByDay: []string{"MON"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Id == "" || !a.GetEnabled() || a.Mode != "alarm" || a.GetRegion() != "CN" || a.Space != pb.Space_SPACE_LIFE ||
		a.Schedule.Recurrence.Interval != 2 || len(a.NextFires) == 0 || len(a.NextFires) > 5 {
		t.Fatalf("created %v", a)
	}
	for _, f := range a.NextFires {
		if l := f.AsTime().In(sh); l.Weekday() != time.Monday || l.Format("15:04") != "15:15" {
			t.Fatalf("a ring off the rule: %v", l)
		}
	}
	// The server's zone (UTC here) when none is named, and no holiday region outside China.
	b, err := al.CreateAlarm(ctx, &pb.CreateAlarmRequest{Alarm: &pb.Alarm{Label: "Stand-up", Mode: "reminder",
		Schedule: &pb.AlarmSchedule{Kind: "recurring", Anchor: today, Time: "09:30", Recurrence: &pb.AlarmRecurrence{Freq: "daily"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if b.Schedule.TimeZone != "UTC" || b.GetRegion() != "" || b.Mode != "reminder" || b.Schedule.Recurrence.Interval != 1 {
		t.Fatalf("defaults: %v", b)
	}
	// A one-shot in the past is kept but rings no more.
	past, err := al.CreateAlarm(ctx, &pb.CreateAlarmRequest{Alarm: &pb.Alarm{Label: "old", Enabled: ptr(false),
		Schedule: &pb.AlarmSchedule{Kind: "oneShot", At: timestamppb.New(time.Now().Add(-time.Hour))}}})
	if err != nil || len(past.NextFires) != 0 || past.GetEnabled() {
		t.Fatalf("past one-shot: %v %v", past, err)
	}

	// Validation errors are INVALID_ARGUMENT in plain words.
	for _, c := range []struct {
		a    *pb.Alarm
		want string
	}{
		{&pb.Alarm{Label: "x", Schedule: &pb.AlarmSchedule{Kind: "recurring", Anchor: today, Time: "08:00",
			Recurrence: &pb.AlarmRecurrence{Freq: "weekly", ByTime: []string{"08:00"}}}}, "only works with freq daily"},
		{&pb.Alarm{Label: "x", Schedule: &pb.AlarmSchedule{Kind: "recurring", Time: "08:00", Recurrence: &pb.AlarmRecurrence{Freq: "daily"}}}, "needs anchor"},
		{&pb.Alarm{Label: "x"}, "schedule is required"},
		{&pb.Alarm{Label: "x", Region: ptr("US"), Schedule: &pb.AlarmSchedule{Kind: "oneShot", At: timestamppb.Now()}}, "no holiday calendar"},
	} {
		_, err := al.CreateAlarm(ctx, &pb.CreateAlarmRequest{Alarm: c.a})
		if code(err) != codes.InvalidArgument || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("want INVALID_ARGUMENT with %q, got %v", c.want, err)
		}
	}

	list, err := al.ListAlarms(ctx, &pb.ListAlarmsRequest{})
	if err != nil || len(list.Alarms) != 3 || list.Alarms[0].Id != a.Id || list.Version == v0.Version {
		t.Fatalf("list: %v %v", list, err)
	}
	on, err := al.ListAlarms(ctx, &pb.ListAlarmsRequest{Enabled: ptr(true)})
	if err != nil || len(on.Alarms) != 2 || on.Version != list.Version {
		t.Fatalf("enabled only: %v %v", on, err)
	}

	// Turning one off empties its rings and moves the version.
	up, err := al.UpdateAlarm(ctx, &pb.UpdateAlarmRequest{Alarm: &pb.Alarm{Id: a.Id, Enabled: ptr(false)}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"enabled"}}})
	if err != nil || up.GetEnabled() || len(up.NextFires) != 0 || up.Schedule.Recurrence.Interval != 2 {
		t.Fatalf("turn off: %v %v", up, err)
	}
	v1, _ := al.GetAlarmsVersion(ctx, &pb.GetAlarmsVersionRequest{})
	if v1.Version == list.Version {
		t.Fatal("version did not move after an update")
	}
	// A new schedule replaces the whole one, keeping the zone.
	up, err = al.UpdateAlarm(ctx, &pb.UpdateAlarmRequest{Alarm: &pb.Alarm{Id: a.Id, Enabled: ptr(true), Region: ptr(""),
		Schedule: &pb.AlarmSchedule{Kind: "recurring", Anchor: today, Recurrence: &pb.AlarmRecurrence{Freq: "daily", ByTime: []string{"18:00", "08:00"}}}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"schedule", "enabled", "region"}}})
	if err != nil || up.Schedule.TimeZone != "Asia/Shanghai" || up.Schedule.Recurrence.Freq != "daily" || len(up.Schedule.Recurrence.ByDay) != 0 ||
		strings.Join(up.Schedule.Recurrence.ByTime, ",") != "08:00,18:00" || up.GetRegion() != "" || len(up.NextFires) != 5 {
		t.Fatalf("new schedule: %v %v", up, err)
	}
	if _, err := al.UpdateAlarm(ctx, &pb.UpdateAlarmRequest{Alarm: &pb.Alarm{Id: a.Id}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"next_fires"}}}); code(err) != codes.InvalidArgument {
		t.Fatalf("next_fires is read-only: %v", err)
	}

	// Another user can neither see nor touch them.
	if _, err := al.GetAlarm(as(e.bob), &pb.GetAlarmRequest{Id: a.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob reads: %v", err)
	}
	if _, err := al.DeleteAlarm(as(e.bob), &pb.DeleteAlarmRequest{Id: a.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob deletes: %v", err)
	}
	if l, _ := al.ListAlarms(as(e.bob), &pb.ListAlarmsRequest{}); len(l.Alarms) != 0 {
		t.Fatalf("bob lists %v", l.Alarms)
	}

	v2, _ := al.GetAlarmsVersion(ctx, &pb.GetAlarmsVersionRequest{})
	if _, err := al.DeleteAlarm(ctx, &pb.DeleteAlarmRequest{Id: b.Id}); err != nil {
		t.Fatal(err)
	}
	v3, _ := al.GetAlarmsVersion(ctx, &pb.GetAlarmsVersionRequest{})
	if v3.Version == v2.Version {
		t.Fatal("version did not move after a delete")
	}
	if _, err := al.GetAlarm(ctx, &pb.GetAlarmRequest{Id: b.Id}); code(err) != codes.NotFound {
		t.Fatalf("deleted: %v", err)
	}

	// The holiday calendar.
	h, err := hol.GetHolidays(ctx, &pb.GetHolidaysRequest{Year: 2026})
	if err != nil || h.Region != "CN" || h.Year != 2026 || len(h.Days) != 39 || h.Version == "" || len(h.AvailableYears) < 2 {
		t.Fatalf("holidays: %v %v", h, err)
	}
	kinds := map[string]string{}
	for _, d := range h.Days {
		kinds[d.Date] = d.Kind + " " + d.Name
	}
	if kinds["2026-10-01"] != "holiday 国庆节" || kinds["2026-10-10"] != "workday 国庆节" {
		t.Fatalf("国庆: %v / %v", kinds["2026-10-01"], kinds["2026-10-10"])
	}
	if h, err := hol.GetHolidays(ctx, &pb.GetHolidaysRequest{Year: 2031, Region: "cn"}); err != nil || len(h.Days) != 0 || h.Region != "CN" {
		t.Fatalf("a year without data: %v %v", h, err)
	}
	if h, err := hol.GetHolidays(ctx, &pb.GetHolidaysRequest{}); err != nil || h.Year != int32(time.Now().UTC().Year()) {
		t.Fatalf("this year: %v %v", h, err)
	}
	if _, err := hol.GetHolidays(ctx, &pb.GetHolidaysRequest{Region: "US"}); code(err) != codes.InvalidArgument {
		t.Fatalf("US: %v", err)
	}
}
