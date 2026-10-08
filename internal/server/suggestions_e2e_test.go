package server_test

import (
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func byKind(ps []*pb.Proposal, kind string) []*pb.Proposal {
	var out []*pb.Proposal
	for _, p := range ps {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out
}

func tomorrowAt(hour int) time.Time {
	return utcMidnight().AddDate(0, 0, 1).Add(time.Duration(hour) * time.Hour)
}

func TestSuggestionsLifecycle(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	cal, sug := pb.NewCalendarServiceClient(conn), pb.NewSuggestionServiceClient(conn)
	ctx := as(e.alice)

	// Three situations, none of which depend on the day of the week.
	overdueDue := utcMidnight().AddDate(0, 0, -3).Add(17 * time.Hour)
	rent, _ := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Pay rent", DueTime: timestamppb.New(overdueDue)}})
	a, _ := cal.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{Title: "Review", StartTime: timestamppb.New(tomorrowAt(10)), EndTime: timestamppb.New(tomorrowAt(11)), Space: pb.Space_SPACE_WORK}})
	b, _ := cal.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{Title: "Check-up", StartTime: timestamppb.New(tomorrowAt(10)), EndTime: timestamppb.New(tomorrowAt(11))}})
	depart := time.Now().UTC().AddDate(0, 0, 40)
	trip, _ := pb.NewProjectServiceClient(conn).CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{Title: "Trip", StartTime: timestamppb.New(depart)}})
	h1, _ := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Hotel", ProjectId: trip.Id}})
	h2, _ := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Insurance", ProjectId: trip.Id}})

	l, err := sug.ListProposals(ctx, &pb.ListProposalsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	over, conflict, dates := byKind(l.Proposals, "overdue"), byKind(l.Proposals, "conflict"), byKind(l.Proposals, "project_dates")
	if len(over) != 1 || len(conflict) != 1 || len(dates) != 1 {
		t.Fatalf("overdue=%d conflict=%d dates=%d (%v)", len(over), len(conflict), len(dates), l.Proposals)
	}
	for _, p := range l.Proposals {
		if p.Status != "pending" || p.Source != "rules" || p.Title == "" || len(p.Operations) == 0 || p.Operations[0].Label == "" {
			t.Fatalf("malformed proposal: %+v", p)
		}
	}
	// Listing changed nothing.
	if got, _ := cal.GetTask(ctx, &pb.GetTaskRequest{Id: rent.Id}); !got.DueTime.AsTime().Equal(overdueDue) {
		t.Fatal("listing suggestions must not modify anything")
	}

	// Accept the overdue one: the task moves to tomorrow, a change is recorded.
	acc, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: over[0].Id})
	if err != nil {
		t.Fatal(err)
	}
	if acc.Proposal.Status != "accepted" || acc.Change.Entries != 1 || acc.Change.Undone {
		t.Fatalf("%+v", acc)
	}
	moved, _ := cal.GetTask(ctx, &pb.GetTaskRequest{Id: rent.Id})
	if !moved.DueTime.AsTime().After(time.Now()) {
		t.Fatalf("task should now be due in the future: %v", moved.DueTime.AsTime())
	}
	// Accepting twice is refused.
	if _, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: over[0].Id}); code(err) != codes.Aborted {
		t.Fatalf("second accept: %v", err)
	}

	// Undo puts it back, once.
	un, err := sug.UndoChange(ctx, &pb.UndoChangeRequest{Id: acc.Change.Id})
	if err != nil || !un.Undone {
		t.Fatalf("%+v %v", un, err)
	}
	back, _ := cal.GetTask(ctx, &pb.GetTaskRequest{Id: rent.Id})
	if !back.DueTime.AsTime().Equal(overdueDue) {
		t.Fatalf("undo did not restore the due date: %v", back.DueTime.AsTime())
	}
	if _, err := sug.UndoChange(ctx, &pb.UndoChangeRequest{Id: acc.Change.Id}); code(err) != codes.FailedPrecondition {
		t.Fatalf("second undo: %v", err)
	}
	if cs, _ := sug.ListChanges(ctx, &pb.ListChangesRequest{}); len(cs.Changes) != 1 || !cs.Changes[0].Undone {
		t.Fatalf("%+v", cs)
	}

	// A dismissed suggestion does not come back.
	if _, err := sug.DismissProposal(ctx, &pb.DismissProposalRequest{Id: conflict[0].Id}); err != nil {
		t.Fatal(err)
	}
	l, _ = sug.ListProposals(ctx, &pb.ListProposalsRequest{})
	if len(byKind(l.Proposals, "conflict")) != 0 {
		t.Fatal("a dismissed suggestion was suggested again")
	}
	if d, _ := sug.ListProposals(ctx, &pb.ListProposalsRequest{Status: "dismissed", NoRefresh: true}); len(d.Proposals) != 1 {
		t.Fatalf("dismissed list: %d", len(d.Proposals))
	}
	_, _ = a, b

	// Choose only some operations: one of the two undated tasks gets a date.
	dates = byKind(l.Proposals, "project_dates")
	if len(dates) != 1 || len(dates[0].Operations) != 2 {
		t.Fatalf("%+v", dates)
	}
	if _, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: dates[0].Id, Selection: &pb.OperationSelection{Indexes: []int32{5}}}); code(err) != codes.InvalidArgument {
		t.Fatalf("out-of-range selection: %v", err)
	}
	if _, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: dates[0].Id, Selection: &pb.OperationSelection{}}); code(err) != codes.InvalidArgument {
		t.Fatalf("empty selection: %v", err)
	}
	if _, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: dates[0].Id, Selection: &pb.OperationSelection{Indexes: []int32{0}}}); err != nil {
		t.Fatal(err)
	}
	dated := 0
	for _, id := range []string{h1.Id, h2.Id} {
		if tk, _ := cal.GetTask(ctx, &pb.GetTaskRequest{Id: id}); tk.DueTime != nil {
			dated++
		}
	}
	if dated != 1 {
		t.Fatalf("exactly one task should have been dated, got %d", dated)
	}
}

func TestSuggestionsWithdrawnWhenNoLongerTrue(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	cal, sug := pb.NewCalendarServiceClient(conn), pb.NewSuggestionServiceClient(conn)
	ctx := as(e.alice)
	due := utcMidnight().AddDate(0, 0, -2).Add(9 * time.Hour)
	tk, _ := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Call bank", DueTime: timestamppb.New(due)}})

	if l, _ := sug.ListProposals(ctx, &pb.ListProposalsRequest{}); len(l.Proposals) != 1 {
		t.Fatalf("want 1, got %d", len(l.Proposals))
	}
	cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: tk.Id, Completed: true}, UpdateMask: mask("completed")})
	if l, _ := sug.ListProposals(ctx, &pb.ListProposalsRequest{}); len(l.Proposals) != 0 {
		t.Fatalf("finishing the task should withdraw its suggestion, still have %d", len(l.Proposals))
	}
}

func TestSuggestionsAreScopedToTheUserAndSpace(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	cal, sug := pb.NewCalendarServiceClient(conn), pb.NewSuggestionServiceClient(conn)
	due := timestamppb.New(utcMidnight().AddDate(0, 0, -2).Add(9 * time.Hour))
	cal.CreateTask(as(e.alice), &pb.CreateTaskRequest{Task: &pb.Task{Title: "Work thing", DueTime: due, Space: pb.Space_SPACE_WORK}})
	cal.CreateTask(as(e.alice), &pb.CreateTaskRequest{Task: &pb.Task{Title: "Life thing", DueTime: due}})

	all, _ := sug.ListProposals(as(e.alice), &pb.ListProposalsRequest{})
	work, _ := sug.ListProposals(as(e.alice), &pb.ListProposalsRequest{Space: pb.Space_SPACE_WORK})
	life, _ := sug.ListProposals(as(e.alice), &pb.ListProposalsRequest{Space: pb.Space_SPACE_LIFE})
	if len(all.Proposals) != 2 || len(work.Proposals) != 1 || len(life.Proposals) != 1 {
		t.Fatalf("all=%d work=%d life=%d", len(all.Proposals), len(work.Proposals), len(life.Proposals))
	}

	id := all.Proposals[0].Id
	if _, err := sug.AcceptProposal(as(e.bob), &pb.AcceptProposalRequest{Id: id}); code(err) != codes.NotFound {
		t.Fatalf("bob accepted alice's proposal: %v", err)
	}
	if _, err := sug.DismissProposal(as(e.bob), &pb.DismissProposalRequest{Id: id}); code(err) != codes.NotFound {
		t.Fatalf("bob dismissed alice's proposal: %v", err)
	}
	if l, _ := sug.ListProposals(as(e.bob), &pb.ListProposalsRequest{}); len(l.Proposals) != 0 {
		t.Fatal("bob sees alice's suggestions")
	}
	acc, _ := sug.AcceptProposal(as(e.alice), &pb.AcceptProposalRequest{Id: id})
	if _, err := sug.UndoChange(as(e.bob), &pb.UndoChangeRequest{Id: acc.Change.Id}); code(err) != codes.NotFound {
		t.Fatalf("bob undid alice's change: %v", err)
	}
}

func TestProposeScheduleThenAccept(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	cal, sug := pb.NewCalendarServiceClient(conn), pb.NewSuggestionServiceClient(conn)
	ctx := as(e.alice)
	t1, _ := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Budget", Space: pb.Space_SPACE_WORK}})
	t2, _ := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "JD", Space: pb.Space_SPACE_WORK}})

	p, err := sug.ProposeSchedule(ctx, &pb.ProposeScheduleRequest{TaskIds: []string{t1.Id, t2.Id}, DurationMinutes: 45, Days: 4})
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "schedule" || len(p.Operations) != 2 || p.Space != pb.Space_SPACE_WORK {
		t.Fatalf("%+v", p)
	}
	// Proposing wrote nothing.
	from := time.Now().Add(-time.Hour)
	l, _ := cal.ListEvents(ctx, &pb.ListEventsRequest{From: timestamppb.New(from), To: timestamppb.New(from.AddDate(0, 0, 10))})
	if len(l.Occurrences) != 0 {
		t.Fatalf("a proposal created %d events", len(l.Occurrences))
	}
	if _, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: p.Id}); err != nil {
		t.Fatal(err)
	}
	l, _ = cal.ListEvents(ctx, &pb.ListEventsRequest{From: timestamppb.New(from), To: timestamppb.New(from.AddDate(0, 0, 10))})
	if len(l.Occurrences) != 2 {
		t.Fatalf("want 2 scheduled blocks, got %d", len(l.Occurrences))
	}
	a, b := l.Occurrences[0], l.Occurrences[1]
	if b.StartTime.AsTime().Before(a.EndTime.AsTime()) {
		t.Fatalf("scheduled blocks overlap: %v-%v and %v", a.StartTime.AsTime(), a.EndTime.AsTime(), b.StartTime.AsTime())
	}
	if a.EndTime.AsTime().Sub(a.StartTime.AsTime()) != 45*time.Minute {
		t.Fatalf("duration = %v", a.EndTime.AsTime().Sub(a.StartTime.AsTime()))
	}

	if _, err := sug.ProposeSchedule(ctx, &pb.ProposeScheduleRequest{}); code(err) != codes.InvalidArgument {
		t.Fatalf("no tasks: %v", err)
	}
	if _, err := sug.ProposeSchedule(ctx, &pb.ProposeScheduleRequest{TaskIds: []string{t1.Id}, DurationMinutes: 14 * 60}); code(err) != codes.FailedPrecondition {
		t.Fatalf("too long to fit: %v", err)
	}
	if _, err := sug.ProposeSchedule(as(e.bob), &pb.ProposeScheduleRequest{TaskIds: []string{t1.Id}}); code(err) != codes.NotFound {
		t.Fatalf("bob scheduled alice's task: %v", err)
	}
}

func TestWeeklyReviewOverGRPC(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	cal, sug := pb.NewCalendarServiceClient(conn), pb.NewSuggestionServiceClient(conn)
	ctx := as(e.alice)

	today := utcMidnight()
	monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	done, _ := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Finished", DueTime: timestamppb.New(monday.Add(9 * time.Hour))}})
	cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: done.Id, Completed: true}, UpdateMask: mask("completed")})
	slipped, _ := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "Slipped", DueTime: timestamppb.New(monday.Add(10 * time.Hour))}})

	r, err := sug.GetWeeklyReview(ctx, &pb.GetWeeklyReviewRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.From.AsTime().Equal(monday) || !r.To.AsTime().Equal(monday.AddDate(0, 0, 7)) {
		t.Fatalf("week = %v - %v", r.From.AsTime(), r.To.AsTime())
	}
	if r.TasksDone != 1 || r.TasksTotal != 2 || len(r.CarriedTasks) != 1 || r.CarriedTasks[0].Id != slipped.Id {
		t.Fatalf("%+v", r)
	}
	if r.NextWeek == nil || r.NextWeek.Kind != "weekly" || len(r.NextWeek.Operations) != 1 {
		t.Fatalf("next week: %+v", r.NextWeek)
	}

	// Asking again returns the same stored proposal rather than piling up copies.
	r2, _ := sug.GetWeeklyReview(ctx, &pb.GetWeeklyReviewRequest{})
	if r2.NextWeek == nil || r2.NextWeek.Id != r.NextWeek.Id {
		t.Fatal("the weekly proposal should not be duplicated")
	}
	// Accepting carries the task to next Monday 09:00.
	if _, err := sug.AcceptProposal(ctx, &pb.AcceptProposalRequest{Id: r.NextWeek.Id}); err != nil {
		t.Fatal(err)
	}
	got, _ := cal.GetTask(ctx, &pb.GetTaskRequest{Id: slipped.Id})
	if want := monday.AddDate(0, 0, 7).Add(9 * time.Hour); !got.DueTime.AsTime().Equal(want) {
		t.Fatalf("due = %v, want %v", got.DueTime.AsTime(), want)
	}
	// After accepting, the review no longer offers it again.
	if r3, _ := sug.GetWeeklyReview(ctx, &pb.GetWeeklyReviewRequest{}); r3.NextWeek != nil && r3.NextWeek.Id == r.NextWeek.Id {
		t.Fatal("an accepted weekly plan should not be offered again")
	}

	if _, err := sug.GetWeeklyReview(ctx, &pb.GetWeeklyReviewRequest{TimeZone: "Mars/Base"}); code(err) != codes.InvalidArgument {
		t.Fatalf("bad zone: %v", err)
	}
}
