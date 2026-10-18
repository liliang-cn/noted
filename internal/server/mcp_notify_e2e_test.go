package server_test

import (
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// One notify for alarms, events and to-dos, and list_fires as the feed a
// phone schedules from.
func TestMCPNotifyAndFires(t *testing.T) {
	e := start(t, nil, false)
	sh, _ := time.LoadLocation("Asia/Shanghai")
	cs := mcpSessionIn(t, e, e.alice, sh)
	al := pb.NewAlarmServiceClient(e.conn(t))
	ctx := as(e.alice)
	day := time.Now().In(sh).AddDate(0, 0, 2).Format("2006-01-02")

	_, sc, _ := callS(t, cs, "create_event", map[string]any{"title": "开会", "start": day + "T10:00:00"})
	meet := item(t, sc, "event")
	if sc["notify"] != "reminder" || sc["remindBeforeMinutes"] != 10.0 {
		t.Fatalf("a timed event by default: %v", sc)
	}
	_, sc, _ = callS(t, cs, "create_event", map[string]any{"title": "生日", "start": day + "T00:00:00", "all_day": true})
	if sc["notify"] != "none" {
		t.Fatalf("an all-day event by default: %v", sc)
	}
	_, sc, _ = callS(t, cs, "create_event", map[string]any{"title": "赶飞机", "start": day + "T14:00:00", "notify": "alarm", "remind_before_minutes": 90})
	flight := item(t, sc, "event")
	_, sc, _ = callS(t, cs, "create_task", map[string]any{"title": "交周报", "due": day + "T18:00:00", "notify": "notification"})
	report := item(t, sc, "task")
	if sc["notify"] != "notification" || sc["remindTime"] != day+"T18:00:00+08:00" {
		t.Fatalf("a to-do told at due: %v", sc)
	}
	_, sc, _ = callS(t, cs, "create_alarm", map[string]any{"label": "起床", "notify": "alarm",
		"schedule": map[string]any{"kind": "oneShot", "at": day + "T07:00:00"}})
	wake := item(t, sc, "alarm")
	if sc["mode"] != "alarm" || sc["notify"] != "alarm" || sc["source"] != "alarm" {
		t.Fatalf("create_alarm: %v", sc)
	}
	_, sc, _ = callS(t, cs, "update_alarm", map[string]any{"id": wake, "notify": "notification"})
	if sc["mode"] != "reminder" || sc["notify"] != "notification" {
		t.Fatalf("update_alarm notify: %v", sc)
	}

	text, sc, isErr := callS(t, cs, "list_fires", map[string]any{"from": day, "days": 1})
	if isErr {
		t.Fatalf("list_fires: %s", text)
	}
	type f struct{ id, notify, fire, at string }
	want := []f{
		{wake, "notification", day + "T07:00:00+08:00", day + "T07:00:00+08:00"},
		{meet, "reminder", day + "T09:50:00+08:00", day + "T10:00:00+08:00"},
		{flight, "alarm", day + "T12:30:00+08:00", day + "T14:00:00+08:00"},
		{report, "notification", day + "T18:00:00+08:00", day + "T18:00:00+08:00"},
	}
	got := arr(t, sc["items"])
	if len(got) != len(want) || sc["version"] == "" {
		t.Fatalf("list_fires: %v", sc)
	}
	for i, w := range want {
		m := obj(t, got[i])
		if m["kind"] != "fire" || m["itemId"] != w.id || m["notify"] != w.notify || m["fireTime"] != w.fire || m["atTime"] != w.at {
			t.Fatalf("fire %d: %v, want %+v", i, m, w)
		}
	}
	if obj(t, got[3])["source"] != "task" || obj(t, got[3])["link"] != "noted://task/"+report {
		t.Fatalf("a to-do's fire: %v", got[3])
	}
	from, _ := time.ParseInLocation("2006-01-02", day, sh)
	r, err := al.ListFires(ctx, &pb.ListFiresRequest{From: timestamppb.New(from), To: timestamppb.New(from.AddDate(0, 0, 1))})
	sameText(t, "list_fires", text, r, err)

	// What older phones read is unchanged: the alarms made with create_alarm,
	// none of them ringing unless they should.
	_, sc, _ = callS(t, cs, "list_alarms", map[string]any{})
	if l := arr(t, sc["items"]); len(l) != 1 || item(t, obj(t, l[0]), "alarm") != wake || obj(t, l[0])["mode"] != "reminder" {
		t.Fatalf("list_alarms: %v", sc)
	}
	_, sc, _ = callS(t, cs, "list_alarms", map[string]any{"notify": "all"})
	if l := arr(t, sc["items"]); len(l) != 4 {
		t.Fatalf("list_alarms all: %v", sc)
	}
	// The unified schedule shows all of them, with how each is told.
	_, sc, _ = callS(t, cs, "list_events", map[string]any{"from": day, "to": day + "T23:59:00"})
	byID := map[string]string{}
	for _, it := range arr(t, sc["items"]) {
		m := obj(t, it)
		byID[m["id"].(string)] = m["notify"].(string)
	}
	if len(byID) != 5 || byID[flight] != "alarm" || byID[wake] != "notification" || byID[report] != "notification" {
		t.Fatalf("list_events: %v", byID)
	}
}
