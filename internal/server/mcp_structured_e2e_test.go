package server_test

import (
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want an object, got %T %v", v, v)
	}
	return m
}

func arr(t *testing.T, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("want an array, got %T %v", v, v)
	}
	return a
}

// item checks the fields every item carries and returns its id.
func item(t *testing.T, m map[string]any, kind string) string {
	t.Helper()
	id, _ := m["id"].(string)
	if m["kind"] != kind || id == "" || m["link"] != "noted://"+kind+"/"+id {
		t.Fatalf("want a %s with id and link, got %v", kind, m)
	}
	return id
}

// inZone checks an RFC 3339 time is written in Asia/Shanghai.
func inZone(t *testing.T, v any) time.Time {
	t.Helper()
	s, _ := v.(string)
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil || !strings.HasSuffix(s, "+08:00") {
		t.Fatalf("want an RFC 3339 time at +08:00, got %q", s)
	}
	return tm
}

func money(t *testing.T, v any, amount float64, currency string) {
	t.Helper()
	m := obj(t, v)
	if m["amount"] != amount || m["currency"] != currency {
		t.Fatalf("want %v %s, got %v", amount, currency, m)
	}
}

// sameText checks a tool's text is still the protojson of what the gRPC API returns.
func sameText(t *testing.T, tool, got string, m proto.Message, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := protojson.MarshalOptions{Multiline: true, Indent: " "}.Marshal(m)
	if got != string(want) {
		t.Fatalf("%s text changed:\n got %s\nwant %s", tool, got, want)
	}
}

func TestMCPStructuredOutput(t *testing.T) {
	e := start(t, nil, false)
	sh, _ := time.LoadLocation("Asia/Shanghai")
	cs := mcpSessionIn(t, e, e.alice, sh)
	conn := e.conn(t)
	cal, notes, goals := pb.NewCalendarServiceClient(conn), pb.NewNoteServiceClient(conn), pb.NewGoalServiceClient(conn)
	holds, objs := pb.NewHoldingServiceClient(conn), pb.NewObjectiveServiceClient(conn)
	ctx := as(e.alice)
	tomorrow := time.Now().In(sh).AddDate(0, 0, 1).Format("2006-01-02")

	// Projects and tasks.
	_, sc, _ := callS(t, cs, "create_project", map[string]any{"title": "Trip", "start": "2030-11-20", "space": "life"})
	pid := item(t, sc, "project")
	inZone(t, sc["startTime"])
	_, sc, _ = callS(t, cs, "create_task", map[string]any{"title": "Visa", "due": tomorrow + "T09:00:00", "priority": 3, "project_id": pid, "tags": []string{"Travel"}})
	tid := item(t, sc, "task")
	if due := inZone(t, sc["dueTime"]); due.Hour() != 9 || sc["priority"] != 3.0 || sc["space"] != "life" ||
		sc["projectId"] != pid || sc["completed"] != false || sc["overdue"] != false || arr(t, sc["tags"])[0] != "travel" {
		t.Fatalf("create_task: %v", sc)
	}
	text, sc, _ := callS(t, cs, "get_task", map[string]any{"id": tid})
	item(t, sc, "task")
	got, err := cal.GetTask(ctx, &pb.GetTaskRequest{Id: tid})
	sameText(t, "get_task", text, got, err)
	_, sc, _ = callS(t, cs, "update_task", map[string]any{"id": tid, "notes": "passport photo"})
	if sc["notes"] != "passport photo" {
		t.Fatalf("update_task: %v", sc)
	}
	text, sc, _ = callS(t, cs, "list_tasks", map[string]any{})
	if l := arr(t, sc["items"]); len(l) != 1 || item(t, obj(t, l[0]), "task") != tid {
		t.Fatalf("list_tasks: %v", sc)
	}
	list, err := cal.ListTasks(ctx, &pb.ListTasksRequest{})
	sameText(t, "list_tasks", text, list, err)

	// Events.
	_, sc, _ = callS(t, cs, "create_event", map[string]any{"title": "Dentist", "start": tomorrow + "T15:00:00", "remind_before_minutes": 30, "project_id": pid})
	eid := item(t, sc, "event")
	if start := inZone(t, sc["startTime"]); start.Hour() != 15 || sc["remindBeforeMinutes"] != 30.0 || sc["allDay"] != false {
		t.Fatalf("create_event: %v", sc)
	}
	inZone(t, sc["endTime"])
	_, sc, _ = callS(t, cs, "update_event", map[string]any{"id": eid, "location": "Main St"})
	if sc["location"] != "Main St" {
		t.Fatalf("update_event: %v", sc)
	}
	_, sc, _ = callS(t, cs, "list_events", map[string]any{"from": tomorrow, "to": tomorrow + "T23:59:00"})
	inZone(t, sc["from"])
	if l := arr(t, sc["items"]); len(l) != 1 || item(t, obj(t, l[0]), "event") != eid {
		t.Fatalf("list_events: %v", sc)
	}

	// The overview mixes tasks and events, told apart by kind.
	_, sc, _ = callS(t, cs, "get_overview", map[string]any{"horizon": "upcoming"})
	kinds := map[string]bool{}
	for _, it := range arr(t, sc["items"]) {
		m := obj(t, it)
		kinds[m["kind"].(string)] = true
		if m["projectTitle"] != "Trip" {
			t.Fatalf("overview item without its project: %v", m)
		}
	}
	if sc["horizon"] != "upcoming" || !kinds["task"] || !kinds["event"] {
		t.Fatalf("get_overview: %v", sc)
	}
	arr(t, sc["goals"])
	arr(t, sc["pinnedProjects"])

	// Notes.
	_, sc, _ = callS(t, cs, "create_note", map[string]any{"title": "Packing", "content": "sunscreen and adapters", "project_id": pid})
	nid := item(t, sc, "note")
	inZone(t, sc["createTime"])
	text, sc, _ = callS(t, cs, "get_note", map[string]any{"id": nid})
	if sc["content"] != "sunscreen and adapters" {
		t.Fatalf("get_note: %v", sc)
	}
	n, err := notes.GetNote(ctx, &pb.GetNoteRequest{Id: nid})
	sameText(t, "get_note", text, n, err)
	_, sc, _ = callS(t, cs, "list_notes", map[string]any{})
	item(t, obj(t, arr(t, sc["items"])[0]), "note")
	_, sc, _ = callS(t, cs, "search_notes", map[string]any{"query": "sunscreen"})
	if l := arr(t, sc["items"]); len(l) != 1 || item(t, obj(t, l[0]), "note") != nid || sc["mode"] != "fulltext" {
		t.Fatalf("search_notes: %v", sc)
	}

	// The project carries what belongs to it.
	_, sc, _ = callS(t, cs, "get_project", map[string]any{"id": pid})
	item(t, obj(t, sc["project"]), "project")
	if len(arr(t, sc["tasks"])) != 1 || len(arr(t, sc["events"])) != 1 || len(arr(t, sc["notes"])) != 1 {
		t.Fatalf("get_project: %v", sc)
	}
	_, sc, _ = callS(t, cs, "list_projects", map[string]any{})
	p := obj(t, arr(t, sc["items"])[0])
	item(t, p, "project")
	if obj(t, p["progress"])["tasksTotal"] != 1.0 {
		t.Fatalf("list_projects: %v", p)
	}

	// Goals and check-ins.
	_, sc, _ = callS(t, cs, "create_goal", map[string]any{"title": "German", "period": "week", "target": 140, "unit": "minutes"})
	gid := item(t, sc, "goal")
	_, sc, _ = callS(t, cs, "check_in", map[string]any{"goal_id": gid, "amount": 20})
	item(t, sc, "goal")
	ci := obj(t, sc["checkIn"])
	if sc["period"] != "week" || obj(t, sc["progress"])["done"] != 20.0 || ci["kind"] != "checkIn" || ci["link"] != "noted://goal/"+gid {
		t.Fatalf("check_in: %v", sc)
	}
	inZone(t, ci["time"])
	text, sc, _ = callS(t, cs, "get_goal", map[string]any{"id": gid})
	item(t, sc, "goal")
	g, err := goals.GetGoal(ctx, &pb.GetGoalRequest{Id: gid})
	sameText(t, "get_goal", text, g, err)
	_, sc, _ = callS(t, cs, "list_goals", map[string]any{})
	item(t, obj(t, arr(t, sc["items"])[0]), "goal")

	// Objectives.
	_, sc, _ = callS(t, cs, "create_objective", map[string]any{"title": "Lose weight", "metric_name": "weight", "metric_unit": "kg", "metric_start": 80, "metric_target": 75})
	oid := item(t, sc, "objective")
	text, sc, _ = callS(t, cs, "get_objective", map[string]any{"id": oid})
	if sc["metricUnit"] != "kg" || sc["metricTarget"] != 75.0 {
		t.Fatalf("get_objective: %v", sc)
	}
	o, err := objs.GetObjective(ctx, &pb.GetObjectiveRequest{Id: oid})
	sameText(t, "get_objective", text, o, err)
	_, sc, _ = callS(t, cs, "list_objectives", map[string]any{})
	item(t, obj(t, arr(t, sc["items"])[0]), "objective")

	// Time tracking.
	if _, sc, _ = callS(t, cs, "get_running_timer", map[string]any{}); sc["running"] != false || sc["entry"] != nil {
		t.Fatalf("get_running_timer with none: %v", sc)
	}
	_, sc, _ = callS(t, cs, "log_time", map[string]any{"title": "游泳", "minutes": 30, "goal_id": gid})
	lid := item(t, sc, "time")
	if sc["minutes"] != 30.0 || sc["running"] != false || sc["goalId"] != gid {
		t.Fatalf("log_time: %v", sc)
	}
	// Only minutes: the start is made up, so not timed; its day is still known.
	if st := inZone(t, sc["startTime"]); sc["timed"] != false || sc["day"] != st.Format("2006-01-02") {
		t.Fatalf("log_time without a time: %v", sc)
	}
	inZone(t, sc["endTime"])
	_, sc, _ = callS(t, cs, "start_timer", map[string]any{"title": "写代码"})
	rid := item(t, sc, "time")
	if sc["running"] != true || sc["endTime"] != nil {
		t.Fatalf("start_timer: %v", sc)
	}
	if _, sc, _ = callS(t, cs, "get_running_timer", map[string]any{}); sc["running"] != true || item(t, obj(t, sc["entry"]), "time") != rid {
		t.Fatalf("get_running_timer: %v", sc)
	}
	if _, sc, _ = callS(t, cs, "stop_timer", map[string]any{}); item(t, sc, "time") != rid || sc["running"] != false || sc["timed"] != true {
		t.Fatalf("stop_timer: %v", sc)
	}
	if _, sc, _ = callS(t, cs, "update_time_entry", map[string]any{"id": lid, "minutes": 45}); sc["minutes"] != 45.0 {
		t.Fatalf("update_time_entry: %v", sc)
	}
	_, sc, _ = callS(t, cs, "list_time_entries", map[string]any{})
	if l := arr(t, sc["items"]); len(l) != 2 {
		t.Fatalf("list_time_entries: %v", sc)
	}
	_, sc, _ = callS(t, cs, "time_summary", map[string]any{"group_by": "goal"})
	if sc["groupBy"] != "goal" || sc["totalMinutes"] != 46.0 {
		t.Fatalf("time_summary: %v", sc)
	}
	for _, r := range arr(t, sc["items"]) {
		if m := obj(t, r); m["id"] == gid && (m["kind"] != "timeGroup" || m["link"] != "noted://goal/"+gid || m["minutes"] != 45.0) {
			t.Fatalf("time_summary row: %v", m)
		}
	}

	// A time the user named: the range is real, on the day it says. Late at
	// night in Shanghai is already the next day in UTC.
	_, sc, _ = callS(t, cs, "log_time", map[string]any{"title": "跑步", "minutes": 30, "time": "2026-10-13T23:40:00"})
	if sc["timed"] != true || sc["day"] != "2026-10-13" || sc["startTime"] != "2026-10-13T23:40:00+08:00" || sc["endTime"] != "2026-10-14T00:10:00+08:00" {
		t.Fatalf("log_time with a time: %v", sc)
	}
	_, sc, _ = callS(t, cs, "list_time_entries", map[string]any{"title": "跑步"})
	if l := arr(t, sc["items"]); len(l) != 1 || obj(t, l[0])["day"] != "2026-10-13" || obj(t, l[0])["timed"] != true {
		t.Fatalf("list_time_entries carries day and timed: %v", sc)
	}
	_, sc, _ = callS(t, cs, "time_summary", map[string]any{"group_by": "day", "from": "2026-10-13T00:00:00", "to": "2026-10-14T00:00:00"})
	if l := arr(t, sc["items"]); len(l) != 1 || obj(t, l[0])["date"] != "2026-10-13" || obj(t, l[0])["id"] != "2026-10-13" || obj(t, l[0])["minutes"] != 30.0 {
		t.Fatalf("time_summary by day: %v", sc)
	}

	// Holdings: money carries its currency.
	_, sc, _ = callS(t, cs, "record_trade", map[string]any{"symbol": "voo", "side": "buy", "shares": 10, "price": 520})
	hid := item(t, sc, "holding")
	pos := obj(t, sc["position"])
	if sc["symbol"] != "VOO" || pos["shares"] != 10.0 || pos["hasPrice"] != false || pos["marketValue"] != nil {
		t.Fatalf("record_trade: %v", sc)
	}
	money(t, pos["avgCost"], 520, "USD")
	_, sc, _ = callS(t, cs, "update_holding", map[string]any{"id": hid, "last_price": 550, "dca_amount": 500, "dca_day": 5})
	money(t, sc["lastPrice"], 550, "USD")
	money(t, obj(t, sc["position"])["marketValue"], 5500, "USD")
	money(t, obj(t, sc["position"])["unrealizedPnl"], 300, "USD")
	plan := obj(t, sc["plan"])
	money(t, plan["amount"], 500, "USD")
	if plan["day"] != 5.0 || plan["active"] != true {
		t.Fatalf("update_holding plan: %v", plan)
	}
	inZone(t, plan["nextTime"])
	text, sc, _ = callS(t, cs, "get_holding", map[string]any{"id": hid})
	item(t, sc, "holding")
	h, err := holds.GetHolding(ctx, &pb.GetHoldingRequest{Id: hid})
	sameText(t, "get_holding", text, h, err)
	_, sc, _ = callS(t, cs, "list_holdings", map[string]any{})
	item(t, obj(t, arr(t, sc["items"])[0]), "holding")
	_, sc, _ = callS(t, cs, "list_trades", map[string]any{"holding_id": hid})
	tr := obj(t, arr(t, sc["items"])[0])
	if tr["kind"] != "trade" || tr["link"] != "noted://holding/"+hid || tr["side"] != "buy" {
		t.Fatalf("list_trades: %v", tr)
	}
	money(t, tr["price"], 520, "USD")

	// Reminders and the weekly review.
	_, sc, _ = callS(t, cs, "list_reminders", map[string]any{})
	arr(t, sc["items"])
	_, sc, _ = callS(t, cs, "weekly_review", map[string]any{})
	inZone(t, sc["from"])
	arr(t, sc["goals"])
	arr(t, sc["carriedTasks"])

	// Completing, then deleting.
	if _, sc, _ = callS(t, cs, "complete_task", map[string]any{"id": tid}); sc["completed"] != true {
		t.Fatalf("complete_task: %v", sc)
	}
	inZone(t, sc["completeTime"])
	for _, d := range []struct{ tool, id, kind, text string }{
		{"delete_task", tid, "task", "deleted task " + tid},
		{"delete_event", eid, "event", "deleted event " + eid},
		{"delete_note", nid, "note", "deleted note " + nid},
		{"delete_time_entry", lid, "time", "deleted time entry " + lid},
		{"delete_goal", gid, "goal", "deleted goal " + gid},
		{"delete_objective", oid, "objective", "deleted objective " + oid},
		{"delete_holding", hid, "holding", "deleted holding " + hid},
		{"delete_project", pid, "project", "deleted project " + pid},
	} {
		text, sc, isErr := callS(t, cs, d.tool, map[string]any{"id": d.id})
		if isErr || text != d.text || sc["deleted"] != d.id || sc["kind"] != d.kind || len(sc) != 2 {
			t.Fatalf("%s: %v %q %v", d.tool, isErr, text, sc)
		}
	}
}

// Every tool, AI ones included, declares an object outputSchema; a call that
// fails is still a plain tool error, without structured content.
func TestMCPEveryToolHasAnOutputSchema(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "Hello.", nil }}
	e, _ := startWithAI(t, f, false)
	cs := mcpSession(t, e, e.alice)
	n := 0
	for name := range toolNames(t, cs) {
		outputSchema(t, cs, name)
		n++
	}
	if n < 70 {
		t.Fatalf("only %d tools", n)
	}
	if outputSchema(t, cs, "get_task").Validate(map[string]any{"kind": "task", "id": "x"}) == nil {
		t.Fatal("the schema should require a task's fields")
	}
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_task", Arguments: map[string]any{"id": "nope"}})
	if err != nil || !res.IsError || res.StructuredContent != nil {
		t.Fatalf("a failed call: %v %+v", err, res)
	}
	_, sc, _ := callS(t, cs, "ask_assistant", map[string]any{"message": "hi"})
	if sc["reply"] != "Hello." {
		t.Fatalf("ask_assistant: %v", sc)
	}
}
