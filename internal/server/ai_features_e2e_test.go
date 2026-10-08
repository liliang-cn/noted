package server_test

import (
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAskReturnsWhatItLookedAt(t *testing.T) {
	f := &fakeProvider{}
	f.script = func(req map[string]any) (string, []map[string]any) {
		if _, has := toolResult(req); has {
			return "还有两项没做完:提交周报,以及提交 Q4 预算。", nil
		}
		return "", []map[string]any{toolCall("c1", "list_tasks", map[string]any{})}
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	cal := pb.NewCalendarServiceClient(conn)
	due := timestamppb.New(time.Now().Add(48 * time.Hour))
	for _, title := range []string{"提交周报", "提交 Q4 预算", "招聘 JD 定稿"} {
		cal.CreateTask(as(e.alice), &pb.CreateTaskRequest{Task: &pb.Task{Title: title, DueTime: due, Space: pb.Space_SPACE_WORK}})
	}
	cal.CreateTask(as(e.bob), &pb.CreateTaskRequest{Task: &pb.Task{Title: "提交周报 (bob)", DueTime: due}})

	r, err := pb.NewAIServiceClient(conn).Ask(as(e.alice), &pb.AskRequest{Message: "这周工作还有什么没做完?", Space: pb.Space_SPACE_WORK})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.References) != 2 {
		t.Fatalf("only the two tasks the answer names should be shown: %+v", r.References)
	}
	for _, ref := range r.References {
		if ref.Kind != "task" || ref.Id == "" || ref.Time == nil || !strings.HasPrefix(ref.Title, "提交") || strings.Contains(ref.Title, "bob") {
			t.Fatalf("%+v", ref)
		}
	}
	// Each reference is a real task the user can open.
	for _, ref := range r.References {
		if _, err := cal.GetTask(as(e.alice), &pb.GetTaskRequest{Id: ref.Id}); err != nil {
			t.Fatalf("reference %s does not resolve: %v", ref.Id, err)
		}
	}
}

func TestFeatureSwitches(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "text", nil }}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	ai, notes, sug, cal := pb.NewAIServiceClient(conn), pb.NewNoteServiceClient(conn), pb.NewSuggestionServiceClient(conn), pb.NewCalendarServiceClient(conn)
	n, _ := notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "t", Content: "订酒店。办电子签。"}})
	cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Pay rent", DueTime: timestamppb.New(utcMidnight().AddDate(0, 0, -3).Add(9 * time.Hour))}})

	got, err := ai.GetAIFeatures(ctx, &pb.GetAIFeaturesRequest{})
	if err != nil || !got.DailyBriefing || !got.Suggestions || !got.WeeklyReview || !got.NoteTools {
		t.Fatalf("everything is on by default: %+v %v", got, err)
	}
	if l, _ := sug.ListProposals(ctx, &pb.ListProposalsRequest{}); len(l.Proposals) != 1 {
		t.Fatalf("suggestions on: %d", len(l.Proposals))
	}

	// Turn them off one family at a time.
	set := func(f *pb.SetAIFeaturesRequest) {
		t.Helper()
		if _, err := ai.SetAIFeatures(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	set(&pb.SetAIFeaturesRequest{Suggestions: true, WeeklyReview: true, NoteTools: true}) // briefing off
	if _, err := ai.DailyBriefing(ctx, &pb.DailyBriefingRequest{}); code(err) != codes.FailedPrecondition {
		t.Fatalf("briefing off: %v", err)
	}
	if _, err := ai.SummarizeNote(ctx, &pb.SummarizeNoteRequest{NoteId: n.Id}); err != nil {
		t.Fatalf("note tools are still on: %v", err)
	}

	set(&pb.SetAIFeaturesRequest{DailyBriefing: true, Suggestions: true, WeeklyReview: true}) // note tools off
	for name, call := range map[string]func() error{
		"summary": func() error { _, err := ai.SummarizeNote(ctx, &pb.SummarizeNoteRequest{NoteId: n.Id}); return err },
		"tags":    func() error { _, err := ai.SuggestTags(ctx, &pb.SuggestTagsRequest{NoteId: n.Id}); return err },
		"extract": func() error { _, err := ai.ExtractTasks(ctx, &pb.ExtractTasksRequest{NoteId: n.Id}); return err },
	} {
		if code(call()) != codes.FailedPrecondition {
			t.Errorf("%s should be off: %v", name, call())
		}
	}
	if _, err := ai.DailyBriefing(ctx, &pb.DailyBriefingRequest{}); err != nil {
		t.Fatalf("briefing is on again: %v", err)
	}

	set(&pb.SetAIFeaturesRequest{DailyBriefing: true, NoteTools: true, WeeklyReview: true}) // suggestions off
	if l, _ := sug.ListProposals(ctx, &pb.ListProposalsRequest{}); len(l.Proposals) != 0 {
		t.Fatalf("suggestions off but %d were listed", len(l.Proposals))
	}
	set(&pb.SetAIFeaturesRequest{DailyBriefing: true, NoteTools: true, Suggestions: true}) // weekly review off
	if _, err := sug.GetWeeklyReview(ctx, &pb.GetWeeklyReviewRequest{}); code(err) != codes.FailedPrecondition {
		t.Fatalf("weekly review off: %v", err)
	}

	// Switching suggestions back on brings them back; nothing was lost meanwhile.
	set(&pb.SetAIFeaturesRequest{DailyBriefing: true, Suggestions: true, WeeklyReview: true, NoteTools: true})
	if l, _ := sug.ListProposals(ctx, &pb.ListProposalsRequest{}); len(l.Proposals) != 1 {
		t.Fatalf("suggestions back on: %d", len(l.Proposals))
	}
	if _, err := sug.GetWeeklyReview(ctx, &pb.GetWeeklyReviewRequest{}); err != nil {
		t.Fatal(err)
	}

	// Per user, and not reachable through the generic preference API.
	if b, _ := ai.GetAIFeatures(as(e.bob), &pb.GetAIFeaturesRequest{}); !b.DailyBriefing {
		t.Fatal("alice's setting changed bob's")
	}
	set(&pb.SetAIFeaturesRequest{})
	prefs := pb.NewPreferenceServiceClient(conn)
	if _, err := prefs.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ai.features", Value: `{"briefing":true}`}); code(err) != codes.InvalidArgument {
		t.Fatalf("writing the reserved key: %v", err)
	}
}
