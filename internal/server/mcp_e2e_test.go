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

var idRE = regexp.MustCompile(`"id":\s*"([^"]+)"`)

// firstID pulls the first "id" out of protojson output, whose whitespace is
// deliberately unstable.
func firstID(s string) string {
	if m := idRE.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}
