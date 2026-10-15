package server_test

import (
	"strings"
	"testing"
	"time"
)

func TestMCPTimeTracking(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)

	out, _ := call(t, cs, "create_goal", map[string]any{"title": "英语", "period": "week", "target": 140, "unit": "分钟"})
	gid := firstID(out)
	out, _ = call(t, cs, "create_project", map[string]any{"title": "App", "space": "work"})
	pid := firstID(out)
	recent := time.Now().UTC().Add(-time.Minute).Format("2006-01-02T15:04:05")

	out, isErr := call(t, cs, "log_time", map[string]any{"title": "游泳", "minutes": 30, "time": recent})
	if isErr || !strings.Contains(out, "游泳") || !has(`"minutes":\s*30`, out) || !strings.Contains(out, "SPACE_LIFE") {
		t.Fatalf("log_time: %v %s", isErr, out)
	}
	swim := firstID(out)
	out, isErr = call(t, cs, "log_time", map[string]any{"title": "英语学习", "minutes": 30, "goal_id": gid, "time": recent})
	if isErr || !strings.Contains(out, "checkInId") {
		t.Fatalf("log_time with a goal: %v %s", isErr, out)
	}
	if out, _ := call(t, cs, "get_goal", map[string]any{"id": gid}); !has(`"done":\s*30`, out) {
		t.Fatalf("the goal should count the 30 minutes: %s", out)
	}
	if _, isErr := call(t, cs, "log_time", map[string]any{"title": "x", "minutes": 0}); !isErr {
		t.Fatal("zero minutes should be a tool error")
	}

	out, isErr = call(t, cs, "time_summary", map[string]any{})
	if isErr || !has(`"totalMinutes":\s*60`, out) || !has(`"label":\s*"游泳"`, out) || !has(`"label":\s*"英语学习"`, out) {
		t.Fatalf("time_summary: %v %s", isErr, out)
	}
	if out, _ := call(t, cs, "time_summary", map[string]any{"group_by": "goal"}); !has(`"label":\s*"英语"`, out) {
		t.Fatalf("time_summary by goal: %s", out)
	}
	if _, isErr := call(t, cs, "time_summary", map[string]any{"group_by": "hour"}); !isErr {
		t.Fatal("a bad group_by should be a tool error")
	}

	// Timer.
	if out, _ := call(t, cs, "get_running_timer", map[string]any{}); !strings.Contains(out, "no timer") {
		t.Fatalf("get_running_timer: %s", out)
	}
	out, isErr = call(t, cs, "start_timer", map[string]any{"title": "写代码", "project_id": pid})
	if isErr || !has(`"running":\s*true`, out) || !strings.Contains(out, "SPACE_WORK") {
		t.Fatalf("start_timer: %v %s", isErr, out)
	}
	if out, isErr := call(t, cs, "start_timer", map[string]any{"title": "游泳"}); !isErr || !strings.Contains(out, "写代码") {
		t.Fatalf("a second timer should be refused, naming the first: %v %s", isErr, out)
	}
	if out, _ := call(t, cs, "get_running_timer", map[string]any{}); !strings.Contains(out, "写代码") {
		t.Fatalf("get_running_timer: %s", out)
	}
	out, isErr = call(t, cs, "stop_timer", map[string]any{})
	if isErr || strings.Contains(out, `"running"`) || !has(`"minutes":\s*1`, out) {
		t.Fatalf("stop_timer: %v %s", isErr, out)
	}
	if out, _ := call(t, cs, "get_project", map[string]any{"id": pid}); !strings.Contains(out, "写代码") || !has(`"timeMinutes":\s*1`, out) {
		t.Fatalf("the project should show its time: %s", out)
	}

	// Edit and delete.
	out, isErr = call(t, cs, "update_time_entry", map[string]any{"id": swim, "minutes": 45, "goal_id": gid})
	if isErr || !has(`"minutes":\s*45`, out) {
		t.Fatalf("update_time_entry: %v %s", isErr, out)
	}
	if out, _ := call(t, cs, "get_goal", map[string]any{"id": gid}); !has(`"done":\s*75`, out) {
		t.Fatalf("the goal should follow the edit: %s", out)
	}
	if _, isErr := call(t, cs, "update_time_entry", map[string]any{"id": swim}); !isErr {
		t.Fatal("an empty update should be a tool error")
	}
	out, _ = call(t, cs, "list_time_entries", map[string]any{"title": "游"})
	if strings.Count(out, `"title"`) != 1 {
		t.Fatalf("list_time_entries by title: %s", out)
	}
	if _, isErr := call(t, cs, "delete_time_entry", map[string]any{"id": swim}); isErr {
		t.Fatal("delete_time_entry")
	}
	if out, _ := call(t, cs, "get_goal", map[string]any{"id": gid}); !has(`"done":\s*30`, out) {
		t.Fatalf("deleting the entry should take its minutes off the goal: %s", out)
	}

	// Bob sees none of it.
	bob := mcpSession(t, e, e.bob)
	if out, _ := call(t, bob, "list_time_entries", map[string]any{}); strings.Contains(out, "英语学习") {
		t.Fatalf("bob sees alice's time: %s", out)
	}
	if out, _ := call(t, bob, "time_summary", map[string]any{}); strings.Contains(out, "totalMinutes") {
		t.Fatalf("bob's summary should be empty: %s", out)
	}
}
