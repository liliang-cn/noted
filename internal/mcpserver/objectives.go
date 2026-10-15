package mcpserver

import (
	"context"
	"fmt"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type listObjectivesIn struct {
	IncludeArchived bool   `json:"include_archived,omitempty"`
	Space           string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
}
type createObjectiveIn struct {
	Title        string                 `json:"title"`
	Notes        string                 `json:"notes,omitempty"`
	Space        string                 `json:"space,omitempty" jsonschema:"work or life; default life"`
	Start        string                 `json:"start,omitempty" jsonschema:"RFC 3339"`
	Due          string                 `json:"due,omitempty" jsonschema:"target date, RFC 3339"`
	MetricName   string                 `json:"metric_name,omitempty" jsonschema:"the one number this objective moves, e.g. weight"`
	MetricUnit   string                 `json:"metric_unit,omitempty" jsonschema:"its unit, e.g. kg; needed for a metric"`
	MetricStart  float64                `json:"metric_start,omitempty" jsonschema:"where the number is now, e.g. 80"`
	MetricTarget float64                `json:"metric_target,omitempty" jsonschema:"where it should get to, e.g. 75; may be below or above the start"`
	ProjectID    string                 `json:"project_id,omitempty"`
	Milestones   []objectiveMilestoneIn `json:"milestones,omitempty" jsonschema:"optional ordered steps on the way, first step first"`
}
type objectiveMilestoneIn struct {
	ID       string `json:"id,omitempty" jsonschema:"update_objective only: an existing milestone's id, which keeps its done state; omit for a new one"`
	Title    string `json:"title"`
	Due      string `json:"due,omitempty" jsonschema:"when it should be done by, RFC 3339"`
	Criteria string `json:"criteria,omitempty" jsonschema:"what counts as done, e.g. passed the B1 exam"`
}
type updateObjectiveIn struct {
	ID           string                  `json:"id"`
	Title        *string                 `json:"title,omitempty"`
	Notes        *string                 `json:"notes,omitempty"`
	Space        *string                 `json:"space,omitempty" jsonschema:"work or life"`
	Start        *string                 `json:"start,omitempty" jsonschema:"RFC 3339; empty string clears it"`
	Due          *string                 `json:"due,omitempty" jsonschema:"RFC 3339; empty string clears it"`
	MetricName   *string                 `json:"metric_name,omitempty"`
	MetricUnit   *string                 `json:"metric_unit,omitempty"`
	MetricStart  *float64                `json:"metric_start,omitempty"`
	MetricTarget *float64                `json:"metric_target,omitempty"`
	Archived     *bool                   `json:"archived,omitempty"`
	ProjectID    *string                 `json:"project_id,omitempty" jsonschema:"empty string takes it out of its project"`
	Milestones   *[]objectiveMilestoneIn `json:"milestones,omitempty" jsonschema:"replaces the whole list, in order; entries with an id from get_objective keep their done state, the rest are new; [] removes them all"`
}
type readingIn struct {
	ObjectiveID string  `json:"objective_id"`
	Value       float64 `json:"value" jsonschema:"in the objective's metric unit, e.g. 78.5"`
	Time        string  `json:"time,omitempty" jsonschema:"RFC 3339; default now"`
	Note        string  `json:"note,omitempty"`
}
type listReadingsIn struct {
	ObjectiveID string `json:"objective_id"`
	Limit       int32  `json:"limit,omitempty" jsonschema:"newest N, returned oldest first; default 365"`
}
type updateGoalIn struct {
	ID          string   `json:"id"`
	Title       *string  `json:"title,omitempty"`
	Notes       *string  `json:"notes,omitempty"`
	Period      *string  `json:"period,omitempty" jsonschema:"day, week or month"`
	Target      *float64 `json:"target,omitempty"`
	Unit        *string  `json:"unit,omitempty"`
	Archived    *bool    `json:"archived,omitempty"`
	Space       *string  `json:"space,omitempty" jsonschema:"work or life"`
	ObjectiveID *string  `json:"objective_id,omitempty" jsonschema:"makes it a dimension of this objective; empty string detaches it"`
	ProjectID   *string  `json:"project_id,omitempty" jsonschema:"puts it into this project; empty string takes it out"`
}
type listCheckInsIn struct {
	GoalID string `json:"goal_id"`
	Limit  int32  `json:"limit,omitempty" jsonschema:"default 100"`
}
type objectiveMilestoneDoneIn struct {
	ObjectiveID string `json:"objective_id"`
	MilestoneID string `json:"milestone_id" jsonschema:"from the objective's milestones"`
	Done        bool   `json:"done"`
}
type milestoneIn struct {
	GoalID      string `json:"goal_id"`
	MilestoneID string `json:"milestone_id" jsonschema:"from the goal's milestones"`
	Done        bool   `json:"done"`
}

func (t *tools) objectiveMilestones(in []objectiveMilestoneIn) ([]*pb.Milestone, error) {
	out := make([]*pb.Milestone, 0, len(in))
	for _, m := range in {
		due, err := t.optWhen(m.Due)
		if err != nil {
			return nil, err
		}
		out = append(out, &pb.Milestone{Id: m.ID, Title: m.Title, DueTime: due, Criteria: m.Criteria})
	}
	return out, nil
}

func periodOf(s string) (pb.GoalPeriod, error) {
	switch s {
	case "day":
		return pb.GoalPeriod_GOAL_PERIOD_DAY, nil
	case "week":
		return pb.GoalPeriod_GOAL_PERIOD_WEEK, nil
	case "month":
		return pb.GoalPeriod_GOAL_PERIOD_MONTH, nil
	}
	return 0, fmt.Errorf("period must be day, week or month")
}

// registerObjectives adds objectives (a larger goal: dimensions plus an optional
// metric with readings) and the goal tools beyond create, list and check-in.
func (t *tools) registerObjectives(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_objectives", Description: "Objectives: larger goals such as losing weight, each with its dimensions (recurring goals with this period's progress) and its metric (latest reading, how far from start to target, whether on track).", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listObjectivesIn) (*mcp.CallToolResult, *listOut[*objectiveOut], error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.objs.ListObjectives(ctx, &pb.ListObjectivesRequest{IncludeArchived: in.IncludeArchived, Space: sp})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &listOut[*objectiveOut]{Items: items(r.Objectives, t.objective)})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_objective", Description: "One objective with its dimensions and metric progress.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *objectiveOut, error) {
			r, err := t.objs.GetObjective(ctx, &pb.GetObjectiveRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.objective(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "create_objective", Description: "Create an objective: a larger goal such as \"lose weight\", made of dimensions (recurring goals such as swimming twice a week) and optionally one metric that moves from a start to a target (weight 80 to 75 kg). " +
		"It may also have ordered milestones, steps such as \"pass A2\" with an optional due date and criteria, ticked with set_objective_milestone_done. " +
		"Progress headlines the metric once it has a reading, else the share of milestones done, else the dimensions. " +
		"After creating it, add each dimension with create_goal passing the returned id as objective_id, and log metric values with record_reading.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createObjectiveIn) (*mcp.CallToolResult, *objectiveOut, error) {
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
			ms, err := t.objectiveMilestones(in.Milestones)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.objs.CreateObjective(ctx, &pb.CreateObjectiveRequest{Objective: &pb.Objective{
				Title: in.Title, Notes: in.Notes, Space: sp, StartTime: start, DueTime: due, MetricName: in.MetricName, MetricUnit: in.MetricUnit,
				MetricStart: in.MetricStart, MetricTarget: in.MetricTarget, ProjectId: in.ProjectID, Milestones: ms}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.objective(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "update_objective", Description: "Change an objective: rename, redate, change the metric or the milestones, archive, or move it into a project; omitted fields stay as they are. " +
		"milestones replaces the whole list: pass every milestone to keep, with its id, to keep its done state. To tick one use set_objective_milestone_done. Dimensions are goals: attach one with update_goal objective_id.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateObjectiveIn) (*mcp.CallToolResult, *objectiveOut, error) {
			o, mask := &pb.Objective{Id: in.ID}, []string{}
			var err error
			if in.Title != nil {
				o.Title, mask = *in.Title, append(mask, "title")
			}
			if in.Notes != nil {
				o.Notes, mask = *in.Notes, append(mask, "notes")
			}
			if in.Space != nil {
				if o.Space, err = setSpace(*in.Space); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "space")
			}
			if in.Start != nil {
				if o.StartTime, err = t.clearableWhen(*in.Start); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "start_time")
			}
			if in.Due != nil {
				if o.DueTime, err = t.clearableWhen(*in.Due); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "due_time")
			}
			if in.MetricName != nil {
				o.MetricName, mask = *in.MetricName, append(mask, "metric_name")
			}
			if in.MetricUnit != nil {
				o.MetricUnit, mask = *in.MetricUnit, append(mask, "metric_unit")
			}
			if in.MetricStart != nil {
				o.MetricStart, mask = *in.MetricStart, append(mask, "metric_start")
			}
			if in.MetricTarget != nil {
				o.MetricTarget, mask = *in.MetricTarget, append(mask, "metric_target")
			}
			if in.Archived != nil {
				o.Archived, mask = *in.Archived, append(mask, "archived")
			}
			if in.ProjectID != nil {
				o.ProjectId, mask = *in.ProjectID, append(mask, "project_id")
			}
			if in.Milestones != nil {
				if o.Milestones, err = t.objectiveMilestones(*in.Milestones); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "milestones")
			}
			if len(mask) == 0 {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field")
			}
			r, err := t.objs.UpdateObjective(ctx, &pb.UpdateObjectiveRequest{Objective: o, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.objective(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_objective", Description: "Permanently delete an objective and its readings. Its dimensions stay, as stand-alone goals.", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			if _, err := t.objs.DeleteObjective(ctx, &pb.DeleteObjectiveRequest{Id: in.ID}); err != nil {
				return nil, nil, err
			}
			return deleted("objective", "objective", in.ID)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "set_objective_milestone_done", Description: "Tick (or untick) one of an objective's milestones (ids from get_objective). Returns the objective with its new progress.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in objectiveMilestoneDoneIn) (*mcp.CallToolResult, *objectiveOut, error) {
			r, err := t.objs.SetObjectiveMilestoneDone(ctx, &pb.SetObjectiveMilestoneDoneRequest{ObjectiveId: in.ObjectiveID, MilestoneId: in.MilestoneID, Done: in.Done})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.objective(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "record_reading", Description: "Record a value of an objective's metric, such as today's weight. Returns the objective with its new progress.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in readingIn) (*mcp.CallToolResult, *objectiveOut, error) {
			at, err := t.optWhen(in.Time)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.objs.RecordMeasurement(ctx, &pb.RecordMeasurementRequest{ObjectiveId: in.ObjectiveID, Value: in.Value, Time: at, Note: in.Note})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.objective(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_readings", Description: "The recorded values of an objective's metric, oldest first.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listReadingsIn) (*mcp.CallToolResult, *listOut[*readingOut], error) {
			r, err := t.objs.ListMeasurements(ctx, &pb.ListMeasurementsRequest{ObjectiveId: in.ObjectiveID, Limit: in.Limit})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &listOut[*readingOut]{Items: items(r.Measurements, t.reading)})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_reading", Description: "Remove a recorded metric value (id from list_readings).", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			if _, err := t.objs.DeleteMeasurement(ctx, &pb.DeleteMeasurementRequest{Id: in.ID}); err != nil {
				return nil, nil, err
			}
			return deleted("reading", "reading", in.ID)
		})

	// Goals beyond create, list and check-in.
	mcp.AddTool(s, &mcp.Tool{Name: "get_goal", Description: "One recurring goal with this period's progress, streak and milestones.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *goalOut, error) {
			r, err := t.goals.GetGoal(ctx, &pb.GetGoalRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.goal(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "update_goal", Description: "Change a recurring goal: target, period, unit, archive it, make it a dimension of an objective (objective_id) or put it into a project (project_id); omitted fields stay as they are.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateGoalIn) (*mcp.CallToolResult, *goalOut, error) {
			g, mask := &pb.Goal{Id: in.ID}, []string{}
			var err error
			if in.Title != nil {
				g.Title, mask = *in.Title, append(mask, "title")
			}
			if in.Notes != nil {
				g.Notes, mask = *in.Notes, append(mask, "notes")
			}
			if in.Period != nil {
				if g.Period, err = periodOf(*in.Period); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "period")
			}
			if in.Target != nil {
				g.Target, mask = *in.Target, append(mask, "target")
			}
			if in.Unit != nil {
				g.Unit, mask = *in.Unit, append(mask, "unit")
			}
			if in.Archived != nil {
				g.Archived, mask = *in.Archived, append(mask, "archived")
			}
			if in.Space != nil {
				if g.Space, err = setSpace(*in.Space); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "space")
			}
			if in.ObjectiveID != nil {
				g.ObjectiveId, mask = *in.ObjectiveID, append(mask, "objective_id")
			}
			if in.ProjectID != nil {
				g.ProjectId, mask = *in.ProjectID, append(mask, "project_id")
			}
			if len(mask) == 0 {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field")
			}
			r, err := t.goals.UpdateGoal(ctx, &pb.UpdateGoalRequest{Goal: g, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.goal(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_goal", Description: "Permanently delete a recurring goal and its check-ins.", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			if _, err := t.goals.DeleteGoal(ctx, &pb.DeleteGoalRequest{Id: in.ID}); err != nil {
				return nil, nil, err
			}
			return deleted("goal", "goal", in.ID)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_check_ins", Description: "A goal's check-ins, newest first.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listCheckInsIn) (*mcp.CallToolResult, *listOut[*checkInOut], error) {
			r, err := t.goals.ListCheckIns(ctx, &pb.ListCheckInsRequest{GoalId: in.GoalID, Limit: in.Limit})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &listOut[*checkInOut]{Items: items(r.CheckIns, t.checkIn)})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "undo_check_in", Description: "Remove a check-in (id from list_check_ins). Returns the goal with fresh progress. One that carries a timeEntryId belongs to a time entry: change or delete the entry instead.", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *goalOut, error) {
			r, err := t.goals.DeleteCheckIn(ctx, &pb.DeleteCheckInRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.goal(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "set_milestone_done", Description: "Tick (or untick) one of a goal's milestones.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in milestoneIn) (*mcp.CallToolResult, *goalOut, error) {
			r, err := t.goals.SetMilestoneDone(ctx, &pb.SetMilestoneDoneRequest{GoalId: in.GoalID, MilestoneId: in.MilestoneID, Done: in.Done})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.goal(r))
		})
}
