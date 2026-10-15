package mcpserver

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type logTimeIn struct {
	Title     string `json:"title" jsonschema:"the activity, short and reused as is so totals group together, e.g. 游泳, 英语学习, 跑步"`
	Minutes   int32  `json:"minutes" jsonschema:"time spent, 1 to 1440 (\"1 小时\" is 60)"`
	Time      string `json:"time,omitempty" jsonschema:"when it started, RFC 3339. Pass it whenever the user says when it happened (下午三点游泳半小时, 昨天晚上跑步 40 分钟, this morning): resolve the day and an hour, a part of the day (上午 09:00, 中午 12:00, 下午 15:00, 晚上 19:00) when that is all they said. Omit it when they said nothing about when; never invent one. Omitted: the entry is not timed (only its day and minutes are known) and starts at now minus minutes."`
	ProjectID string `json:"project_id,omitempty" jsonschema:"the project it was for (id from list_projects)"`
	GoalID    string `json:"goal_id,omitempty" jsonschema:"a goal it counts toward (id from list_goals): its minutes for a goal counted in minutes or hours, else one session. Do not also call check_in."`
	Note      string `json:"note,omitempty"`
	Space     string `json:"space,omitempty" jsonschema:"work or life; omit to inherit from the project or goal, else life"`
}
type startTimerIn struct {
	Title     string `json:"title" jsonschema:"the activity, e.g. 英语学习"`
	ProjectID string `json:"project_id,omitempty"`
	GoalID    string `json:"goal_id,omitempty" jsonschema:"a goal the time counts toward once the timer is stopped"`
	Note      string `json:"note,omitempty"`
	Space     string `json:"space,omitempty" jsonschema:"work or life; omit to inherit from the project or goal, else life"`
}
type stopTimerIn struct {
	End string `json:"end,omitempty" jsonschema:"when it really ended, RFC 3339; default now"`
}
type listTimeIn struct {
	From      string `json:"from,omitempty" jsonschema:"entries that started at or after this, RFC 3339"`
	To        string `json:"to,omitempty" jsonschema:"entries that started before this, RFC 3339"`
	Title     string `json:"title,omitempty" jsonschema:"only activities whose name contains this, ignoring case"`
	ProjectID string `json:"project_id,omitempty"`
	GoalID    string `json:"goal_id,omitempty"`
	Space     string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
	Limit     int32  `json:"limit,omitempty" jsonschema:"max entries (default 50, at most 200)"`
}
type updateTimeIn struct {
	ID        string  `json:"id"`
	Title     *string `json:"title,omitempty"`
	Minutes   *int32  `json:"minutes,omitempty" jsonschema:"1 to 1440"`
	Time      string  `json:"time,omitempty" jsonschema:"new start, RFC 3339; the minutes stay"`
	End       string  `json:"end,omitempty" jsonschema:"new end, RFC 3339; the minutes become the time from the start (on an entry that is not timed, the minutes stay and the start moves)"`
	Note      *string `json:"note,omitempty"`
	ProjectID *string `json:"project_id,omitempty" jsonschema:"empty string takes it out of its project"`
	GoalID    *string `json:"goal_id,omitempty" jsonschema:"empty string stops it counting toward a goal"`
	Space     *string `json:"space,omitempty" jsonschema:"work or life"`
}
type timeSummaryIn struct {
	From      string `json:"from,omitempty" jsonschema:"RFC 3339; default 7 days before to"`
	To        string `json:"to,omitempty" jsonschema:"RFC 3339, exclusive; default now"`
	GroupBy   string `json:"group_by,omitempty" jsonschema:"title (activity, default), project, goal or day"`
	ProjectID string `json:"project_id,omitempty"`
	GoalID    string `json:"goal_id,omitempty"`
	Space     string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
}

func groupByOf(s string) (pb.TimeGroupBy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "title", "activity":
		return pb.TimeGroupBy_TIME_GROUP_BY_TITLE, nil
	case "project":
		return pb.TimeGroupBy_TIME_GROUP_BY_PROJECT, nil
	case "goal":
		return pb.TimeGroupBy_TIME_GROUP_BY_GOAL, nil
	case "day":
		return pb.TimeGroupBy_TIME_GROUP_BY_DAY, nil
	}
	return 0, fmt.Errorf("group_by must be title, project, goal or day")
}

// registerTime adds time tracking (时间记录): what the user spent time on.
func (t *tools) registerTime(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "log_time", Description: "Record time spent on an activity (时间记录), e.g. 游泳 30 分钟 or 英语学习 30 分钟, optionally for a project or counting toward a goal. Reuse the user's earlier activity names (see time_summary) so totals add up. When the user says when it happened (\"下午游泳半小时\", \"昨天跑步 40 分钟\"), pass that start as time, on the right day; otherwise leave time out rather than guess. The result's timed says whether startTime and endTime are real.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in logTimeIn) (*mcp.CallToolResult, *timeOut, error) {
			start, err := t.optWhen(in.Time)
			if err != nil {
				return nil, nil, err
			}
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.time.CreateTimeEntry(ctx, &pb.CreateTimeEntryRequest{Entry: &pb.TimeEntry{Title: in.Title, Minutes: in.Minutes,
				StartTime: start, Note: in.Note, ProjectId: in.ProjectID, GoalId: in.GoalID, Space: sp}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.timeEntry(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "start_timer", Description: "Start a timer for an activity that begins now (开始计时). Only one timer runs at a time; starting another fails and names the running one. Stop it with stop_timer.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in startTimerIn) (*mcp.CallToolResult, *timeOut, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.time.StartTimer(ctx, &pb.StartTimerRequest{Title: in.Title, ProjectId: in.ProjectID, GoalId: in.GoalID, Note: in.Note, Space: sp})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.timeEntry(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "stop_timer", Description: "Stop the running timer (停止计时) and record the time; under a minute counts as one. A linked goal gets its check-in now.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in stopTimerIn) (*mcp.CallToolResult, *timeOut, error) {
			end, err := t.optWhen(in.End)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.time.StopTimer(ctx, &pb.StopTimerRequest{EndTime: end})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.timeEntry(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_running_timer", Description: "The timer that is running now, with the minutes so far; empty when none is.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, *runningOut, error) {
			r, err := t.time.GetRunningTimer(ctx, &pb.GetRunningTimerRequest{})
			if err != nil {
				return nil, nil, err
			}
			if r.Entry == nil {
				return sayWith(&runningOut{}, "no timer is running")
			}
			return reply(r.Entry, &runningOut{Running: true, Entry: t.timeEntry(r.Entry)})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_time_entries", Description: "Time entries, newest first, a running timer included. Filter by start time, activity name, project, goal or space. Each has its day; show its startTime-endTime range only when timed is true.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listTimeIn) (*mcp.CallToolResult, *listOut[*timeOut], error) {
			from, err := t.optWhen(in.From)
			if err != nil {
				return nil, nil, err
			}
			to, err := t.optWhen(in.To)
			if err != nil {
				return nil, nil, err
			}
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.time.ListTimeEntries(ctx, &pb.ListTimeEntriesRequest{From: from, To: to, Title: in.Title, ProjectId: in.ProjectID,
				GoalId: in.GoalID, Space: sp, PageSize: in.Limit})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &listOut[*timeOut]{Items: items(r.Entries, t.timeEntry), NextPageToken: r.NextPageToken})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "update_time_entry", Description: "Correct a time entry: activity, minutes, start or end, note, project or goal. Omitted fields stay as they are; a linked goal's check-in follows the change.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateTimeIn) (*mcp.CallToolResult, *timeOut, error) {
			e, mask := &pb.TimeEntry{Id: in.ID}, []string{}
			var err error
			if in.Title != nil {
				e.Title, mask = *in.Title, append(mask, "title")
			}
			if in.Minutes != nil {
				e.Minutes, mask = *in.Minutes, append(mask, "minutes")
			}
			if in.Time != "" {
				if e.StartTime, err = t.when(in.Time); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "start_time")
			}
			if in.End != "" {
				if e.EndTime, err = t.when(in.End); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "end_time")
			}
			if in.Note != nil {
				e.Note, mask = *in.Note, append(mask, "note")
			}
			if in.ProjectID != nil {
				e.ProjectId, mask = *in.ProjectID, append(mask, "project_id")
			}
			if in.GoalID != nil {
				e.GoalId, mask = *in.GoalID, append(mask, "goal_id")
			}
			if in.Space != nil {
				if e.Space, err = setSpace(*in.Space); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "space")
			}
			if len(mask) == 0 {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field")
			}
			r, err := t.time.UpdateTimeEntry(ctx, &pb.UpdateTimeEntryRequest{Entry: e, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.timeEntry(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_time_entry", Description: "Permanently delete a time entry (or a running timer); a goal check-in it made goes too.", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			if _, err := t.time.DeleteTimeEntry(ctx, &pb.DeleteTimeEntryRequest{Id: in.ID}); err != nil {
				return nil, nil, err
			}
			return deleted("time", "time entry", in.ID)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "time_summary", Description: "How much time went where (花了多少时间): finished entries that started in [from, to), totalled by activity, project, goal or day, most minutes first (days in order). A row without a key is the entries with no project or goal.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in timeSummaryIn) (*mcp.CallToolResult, *timeSummaryOut, error) {
			from, err := t.optWhen(in.From)
			if err != nil {
				return nil, nil, err
			}
			to, err := t.optWhen(in.To)
			if err != nil {
				return nil, nil, err
			}
			by, err := groupByOf(in.GroupBy)
			if err != nil {
				return nil, nil, err
			}
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.time.SummarizeTime(ctx, &pb.SummarizeTimeRequest{From: from, To: to, GroupBy: by, Space: sp,
				ProjectId: in.ProjectID, GoalId: in.GoalID, TimeZone: t.loc.String()})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.timeSummary(r))
		})
}
