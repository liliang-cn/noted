package server_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/liliang-cn/noted/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// mcpSession connects an in-memory MCP client to an MCP server that talks to
// the test noted server as the given token's user.
func mcpSession(t *testing.T, e *env, token string) *mcp.ClientSession {
	t.Helper()
	conn, err := grpc.NewClient(e.addr, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(func(ctx context.Context, m string, req, rep any, cc *grpc.ClientConn, inv grpc.UnaryInvoker, o ...grpc.CallOption) error {
			return inv(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token), m, req, rep, cc, o...)
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	srv, err := mcpserver.New(context.Background(), conn, time.UTC, "test")
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Run(ctx, st)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func toolNames(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	l, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, tl := range l.Tools {
		out[tl.Name] = true
	}
	return out
}

func TestMCPNotesEventsTasks(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)

	names := toolNames(t, cs)
	for _, want := range []string{"create_note", "search_notes", "create_event", "list_events", "update_event", "create_task", "complete_task", "delete_note"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	if names["ask_assistant"] {
		t.Error("AI tools must not be offered when the server has AI off")
	}

	out, isErr := call(t, cs, "create_note", map[string]any{"title": "Trip", "content": "book flights to Osaka", "tags": []string{"Travel"}})
	if isErr || !strings.Contains(out, `"travel"`) {
		t.Fatalf("create_note: %s", out)
	}
	out, _ = call(t, cs, "search_notes", map[string]any{"query": "Osaka"})
	if !strings.Contains(out, "Trip") {
		t.Fatalf("search_notes: %s", out)
	}

	// Event with a zone-less time (read in the MCP server's zone), then reschedule it.
	out, isErr = call(t, cs, "create_event", map[string]any{"title": "Dentist", "start": "2026-11-03T15:00:00", "remind_before_minutes": 30})
	if isErr || !strings.Contains(out, "2026-11-03T15:00:00Z") {
		t.Fatalf("create_event: %s", out)
	}
	id := firstID(out)
	out, isErr = call(t, cs, "update_event", map[string]any{"id": id, "start": "2026-11-04T16:00:00Z"})
	if isErr || !strings.Contains(out, "2026-11-04T16:00:00Z") || !strings.Contains(out, "2026-11-04T17:00:00Z") {
		t.Fatalf("update_event should move start and keep the 1h duration: %s", out)
	}
	out, _ = call(t, cs, "list_events", map[string]any{"from": "2026-11-01", "to": "2026-11-08"})
	if !strings.Contains(out, "Dentist") {
		t.Fatalf("list_events: %s", out)
	}
	if out, isErr = call(t, cs, "update_event", map[string]any{"id": id}); !isErr {
		t.Fatalf("empty update should be a tool error, got %s", out)
	}

	out, _ = call(t, cs, "create_task", map[string]any{"title": "Pay rent", "priority": 3})
	tid := firstID(out)
	call(t, cs, "complete_task", map[string]any{"id": tid})
	if open, _ := call(t, cs, "list_tasks", map[string]any{}); strings.Contains(open, "Pay rent") {
		t.Fatalf("completed task still open: %s", open)
	}

	// Errors surface as tool errors the model can read, not protocol failures.
	if out, isErr = call(t, cs, "create_event", map[string]any{"title": "x", "start": "not a time"}); !isErr || !strings.Contains(out, "RFC 3339") {
		t.Fatalf("bad time: %v %s", isErr, out)
	}
	if out, isErr = call(t, cs, "delete_event", map[string]any{"id": id}); isErr {
		t.Fatalf("delete_event: %s", out)
	}
	if _, isErr = call(t, cs, "delete_event", map[string]any{"id": id}); !isErr {
		t.Fatal("second delete should report not found")
	}
}

func TestMCPIsScopedToTheTokenUser(t *testing.T) {
	e := start(t, nil, false)
	alice, bob := mcpSession(t, e, e.alice), mcpSession(t, e, e.bob)
	out, _ := call(t, alice, "create_note", map[string]any{"title": "private", "content": "x"})
	id := firstID(out)
	if _, isErr := call(t, bob, "get_note", map[string]any{"id": id}); !isErr {
		t.Fatal("bob read alice's note over MCP")
	}
	if _, isErr := call(t, bob, "delete_note", map[string]any{"id": id}); !isErr {
		t.Fatal("bob deleted alice's note over MCP")
	}
}

func TestMCPBadTokenFailsAtStartup(t *testing.T) {
	e := start(t, nil, false)
	conn, _ := grpc.NewClient(e.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer conn.Close()
	if _, err := mcpserver.New(context.Background(), conn, time.UTC, "test"); err == nil {
		t.Fatal("a missing token should fail fast with a clear error")
	}
}

func TestMCPOffersAITools(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "Hello from the assistant.", nil }}
	e, _ := startWithAI(t, f, false)
	cs := mcpSession(t, e, e.alice)
	if !toolNames(t, cs)["ask_assistant"] {
		t.Fatal("ask_assistant missing with AI on")
	}
	out, isErr := call(t, cs, "ask_assistant", map[string]any{"message": "hi"})
	if isErr || !strings.Contains(out, "Hello from the assistant.") {
		t.Fatalf("%v %s", isErr, out)
	}
}

var (
	doneRE   = regexp.MustCompile(`"done":\s*20`)
	pinnedRE = regexp.MustCompile(`"pinned":\s*true`)
)

var idRE = regexp.MustCompile(`"id":\s*"([^"]+)"`)

// firstID pulls the first "id" out of protojson output, whose whitespace is
// deliberately unstable.
func firstID(s string) string {
	if m := idRE.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func TestMCPProjectsGoalsAndOverview(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)

	names := toolNames(t, cs)
	for _, want := range []string{"get_overview", "list_projects", "get_project", "create_project", "update_project", "list_goals", "create_goal", "check_in"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}

	out, isErr := call(t, cs, "create_project", map[string]any{"title": "Philippines trip", "pinned": true, "space": "life", "start": "2030-11-20"})
	if isErr {
		t.Fatalf("create_project: %s", out)
	}
	pid := firstID(out)
	out, isErr = call(t, cs, "create_task", map[string]any{"title": "Apply for visa", "project_id": pid, "due": "2030-10-18"})
	if isErr || !strings.Contains(out, "SPACE_LIFE") {
		t.Fatalf("a task should join the project and inherit its space: %v %s", isErr, out)
	}
	call(t, cs, "create_task", map[string]any{"title": "Ship release", "space": "work"})

	out, _ = call(t, cs, "get_project", map[string]any{"id": pid})
	if !strings.Contains(out, "Apply for visa") || !strings.Contains(out, "tasksTotal") {
		t.Fatalf("get_project: %s", out)
	}
	work, _ := call(t, cs, "list_tasks", map[string]any{"space": "work"})
	life, _ := call(t, cs, "list_tasks", map[string]any{"space": "life"})
	if !strings.Contains(work, "Ship release") || strings.Contains(work, "Apply for visa") ||
		!strings.Contains(life, "Apply for visa") || strings.Contains(life, "Ship release") {
		t.Fatalf("space filter over MCP:\nwork=%s\nlife=%s", work, life)
	}
	if _, isErr := call(t, cs, "list_tasks", map[string]any{"space": "play"}); !isErr {
		t.Fatal("a bad space should be a tool error")
	}

	// Pin state changes through update_project.
	out, isErr = call(t, cs, "update_project", map[string]any{"id": pid, "pinned": false})
	if isErr || pinnedRE.MatchString(out) {
		t.Fatalf("update_project: %v %s", isErr, out)
	}
	if _, isErr := call(t, cs, "update_project", map[string]any{"id": pid}); !isErr {
		t.Fatal("an empty update should be a tool error")
	}

	// Goals.
	out, isErr = call(t, cs, "create_goal", map[string]any{"title": "German", "period": "week", "target": 140, "unit": "minutes",
		"milestones": []string{"A1", "A2"}})
	if isErr {
		t.Fatalf("create_goal: %s", out)
	}
	gid := firstID(out)
	out, isErr = call(t, cs, "check_in", map[string]any{"goal_id": gid, "amount": 20})
	if isErr || !doneRE.MatchString(out) {
		t.Fatalf("check_in: %v %s", isErr, out)
	}
	if _, isErr := call(t, cs, "create_goal", map[string]any{"title": "x", "period": "year", "target": 1}); !isErr {
		t.Fatal("a bad period should be a tool error")
	}

	// The overview ties it together.
	out, isErr = call(t, cs, "get_overview", map[string]any{"horizon": "upcoming"})
	if isErr || !strings.Contains(out, "German") {
		t.Fatalf("get_overview: %v %s", isErr, out)
	}
	if _, isErr := call(t, cs, "get_overview", map[string]any{"horizon": "decade"}); !isErr {
		t.Fatal("a bad horizon should be a tool error")
	}

	// Another user sees none of it.
	other := mcpSession(t, e, e.bob)
	if out, _ := call(t, other, "list_projects", map[string]any{}); strings.Contains(out, "Philippines") {
		t.Fatalf("project leaked over MCP: %s", out)
	}
}

func TestMCPSuggestionsAcceptAndUndo(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)
	for _, want := range []string{"list_suggestions", "accept_suggestion", "dismiss_suggestion", "undo_change", "list_changes", "weekly_review"} {
		if !toolNames(t, cs)[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	if toolNames(t, cs)["plan_from_text"] {
		t.Error("plan_from_text needs a language model and must not be offered when AI is off")
	}

	out, _ := call(t, cs, "create_task", map[string]any{"title": "Pay rent", "due": time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02") + "T17:00:00Z"})
	taskID := firstID(out)

	out, isErr := call(t, cs, "list_suggestions", map[string]any{})
	if isErr || !strings.Contains(out, "Pay rent") || !strings.Contains(out, "overdue") {
		t.Fatalf("list_suggestions: %v %s", isErr, out)
	}
	sid := firstID(out)
	if got, _ := call(t, cs, "get_overview", map[string]any{}); !strings.Contains(got, "Pay rent") {
		t.Fatalf("overview: %s", got)
	}

	out, isErr = call(t, cs, "accept_suggestion", map[string]any{"id": sid})
	if isErr || !strings.Contains(out, "accepted") {
		t.Fatalf("accept_suggestion: %v %s", isErr, out)
	}
	changes, _ := call(t, cs, "list_changes", map[string]any{})
	cid := firstID(changes)
	if _, isErr := call(t, cs, "undo_change", map[string]any{"id": cid}); isErr {
		t.Fatal("undo_change failed")
	}
	if _, isErr := call(t, cs, "undo_change", map[string]any{"id": cid}); !isErr {
		t.Fatal("a second undo should be a tool error")
	}
	if _, isErr := call(t, cs, "accept_suggestion", map[string]any{"id": "nope"}); !isErr {
		t.Fatal("an unknown suggestion should be a tool error")
	}
	_ = taskID

	if out, isErr := call(t, cs, "weekly_review", map[string]any{}); isErr || !strings.Contains(out, "tasksTotal") && !strings.Contains(out, "from") {
		t.Fatalf("weekly_review: %v %s", isErr, out)
	}
	// Another user's assistant tools never see these.
	other := mcpSession(t, e, e.bob)
	if out, _ := call(t, other, "list_suggestions", map[string]any{}); strings.Contains(out, "Pay rent") {
		t.Fatalf("suggestions leaked: %s", out)
	}
}

func TestMCPOffersAIPlanningWhenAIIsOn(t *testing.T) {
	f := &fakeProvider{script: answerJSON("break it into ONE project", `{"project":"Trip","tasks":[{"title":"Visa"}]}`)}
	e, _ := startWithAI(t, f, false)
	cs := mcpSession(t, e, e.alice)
	if !toolNames(t, cs)["plan_from_text"] || !toolNames(t, cs)["extract_tasks"] {
		t.Fatal("AI planning tools missing")
	}
	out, isErr := call(t, cs, "plan_from_text", map[string]any{"text": "going on a trip, need a visa"})
	if isErr || !strings.Contains(out, "Visa") || !strings.Contains(out, `"pending"`) {
		t.Fatalf("%v %s", isErr, out)
	}
	if list, _ := call(t, cs, "list_projects", map[string]any{}); strings.Contains(list, "Trip") {
		t.Fatalf("planning must not create the project: %s", list)
	}
}

func TestMCPGoalWithACounter(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)
	out, isErr := call(t, cs, "create_goal", map[string]any{"title": "German", "period": "week", "target": 140, "unit": "minutes",
		"counter_unit": "lessons", "counter_target": 40})
	if isErr || !strings.Contains(out, "lessons") {
		t.Fatalf("%v %s", isErr, out)
	}
	gid := firstID(out)
	out, isErr = call(t, cs, "check_in", map[string]any{"goal_id": gid, "amount": 20, "count": 1})
	if isErr || !regexp.MustCompile(`"counterDone":\s*1`).MatchString(out) {
		t.Fatalf("%v %s", isErr, out)
	}
	if _, isErr := call(t, cs, "create_goal", map[string]any{"title": "x", "period": "week", "target": 1, "counter_target": 5}); !isErr {
		t.Fatal("a counter target without a unit should be a tool error")
	}
}
