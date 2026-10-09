package server_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/export"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestExportContainsEverythingTheUserHasAndNothingElse(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	notes, cal := pb.NewNoteServiceClient(conn), pb.NewCalendarServiceClient(conn)
	goals, projects := pb.NewGoalServiceClient(conn), pb.NewProjectServiceClient(conn)
	prefs := pb.NewPreferenceServiceClient(conn)

	for _, n := range []*pb.Note{
		{Title: "行程想法", Content: "先去巴拉望\n留一天机动", Tags: []string{"travel"}, Space: pb.Space_SPACE_LIFE},
		{Title: "行程想法", Content: "same title", Space: pb.Space_SPACE_LIFE},
		{Title: "a/b: c?", Content: "unsafe name", Space: pb.Space_SPACE_WORK},
		{Title: "old", Content: "archived one", Archived: true},
	} {
		if _, err := notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: n}); err != nil {
			t.Fatal(err)
		}
	}
	start0 := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	remind := int32(10)
	if _, err := cal.CreateEvent(ctx, &pb.CreateEventRequest{Event: &pb.Event{Title: "站会, 每天; 重复", Location: "A\\B", StartTime: timestamppb.New(start0), EndTime: timestamppb.New(start0.Add(30 * time.Minute)), Rrule: "FREQ=DAILY;COUNT=3", RemindBeforeMinutes: &remind}}); err != nil {
		t.Fatal(err)
	}
	if _, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "交房租", DueTime: timestamppb.New(start0), Priority: 3}}); err != nil {
		t.Fatal(err)
	}
	if _, err := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "已完成", Completed: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{Title: "菲律宾旅行", Pinned: true}}); err != nil {
		t.Fatal(err)
	}
	g, err := goals.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "运动", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 3, Unit: "次"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goals.RecordCheckIn(ctx, &pb.RecordCheckInRequest{GoalId: g.Id, Amount: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := prefs.SetPreference(ctx, &pb.SetPreferenceRequest{Key: "ui.theme", Value: `{"id":"paper"}`}); err != nil {
		t.Fatal(err)
	}
	ob, err := pb.NewObjectiveServiceClient(conn).CreateObjective(ctx, &pb.CreateObjectiveRequest{Objective: &pb.Objective{Title: "减肥", MetricName: "体重", MetricUnit: "kg", MetricStart: 75, MetricTarget: 68}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pb.NewObjectiveServiceClient(conn).RecordMeasurement(ctx, &pb.RecordMeasurementRequest{ObjectiveId: ob.Id, Value: 74.2}); err != nil {
		t.Fatal(err)
	}
	hc := pb.NewHoldingServiceClient(conn)
	hv, err := hc.CreateHolding(ctx, &pb.CreateHoldingRequest{Holding: &pb.Holding{Symbol: "VOO", DcaDay: 15, DcaAmount: 500}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hc.RecordTrade(ctx, &pb.RecordTradeRequest{HoldingId: hv.Id, Trade: &pb.Trade{Side: "buy", Shares: 14, Price: 699.191, Note: "=HYPERLINK(1)"}}); err != nil {
		t.Fatal(err)
	}
	// Bob's data must not leak into Alice's export.
	if _, err := notes.CreateNote(as(e.bob), &pb.CreateNoteRequest{Note: &pb.Note{Title: "bob secret", Content: "x"}}); err != nil {
		t.Fatal(err)
	}

	stream, err := pb.NewExportServiceClient(conn).Export(ctx, &pb.ExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	name := ""
	for {
		c, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if c.Filename != "" {
			name = c.Filename
		}
		buf.Write(c.Data)
	}
	if !strings.HasPrefix(name, "noted-") || !strings.HasSuffix(name, ".zip") {
		t.Fatalf("file name %q", name)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		_ = r.Close()
		files[f.Name] = string(b)
	}
	all := ""
	for _, v := range files {
		all += v
	}
	if strings.Contains(all, "bob secret") {
		t.Fatal("another user's note is in the export")
	}

	var d export.Data
	if err := json.Unmarshal([]byte(files["noted.json"]), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Notes) != 4 || len(d.Events) != 2 || len(d.Tasks) != 2 || len(d.Projects) != 1 || len(d.Goals) != 1 {
		t.Fatalf("counts: notes %d events %d tasks %d projects %d goals %d", len(d.Notes), len(d.Events), len(d.Tasks), len(d.Projects), len(d.Goals))
	}
	if len(d.Goals[0].CheckIns) != 1 || d.Settings["ui.theme"] != `{"id":"paper"}` {
		t.Fatalf("goal check-ins / settings missing: %+v %v", d.Goals[0], d.Settings)
	}
	if len(d.Objectives) != 1 || len(d.Objectives[0].Readings) != 1 || d.Objectives[0].Readings[0].Value != 74.2 {
		t.Fatalf("objectives: %+v", d.Objectives)
	}
	if len(d.Holdings) != 1 || d.Holdings[0].Symbol != "VOO" || len(d.Holdings[0].Trades) != 1 || d.Holdings[0].DCADay != 15 {
		t.Fatalf("holdings: %+v", d.Holdings)
	}
	if csv := files["trades.csv"]; !strings.Contains(csv, "VOO,USD,") || !strings.Contains(csv, ",buy,14,699.191,0,'=HYPERLINK(1)") {
		t.Fatalf("trades.csv: %q", csv)
	}
	archived := 0
	for _, n := range d.Notes {
		if n.Archived {
			archived++
		}
	}
	if archived != 1 {
		t.Fatalf("archived notes in export: %d", archived)
	}

	md := 0
	for n, body := range files {
		if strings.HasPrefix(n, "notes/") {
			md++
			if strings.ContainsAny(strings.TrimPrefix(n, "notes/"), `/\:*?"<>|`) {
				t.Errorf("unsafe file name %q", n)
			}
			if !strings.HasPrefix(body, "---\ntitle: ") {
				t.Errorf("%s has no front matter", n)
			}
		}
	}
	if md != 4 {
		t.Fatalf("%d markdown files, want 4 (two notes share a title and must not overwrite each other)", md)
	}

	ics := files["calendar.ics"]
	for _, want := range []string{"BEGIN:VCALENDAR", "BEGIN:VEVENT", "RRULE:FREQ=DAILY;COUNT=3", `SUMMARY:站会\, 每天\; 重复`, `LOCATION:A\\B`, "TRIGGER:-PT10M", "BEGIN:VTODO", "PRIORITY:1", "STATUS:COMPLETED", "DTSTART:20261009T090000Z"} {
		if !strings.Contains(ics, want) {
			t.Errorf("calendar.ics lacks %q", want)
		}
	}
	for _, l := range strings.Split(ics, "\r\n") {
		if len(l) > 75 {
			t.Errorf("unfolded line of %d bytes: %.40s", len(l), l)
		}
	}
}
