package server_test

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func has(re, s string) bool { return regexp.MustCompile(re).MatchString(s) }

func TestMCPOffersTheWholeSurface(t *testing.T) {
	e := start(t, nil, false)
	names := toolNames(t, mcpSession(t, e, e.alice))
	for _, want := range []string{
		"get_task", "update_task", "log_task_work", "list_task_work", "delete_task_work",
		"list_objectives", "get_objective", "create_objective", "update_objective", "delete_objective",
		"record_reading", "list_readings", "delete_reading", "set_objective_milestone_done",
		"get_goal", "update_goal", "delete_goal", "list_check_ins", "undo_check_in", "set_milestone_done",
		"list_holdings", "get_holding", "create_holding", "update_holding", "delete_holding",
		"record_trade", "list_trades", "delete_trade",
		"delete_project", "propose_schedule",
		"log_time", "start_timer", "stop_timer", "get_running_timer", "list_time_entries", "update_time_entry", "delete_time_entry", "time_summary",
		"create_alarm", "list_alarms", "get_alarm", "update_alarm", "delete_alarm", "get_holidays",
	} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}

func TestMCPTaskWorkAndMovingIntoAProject(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)

	out, _ := call(t, cs, "create_project", map[string]any{"title": "Philippines trip", "space": "life"})
	pid := firstID(out)
	out, _ = call(t, cs, "create_task", map[string]any{"title": "Book hotel", "due": "2030-11-01T09:00:00Z"})
	tid := firstID(out)

	// Move it into the project, set a reminder, clear the due date.
	out, isErr := call(t, cs, "update_task", map[string]any{"id": tid, "project_id": pid, "remind_at": "2030-10-30T09:00:00Z", "due": ""})
	if isErr || !strings.Contains(out, pid) || !strings.Contains(out, "2030-10-30T09:00:00Z") || strings.Contains(out, "dueTime") {
		t.Fatalf("update_task: %v %s", isErr, out)
	}
	if got, _ := call(t, cs, "get_project", map[string]any{"id": pid}); !strings.Contains(got, "Book hotel") {
		t.Fatalf("the task should now be in the project: %s", got)
	}
	if out, isErr := call(t, cs, "update_task", map[string]any{"id": tid}); !isErr {
		t.Fatalf("an empty update should be a tool error: %s", out)
	}
	if _, isErr := call(t, cs, "update_task", map[string]any{"id": tid, "space": "both"}); !isErr {
		t.Fatal("a bad space should be a tool error")
	}

	// Sittings.
	out, isErr = call(t, cs, "log_task_work", map[string]any{"task_id": tid, "minutes": 40, "progress": 60, "note": "compared three hotels"})
	if isErr || !has(`"workedMinutes":\s*40`, out) || !has(`"progress":\s*60`, out) || !has(`"workSessions":\s*1`, out) {
		t.Fatalf("log_task_work: %v %s", isErr, out)
	}
	out, _ = call(t, cs, "list_task_work", map[string]any{"task_id": tid})
	if !strings.Contains(out, "compared three hotels") {
		t.Fatalf("list_task_work: %s", out)
	}
	wid := firstID(out)
	out, isErr = call(t, cs, "delete_task_work", map[string]any{"id": wid})
	if isErr || has(`"workedMinutes"`, out) {
		t.Fatalf("delete_task_work should zero the totals: %v %s", isErr, out)
	}
	if _, isErr := call(t, cs, "log_task_work", map[string]any{"task_id": tid}); !isErr {
		t.Fatal("a sitting with nothing in it should be a tool error")
	}
	if out, _ := call(t, cs, "get_task", map[string]any{"id": tid}); !strings.Contains(out, "Book hotel") {
		t.Fatalf("get_task: %s", out)
	}

	// Another user cannot touch it.
	bob := mcpSession(t, e, e.bob)
	if _, isErr := call(t, bob, "log_task_work", map[string]any{"task_id": tid, "minutes": 5}); !isErr {
		t.Fatal("bob logged work on alice's task")
	}
	if _, isErr := call(t, bob, "update_task", map[string]any{"id": tid, "title": "x"}); !isErr {
		t.Fatal("bob renamed alice's task")
	}

	// Deleting the project keeps the task by default.
	if _, isErr := call(t, cs, "delete_project", map[string]any{"id": pid}); isErr {
		t.Fatal("delete_project")
	}
	if out, isErr := call(t, cs, "get_task", map[string]any{"id": tid}); isErr || strings.Contains(out, pid) {
		t.Fatalf("the task should stay, outside any project: %v %s", isErr, out)
	}
}

func TestMCPObjectivesAndReadings(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)

	out, _ := call(t, cs, "create_project", map[string]any{"title": "Health"})
	pid := firstID(out)
	out, isErr := call(t, cs, "create_objective", map[string]any{"title": "Lose weight", "metric_name": "weight", "metric_unit": "kg",
		"metric_start": 80, "metric_target": 75, "due": "2030-12-31", "project_id": pid})
	if isErr || !strings.Contains(out, "Lose weight") {
		t.Fatalf("create_objective: %v %s", isErr, out)
	}
	oid := firstID(out)
	out, isErr = call(t, cs, "create_goal", map[string]any{"title": "Swim", "period": "week", "target": 2, "objective_id": oid, "milestones": []string{"1 km"}})
	if isErr || !strings.Contains(out, oid) {
		t.Fatalf("a goal should become a dimension: %v %s", isErr, out)
	}
	gid := firstID(out)
	out, _ = call(t, cs, "create_goal", map[string]any{"title": "Run", "period": "week", "target": 3})
	run := firstID(out)
	if out, isErr := call(t, cs, "update_goal", map[string]any{"id": run, "objective_id": oid, "target": 4}); isErr || !strings.Contains(out, oid) || !has(`"target":\s*4`, out) {
		t.Fatalf("update_goal: %v %s", isErr, out)
	}

	out, isErr = call(t, cs, "record_reading", map[string]any{"objective_id": oid, "value": 78, "note": "morning"})
	if isErr || !has(`"metricCurrent":\s*78`, out) || !has(`"goalsTotal":\s*2`, out) {
		t.Fatalf("record_reading: %v %s", isErr, out)
	}
	out, _ = call(t, cs, "list_readings", map[string]any{"objective_id": oid})
	if !strings.Contains(out, "morning") {
		t.Fatalf("list_readings: %s", out)
	}
	rid := firstID(out)
	if out, _ := call(t, cs, "list_objectives", map[string]any{}); !strings.Contains(out, "Swim") || !strings.Contains(out, "Run") {
		t.Fatalf("list_objectives should carry the dimensions: %s", out)
	}
	if out, _ := call(t, cs, "get_project", map[string]any{"id": pid}); !strings.Contains(out, "Lose weight") {
		t.Fatalf("get_project should list the project's objectives: %s", out)
	}
	if _, isErr := call(t, cs, "update_objective", map[string]any{"id": oid}); !isErr {
		t.Fatal("an empty update should be a tool error")
	}
	if out, isErr := call(t, cs, "update_objective", map[string]any{"id": oid, "due": "", "metric_target": 74}); isErr || strings.Contains(out, "dueTime") || !has(`"metricTarget":\s*74`, out) {
		t.Fatalf("update_objective: %v %s", isErr, out)
	}
	if _, isErr := call(t, cs, "delete_reading", map[string]any{"id": rid}); isErr {
		t.Fatal("delete_reading")
	}

	// Check-ins and milestones.
	call(t, cs, "check_in", map[string]any{"goal_id": gid})
	out, _ = call(t, cs, "list_check_ins", map[string]any{"goal_id": gid})
	cid := firstID(out)
	if out, isErr := call(t, cs, "undo_check_in", map[string]any{"id": cid}); isErr || has(`"done":\s*1`, out) {
		t.Fatalf("undo_check_in: %v %s", isErr, out)
	}
	out, _ = call(t, cs, "get_goal", map[string]any{"id": gid})
	mid := regexp.MustCompile(`"milestones":\s*\[\s*\{\s*"id":\s*"([^"]+)"`).FindStringSubmatch(out)
	if mid == nil {
		t.Fatalf("get_goal: %s", out)
	}
	if out, isErr := call(t, cs, "set_milestone_done", map[string]any{"goal_id": gid, "milestone_id": mid[1], "done": true}); isErr || !has(`"milestonesDone":\s*1`, out) {
		t.Fatalf("set_milestone_done: %v %s", isErr, out)
	}

	// Deleting the objective leaves its dimensions as plain goals.
	if _, isErr := call(t, cs, "delete_objective", map[string]any{"id": oid}); isErr {
		t.Fatal("delete_objective")
	}
	if out, _ := call(t, cs, "list_goals", map[string]any{}); !strings.Contains(out, "Swim") || strings.Contains(out, oid) {
		t.Fatalf("goals should stay, detached: %s", out)
	}
	if _, isErr := call(t, cs, "delete_goal", map[string]any{"id": gid}); isErr {
		t.Fatal("delete_goal")
	}
	if out, _ := call(t, mcpSession(t, e, e.bob), "list_objectives", map[string]any{}); strings.Contains(out, "Lose weight") {
		t.Fatalf("objective leaked: %s", out)
	}
}

func TestMCPHoldingsAndTrades(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)

	// A buy by symbol creates the holding; a second one finds it.
	out, isErr := call(t, cs, "record_trade", map[string]any{"symbol": "voo", "side": "buy", "shares": 10, "price": 520})
	if isErr || !strings.Contains(out, `"VOO"`) || !has(`"shares":\s*10`, out) {
		t.Fatalf("record_trade: %v %s", isErr, out)
	}
	hid := firstID(out)
	out, isErr = call(t, cs, "record_trade", map[string]any{"symbol": "VOO", "side": "Buy", "shares": 10, "price": 500, "time": "2030-01-05T10:00:00Z"})
	if isErr || !strings.Contains(out, hid) || !has(`"shares":\s*20`, out) || !has(`"avgCost":\s*510`, out) {
		t.Fatalf("the second buy should join the same holding: %v %s", isErr, out)
	}
	if l, _ := call(t, cs, "list_holdings", map[string]any{}); strings.Count(l, `"symbol"`) != 1 {
		t.Fatalf("one holding expected: %s", l)
	}
	// Selling something never bought does not invent a holding.
	if _, isErr := call(t, cs, "record_trade", map[string]any{"symbol": "QQQM", "side": "sell", "shares": 1, "price": 200}); !isErr {
		t.Fatal("a sell of an unknown symbol should be a tool error")
	}
	if l, _ := call(t, cs, "list_holdings", map[string]any{}); strings.Contains(l, "QQQM") {
		t.Fatalf("a failed sell created a holding: %s", l)
	}
	if _, isErr := call(t, cs, "record_trade", map[string]any{"holding_id": hid, "side": "hold", "shares": 1, "price": 1}); !isErr {
		t.Fatal("a bad side should be a tool error")
	}

	out, isErr = call(t, cs, "update_holding", map[string]any{"id": hid, "last_price": 550, "dca_amount": 500, "dca_day": 15})
	if isErr || !has(`"unrealizedPnl":\s*800`, out) || !has(`"active":\s*true`, out) {
		t.Fatalf("update_holding: %v %s", isErr, out)
	}
	if out, isErr := call(t, cs, "update_holding", map[string]any{"id": hid, "clear_price": true}); isErr || strings.Contains(out, "lastPrice") {
		t.Fatalf("clear_price: %v %s", isErr, out)
	}
	out, _ = call(t, cs, "list_trades", map[string]any{"holding_id": hid})
	if strings.Count(out, `"side"`) != 2 {
		t.Fatalf("list_trades: %s", out)
	}
	if _, isErr := call(t, cs, "delete_trade", map[string]any{"id": firstID(out)}); isErr {
		t.Fatal("delete_trade")
	}
	if out, _ := call(t, cs, "get_holding", map[string]any{"id": hid}); !has(`"shares":\s*10`, out) {
		t.Fatalf("after deleting a trade: %s", out)
	}
	out, isErr = call(t, cs, "create_holding", map[string]any{"symbol": "QQQM", "name": "Invesco NASDAQ 100", "last_price": 200})
	if isErr || !strings.Contains(out, "QQQM") {
		t.Fatalf("create_holding: %v %s", isErr, out)
	}
	if _, isErr := call(t, mcpSession(t, e, e.bob), "get_holding", map[string]any{"id": hid}); !isErr {
		t.Fatal("bob read alice's holding")
	}
	if _, isErr := call(t, cs, "delete_holding", map[string]any{"id": hid}); isErr {
		t.Fatal("delete_holding")
	}
}

func TestMCPProposeSchedule(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)
	out, _ := call(t, cs, "create_task", map[string]any{"title": "Write report", "due": time.Now().UTC().AddDate(0, 0, 3).Format(time.RFC3339)})
	out, isErr := call(t, cs, "propose_schedule", map[string]any{"task_ids": []string{firstID(out)}, "duration_minutes": 30})
	if isErr || !strings.Contains(out, "Write report") || !strings.Contains(out, `"pending"`) {
		t.Fatalf("propose_schedule: %v %s", isErr, out)
	}
}

func TestMCPSuggestTags(t *testing.T) {
	f := &fakeProvider{script: answerJSON("short topic tags", `["旅行","签证"]`)}
	e, _ := startWithAI(t, f, false)
	cs := mcpSession(t, e, e.alice)
	out, _ := call(t, cs, "create_note", map[string]any{"title": "菲律宾", "content": "记得办签证"})
	nid := firstID(out)
	out, isErr := call(t, cs, "suggest_tags", map[string]any{"id": nid, "apply": true})
	if isErr || !strings.Contains(out, "签证") {
		t.Fatalf("suggest_tags: %v %s", isErr, out)
	}
	if got, _ := call(t, cs, "get_note", map[string]any{"id": nid}); !strings.Contains(got, "旅行") {
		t.Fatalf("apply should add the tags to the note: %s", got)
	}
}
