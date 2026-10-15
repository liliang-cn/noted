package cortexsync

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/liliang-cn/cortexdb/v2/pkg/cortexdb"
	"github.com/liliang-cn/cortexdb/v2/pkg/rpcserver"
	"github.com/liliang-cn/noted/internal/store"
)

// startBrain serves a temporary CortexDB over gRPC on a loopback port, the
// way the shared brain is served, for the life of the test.
func startBrain(t *testing.T, token string) (addr string, db *cortexdb.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "brain.db")
	db, err := cortexdb.Open(cortexdb.DefaultConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := rpcserver.New(db, rpcserver.Options{Token: token, DBPath: path})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String(), db
}

func TestRemoteBrainOverGRPC(t *testing.T) {
	addr, db := startBrain(t, "t0ken")
	e := newEnv(t)
	n, err := e.st.CreateNote(e.ctx, store.Note{UserID: e.uid, Title: "Over the wire", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}

	bad, err := Dial(addr, "wrong", false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bad.Close() }()
	e.sync.Brain = bad
	rep, err := e.sync.SyncUser(e.ctx, "alice", Options{})
	if err != nil || !rep.Stopped || errCode(rep.Failed[0].Err) != "Unauthenticated" {
		t.Fatalf("wrong token: %+v %v", rep, err)
	}

	r, err := Dial(addr, "t0ken", false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	e.sync.Brain = r
	if rep := e.run(t, Options{}); len(rep.Upserts) != 1 {
		t.Fatalf("sent %v", rep.Upserts)
	}
	var got cortexdb.KnowledgeGetResponse
	if err := Local(db).Call(e.ctx, "knowledge_get", cortexdb.KnowledgeGetRequest{KnowledgeID: KnowledgeID("alice", "note:"+n.ID)}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Knowledge.SourceURL != "noted://note/"+n.ID {
		t.Fatalf("record = %+v", got.Knowledge)
	}

	// Deleting twice: the second time the record is already gone, which is fine.
	if err := e.st.DeleteNote(e.ctx, e.uid, n.ID); err != nil {
		t.Fatal(err)
	}
	if rep := e.run(t, Options{}); len(rep.Deletes) != 1 {
		t.Fatalf("deleted %v", rep.Deletes)
	}
	err = r.Call(e.ctx, "knowledge_delete", cortexdb.KnowledgeDeleteRequest{KnowledgeID: KnowledgeID("alice", "note:"+n.ID)}, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if err := r.Call(context.Background(), "no_such_tool", map[string]string{}, nil); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown tool must be an error, not 'already gone': %v", err)
	}
}
