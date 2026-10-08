package server_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// Many clients writing and reading across services at once: nothing may fail,
// nothing may be lost, and the race detector must stay quiet.
func TestConcurrentMixedLoad(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	notes, cal, focus := pb.NewNoteServiceClient(conn), pb.NewCalendarServiceClient(conn), pb.NewFocusServiceClient(conn)
	const workers, each = 10, 15

	var wg sync.WaitGroup
	errs := make(chan error, workers*each*4)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			ctx := as(e.alice)
			for i := 0; i < each; i++ {
				title := fmt.Sprintf("w%d-n%d", w, i)
				n, err := notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: title, Content: "并发写入 " + title}})
				if err != nil {
					errs <- err
					continue
				}
				if _, err := notes.UpdateNote(ctx, &pb.UpdateNoteRequest{Note: &pb.Note{Id: n.Id, Pinned: i%2 == 0}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"pinned"}}}); err != nil {
					errs <- err
				}
				if _, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: title}}); err != nil {
					errs <- err
				}
				if _, err := notes.SearchNotes(ctx, &pb.SearchNotesRequest{Query: "并发写入"}); err != nil {
					errs <- err
				}
				if _, err := focus.GetFocus(ctx, &pb.GetFocusRequest{Horizon: pb.Horizon_HORIZON_TODAY}); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("stress run did not finish in 60s (deadlock?)")
	}
	close(errs)
	for err := range errs {
		t.Errorf("rpc failed under load: %v", err)
	}

	want := workers * each
	list, err := notes.ListNotes(as(e.alice), &pb.ListNotesRequest{PageSize: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Notes) != want {
		t.Fatalf("notes after load = %d, want %d", len(list.Notes), want)
	}
	tasks, err := cal.ListTasks(as(e.alice), &pb.ListTasksRequest{PageSize: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks.Tasks) != want {
		t.Fatalf("tasks after load = %d, want %d", len(tasks.Tasks), want)
	}
	// Every note must be findable through the full-text index, not only listed.
	for _, n := range list.Notes {
		hit, err := notes.SearchNotes(as(e.alice), &pb.SearchNotesRequest{Query: n.Title + " 并发写入"})
		if err != nil || len(hit.Hits) == 0 {
			t.Fatalf("note %q missing from the search index (err=%v)", n.Title, err)
		}
	}
}
