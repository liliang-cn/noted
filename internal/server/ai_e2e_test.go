package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/ai"
	"github.com/liliang-cn/noted/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeProvider is a minimal OpenAI-compatible API: scripted chat completions
// (with tool calls) and bag-of-words embeddings.
type fakeProvider struct {
	mu       sync.Mutex
	chatReqs []map[string]any
	script   func(req map[string]any) (content string, toolCalls []map[string]any)
}

func (f *fakeProvider) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.chatReqs = append(f.chatReqs, req)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if os.Getenv("FAKE_DEBUG") != "" {
			msgs, _ := req["messages"].([]any)
			for _, m := range msgs {
				mm := m.(map[string]any)
				fmt.Fprintf(os.Stderr, "  [%v] %.100v tc=%v\n", mm["role"], mm["content"], mm["tool_calls"] != nil)
			}
			fmt.Fprintln(os.Stderr, "  ---- stream:", req["stream"], "tools:", req["tools"] != nil)
		}
		content, calls := f.script(req)
		msg := map[string]any{"role": "assistant", "content": content}
		finish := "stop"
		if len(calls) > 0 {
			msg["tool_calls"] = calls
			finish = "tool_calls"
		}
		if stream, _ := req["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			delta := map[string]any{"role": "assistant", "content": content}
			if len(calls) > 0 {
				tcs := make([]map[string]any, len(calls))
				for i, c := range calls {
					tcs[i] = map[string]any{"index": i, "id": c["id"], "type": "function", "function": c["function"]}
				}
				delta["tool_calls"] = tcs
			}
			chunk := func(d map[string]any, fin any) {
				b, _ := json.Marshal(map[string]any{"id": "x", "object": "chat.completion.chunk", "model": "fake",
					"choices": []map[string]any{{"index": 0, "delta": d, "finish_reason": fin}}})
				fmt.Fprintf(w, "data: %s\n\n", b)
			}
			chunk(delta, nil)
			chunk(map[string]any{}, finish)
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "x", "object": "chat.completion", "model": "fake",
			"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finish}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
	})
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input any `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var inputs []string
		switch v := req.Input.(type) {
		case string:
			inputs = []string{v}
		case []any:
			for _, x := range v {
				inputs = append(inputs, fmt.Sprint(x))
			}
		}
		data := make([]map[string]any, len(inputs))
		for i, s := range inputs {
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": embed(s)}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "model": "fake-embed", "data": data,
			"usage": map[string]any{"prompt_tokens": 1, "total_tokens": 1}})
	})
	return mux
}

// embed hashes words into a 64-dim unit vector, so texts that share words are close.
func embed(s string) []float64 {
	v := make([]float64, 64)
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127)
	}) {
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%64]++
	}
	var n float64
	for _, x := range v {
		n += x * x
	}
	if n == 0 {
		v[0], n = 1, 1
	}
	for i := range v {
		v[i] /= math.Sqrt(n)
	}
	return v
}

func toolCall(id, name string, args map[string]any) map[string]any {
	b, _ := json.Marshal(args)
	return map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(b)}}
}

// toolResult returns the content of the latest tool message, if the model
// has already seen a tool result in this run. agent-go appends further user
// messages after tool results, so "last message" is not a reliable signal.
func toolResult(req map[string]any) (string, bool) {
	msgs, _ := req["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m, _ := msgs[i].(map[string]any)
		if m["role"] == "tool" {
			c, _ := m["content"].(string)
			return c, true
		}
	}
	return "", false
}

func newEngine(t *testing.T, e *env, f *fakeProvider, withEmbedding bool) ai.Engine {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	opts := ai.Options{
		Store: e.st, Dir: t.TempDir(), Location: time.UTC,
		LLM: config.Endpoint{BaseURL: srv.URL + "/v1", APIKey: "k", Model: "fake"},
	}
	if withEmbedding {
		opts.Embed = config.Endpoint{BaseURL: srv.URL + "/v1", APIKey: "k", Model: "fake-embed"}
	}
	eng, err := ai.New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	return eng
}

// startWithAI builds the env first (the engine needs its store), then swaps
// in a server that has the engine.
func startWithAI(t *testing.T, f *fakeProvider, withEmbedding bool) (*env, ai.Engine) {
	t.Helper()
	probe := start(t, nil, false)
	eng := newEngine(t, probe, f, withEmbedding)
	return restart(t, probe, eng), eng
}

func TestAskCreatesEventForTheCallerOnly(t *testing.T) {
	f := &fakeProvider{}
	f.script = func(req map[string]any) (string, []map[string]any) {
		if _, done := toolResult(req); done {
			return "Scheduled it.", nil
		}
		return "", []map[string]any{toolCall("call_1", "create_event", map[string]any{
			"title": "Dentist", "start": "2026-11-03T15:00:00+00:00", "remind_before_minutes": 30})}
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	aiC, cal := pb.NewAIServiceClient(conn), pb.NewCalendarServiceClient(conn)

	st, err := aiC.GetStatus(as(e.alice), &pb.GetStatusRequest{})
	if err != nil || !st.Enabled || !st.Chat || st.SemanticSearch {
		t.Fatalf("status: %+v %v", st, err)
	}
	resp, err := aiC.Ask(as(e.alice), &pb.AskRequest{Message: "book a dentist visit on Nov 3 at 3pm"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Reply != "Scheduled it." || resp.SessionId == "" || len(resp.ToolsUsed) == 0 {
		t.Fatalf("%+v", resp)
	}
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	rng := &pb.ListEventsRequest{From: timestamppb.New(from), To: timestamppb.New(from.AddDate(0, 0, 7))}
	mine, _ := cal.ListEvents(as(e.alice), rng)
	if len(mine.Occurrences) != 1 || mine.Occurrences[0].Event.Title != "Dentist" ||
		mine.Occurrences[0].Event.GetRemindBeforeMinutes() != 30 {
		t.Fatalf("alice's calendar: %+v", mine)
	}
	// The tool ran as alice; bob must not see it.
	theirs, _ := cal.ListEvents(as(e.bob), rng)
	if len(theirs.Occurrences) != 0 {
		t.Fatalf("assistant event leaked to another user: %+v", theirs)
	}
}

func TestAskToolErrorsAreReportedToTheModel(t *testing.T) {
	f := &fakeProvider{}
	var sawError bool
	f.script = func(req map[string]any) (string, []map[string]any) {
		if content, done := toolResult(req); done {
			sawError = strings.Contains(content, `"ok":false`) && strings.Contains(content, "end_time is before")
			return "That time range is invalid.", nil
		}
		return "", []map[string]any{toolCall("c", "create_event", map[string]any{
			"title": "Broken", "start": "2026-11-03T15:00:00Z", "end": "2026-11-03T10:00:00Z"})}
	}
	e, _ := startWithAI(t, f, false)
	resp, err := pb.NewAIServiceClient(e.conn(t)).Ask(as(e.alice), &pb.AskRequest{Message: "make a broken event"})
	if err != nil {
		t.Fatalf("a tool failure must not fail the RPC: %v", err)
	}
	if !sawError || resp.Reply != "That time range is invalid." {
		t.Fatalf("sawError=%v resp=%+v", sawError, resp)
	}
}

func TestSessionsAreScopedPerUser(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "ok", nil }}
	e, _ := startWithAI(t, f, false)
	c := pb.NewAIServiceClient(e.conn(t))
	a, err := c.Ask(as(e.alice), &pb.AskRequest{Message: "remember the code word: mango"})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.chatReqs = nil
	f.mu.Unlock()
	// Bob replays alice's session id; he must not receive her history.
	if _, err := c.Ask(as(e.bob), &pb.AskRequest{Message: "what was the code word?", SessionId: a.SessionId}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.chatReqs {
		if b, _ := json.Marshal(r); strings.Contains(string(b), "mango") {
			t.Fatal("alice's conversation was sent to the model for bob")
		}
	}
}

func TestSemanticSearchAndIndexer(t *testing.T) {
	f := &fakeProvider{script: func(map[string]any) (string, []map[string]any) { return "ok", nil }}
	e, eng := startWithAI(t, f, true)
	conn := e.conn(t)
	notes := pb.NewNoteServiceClient(conn)

	st, _ := pb.NewAIServiceClient(conn).GetStatus(as(e.alice), &pb.GetStatusRequest{})
	if !st.SemanticSearch {
		t.Fatalf("semantic search should be on: %+v", st)
	}
	osaka, _ := notes.CreateNote(as(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "Japan trip", Content: "book flights to osaka and a hotel"}})
	notes.CreateNote(as(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "Pasta", Content: "boil water add salt"}})
	bobs, _ := notes.CreateNote(as(e.bob), &pb.CreateNoteRequest{Note: &pb.Note{Title: "Osaka", Content: "bob's osaka flights notes"}})

	ix := ai.NewIndexer(e.st, eng)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ix.Run(ctx)
	ix.Wake()

	var res *pb.SearchNotesResponse
	deadline := time.Now().Add(15 * time.Second)
	for {
		var err error
		res, err = notes.SearchNotes(as(e.alice), &pb.SearchNotesRequest{Query: "osaka flights", Semantic: true})
		if err == nil && res.Mode == "semantic" && len(res.Hits) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("semantic search never became available: %+v %v", res, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if res.Hits[0].Note.Id != osaka.Id {
		t.Fatalf("top hit = %q, want the Osaka note", res.Hits[0].Note.Title)
	}
	for _, h := range res.Hits {
		if h.Note.Id == bobs.Id {
			t.Fatal("semantic search returned another user's note")
		}
	}

	// Deleted notes disappear from results even if the index lags.
	notes.DeleteNote(as(e.alice), &pb.DeleteNoteRequest{Id: osaka.Id})
	res, _ = notes.SearchNotes(as(e.alice), &pb.SearchNotesRequest{Query: "osaka flights", Semantic: true})
	for _, h := range res.Hits {
		if h.Note.Id == osaka.Id {
			t.Fatal("deleted note still returned")
		}
	}
}

func TestSummarizeAndSuggestTags(t *testing.T) {
	f := &fakeProvider{}
	f.script = func(req map[string]any) (string, []map[string]any) {
		b, _ := json.Marshal(req)
		switch {
		case strings.Contains(string(b), "Summarize this note"):
			return "A short summary.", nil
		case strings.Contains(string(b), "topic tags"):
			return `Sure! ["Budget", "q3", "budget", "` + strings.Repeat("x", 80) + `"]`, nil
		}
		return "ok", nil
	}
	e, _ := startWithAI(t, f, false)
	conn := e.conn(t)
	notes, aiC := pb.NewNoteServiceClient(conn), pb.NewAIServiceClient(conn)
	n, _ := notes.CreateNote(as(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "Q3 plan", Content: "review the budget", Tags: []string{"work"}}})

	s, err := aiC.SummarizeNote(as(e.alice), &pb.SummarizeNoteRequest{NoteId: n.Id})
	if err != nil || s.Summary != "A short summary." {
		t.Fatalf("%+v %v", s, err)
	}
	tg, err := aiC.SuggestTags(as(e.alice), &pb.SuggestTagsRequest{NoteId: n.Id, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(tg.Tags) != 2 || tg.Tags[0] != "budget" || tg.Tags[1] != "q3" {
		t.Fatalf("tags should be normalized, deduped and bounded: %v", tg.Tags)
	}
	got, _ := notes.GetNote(as(e.alice), &pb.GetNoteRequest{Id: n.Id})
	if strings.Join(got.Tags, ",") != "budget,q3,work" {
		t.Fatalf("apply should merge into existing tags: %v", got.Tags)
	}
	// Another user cannot summarize alice's note.
	if _, err := aiC.SummarizeNote(as(e.bob), &pb.SummarizeNoteRequest{NoteId: n.Id}); code(err) != codes.NotFound {
		t.Fatalf("cross-user summarize: %v", err)
	}
}

func TestBriefingUsesTodaysData(t *testing.T) {
	f := &fakeProvider{}
	var prompt string
	f.script = func(req map[string]any) (string, []map[string]any) {
		b, _ := json.Marshal(req)
		if strings.Contains(string(b), "morning briefing") {
			prompt = string(b)
			return "Busy day ahead.", nil
		}
		return "ok", nil
	}
	e, _ := startWithAI(t, f, false)
	cal := pb.NewCalendarServiceClient(e.conn(t))
	day := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	cal.CreateEvent(as(e.alice), &pb.CreateEventRequest{Event: &pb.Event{Title: "Board meeting", StartTime: timestamppb.New(day.Add(10 * time.Hour))}})
	cal.CreateEvent(as(e.bob), &pb.CreateEventRequest{Event: &pb.Event{Title: "Bobs secret", StartTime: timestamppb.New(day.Add(11 * time.Hour))}})
	cal.CreateEvent(as(e.alice), &pb.CreateEventRequest{Event: &pb.Event{Title: "Tomorrow thing", StartTime: timestamppb.New(day.Add(34 * time.Hour))}})

	r, err := pb.NewAIServiceClient(e.conn(t)).DailyBriefing(as(e.alice), &pb.DailyBriefingRequest{Day: timestamppb.New(day.Add(time.Hour))})
	if err != nil || r.Briefing != "Busy day ahead." {
		t.Fatalf("%+v %v", r, err)
	}
	if !strings.Contains(prompt, "Board meeting") || strings.Contains(prompt, "Bobs secret") || strings.Contains(prompt, "Tomorrow thing") {
		t.Fatalf("briefing prompt has wrong data: %s", prompt)
	}
}

func TestUpstreamFailureDoesNotLeakDetails(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid api key sk-SECRET123"}}`, 401)
	}))
	defer broken.Close()
	probe := start(t, nil, false)
	eng, err := ai.New(context.Background(), ai.Options{Store: probe.st, Dir: t.TempDir(), Location: time.UTC,
		LLM: config.Endpoint{BaseURL: broken.URL + "/v1", APIKey: "sk-SECRET123", Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	e := restart(t, probe, eng)
	_, err = pb.NewAIServiceClient(e.conn(t)).Ask(as(e.alice), &pb.AskRequest{Message: "hi"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRET") || code(err) != codes.Unavailable {
		t.Fatalf("upstream detail leaked or wrong code: %v", err)
	}
}
