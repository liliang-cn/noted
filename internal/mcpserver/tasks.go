package mcpserver

import (
	"context"
	"fmt"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type updateTaskIn struct {
	ID        string    `json:"id"`
	Title     *string   `json:"title,omitempty"`
	Notes     *string   `json:"notes,omitempty"`
	Due       *string   `json:"due,omitempty" jsonschema:"RFC 3339; empty string clears it"`
	RemindAt  *string   `json:"remind_at,omitempty" jsonschema:"RFC 3339; empty string clears it"`
	Priority  *int32    `json:"priority,omitempty" jsonschema:"0 none, 1 low, 2 medium, 3 high"`
	Tags      *[]string `json:"tags,omitempty" jsonschema:"replaces all tags"`
	Completed *bool     `json:"completed,omitempty" jsonschema:"false reopens a finished task"`
	Space     *string   `json:"space,omitempty" jsonschema:"work or life"`
	ProjectID *string   `json:"project_id,omitempty" jsonschema:"puts the task into this project (id from list_projects); empty string takes it out of its project"`
	RRule     *string   `json:"rrule,omitempty" jsonschema:"repeat rule (RFC 5545 RRULE subset): FREQ=DAILY|WEEKLY|MONTHLY|YEARLY with optional INTERVAL, COUNT or UNTIL (YYYYMMDD), and BYDAY (MO,TU,WE,TH,FR,SA,SU; weekly only), e.g. FREQ=DAILY, FREQ=WEEKLY;BYDAY=MO,WE,FR, FREQ=MONTHLY;COUNT=6. Empty string makes it one-off; a repeating task needs a due time."`
}
type logWorkIn struct {
	TaskID   string `json:"task_id"`
	Minutes  int32  `json:"minutes,omitempty" jsonschema:"time spent in this sitting, 0 to 1440"`
	Progress *int32 `json:"progress,omitempty" jsonschema:"how far the whole task is now, 0-100 percent (not the share done in this sitting); 100 completes the task (a repeating one then gets its next)"`
	Note     string `json:"note,omitempty"`
	Time     string `json:"time,omitempty" jsonschema:"when the sitting happened, RFC 3339; default now"`
}
type taskIDIn struct {
	TaskID string `json:"task_id"`
}

// setSpace reads a space that is being written, where both is not a choice.
func setSpace(s string) (pb.Space, error) {
	sp, err := spaceOf(s)
	if err != nil || sp == pb.Space_SPACE_UNSPECIFIED {
		return 0, fmt.Errorf("space must be work or life")
	}
	return sp, nil
}

// clearableWhen reads a time that may be cleared: "" is nil.
func (t *tools) clearableWhen(s string) (*timestamppb.Timestamp, error) {
	if s == "" {
		return nil, nil
	}
	return t.when(s)
}

// registerTasks adds the to-do tools beyond create/complete/delete: editing a
// task, and the sittings (time spent, progress) logged against it.
func (t *tools) registerTasks(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "get_task", Description: "Read one to-do item, with its progress, the time logged on it, and its rrule if it repeats.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *taskOut, error) {
			r, err := t.cal.GetTask(ctx, &pb.GetTaskRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.task(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "update_task", Description: "Change a to-do item: rename, reschedule, set or clear a reminder, reprioritise, reopen, make it repeat or stop repeating (rrule), or move it into a project with project_id. Omitted fields stay as they are. Completing a repeating task creates the next one; reopening it later does not remove that.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateTaskIn) (*mcp.CallToolResult, *taskOut, error) {
			tk, mask := &pb.Task{Id: in.ID}, []string{}
			var err error
			if in.Title != nil {
				tk.Title, mask = *in.Title, append(mask, "title")
			}
			if in.Notes != nil {
				tk.Notes, mask = *in.Notes, append(mask, "notes")
			}
			if in.Due != nil {
				if tk.DueTime, err = t.clearableWhen(*in.Due); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "due_time")
			}
			if in.RemindAt != nil {
				if tk.RemindTime, err = t.clearableWhen(*in.RemindAt); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "remind_time")
			}
			if in.Priority != nil {
				tk.Priority, mask = pb.Priority(*in.Priority), append(mask, "priority")
			}
			if in.Tags != nil {
				tk.Tags, mask = *in.Tags, append(mask, "tags")
			}
			if in.Completed != nil {
				tk.Completed, mask = *in.Completed, append(mask, "completed")
			}
			if in.ProjectID != nil {
				tk.ProjectId, mask = *in.ProjectID, append(mask, "project_id")
			}
			if in.Space != nil {
				if tk.Space, err = setSpace(*in.Space); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "space")
			}
			if in.RRule != nil {
				tk.Rrule, mask = *in.RRule, append(mask, "rrule")
			}
			if len(mask) == 0 {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field")
			}
			r, err := t.cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: tk, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.task(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "log_task_work", Description: "Log a sitting on a to-do item: minutes spent, and optionally how far the whole task now is (progress 0-100) and a note. Needs minutes, progress or a note. Returns the task with its new totals.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in logWorkIn) (*mcp.CallToolResult, *taskOut, error) {
			at, err := t.optWhen(in.Time)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.cal.LogTaskWork(ctx, &pb.LogTaskWorkRequest{TaskId: in.TaskID, Time: at, Minutes: in.Minutes, Progress: in.Progress, Note: in.Note})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.task(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_task_work", Description: "The sittings logged on a to-do item, newest first.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in taskIDIn) (*mcp.CallToolResult, *listOut[*taskWorkOut], error) {
			r, err := t.cal.ListTaskWork(ctx, &pb.ListTaskWorkRequest{TaskId: in.TaskID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &listOut[*taskWorkOut]{Items: items(r.Entries, t.taskWork)})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_task_work", Description: "Remove a logged sitting (id from list_task_work); the task's totals are recomputed.", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			r, err := t.cal.DeleteTaskWork(ctx, &pb.DeleteTaskWorkRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &deletedOut{Deleted: in.ID, Kind: "taskWork", Task: t.task(r)})
		})
}
