package server_test

import (
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
)

// The alarm tools: every structured result is checked against its tool's
// outputSchema by callS; here the contract's fields are checked too.
func TestMCPAlarms(t *testing.T) {
	e := start(t, nil, false)
	sh, _ := time.LoadLocation("Asia/Shanghai")
	cs := mcpSessionIn(t, e, e.alice, sh)
	al := pb.NewAlarmServiceClient(e.conn(t))
	ctx := as(e.alice)
	today := time.Now().In(sh).Format("2006-01-02")

	// 每隔一周的周一 15:15 接三宝, with the zone and region left to their defaults.
	text, sc, isErr := callS(t, cs, "create_alarm", map[string]any{"label": "接三宝", "mode": "alarm", "holiday_policy": "workdayOnly",
		"original_text": "每隔一周的周一下午三点一刻接三宝",
		"schedule": map[string]any{"kind": "recurring", "anchor": today, "time": "15:15",
			"recurrence": map[string]any{"freq": "weekly", "interval": 2, "byDay": []string{"MON"}}}})
	if isErr {
		t.Fatalf("create_alarm: %s", text)
	}
	id := item(t, sc, "alarm")
	s := obj(t, sc["schedule"])
	r := obj(t, s["recurrence"])
	if sc["label"] != "接三宝" || sc["mode"] != "alarm" || sc["enabled"] != true || sc["holidayPolicy"] != "workdayOnly" || sc["region"] != "CN" ||
		sc["space"] != "life" || sc["originalText"] != "每隔一周的周一下午三点一刻接三宝" || s["kind"] != "recurring" || s["anchor"] != today ||
		s["time"] != "15:15" || s["timeZone"] != "Asia/Shanghai" || r["freq"] != "weekly" || r["interval"] != 2.0 || arr(t, r["byDay"])[0] != "MON" {
		t.Fatalf("create_alarm: %v", sc)
	}
	inZone(t, sc["createTime"])
	fires := arr(t, sc["nextFires"])
	if len(fires) != 5 {
		t.Fatalf("nextFires: %v", fires)
	}
	for _, f := range fires {
		if l := inZone(t, f).In(sh); l.Weekday() != time.Monday || l.Format("15:04") != "15:15" {
			t.Fatalf("a ring off the rule: %v", f)
		}
	}
	got, err := al.GetAlarm(ctx, &pb.GetAlarmRequest{Id: id})
	text, sc, _ = callS(t, cs, "get_alarm", map[string]any{"id": id})
	sameText(t, "get_alarm", text, got, err)
	if item(t, sc, "alarm") != id {
		t.Fatalf("get_alarm: %v", sc)
	}

	// A reminder at one moment, said with a zone-less time.
	_, sc, _ = callS(t, cs, "create_alarm", map[string]any{"label": "收快递", "mode": "reminder",
		"schedule": map[string]any{"kind": "oneShot", "at": "2030-03-05T18:00"}})
	rid := item(t, sc, "alarm")
	// 2030 is past the 180-day window, but a one-shot has no window.
	if fires := arr(t, sc["nextFires"]); sc["mode"] != "reminder" || obj(t, sc["schedule"])["at"] != "2030-03-05T18:00:00+08:00" ||
		len(fires) != 1 || fires[0] != "2030-03-05T18:00:00+08:00" {
		t.Fatalf("one-shot reminder: %v", sc)
	}
	if _, ok := obj(t, sc["schedule"])["recurrence"]; ok {
		t.Fatalf("a one-shot has no recurrence: %v", sc)
	}

	// Mistakes come back as plain words.
	for args, want := range map[string]map[string]any{
		"byTime (several times a day) only works with freq daily (got weekly)": {"label": "x", "schedule": map[string]any{"kind": "recurring", "anchor": today,
			"recurrence": map[string]any{"freq": "weekly", "byTime": []string{"08:00", "20:00"}}}},
		"needs anchor":             {"label": "x", "schedule": map[string]any{"kind": "recurring", "time": "07:00", "recurrence": map[string]any{"freq": "daily"}}},
		"label is empty":           {"label": " ", "schedule": map[string]any{"kind": "oneShot", "at": "2030-01-01T07:00:00+08:00"}},
		"mode \"loud\" is unknown": {"label": "x", "mode": "loud", "schedule": map[string]any{"kind": "oneShot", "at": "2030-01-01T07:00:00+08:00"}},
	} {
		text, _, isErr := callS(t, cs, "create_alarm", want)
		if !isErr || !strings.Contains(text, args) {
			t.Fatalf("want an error with %q, got %v %q", args, isErr, text)
		}
	}

	// The list carries a version that moves with every change.
	lr, err := al.ListAlarms(ctx, &pb.ListAlarmsRequest{})
	text, sc, _ = callS(t, cs, "list_alarms", map[string]any{})
	sameText(t, "list_alarms", text, lr, err)
	v1, _ := sc["version"].(string)
	if l := arr(t, sc["items"]); len(l) != 2 || item(t, obj(t, l[0]), "alarm") != id || v1 == "" {
		t.Fatalf("list_alarms: %v", sc)
	}

	// 关闭闹钟: enabled false, no rings, a new version.
	_, sc, _ = callS(t, cs, "update_alarm", map[string]any{"id": id, "enabled": false})
	if sc["enabled"] != false || len(arr(t, sc["nextFires"])) != 0 || obj(t, obj(t, sc["schedule"])["recurrence"])["interval"] != 2.0 {
		t.Fatalf("update_alarm off: %v", sc)
	}
	_, sc, _ = callS(t, cs, "list_alarms", map[string]any{"enabled": true})
	if l := arr(t, sc["items"]); len(l) != 1 || item(t, obj(t, l[0]), "alarm") != rid || sc["version"] == v1 {
		t.Fatalf("list_alarms enabled: %v", sc)
	}
	v2 := sc["version"]
	// Move it to 16:00 by sending the whole schedule back.
	_, sc, _ = callS(t, cs, "update_alarm", map[string]any{"id": id, "enabled": true, "schedule": map[string]any{"kind": "recurring", "anchor": today, "time": "16:00",
		"recurrence": map[string]any{"freq": "weekly", "interval": 2, "byDay": []string{"MON"}}}})
	if obj(t, sc["schedule"])["time"] != "16:00" || sc["enabled"] != true || len(arr(t, sc["nextFires"])) != 5 {
		t.Fatalf("update_alarm time: %v", sc)
	}
	if text, _, isErr := callS(t, cs, "update_alarm", map[string]any{"id": id}); !isErr || !strings.Contains(text, "nothing to update") {
		t.Fatalf("empty update: %q", text)
	}

	_, sc, _ = callS(t, cs, "delete_alarm", map[string]any{"id": rid})
	if sc["deleted"] != rid || sc["kind"] != "alarm" {
		t.Fatalf("delete_alarm: %v", sc)
	}
	_, sc, _ = callS(t, cs, "list_alarms", map[string]any{})
	if l := arr(t, sc["items"]); len(l) != 1 || sc["version"] == v2 || sc["version"] == v1 {
		t.Fatalf("after delete: %v", sc)
	}
	if text, _, isErr := callS(t, cs, "get_alarm", map[string]any{"id": rid}); !isErr {
		t.Fatalf("deleted alarm: %s", text)
	}

	// The holiday calendar.
	_, sc, _ = callS(t, cs, "get_holidays", map[string]any{"year": 2026})
	days := arr(t, sc["days"])
	if sc["year"] != 2026.0 || sc["region"] != "CN" || len(days) != 39 || sc["version"] == "" || len(arr(t, sc["availableYears"])) < 2 {
		t.Fatalf("get_holidays: %v", sc)
	}
	if d := obj(t, days[len(days)-1]); d["date"] != "2026-10-10" || d["kind"] != "workday" || d["name"] != "国庆节" {
		t.Fatalf("last 2026 day: %v", d)
	}
	_, sc, _ = callS(t, cs, "get_holidays", map[string]any{"year": 2031})
	if len(arr(t, sc["days"])) != 0 {
		t.Fatalf("2031: %v", sc)
	}
	if text, _, isErr := callS(t, cs, "get_holidays", map[string]any{"region": "US"}); !isErr || !strings.Contains(text, "only CN") {
		t.Fatalf("US: %q", text)
	}
}
