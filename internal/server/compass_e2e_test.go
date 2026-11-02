package server_test

import (
	"strings"
	"testing"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
)

func TestCompassOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	cp, focus := pb.NewCompassServiceClient(conn), pb.NewFocusServiceClient(conn)
	ctx := as(e.alice)

	c, err := cp.GetCompass(ctx, &pb.GetCompassRequest{})
	if err != nil || c.Version != 0 || c.Space != pb.Space_SPACE_LIFE && c.Space != pb.Space_SPACE_UNSPECIFIED {
		t.Fatalf("empty: %+v %v", c, err)
	}
	if _, err := cp.SetCompass(ctx, &pb.SetCompassRequest{}); code(err) != codes.InvalidArgument {
		t.Fatalf("no compass: %v", err)
	}
	if _, err := cp.SetCompass(ctx, &pb.SetCompassRequest{Compass: &pb.Compass{Vision: "x"}, UpdateMask: mask("nope")}); code(err) != codes.InvalidArgument {
		t.Fatalf("bad mask: %v", err)
	}

	c, err = cp.SetCompass(ctx, &pb.SetCompassRequest{Compass: &pb.Compass{AntiVision: "a life of meetings", Vision: "build things that last",
		Identity: "我是那种每天动手的人", Constraints: []string{"health", "family"}}})
	if err != nil || c.Version != 1 || c.UpdateTime == nil || len(c.Constraints) != 2 {
		t.Fatalf("set: %+v %v", c, err)
	}
	// The mask merges: only the vision changes.
	c, err = cp.SetCompass(ctx, &pb.SetCompassRequest{Compass: &pb.Compass{Vision: "build things people use"}, UpdateMask: mask("vision")})
	if err != nil || c.Version != 2 || c.Vision != "build things people use" || c.AntiVision != "a life of meetings" || len(c.Constraints) != 2 {
		t.Fatalf("merge: %+v %v", c, err)
	}
	h, err := cp.ListCompassRevisions(ctx, &pb.ListCompassRevisionsRequest{})
	if err != nil || len(h.Revisions) != 2 || h.Revisions[0].Version != 2 || h.Revisions[1].Vision != "build things that last" {
		t.Fatalf("history: %+v %v", h, err)
	}

	// Work has its own, and Bob never sees Alice's.
	if w, _ := cp.GetCompass(ctx, &pb.GetCompassRequest{Space: pb.Space_SPACE_WORK}); w.Version != 0 {
		t.Fatalf("work: %+v", w)
	}
	if b, _ := cp.GetCompass(as(e.bob), &pb.GetCompassRequest{}); b.Version != 0 || b.Vision != "" {
		t.Fatalf("bob: %+v", b)
	}

	// The overview carries it.
	f, err := focus.GetFocus(ctx, &pb.GetFocusRequest{})
	if err != nil || f.Compass.GetVision() != "build things people use" {
		t.Fatalf("focus: %+v %v", f.Compass, err)
	}
	if f, _ := focus.GetFocus(as(e.bob), &pb.GetFocusRequest{}); f.Compass != nil {
		t.Fatalf("bob focus: %+v", f.Compass)
	}
}

func TestMCPCompassAndWeeklyReviewBehavior(t *testing.T) {
	e := start(t, nil, false)
	cs := mcpSession(t, e, e.alice)

	if out, _, isErr := callS(t, cs, "get_compass", map[string]any{}); isErr || !strings.Contains(out, `"version"`) && strings.Contains(out, "vision") {
		t.Fatalf("empty compass: %s", out)
	}
	if _, isErr := call(t, cs, "set_compass", map[string]any{}); !isErr {
		t.Fatal("set_compass with nothing should fail")
	}
	_, sc, isErr := callS(t, cs, "set_compass", map[string]any{"vision": "build things that last", "anti_vision": "a life of meetings", "constraints": []string{"health"}})
	if isErr || sc["version"] != float64(1) {
		t.Fatalf("set_compass: %v", sc)
	}
	// Omitted fields are merged.
	_, sc, _ = callS(t, cs, "set_compass", map[string]any{"identity": "我是那种每天动手的人"})
	if sc["version"] != float64(2) || sc["vision"] != "build things that last" || sc["identity"] != "我是那种每天动手的人" {
		t.Fatalf("merge: %v", sc)
	}
	_, sc, _ = callS(t, cs, "get_compass", map[string]any{"include_history": true})
	if hist, _ := sc["history"].([]any); len(hist) != 2 {
		t.Fatalf("history: %v", sc)
	}

	_, sc, _ = callS(t, cs, "get_overview", map[string]any{})
	if c, _ := sc["compass"].(map[string]any); c["vision"] != "build things that last" {
		t.Fatalf("overview compass: %v", sc["compass"])
	}

	// An objective whose goal has no check-in this week shows up as a mismatch, after the compass.
	call(t, cs, "create_objective", map[string]any{"title": "Fitness"})
	out, sc, isErr := callS(t, cs, "weekly_review", map[string]any{})
	if isErr {
		t.Fatalf("weekly_review: %s", out)
	}
	c, _ := sc["compass"].(map[string]any)
	b, _ := sc["behavior"].(map[string]any)
	if c["antiVision"] != "a life of meetings" || b == nil {
		t.Fatalf("review: compass %v behavior %v", c, b)
	}
	if m, _ := b["mismatches"].([]any); len(m) != 1 || !strings.Contains(m[0].(string), "Fitness") {
		t.Fatalf("mismatches: %v", b["mismatches"])
	}
	if i, j := strings.Index(out, "build things that last"), strings.Index(out, "tasksDone"); i < 0 || j >= 0 && i > j {
		t.Fatalf("the compass should head the text: %s", out)
	}
}
