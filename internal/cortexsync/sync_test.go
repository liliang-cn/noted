package cortexsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liliang-cn/cortexdb/v2/pkg/cortexdb"
	"github.com/liliang-cn/noted/internal/alarm"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// recorder is a Brain that counts calls and can be told to fail some.
type recorder struct {
	Brain
	mu    sync.Mutex
	calls []string
	fail  func(tool string, args any) error
}

func (r *recorder) Call(ctx context.Context, tool string, args, out any) error {
	r.mu.Lock()
	r.calls = append(r.calls, tool)
	fail := r.fail
	r.mu.Unlock()
	if fail != nil {
		if err := fail(tool, args); err != nil {
			return err
		}
	}
	return r.Brain.Call(ctx, tool, args, out)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

type env struct {
	st   *store.Store
	uid  string
	db   *cortexdb.DB
	rec  *recorder
	sync *Syncer
	ctx  context.Context
}

// newEnv is a temporary noted store and a temporary local CortexDB; nothing
// here reaches a remote brain.
func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "noted.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	db, err := cortexdb.Open(cortexdb.DefaultConfig(filepath.Join(dir, "brain.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	u, err := st.EnsureUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureUser(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{Brain: Local(db)}
	return &env{st: st, uid: u.ID, db: db, rec: rec, ctx: ctx,
		sync: &Syncer{Store: st, Brain: rec, Target: "local/noted", Users: []string{"alice"}, Location: time.UTC, Lang: "en"}}
}

func (e *env) run(t *testing.T, opt Options) Report {
	t.Helper()
	rep, err := e.sync.SyncUser(e.ctx, "alice", opt)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Failed {
		t.Errorf("%s %s failed: %v", f.Op, f.Key, f.Err)
	}
	return rep
}

func (e *env) knowledge(t *testing.T, k string) (cortexdb.KnowledgeRecord, bool) {
	t.Helper()
	var resp cortexdb.KnowledgeGetResponse
	err := Local(e.db).Call(e.ctx, "knowledge_get", cortexdb.KnowledgeGetRequest{KnowledgeID: KnowledgeID("alice", k)}, &resp)
	if errors.Is(err, ErrNotFound) {
		return cortexdb.KnowledgeRecord{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return resp.Knowledge, true
}

// edges returns "type to" for every edge leaving an item's entity.
func (e *env) edges(t *testing.T, k string) []string {
	t.Helper()
	var resp cortexdb.ToolExpandGraphResponse
	id := EntityID("alice", k)
	if err := Local(e.db).Call(e.ctx, "expand_graph", cortexdb.ToolExpandGraphRequest{NodeIDs: []string{id}, MaxHops: 1}, &resp); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ed := range resp.Edges {
		if ed.FromNodeID == id && ed.EdgeType != "mentions" {
			out = append(out, ed.EdgeType+" "+ed.ToNodeID)
		}
	}
	slices.Sort(out)
	return out
}

func (e *env) nodeExists(t *testing.T, k string) bool {
	t.Helper()
	var resp cortexdb.ToolGetNodesResponse
	if err := Local(e.db).Call(e.ctx, "get_nodes", cortexdb.ToolGetNodesRequest{NodeIDs: []string{EntityID("alice", k)}}, &resp); err != nil {
		t.Fatal(err)
	}
	return len(resp.Nodes) > 0
}

func sorted(xs []string) []string {
	out := slices.Clone(xs)
	slices.Sort(out)
	return out
}

func TestSyncIntoALocalBrain(t *testing.T) {
	e := newEnv(t)
	ctx, st, uid := e.ctx, e.st, e.uid
	now := time.Now().UTC()

	kyoto, err := st.CreateProject(ctx, store.Project{UserID: uid, Title: "Kyoto trip"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := st.CreateProject(ctx, store.Project{UserID: uid, Title: "Quarterly report"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(ctx, store.Task{UserID: uid, Title: "Book the ryokan", ProjectID: kyoto.ID})
	if err != nil {
		t.Fatal(err)
	}
	note, err := st.CreateNote(ctx, store.Note{UserID: uid, Title: "Trip ideas", ProjectID: kyoto.ID,
		Content: "Stay two nights in a ryokan near Arashiyama. First [@Book the ryokan](noted://task/" + task.ID + ")."})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := st.CreateTimeEntry(ctx, store.TimeEntry{UserID: uid, Title: "Planning", Minutes: 45, ProjectID: kyoto.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	h, err := st.CreateHolding(ctx, store.Holding{UserID: uid, Symbol: "VOO", Currency: "USD", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordTrade(ctx, uid, h.ID, store.Trade{Time: now.Add(-time.Hour), Side: "buy", Shares: 10, Price: 700}); err != nil {
		t.Fatal(err)
	}
	bobs, err := st.CreateNote(ctx, store.Note{UserID: e.sync.userIDMust(t, "bob"), Title: "Bob's secret", Content: "not alice's"})
	if err != nil {
		t.Fatal(err)
	}

	// First run: everything goes.
	rep := e.run(t, Options{})
	week := "time:" + isoWeek(entry.Start)
	want := sorted([]string{"project:" + kyoto.ID, "project:" + work.ID, "task:" + task.ID, "note:" + note.ID, "holding:" + h.ID, week})
	if got := sorted(rep.Upserts); !slices.Equal(got, want) || len(rep.Deletes) != 0 {
		t.Fatalf("first run sent %v, deleted %v; want %v", got, rep.Deletes, want)
	}
	rec, ok := e.knowledge(t, "note:"+note.ID)
	if !ok || !strings.Contains(rec.Content, "ryokan near Arashiyama") || rec.SourceURL != "noted://note/"+note.ID || rec.Collection != "noted" {
		t.Fatalf("note record = %+v", rec)
	}
	if got := e.edges(t, "task:"+task.ID); !slices.Equal(got, []string{"part_of " + EntityID("alice", "project:"+kyoto.ID)}) {
		t.Fatalf("task edges = %v", got)
	}
	if got := e.edges(t, "note:"+note.ID); !slices.Equal(got, sorted([]string{
		"part_of " + EntityID("alice", "project:"+kyoto.ID), "references " + EntityID("alice", "task:"+task.ID)})) {
		t.Fatalf("note edges = %v", got)
	}
	// The note's text leads to its entity: its first chunk mentions it.
	var around cortexdb.ToolExpandGraphResponse
	noteEntity := EntityID("alice", "note:"+note.ID)
	if err := Local(e.db).Call(ctx, "expand_graph", cortexdb.ToolExpandGraphRequest{NodeIDs: []string{noteEntity}, MaxHops: 1}, &around); err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, ed := range around.Edges {
		linked = linked || (ed.EdgeType == "mentions" && ed.FromNodeID == firstChunkID(KnowledgeID("alice", "note:"+note.ID)) && ed.ToNodeID == noteEntity)
	}
	if !linked {
		t.Fatalf("no chunk mentions the note's entity: %+v", around.Edges)
	}
	if got := e.edges(t, "holding:"+h.ID); !slices.Equal(got, []string{"held_in entity:noted:currency:usd"}) {
		t.Fatalf("holding edges = %v", got)
	}
	if got := e.edges(t, week); !slices.Equal(got, []string{"spent_on " + EntityID("alice", "project:"+kyoto.ID)}) {
		t.Fatalf("week edges = %v", got)
	}
	// The recall tools find it.
	var recall cortexdb.KnowledgeMemoryRecallResponse
	if err := Local(e.db).Call(ctx, "knowledge_memory_recall", cortexdb.KnowledgeMemoryRecallRequest{Query: "ryokan Arashiyama"}, &recall); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range recall.Knowledge {
		found = found || k.KnowledgeID == KnowledgeID("alice", "note:"+note.ID)
	}
	if !found {
		t.Fatalf("knowledge_memory_recall did not find the note: %+v", recall.Knowledge)
	}
	err = Local(e.db).Call(ctx, "knowledge_get", cortexdb.KnowledgeGetRequest{KnowledgeID: KnowledgeID("bob", "note:"+bobs.ID)}, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob is not listed, yet his note: %v", err)
	}

	// Nothing changed: nothing is sent.
	calls := e.rec.count()
	rep = e.run(t, Options{})
	if len(rep.Upserts) != 0 || len(rep.Deletes) != 0 || rep.Unchanged != len(want) || e.rec.count() != calls {
		t.Fatalf("idle run: %+v, %d calls", rep, e.rec.count()-calls)
	}

	// Moving the task and editing the note send just those two; the old
	// part_of edge goes with the old record.
	if _, err := st.UpdateTask(ctx, uid, task.ID, store.TaskPatch{ProjectID: &work.ID}); err != nil {
		t.Fatal(err)
	}
	content := note.Content + " Bring cash."
	if _, err := st.UpdateNote(ctx, uid, note.ID, store.NotePatch{Content: &content}); err != nil {
		t.Fatal(err)
	}
	rep = e.run(t, Options{})
	if got := sorted(rep.Upserts); !slices.Equal(got, sorted([]string{"task:" + task.ID, "note:" + note.ID})) {
		t.Fatalf("after edits sent %v", got)
	}
	if got := e.edges(t, "task:"+task.ID); !slices.Equal(got, []string{"part_of " + EntityID("alice", "project:"+work.ID)}) {
		t.Fatalf("moved task edges = %v", got)
	}
	if rec, _ := e.knowledge(t, "note:"+note.ID); !strings.Contains(rec.Content, "Bring cash.") {
		t.Fatalf("note not replaced: %s", rec.Content)
	}

	// A dry run reports and touches nothing.
	extra, err := st.CreateNote(ctx, store.Note{UserID: uid, Title: "Packing list", Content: "socks"})
	if err != nil {
		t.Fatal(err)
	}
	calls = e.rec.count()
	rep = e.run(t, Options{DryRun: true})
	if !slices.Equal(rep.Upserts, []string{"note:" + extra.ID}) || e.rec.count() != calls {
		t.Fatalf("dry run: %+v, %d calls", rep, e.rec.count()-calls)
	}
	if _, ok := e.knowledge(t, "note:"+extra.ID); ok {
		t.Fatal("a dry run sent the note")
	}
	if rep = e.run(t, Options{}); !slices.Equal(rep.Upserts, []string{"note:" + extra.ID}) {
		t.Fatalf("after the dry run, the real run sent %v", rep.Upserts)
	}

	// Deletes propagate: the record and the entity, with its edges.
	if err := st.DeleteNote(ctx, uid, note.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteProject(ctx, uid, kyoto.ID, false); err != nil {
		t.Fatal(err)
	}
	rep = e.run(t, Options{})
	if got := rep.Deletes; !slices.Equal(got, sorted([]string{"note:" + note.ID, "project:" + kyoto.ID})) {
		t.Fatalf("deleted %v", got)
	}
	if !slices.Contains(rep.Upserts, week) { // the time entry left the project
		t.Fatalf("the week was not re-sent: %v", rep.Upserts)
	}
	for _, k := range []string{"note:" + note.ID, "project:" + kyoto.ID} {
		if _, ok := e.knowledge(t, k); ok {
			t.Errorf("%s still has a record", k)
		}
		if e.nodeExists(t, k) {
			t.Errorf("%s still has an entity", k)
		}
	}
	if got := e.edges(t, week); len(got) != 0 {
		t.Fatalf("week edges after the project went = %v", got)
	}
	ledger, err := st.CortexLedger(ctx, uid, "local/noted")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger["note:"+note.ID]; ok || len(ledger) != 5 {
		t.Fatalf("ledger = %v", ledger)
	}

	// --full resends everything; a new target starts over.
	if rep = e.run(t, Options{Full: true}); len(rep.Upserts) != 5 || rep.Unchanged != 0 {
		t.Fatalf("full: %+v", rep)
	}
	e.sync.Target = "elsewhere/noted"
	if rep = e.run(t, Options{}); len(rep.Upserts) != 5 || len(rep.Deletes) != 0 {
		t.Fatalf("new target: %+v", rep)
	}
}

func (s *Syncer) userIDMust(t *testing.T, name string) string {
	t.Helper()
	id, err := s.userID(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func isoWeek(t time.Time) string {
	y, w := t.UTC().ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

func TestOnlyListedUsersSync(t *testing.T) {
	e := newEnv(t)
	if _, err := e.st.CreateNote(e.ctx, store.Note{UserID: e.sync.userIDMust(t, "bob"), Title: "Bob", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, opt := range []Options{{}, {DryRun: true}, {Full: true}} {
		if _, err := e.sync.SyncUser(e.ctx, "bob", opt); !errors.Is(err, ErrNotOptedIn) {
			t.Fatalf("bob %+v: err = %v", opt, err)
		}
	}
	e.sync.Users = nil
	if _, err := e.sync.SyncUser(e.ctx, "alice", Options{}); !errors.Is(err, ErrNotOptedIn) {
		t.Fatalf("nobody listed: err = %v", err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	cancel()
	e.sync.Run(ctx) // returns at once, and with no users would send nothing anyway
	if e.rec.count() != 0 {
		t.Fatalf("%d calls reached the brain", e.rec.count())
	}
	ledger, err := e.st.CortexLedger(e.ctx, e.sync.userIDMust(t, "bob"), "local/noted")
	if err != nil || len(ledger) != 0 {
		t.Fatalf("bob's ledger = %v, %v", ledger, err)
	}
}

func TestFailuresAreRetriedNextRun(t *testing.T) {
	e := newEnv(t)
	ctx, st, uid := e.ctx, e.st, e.uid
	secret := "Do not log this title"
	n, err := st.CreateNote(ctx, store.Note{UserID: uid, Title: secret, Content: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTask(ctx, store.Task{UserID: uid, Title: "Another"}); err != nil {
		t.Fatal(err)
	}
	e.rec.fail = func(tool string, args any) error {
		if req, ok := args.(cortexdb.KnowledgeSaveRequest); ok && strings.HasSuffix(req.KnowledgeID, n.ID) {
			return errors.New("refused: " + req.Title)
		}
		return nil
	}
	var logs bytes.Buffer
	e.sync.Log = slog.New(slog.NewTextHandler(&logs, nil))
	e.sync.runUser(ctx, "alice", time.Minute)
	if !strings.Contains(logs.String(), "sent=1") || !strings.Contains(logs.String(), "failed=1") || !strings.Contains(logs.String(), "item=note:"+n.ID) {
		t.Fatalf("log:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("the log quotes content:\n%s", logs.String())
	}
	e.rec.fail = nil
	rep := e.run(t, Options{})
	if !slices.Equal(rep.Upserts, []string{"note:" + n.ID}) {
		t.Fatalf("retry sent %v", rep.Upserts)
	}

	// An unreachable brain stops the run at the first call; nothing is recorded.
	if err := st.DeleteNote(ctx, uid, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNote(ctx, store.Note{UserID: uid, Title: "New", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	e.rec.fail = func(string, any) error { return status.Error(codes.Unavailable, "connection refused") }
	rep, err = e.sync.SyncUser(ctx, "alice", Options{})
	if err != nil || !rep.Stopped || len(rep.Failed) != 1 || len(rep.Upserts)+len(rep.Deletes) != 0 {
		t.Fatalf("unreachable: %+v, %v", rep, err)
	}
	if errCode(rep.Failed[0].Err) != "Unavailable" {
		t.Fatalf("code = %s", errCode(rep.Failed[0].Err))
	}
	e.rec.fail = nil
	rep = e.run(t, Options{})
	if len(rep.Deletes) != 1 || len(rep.Upserts) != 1 {
		t.Fatalf("after recovery: %+v", rep)
	}
}

func TestRemoteNotFoundIsAlreadyGone(t *testing.T) {
	e := newEnv(t)
	// Removing something the brain never had is not an error.
	if err := e.sync.remove(e.ctx, "alice", "note:never"); err != nil {
		t.Fatal(err)
	}
}

func TestSpacesClosedToAIStayOut(t *testing.T) {
	e := newEnv(t)
	ctx, st, uid := e.ctx, e.st, e.uid
	work, err := st.CreateNote(ctx, store.Note{UserID: uid, Title: "Salary review", Content: "confidential", Space: store.SpaceWork})
	if err != nil {
		t.Fatal(err)
	}
	life, err := st.CreateNote(ctx, store.Note{UserID: uid, Title: "Garden", Content: "tomatoes", Space: store.SpaceLife})
	if err != nil {
		t.Fatal(err)
	}
	h, err := st.CreateHolding(ctx, store.Holding{UserID: uid, Symbol: "VOO", Currency: "USD", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if rep := e.run(t, Options{}); len(rep.Upserts) != 3 {
		t.Fatalf("sent %v", rep.Upserts)
	}
	// Closing work to AI withdraws what came from it.
	if _, err := st.SetAIAccess(ctx, uid, false, true); err != nil {
		t.Fatal(err)
	}
	rep := e.run(t, Options{})
	if !slices.Equal(rep.Deletes, []string{"note:" + work.ID}) || len(rep.Upserts) != 0 {
		t.Fatalf("after closing work: %+v", rep)
	}
	if _, ok := e.knowledge(t, "note:"+work.ID); ok {
		t.Fatal("the work note is still in the brain")
	}
	if _, err := st.CreateNote(ctx, store.Note{UserID: uid, Title: "Roadmap", Content: "q4", Space: store.SpaceWork}); err != nil {
		t.Fatal(err)
	}
	if rep := e.run(t, Options{}); len(rep.Upserts) != 0 {
		t.Fatalf("a new work note was sent: %v", rep.Upserts)
	}
	// Holdings count as life.
	if _, err := st.SetAIAccess(ctx, uid, true, false); err != nil {
		t.Fatal(err)
	}
	rep = e.run(t, Options{})
	if got := sorted(rep.Deletes); !slices.Equal(got, sorted([]string{"note:" + life.ID, "holding:" + h.ID})) || len(rep.Upserts) != 2 {
		t.Fatalf("after closing life: %+v", rep)
	}
}

func TestClearedFieldsLeaveTheEntity(t *testing.T) {
	e := newEnv(t)
	due := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	task, err := e.st.CreateTask(e.ctx, store.Task{UserID: e.uid, Title: "File taxes", Due: &due})
	if err != nil {
		t.Fatal(err)
	}
	e.run(t, Options{})
	prop := func() any {
		var resp cortexdb.ToolGetNodesResponse
		if err := Local(e.db).Call(e.ctx, "get_nodes", cortexdb.ToolGetNodesRequest{NodeIDs: []string{EntityID("alice", "task:"+task.ID)}}, &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Nodes) != 1 {
			t.Fatalf("nodes = %+v", resp.Nodes)
		}
		return resp.Nodes[0].Properties["due"]
	}
	if got := prop(); got != "2026-10-20" {
		t.Fatalf("due = %v", got)
	}
	var none *time.Time
	if _, err := e.st.UpdateTask(e.ctx, e.uid, task.ID, store.TaskPatch{Due: &none}); err != nil {
		t.Fatal(err)
	}
	e.run(t, Options{})
	if got := prop(); got != "" {
		t.Fatalf("a cleared due date stayed on the entity: %v", got)
	}
}

// Alarms reach the brain as noted_alarm entities, follow edits and deletes,
// and stay out when their space is closed to AI.
func TestAlarmsSync(t *testing.T) {
	e := newEnv(t)
	ctx, st, uid := e.ctx, e.st, e.uid
	proj, err := st.CreateProject(ctx, store.Project{UserID: uid, Title: "School run", Space: store.SpaceWork})
	if err != nil {
		t.Fatal(err)
	}
	// Next week's Monday, so the rings are inside the 180-day window.
	today := time.Now().UTC()
	monday := today.AddDate(0, 0, 7-(int(today.Weekday())+6)%7)
	a, err := st.CreateAlarm(ctx, store.Alarm{UserID: uid, Label: "Pick up Sanbao", Enabled: true, ProjectID: proj.ID,
		Schedule: alarm.Schedule{Kind: alarm.KindRecurring, Anchor: monday.Format("2006-01-02"), Time: "15:15", TimeZone: "UTC",
			Recurrence: &alarm.Recurrence{Freq: alarm.FreqWeekly, Interval: 2, ByDay: []string{"MON"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Space != store.SpaceWork {
		t.Fatalf("space %q: want the project's", a.Space)
	}
	rep := e.run(t, Options{})
	if !slices.Contains(rep.Upserts, "alarm:"+a.ID) {
		t.Fatalf("sent %v", rep.Upserts)
	}
	rec, ok := e.knowledge(t, "alarm:"+a.ID)
	if !ok || !strings.Contains(rec.Content, "Rule: every other week on Mon at 15:15") ||
		!strings.Contains(rec.Content, "Next: as of ") || !strings.Contains(rec.Content, monday.Format("2006-01-02")+" 15:15, "+monday.AddDate(0, 0, 14).Format("2006-01-02")+" 15:15") {
		t.Fatalf("alarm record:\n%s", rec.Content)
	}
	if got := e.edges(t, "alarm:"+a.ID); !slices.Equal(got, []string{"part_of " + EntityID("alice", "project:"+proj.ID)}) {
		t.Fatalf("alarm edges = %v", got)
	}
	if rep := e.run(t, Options{}); len(rep.Upserts) != 0 {
		t.Fatalf("idle run sent %v", rep.Upserts)
	}
	off := false
	if _, err := st.UpdateAlarm(ctx, uid, a.ID, store.AlarmPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if rep := e.run(t, Options{}); !slices.Equal(rep.Upserts, []string{"alarm:" + a.ID}) {
		t.Fatalf("after turning it off sent %v", rep.Upserts)
	}
	if rec, _ := e.knowledge(t, "alarm:"+a.ID); !strings.Contains(rec.Content, "No upcoming rings") {
		t.Fatalf("off alarm:\n%s", rec.Content)
	}
	if _, err := st.SetAIAccess(ctx, uid, false, true); err != nil {
		t.Fatal(err)
	}
	if rep := e.run(t, Options{}); !slices.Contains(rep.Deletes, "alarm:"+a.ID) {
		t.Fatalf("closing work kept the alarm: %+v", rep)
	}
	if _, err := st.SetAIAccess(ctx, uid, true, true); err != nil {
		t.Fatal(err)
	}
	e.run(t, Options{})
	if err := st.DeleteAlarm(ctx, uid, a.ID); err != nil {
		t.Fatal(err)
	}
	if rep := e.run(t, Options{}); !slices.Equal(rep.Deletes, []string{"alarm:" + a.ID}) {
		t.Fatalf("delete: %+v", rep)
	}
	if _, ok := e.knowledge(t, "alarm:"+a.ID); ok || e.nodeExists(t, "alarm:"+a.ID) {
		t.Fatal("the deleted alarm is still in the brain")
	}
}
