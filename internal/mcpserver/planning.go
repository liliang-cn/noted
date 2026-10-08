package mcpserver

import (
	"context"
	"fmt"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type overviewIn struct {
	Horizon string `json:"horizon,omitempty" jsonschema:"today (default), upcoming (tomorrow + 14 days), week or month"`
	Space   string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
}
type listProjectsIn struct {
	PinnedOnly      bool   `json:"pinned_only,omitempty"`
	IncludeArchived bool   `json:"include_archived,omitempty"`
	Space           string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
}
type createProjectIn struct {
	Title  string `json:"title"`
	Notes  string `json:"notes,omitempty"`
	Pinned bool   `json:"pinned,omitempty"`
	Start  string `json:"start,omitempty" jsonschema:"start or departure date, RFC 3339"`
	Due    string `json:"due,omitempty" jsonschema:"deadline or end date, RFC 3339"`
	Space  string `json:"space,omitempty" jsonschema:"work or life; default life"`
}
type updateProjectIn struct {
	ID       string  `json:"id"`
	Title    *string `json:"title,omitempty"`
	Notes    *string `json:"notes,omitempty"`
	Pinned   *bool   `json:"pinned,omitempty"`
	Archived *bool   `json:"archived,omitempty"`
	Space    *string `json:"space,omitempty"`
	Start    string  `json:"start,omitempty"`
	Due      string  `json:"due,omitempty"`
}
type listGoalsIn struct {
	Space string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
}
type createGoalIn struct {
	Title         string   `json:"title"`
	Period        string   `json:"period" jsonschema:"day, week (starts Monday) or month"`
	Target        float64  `json:"target" jsonschema:"amount per period, e.g. 3 for three times a week or 140 for 140 minutes"`
	Unit          string   `json:"unit,omitempty" jsonschema:"what the target counts, e.g. times or minutes; default times"`
	TimeZone      string   `json:"time_zone,omitempty"`
	Space         string   `json:"space,omitempty" jsonschema:"work or life; default life"`
	Steps         []string `json:"milestones,omitempty" jsonschema:"optional ordered milestones, e.g. A1, A2, exam"`
	CounterUnit   string   `json:"counter_unit,omitempty" jsonschema:"unit of an optional running total that is not tied to a period, e.g. lessons"`
	CounterTarget float64  `json:"counter_target,omitempty" jsonschema:"the running total to reach, e.g. 40 lessons"`
}
type checkInIn struct {
	GoalID string  `json:"goal_id"`
	Amount float64 `json:"amount,omitempty" jsonschema:"in the goal's unit; default 1 unless count is given"`
	Count  float64 `json:"count,omitempty" jsonschema:"added to the goal's running total, e.g. one finished lesson"`
	Note   string  `json:"note,omitempty"`
}

func (t *tools) registerPlanning(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "get_overview", Description: "What needs attention: events and tasks for today, the next 14 days, this week or this month, plus goals and pinned projects. The best first call for \"what's on\".", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in overviewIn) (*mcp.CallToolResult, any, error) {
			h := pb.Horizon_HORIZON_TODAY
			switch in.Horizon {
			case "", "today":
			case "upcoming":
				h = pb.Horizon_HORIZON_UPCOMING
			case "week":
				h = pb.Horizon_HORIZON_WEEK
			case "month":
				h = pb.Horizon_HORIZON_MONTH
			default:
				return nil, nil, fmt.Errorf("horizon must be today, upcoming, week or month")
			}
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.focus.GetFocus(ctx, &pb.GetFocusRequest{Horizon: h, Space: sp, TimeZone: t.loc.String()})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_projects", Description: "List projects (finite efforts such as a trip) with progress, days left and the next step. Pinned first.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listProjectsIn) (*mcp.CallToolResult, any, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.projs.ListProjects(ctx, &pb.ListProjectsRequest{PinnedOnly: in.PinnedOnly, IncludeArchived: in.IncludeArchived, Space: sp})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_project", Description: "One project with all its tasks, events and notes.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
			r, err := t.projs.GetProject(ctx, &pb.GetProjectRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "create_project", Description: "Create a project. Then add tasks, events and notes to it by passing its id as project_id to create_task, create_event and create_note.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createProjectIn) (*mcp.CallToolResult, any, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			start, err := t.optWhen(in.Start)
			if err != nil {
				return nil, nil, err
			}
			due, err := t.optWhen(in.Due)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.projs.CreateProject(ctx, &pb.CreateProjectRequest{Project: &pb.Project{
				Title: in.Title, Notes: in.Notes, Pinned: in.Pinned, StartTime: start, DueTime: due, Space: sp}})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "update_project", Description: "Rename, pin, archive or reschedule a project; omitted fields stay as they are.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateProjectIn) (*mcp.CallToolResult, any, error) {
			p, paths := &pb.Project{Id: in.ID}, []string{}
			if in.Title != nil {
				p.Title, paths = *in.Title, append(paths, "title")
			}
			if in.Notes != nil {
				p.Notes, paths = *in.Notes, append(paths, "notes")
			}
			if in.Pinned != nil {
				p.Pinned, paths = *in.Pinned, append(paths, "pinned")
			}
			if in.Archived != nil {
				p.Archived, paths = *in.Archived, append(paths, "archived")
			}
			if in.Space != nil {
				sp, err := spaceOf(*in.Space)
				if err != nil || sp == pb.Space_SPACE_UNSPECIFIED {
					return nil, nil, fmt.Errorf("space must be work or life")
				}
				p.Space, paths = sp, append(paths, "space")
			}
			var err error
			if in.Start != "" {
				if p.StartTime, err = t.when(in.Start); err != nil {
					return nil, nil, err
				}
				paths = append(paths, "start_time")
			}
			if in.Due != "" {
				if p.DueTime, err = t.when(in.Due); err != nil {
					return nil, nil, err
				}
				paths = append(paths, "due_time")
			}
			if len(paths) == 0 {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field")
			}
			r, err := t.projs.UpdateProject(ctx, &pb.UpdateProjectRequest{Project: p, UpdateMask: &fieldmaskpb.FieldMask{Paths: paths}})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_goals", Description: "Recurring goals (3 workouts a week, 140 minutes of study a week) with this period's progress, whether the user is behind pace, and the streak.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listGoalsIn) (*mcp.CallToolResult, any, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.goals.ListGoals(ctx, &pb.ListGoalsRequest{Space: sp})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "create_goal", Description: "Create a recurring goal.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createGoalIn) (*mcp.CallToolResult, any, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			var per pb.GoalPeriod
			switch in.Period {
			case "day":
				per = pb.GoalPeriod_GOAL_PERIOD_DAY
			case "week":
				per = pb.GoalPeriod_GOAL_PERIOD_WEEK
			case "month":
				per = pb.GoalPeriod_GOAL_PERIOD_MONTH
			default:
				return nil, nil, fmt.Errorf("period must be day, week or month")
			}
			tz := in.TimeZone
			if tz == "" {
				tz = t.loc.String()
			}
			g := &pb.Goal{Title: in.Title, Period: per, Target: in.Target, Unit: in.Unit, TimeZone: tz, Space: sp,
				CounterUnit: in.CounterUnit, CounterTarget: in.CounterTarget}
			for _, m := range in.Steps {
				g.Milestones = append(g.Milestones, &pb.Milestone{Title: m})
			}
			r, err := t.goals.CreateGoal(ctx, &pb.CreateGoalRequest{Goal: g})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "check_in", Description: "Record progress on a goal: one workout, 20 minutes of study.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in checkInIn) (*mcp.CallToolResult, any, error) {
			r, err := t.goals.RecordCheckIn(ctx, &pb.RecordCheckInRequest{GoalId: in.GoalID, Amount: in.Amount, Count: in.Count, Note: in.Note})
			if err != nil {
				return nil, nil, err
			}
			return text(r.Goal)
		})
}
