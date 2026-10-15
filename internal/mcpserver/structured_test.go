package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeConn answers gRPC calls from canned replies by method name.
type fakeConn map[string]proto.Message

func (f fakeConn) Invoke(_ context.Context, method string, _, reply any, _ ...grpc.CallOption) error {
	m, ok := f[method]
	if !ok {
		return fmt.Errorf("unexpected call %s", method)
	}
	proto.Merge(reply.(proto.Message), m)
	return nil
}

func (fakeConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, fmt.Errorf("no streams")
}

var (
	shanghai, _ = time.LoadLocation("Asia/Shanghai")
	due         = timestamppb.New(time.Date(2030, 3, 5, 1, 0, 0, 0, time.UTC)) // 09:00 in Shanghai
	cannedTask  = &pb.Task{Id: "t1", Title: "Visa", DueTime: due, Priority: pb.Priority_PRIORITY_HIGH, Space: pb.Space_SPACE_WORK, Tags: []string{"travel"}}
	cannedEvent = &pb.Event{Id: "e1", Title: "Dentist", StartTime: due, EndTime: timestamppb.New(due.AsTime().Add(time.Hour)), Space: pb.Space_SPACE_LIFE}
)

func session(t *testing.T, conn fakeConn) *mcp.ClientSession {
	t.Helper()
	conn[pb.AIService_GetStatus_FullMethodName] = &pb.GetStatusResponse{Chat: true}
	srv, err := New(context.Background(), conn, shanghai, "test")
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

func resolve(t *testing.T, schema any) *jsonschema.Resolved {
	t.Helper()
	raw, _ := json.Marshal(schema)
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Type != "object" {
		t.Fatalf("outputSchema type %q, want object", s.Type)
	}
	r, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestEveryToolDeclaresAnObjectOutputSchema(t *testing.T) {
	cs := session(t, fakeConn{})
	l, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range l.Tools {
		if tl.OutputSchema == nil {
			t.Errorf("%s has no outputSchema", tl.Name)
			continue
		}
		resolve(t, tl.OutputSchema)
	}
	if len(l.Tools) < 70 {
		t.Fatalf("only %d tools", len(l.Tools))
	}
}

func TestStructuredTaskKeepsTheText(t *testing.T) {
	cs := session(t, fakeConn{pb.CalendarService_GetTask_FullMethodName: cannedTask})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_task", Arguments: map[string]any{"id": "t1"}})
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	want, _ := protojson.MarshalOptions{Multiline: true, Indent: " "}.Marshal(cannedTask)
	if len(res.Content) != 1 || res.Content[0].(*mcp.TextContent).Text != string(want) {
		t.Fatalf("text changed: %+v", res.Content)
	}
	got, _ := json.Marshal(res.StructuredContent)
	var tk taskOut
	if err := json.Unmarshal(got, &tk); err != nil {
		t.Fatal(err)
	}
	if tk.Kind != "task" || tk.ID != "t1" || tk.Link != "noted://task/t1" || tk.DueTime != "2030-03-05T09:00:00+08:00" ||
		tk.Priority != 3 || tk.Space != "work" || tk.Overdue || len(tk.Tags) != 1 {
		t.Fatalf("structured task: %s", got)
	}
}

func TestOverviewItemsAreTasksOrEvents(t *testing.T) {
	focus := &pb.GetFocusResponse{From: due, To: due, Items: []*pb.FocusItem{
		{Item: &pb.FocusItem_Task{Task: cannedTask}, Overdue: true, ProjectTitle: "Trip"},
		{Item: &pb.FocusItem_Event{Event: &pb.Occurrence{Event: cannedEvent, StartTime: due, EndTime: due}}},
	}}
	cs := session(t, fakeConn{pb.FocusService_GetFocus_FullMethodName: focus})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_overview", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	var out struct {
		Horizon string           `json:"horizon"`
		Items   []map[string]any `json:"items"`
	}
	raw, _ := json.Marshal(res.StructuredContent)
	json.Unmarshal(raw, &out)
	if out.Horizon != "today" || len(out.Items) != 2 || out.Items[0]["kind"] != "task" || out.Items[0]["overdue"] != true ||
		out.Items[0]["projectTitle"] != "Trip" || out.Items[1]["kind"] != "event" || out.Items[1]["link"] != "noted://event/e1" {
		t.Fatalf("get_overview: %s", raw)
	}

	// The schema holds tasks and events only.
	l, _ := cs.ListTools(context.Background(), nil)
	for _, tl := range l.Tools {
		if tl.Name != "get_overview" {
			continue
		}
		r := resolve(t, tl.OutputSchema)
		var v map[string]any
		json.Unmarshal(raw, &v)
		if err := r.Validate(v); err != nil {
			t.Fatal(err)
		}
		v["items"] = []any{map[string]any{"kind": "note", "id": "n1", "link": "noted://note/n1", "title": "x"}}
		if err := r.Validate(v); err == nil {
			t.Fatal("a note passed as an overview item")
		}
	}
}

func TestConversions(t *testing.T) {
	tl := &tools{loc: shanghai}

	h := tl.holding(&pb.Holding{Id: "h1", Symbol: "VOO", Currency: "USD", Position: &pb.Position{Shares: 2, AvgCost: 500, CostBasis: 1000}})
	if h.LastPrice != nil || h.Plan != nil || h.Position.MarketValue != nil || h.Position.UnrealizedPercent != nil ||
		h.Position.AvgCost != (money{Amount: 500, Currency: "USD"}) || h.Link != "noted://holding/h1" {
		t.Fatalf("holding without a price or plan: %+v", h)
	}

	if w := tl.taskWork(&pb.TaskWork{Id: "w1", TaskId: "t1", Progress: -1}); w.Progress != nil || w.Link != "noted://task/t1" {
		t.Fatalf("a sitting without progress: %+v", w)
	}
	if w := tl.taskWork(&pb.TaskWork{Id: "w1", TaskId: "t1", Progress: 0}); w.Progress == nil || *w.Progress != 0 {
		t.Fatalf("a sitting at 0%%: %+v", w)
	}

	rows := []*pb.TimeSummaryRow{{Key: "p1", Label: "Trip", Minutes: 30}, {Key: "", Label: "", Minutes: 5}}
	g := timeGroups(rows, pb.TimeGroupBy_TIME_GROUP_BY_PROJECT)
	if g[0].Link != "noted://project/p1" || g[1].Link != "" || g[0].Kind != "timeGroup" {
		t.Fatalf("time groups: %+v %+v", g[0], g[1])
	}
	if g := timeGroups(rows, pb.TimeGroupBy_TIME_GROUP_BY_TITLE); g[0].Link != "" {
		t.Fatalf("an activity has no page: %+v", g[0])
	}
	if g := timeGroups([]*pb.TimeSummaryRow{{Key: "2026-10-13", Label: "2026-10-13", Minutes: 30}}, pb.TimeGroupBy_TIME_GROUP_BY_DAY); g[0].Date != "2026-10-13" || g[0].Link != "" {
		t.Fatalf("a day row carries its date: %+v", g[0])
	}
	if g[0].Date != "" {
		t.Fatalf("only day rows have a date: %+v", g[0])
	}

	// 23:40 in Shanghai on the 13th is the 13th, though it is 15:40 UTC.
	start := timestamppb.New(time.Date(2026, 10, 13, 15, 40, 0, 0, time.UTC))
	te := tl.timeEntry(&pb.TimeEntry{Id: "x1", Title: "游泳", Minutes: 30, StartTime: start,
		EndTime: timestamppb.New(start.AsTime().Add(30 * time.Minute)), Timed: true})
	if te.Day != "2026-10-13" || !te.Timed || te.StartTime != "2026-10-13T23:40:00+08:00" || te.EndTime != "2026-10-14T00:10:00+08:00" {
		t.Fatalf("time entry: %+v", te)
	}
	if te := tl.timeEntry(&pb.TimeEntry{Id: "x2", Title: "游泳", Minutes: 30, StartTime: start}); te.Timed || te.Day != "2026-10-13" {
		t.Fatalf("untimed entry: %+v", te)
	}

	r := tl.reminder(&pb.Reminder{Id: "r1", Kind: pb.Reminder_KIND_TASK, RefId: "t1", FireTime: due})
	if r.Link != "noted://task/t1" || r.RefKind != "task" || r.FireTime != "2030-03-05T09:00:00+08:00" {
		t.Fatalf("reminder: %+v", r)
	}
	if tl.ts(nil) != "" {
		t.Fatal("an unset time should be empty")
	}
}
