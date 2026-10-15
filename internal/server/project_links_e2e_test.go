package server_test

import (
	"testing"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"google.golang.org/grpc/codes"
)

func TestProjectGathersItsItemsAndLinksExistingOnes(t *testing.T) {
	e := start(t, nil, false)
	conn := e.conn(t)
	ctx := as(e.alice)
	proj, goals, objs := pb.NewProjectServiceClient(conn), pb.NewGoalServiceClient(conn), pb.NewObjectiveServiceClient(conn)
	cal, notes := pb.NewCalendarServiceClient(conn), pb.NewNoteServiceClient(conn)

	p, err := proj.CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{Title: "减肥"}})
	if err != nil {
		t.Fatal(err)
	}
	// Items made elsewhere join the project afterwards.
	g, err := goals.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "游泳", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 2}})
	if err != nil {
		t.Fatal(err)
	}
	o, err := objs.CreateObjective(ctx, &pb.CreateObjectiveRequest{Objective: &pb.Objective{Title: "瘦 5 公斤"}})
	if err != nil {
		t.Fatal(err)
	}
	task, _ := cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{Title: "买体重秤"}})
	note, _ := notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: "食谱"}})

	if _, err := goals.UpdateGoal(ctx, &pb.UpdateGoalRequest{Goal: &pb.Goal{Id: g.Id, ProjectId: p.Id}, UpdateMask: mask("project_id")}); err != nil {
		t.Fatal(err)
	}
	if _, err := objs.UpdateObjective(ctx, &pb.UpdateObjectiveRequest{Objective: &pb.Objective{Id: o.Id, ProjectId: p.Id}, UpdateMask: mask("project_id")}); err != nil {
		t.Fatal(err)
	}
	if _, err := cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: task.Id, ProjectId: p.Id}, UpdateMask: mask("project_id")}); err != nil {
		t.Fatal(err)
	}
	if _, err := notes.UpdateNote(ctx, &pb.UpdateNoteRequest{Note: &pb.Note{Id: note.Id, ProjectId: p.Id}, UpdateMask: mask("project_id")}); err != nil {
		t.Fatal(err)
	}

	d, err := proj.GetProject(ctx, &pb.GetProjectRequest{Id: p.Id})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Goals) != 1 || d.Goals[0].Id != g.Id || len(d.Objectives) != 1 || d.Objectives[0].Id != o.Id || len(d.Tasks) != 1 || len(d.Notes) != 1 {
		t.Fatalf("detail: goals=%d objectives=%d tasks=%d notes=%d", len(d.Goals), len(d.Objectives), len(d.Tasks), len(d.Notes))
	}
	if d.Goals[0].Progress == nil || d.Goals[0].ProjectId != p.Id {
		t.Fatalf("goal view: %+v", d.Goals[0])
	}

	// A dimension of an objective is shown under its objective, not twice.
	dim, _ := goals.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: &pb.Goal{Title: "跑步", Period: pb.GoalPeriod_GOAL_PERIOD_WEEK, Target: 3, ObjectiveId: o.Id, ProjectId: p.Id}})
	d, _ = proj.GetProject(ctx, &pb.GetProjectRequest{Id: p.Id})
	if len(d.Goals) != 1 || len(d.Objectives[0].Goals) != 1 || d.Objectives[0].Goals[0].Id != dim.Id {
		t.Fatalf("dimension listed twice or lost: goals=%d dims=%d", len(d.Goals), len(d.Objectives[0].Goals))
	}

	// Another user's project cannot be linked to, and unlinking takes an item out again.
	other, _ := proj.CreateProject(as(e.bob), &pb.CreateProjectRequest{Project: &pb.Project{Title: "bob's"}})
	if _, err := goals.UpdateGoal(ctx, &pb.UpdateGoalRequest{Goal: &pb.Goal{Id: g.Id, ProjectId: other.Id}, UpdateMask: mask("project_id")}); code(err) != codes.InvalidArgument {
		t.Fatalf("linking to someone else's project: %v", err)
	}
	if _, err := goals.UpdateGoal(ctx, &pb.UpdateGoalRequest{Goal: &pb.Goal{Id: g.Id}, UpdateMask: mask("project_id")}); err != nil {
		t.Fatal(err)
	}
	d, _ = proj.GetProject(ctx, &pb.GetProjectRequest{Id: p.Id})
	if len(d.Goals) != 0 {
		t.Fatalf("unlinked goal still listed: %d", len(d.Goals))
	}

	// Deleting the project keeps the goals, now standing alone.
	if _, err := proj.DeleteProject(ctx, &pb.DeleteProjectRequest{Id: p.Id}); err != nil {
		t.Fatal(err)
	}
	if got, err := objs.GetObjective(ctx, &pb.GetObjectiveRequest{Id: o.Id}); err != nil || got.ProjectId != "" {
		t.Fatalf("objective after project delete: %+v %v", got, err)
	}
}
