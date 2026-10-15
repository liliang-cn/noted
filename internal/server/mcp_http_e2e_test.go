package server_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/mcphttp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startMCPHTTP serves remote MCP the way `noted serve` does with mcp_listen set.
func startMCPHTTP(t *testing.T, e *env, authDisabled bool) string {
	t.Helper()
	a, err := auth.New(context.Background(), e.st, authDisabled)
	if err != nil {
		t.Fatal(err)
	}
	h := mcphttp.New(mcphttp.Options{Auth: a, Conn: e.conn(t), Location: time.UTC, Version: "test"})
	hs := httptest.NewServer(h)
	t.Cleanup(hs.Close)
	return hs.URL
}

type withBearer struct{ tok string }

func (b withBearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return http.DefaultTransport.RoundTrip(r)
}

func httpSession(t *testing.T, base, token string) (*mcp.ClientSession, error) {
	t.Helper()
	tr := &mcp.StreamableClientTransport{Endpoint: base + "/mcp", HTTPClient: &http.Client{Transport: withBearer{token}}, MaxRetries: -1}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(context.Background(), tr, nil)
	if err == nil {
		t.Cleanup(func() { cs.Close() })
	}
	return cs, err
}

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}`

func post(t *testing.T, url, auth, session, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
		req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestMCPHTTPNeedsAToken(t *testing.T) {
	e := start(t, nil, false)
	base := startMCPHTTP(t, e, false)

	for name, h := range map[string]string{"none": "", "bad": "Bearer noted_bogus", "not bearer": "Basic " + e.alice, "empty": "Bearer "} {
		resp := post(t, base+"/mcp", h, "", initBody)
		if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s: %d %q", name, resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
		}
	}
	if _, err := httpSession(t, base, "noted_bogus"); err == nil {
		t.Fatal("a client with a bad token must not connect")
	}

	resp, err := http.Get(base + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %v %v", err, resp)
	}
	resp.Body.Close()
}

func TestMCPHTTPToolsActAsTheTokensUser(t *testing.T) {
	e := start(t, nil, false)
	base := startMCPHTTP(t, e, false)

	alice, err := httpSession(t, base, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	if alice.InitializeResult().ServerInfo.Name != "noted" {
		t.Fatalf("server info: %+v", alice.InitializeResult().ServerInfo)
	}
	names := toolNames(t, alice)
	for _, want := range []string{"create_task", "list_tasks", "create_note", "list_events"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	// Same tools as the stdio server.
	stdio := toolNames(t, mcpSession(t, e, e.alice))
	if len(stdio) != len(names) {
		t.Fatalf("HTTP offers %d tools, stdio %d", len(names), len(stdio))
	}

	out, isErr := call(t, alice, "create_task", map[string]any{"title": "Water the plants"})
	if isErr || firstID(out) == "" {
		t.Fatalf("create_task: %s", out)
	}
	if out, _ := call(t, alice, "list_tasks", map[string]any{}); !strings.Contains(out, "Water the plants") {
		t.Fatalf("alice's list_tasks: %s", out)
	}
	// The task really is alice's, over gRPC too.
	tl, err := pb.NewCalendarServiceClient(e.conn(t)).ListTasks(as(e.alice), &pb.ListTasksRequest{})
	if err != nil || len(tl.Tasks) != 1 {
		t.Fatalf("alice's tasks over gRPC: %v %+v", err, tl)
	}

	bob, err := httpSession(t, base, e.bob)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := call(t, bob, "list_tasks", map[string]any{}); strings.Contains(out, "Water the plants") {
		t.Fatalf("bob sees alice's task: %s", out)
	}
}

func TestMCPHTTPSessionsAreBoundToTheirToken(t *testing.T) {
	e := start(t, nil, false)
	base := startMCPHTTP(t, e, false)

	resp := post(t, base+"/mcp", "Bearer "+e.alice, "", initBody)
	sid := resp.Header.Get("Mcp-Session-Id")
	if resp.StatusCode != http.StatusOK || sid == "" {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("initialize: %d %s", resp.StatusCode, b)
	}
	// Bob holding alice's session id gets nowhere.
	list := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	if r := post(t, base+"/mcp", "Bearer "+e.bob, sid, list); r.StatusCode != http.StatusForbidden {
		t.Fatalf("bob on alice's session: %d", r.StatusCode)
	}
	if r := post(t, base+"/mcp", "", sid, list); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token on alice's session: %d", r.StatusCode)
	}
}

func TestMCPHTTPRevokedTokenIsRefused(t *testing.T) {
	e := start(t, nil, false)
	base := startMCPHTTP(t, e, false)
	acc := pb.NewAccountServiceClient(e.conn(t))
	cr, err := acc.CreateToken(as(e.alice), &pb.CreateTokenRequest{Label: "Codex"})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := httpSession(t, base, cr.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, isErr := call(t, cs, "list_tasks", map[string]any{}); isErr {
		t.Fatal("list_tasks before revoking")
	}
	if _, err := acc.RevokeToken(as(e.alice), &pb.RevokeTokenRequest{Id: cr.Info.Id}); err != nil {
		t.Fatal(err)
	}
	// The open session is cut off, and a new one cannot start.
	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_tasks", Arguments: map[string]any{}}); err == nil {
		t.Fatal("a revoked token's session still answers")
	}
	if r := post(t, base+"/mcp", "Bearer "+cr.Token, "", initBody); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", r.StatusCode)
	}
}

func TestMCPHTTPAuthDisabled(t *testing.T) {
	e := start(t, nil, true)
	base := startMCPHTTP(t, e, true)
	tr := &mcp.StreamableClientTransport{Endpoint: base + "/mcp", MaxRetries: -1}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(context.Background(), tr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if out, isErr := call(t, cs, "create_note", map[string]any{"title": "solo"}); isErr {
		t.Fatalf("create_note: %s", out)
	}
}
