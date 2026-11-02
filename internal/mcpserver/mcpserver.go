// Package mcpserver exposes a running noted server as MCP tools. It is a
// plain gRPC client of noted, so authentication, validation and per-user
// isolation are exactly those of the gRPC API, and it can point at a remote
// server.
package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type tools struct {
	notes    pb.NoteServiceClient
	cal      pb.CalendarServiceClient
	goals    pb.GoalServiceClient
	projs    pb.ProjectServiceClient
	focus    pb.FocusServiceClient
	sugg     pb.SuggestionServiceClient
	objs     pb.ObjectiveServiceClient
	holds    pb.HoldingServiceClient
	time     pb.TimeServiceClient
	alarms   pb.AlarmServiceClient
	hols     pb.HolidayServiceClient
	ai       pb.AIServiceClient
	compassC pb.CompassServiceClient
	loc      *time.Location // reads zone-less times
}

// New builds the MCP server. AI tools are registered only when the noted
// server reports AI enabled, so clients never see tools that cannot work.
func New(ctx context.Context, conn grpc.ClientConnInterface, loc *time.Location, version string) (*mcp.Server, error) {
	t := &tools{notes: pb.NewNoteServiceClient(conn), cal: pb.NewCalendarServiceClient(conn), goals: pb.NewGoalServiceClient(conn), projs: pb.NewProjectServiceClient(conn), focus: pb.NewFocusServiceClient(conn), sugg: pb.NewSuggestionServiceClient(conn),
		objs: pb.NewObjectiveServiceClient(conn), holds: pb.NewHoldingServiceClient(conn), time: pb.NewTimeServiceClient(conn), alarms: pb.NewAlarmServiceClient(conn), hols: pb.NewHolidayServiceClient(conn), ai: pb.NewAIServiceClient(conn), compassC: pb.NewCompassServiceClient(conn), loc: loc}
	st, err := t.ai.GetStatus(ctx, &pb.GetStatusRequest{})
	if err != nil {
		return nil, fmt.Errorf("cannot reach the noted server (check address and token): %w", err)
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "noted", Version: version}, &mcp.ServerOptions{
		SchemaCache: schemas,
		Instructions: "Personal notes, the schedule (日程安排: events, alarms and timed to-dos in one list, list_events; every item has notify, how it reaches the person: alarm rings the phone like an alarm clock, reminder is a timed notification, notification a plain heads-up, none nobody; remind_before_minutes says how far ahead; list_fires is what will go off when), to-dos, projects, goals and objectives, time tracking (时间记录: log_time, timers, time_summary), investment holdings, and alarms (闹钟: create_alarm, a schedule item with notify alarm; the user's phone rings them, following CN 法定节假日 and 调休 when asked; notify alarm for 叫我/闹钟/准时响, reminder when a timed notification is enough). Look up ids with the list_* tools rather than guessing them. Times are RFC 3339 (e.g. 2026-03-05T15:00:00+08:00); " +
			"zone-less times are read in " + loc.String() + ". Resolve relative dates like \"next Friday\" yourself before calling.",
	})
	t.register(s)
	t.registerTasks(s)
	t.registerPlanning(s)
	t.registerObjectives(s)
	t.registerCompass(s)
	t.registerHoldings(s)
	t.registerTime(s)
	t.registerAlarms(s)
	t.registerSuggestions(s)
	if st.Chat {
		t.registerAI(s)
		t.registerAIPlanning(s)
	}
	return s, nil
}

var (
	readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true}
	additive = &mcp.ToolAnnotations{DestructiveHint: ptr(false)}
	editing  = &mcp.ToolAnnotations{DestructiveHint: ptr(false), IdempotentHint: true}
	deleting = &mcp.ToolAnnotations{DestructiveHint: ptr(true), IdempotentHint: true}
)

func ptr[T any](v T) *T { return &v }

// spaceOf reads the optional "work" / "life" argument; empty means both.
func spaceOf(s string) (pb.Space, error) {
	switch s {
	case "":
		return pb.Space_SPACE_UNSPECIFIED, nil
	case "work":
		return pb.Space_SPACE_WORK, nil
	case "life":
		return pb.Space_SPACE_LIFE, nil
	}
	return 0, fmt.Errorf("space must be work or life")
}

func (t *tools) when(s string) (*timestamppb.Timestamp, error) {
	s = strings.TrimSpace(s)
	if tm, err := time.Parse(time.RFC3339, s); err == nil {
		return timestamppb.New(tm), nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if tm, err := time.ParseInLocation(layout, s, t.loc); err == nil {
			return timestamppb.New(tm), nil
		}
	}
	return nil, fmt.Errorf("cannot read %q as a time; use RFC 3339, e.g. 2026-03-05T15:00:00+08:00", s)
}

func (t *tools) optWhen(s string) (*timestamppb.Timestamp, error) {
	if s == "" {
		return nil, nil
	}
	return t.when(s)
}

// ---- inputs ----

type searchIn struct {
	Query    string `json:"query" jsonschema:"words to look for"`
	Semantic bool   `json:"semantic,omitempty" jsonschema:"rank by meaning when the server has embeddings; falls back to keyword search"`
	Limit    int32  `json:"limit,omitempty" jsonschema:"max results (default 20)"`
	Space    string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
}
type listNotesIn struct {
	Tag             string `json:"tag,omitempty"`
	PinnedOnly      bool   `json:"pinned_only,omitempty"`
	IncludeArchived bool   `json:"include_archived,omitempty"`
	Limit           int32  `json:"limit,omitempty" jsonschema:"max notes (default 50)"`
	Space           string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
	ProjectID       string `json:"project_id,omitempty"`
}
type idIn struct {
	ID string `json:"id"`
}
type createNoteIn struct {
	Title     string   `json:"title,omitempty"`
	Content   string   `json:"content,omitempty" jsonschema:"Markdown body"`
	Tags      []string `json:"tags,omitempty"`
	Pinned    bool     `json:"pinned,omitempty"`
	Space     string   `json:"space,omitempty" jsonschema:"work or life; omit to inherit from the project, else life"`
	ProjectID string   `json:"project_id,omitempty"`
}
type updateNoteIn struct {
	ID        string    `json:"id"`
	Title     *string   `json:"title,omitempty"`
	Content   *string   `json:"content,omitempty"`
	Tags      *[]string `json:"tags,omitempty" jsonschema:"replaces all tags"`
	Pinned    *bool     `json:"pinned,omitempty"`
	Archived  *bool     `json:"archived,omitempty"`
	Space     *string   `json:"space,omitempty" jsonschema:"work or life"`
	ProjectID *string   `json:"project_id,omitempty" jsonschema:"empty string removes it from its project"`
}
type listEventsIn struct {
	From          string `json:"from" jsonschema:"window start, RFC 3339"`
	To            string `json:"to" jsonschema:"window end (exclusive), RFC 3339; at most 366 days after from"`
	Space         string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
	IncludeTasks  *bool  `json:"include_tasks,omitempty" jsonschema:"default true: the to-dos due in the window are items too (kind task), and the open to-dos with no time come back as unscheduled"`
	IncludeAlarms *bool  `json:"include_alarms,omitempty" jsonschema:"default true: the alarms (create_alarm's) are items too"`
}

type createEventIn struct {
	Title               string           `json:"title"`
	Start               string           `json:"start,omitempty" jsonschema:"RFC 3339; required unless schedule is given"`
	End                 string           `json:"end,omitempty" jsonschema:"RFC 3339; default one hour after start"`
	AllDay              bool             `json:"all_day,omitempty"`
	Location            string           `json:"location,omitempty"`
	Description         string           `json:"description,omitempty"`
	TimeZone            string           `json:"time_zone,omitempty" jsonschema:"IANA name, e.g. Asia/Shanghai; recurrence is expanded in it"`
	RRule               string           `json:"rrule,omitempty" jsonschema:"repeat rule, e.g. FREQ=WEEKLY;BYDAY=MO,WE or FREQ=DAILY;COUNT=5"`
	RemindBeforeMinutes *int32           `json:"remind_before_minutes,omitempty" jsonschema:"minutes before the start the person is told; 0 = at the start"`
	Notify              string           `json:"notify,omitempty" jsonschema:"how it reaches the person: alarm (rings like an alarm clock until stopped, through silent mode: 闹钟/叫我/叫醒/必须提醒到), reminder (a timed notification at the moment, with snooze: 提醒我/到点提醒), notification (a plain system notification, not urgent: 通知我一下/告诉我一声) or none (on the schedule only, nobody pinged: 记一下/不用提醒). Omit for the default: reminder remind_before_minutes ahead when that is given; none for an all-day event; else reminder 10 minutes before"`
	Schedule            *alarmScheduleIn `json:"schedule,omitempty" jsonschema:"an alarm-style rule instead of start+rrule (the shape create_alarm takes): every N minutes/hours in a daily window, several times a day (byTime), every other week, monthly on a day, until/count. start is then the first ring and end the same"`
	HolidayPolicy       string           `json:"holiday_policy,omitempty" jsonschema:"for a repeating event, rrule or schedule: none (default); skipHoliday: 节假日跳过; workdayOnly: 工作日 only, following 法定节假日 and 调休补班 in CN; followStatutory: 按调休"`
	Region              *string          `json:"region,omitempty" jsonschema:"CN (mainland China holidays and 调休) or empty for none; omit for the default: CN when the zone is in mainland China"`
	OriginalText        string           `json:"original_text,omitempty" jsonschema:"the user's own words"`
	Space               string           `json:"space,omitempty" jsonschema:"work or life; omit to inherit from the project, else life"`
	ProjectID           string           `json:"project_id,omitempty"`
}
type updateEventIn struct {
	ID                  string           `json:"id"`
	Title               *string          `json:"title,omitempty"`
	Start               string           `json:"start,omitempty" jsonschema:"moving start keeps the duration unless end is given; on an item with a repeating alarm-style schedule change schedule instead"`
	End                 string           `json:"end,omitempty"`
	Location            *string          `json:"location,omitempty"`
	Description         *string          `json:"description,omitempty"`
	RRule               *string          `json:"rrule,omitempty" jsonschema:"empty string makes it one-off"`
	RemindBeforeMinutes *int32           `json:"remind_before_minutes,omitempty" jsonschema:"0 is at the start"`
	ClearReminder       bool             `json:"clear_reminder,omitempty" jsonschema:"stop notifying: notify none unless notify is given"`
	Notify              *string          `json:"notify,omitempty" jsonschema:"alarm, reminder, notification or none"`
	Schedule            *alarmScheduleIn `json:"schedule,omitempty" jsonschema:"replaces the whole alarm-style schedule (send all of it); on a plain event it replaces start+rrule"`
	HolidayPolicy       *string          `json:"holiday_policy,omitempty"`
	Region              *string          `json:"region,omitempty" jsonschema:"CN or empty string for none"`
	Enabled             *bool            `json:"enabled,omitempty" jsonschema:"false pauses the item (no occurrences, nothing told) without deleting it"`
	OriginalText        *string          `json:"original_text,omitempty"`
	Space               *string          `json:"space,omitempty" jsonschema:"work or life"`
	ProjectID           *string          `json:"project_id,omitempty"`
}
type listTasksIn struct {
	State     string `json:"state,omitempty" jsonschema:"open (default), done or all"`
	Tag       string `json:"tag,omitempty"`
	Limit     int32  `json:"limit,omitempty"`
	Space     string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
	ProjectID string `json:"project_id,omitempty"`
}
type createTaskIn struct {
	Title     string   `json:"title"`
	Notes     string   `json:"notes,omitempty"`
	Due       string   `json:"due,omitempty" jsonschema:"RFC 3339"`
	RemindAt  string   `json:"remind_at,omitempty" jsonschema:"RFC 3339"`
	Priority  int32    `json:"priority,omitempty" jsonschema:"1 low, 2 medium, 3 high"`
	Tags      []string `json:"tags,omitempty"`
	Space     string   `json:"space,omitempty" jsonschema:"work or life; omit to inherit from the project, else life"`
	ProjectID string   `json:"project_id,omitempty"`
	RRule     string   `json:"rrule,omitempty" jsonschema:"repeat rule (RFC 5545 RRULE subset): FREQ=DAILY|WEEKLY|MONTHLY|YEARLY with optional INTERVAL, COUNT or UNTIL (YYYYMMDD), and BYDAY (MO,TU,WE,TH,FR,SA,SU; weekly only), e.g. FREQ=DAILY, FREQ=WEEKLY;BYDAY=MO,WE,FR, FREQ=MONTHLY;COUNT=6. Needs due."`
	Notify    string   `json:"notify,omitempty" jsonschema:"how it reaches the person at remind_at: alarm (rings like an alarm clock until stopped, through silent mode: 闹钟/叫我/叫醒/必须提醒到), reminder (a timed notification at the moment, with snooze: 提醒我/到点提醒), notification (a plain system notification, not urgent: 通知我一下/告诉我一声) or none (on the schedule only, nobody pinged: 记一下/不用提醒). Omit for the default: reminder when remind_at is given, else none. Given without remind_at, it is told at due"`
}
type askIn struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id,omitempty" jsonschema:"continue an earlier conversation"`
}
type suggestTagsIn struct {
	ID    string `json:"id" jsonschema:"the note's id"`
	Apply bool   `json:"apply,omitempty" jsonschema:"also add the tags to the note"`
}
type briefingIn struct {
	Day      string `json:"day,omitempty" jsonschema:"date to brief on; default today"`
	TimeZone string `json:"time_zone,omitempty"`
}

func (t *tools) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "search_notes", Description: "Search notes by keywords (or meaning, with semantic=true).", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, *searchOut, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.notes.SearchNotes(ctx, &pb.SearchNotesRequest{Query: in.Query, Semantic: in.Semantic, Limit: in.Limit, Space: sp})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &searchOut{Items: items(r.Hits, t.noteHit), Mode: r.Mode})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_notes", Description: "List notes, pinned first then most recently edited.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listNotesIn) (*mcp.CallToolResult, *listOut[*noteOut], error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.notes.ListNotes(ctx, &pb.ListNotesRequest{Tag: in.Tag, PinnedOnly: in.PinnedOnly, IncludeArchived: in.IncludeArchived, PageSize: in.Limit, Space: sp, ProjectId: in.ProjectID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &listOut[*noteOut]{Items: items(r.Notes, t.note), NextPageToken: r.NextPageToken})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_note", Description: "Read one note in full.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *noteOut, error) {
			r, err := t.notes.GetNote(ctx, &pb.GetNoteRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.note(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "create_note", Description: "Save a new note.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createNoteIn) (*mcp.CallToolResult, *noteOut, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.notes.CreateNote(ctx, &pb.CreateNoteRequest{Note: &pb.Note{Title: in.Title, Content: in.Content, Tags: in.Tags, Pinned: in.Pinned, Space: sp, ProjectId: in.ProjectID}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.note(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "update_note", Description: "Change fields of a note; omitted fields stay as they are.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateNoteIn) (*mcp.CallToolResult, *noteOut, error) {
			n, mask := &pb.Note{Id: in.ID}, []string{}
			if in.Title != nil {
				n.Title, mask = *in.Title, append(mask, "title")
			}
			if in.Content != nil {
				n.Content, mask = *in.Content, append(mask, "content")
			}
			if in.Tags != nil {
				n.Tags, mask = *in.Tags, append(mask, "tags")
			}
			if in.Pinned != nil {
				n.Pinned, mask = *in.Pinned, append(mask, "pinned")
			}
			if in.Archived != nil {
				n.Archived, mask = *in.Archived, append(mask, "archived")
			}
			if in.ProjectID != nil {
				n.ProjectId, mask = *in.ProjectID, append(mask, "project_id")
			}
			if in.Space != nil {
				sp, err := spaceOf(*in.Space)
				if err != nil || sp == pb.Space_SPACE_UNSPECIFIED {
					return nil, nil, fmt.Errorf("space must be work or life")
				}
				n.Space, mask = sp, append(mask, "space")
			}
			if len(mask) == 0 {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field")
			}
			r, err := t.notes.UpdateNote(ctx, &pb.UpdateNoteRequest{Note: n, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.note(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_note", Description: "Permanently delete a note.", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			if _, err := t.notes.DeleteNote(ctx, &pb.DeleteNoteRequest{Id: in.ID}); err != nil {
				return nil, nil, err
			}
			return deleted("note", "note", in.ID)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_events", Description: "The schedule (日程安排) in a time window: calendar events, alarms and the to-dos due in it, as occurrences by start time (repeating ones expanded, holidays applied), each with notify (alarm / reminder / notification / none) and remindBeforeMinutes saying how and how far ahead the person is told. " +
		"The open to-dos with no time yet come back as unscheduled, a column of their own: give one a due to put it on the schedule.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listEventsIn) (*mcp.CallToolResult, *eventsOut, error) {
			from, err := t.when(in.From)
			if err != nil {
				return nil, nil, err
			}
			to, err := t.when(in.To)
			if err != nil {
				return nil, nil, err
			}
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			tasks := in.IncludeTasks == nil || *in.IncludeTasks
			alarms := in.IncludeAlarms == nil || *in.IncludeAlarms
			r, err := t.cal.ListEvents(ctx, &pb.ListEventsRequest{From: from, To: to, Space: sp, IncludeTasks: tasks, IncludeAlarms: alarms})
			if err != nil {
				return nil, nil, err
			}
			out := &eventsOut{Items: items(r.Occurrences, t.occurrence), From: t.ts(from), To: t.ts(to)}
			if tasks {
				out.Unscheduled = items(r.Unscheduled, t.task)
			}
			return reply(r, out)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "create_event", Description: "Create a schedule item (日程): a calendar event, optionally recurring (rrule, or an alarm-style schedule), with notify saying how it reaches the person (alarm rings the phone like an alarm clock, reminder is a timed notification, notification a plain heads-up, none nobody) and remind_before_minutes how far ahead. Left out, a timed event is a reminder 10 minutes before and an all-day one tells nobody; a meeting usually wants reminder, a birthday or a holiday none. " +
		"With notify alarm this is an alarm (闹钟) like create_alarm's; a repeating one can skip holidays or run on workdays only (holiday_policy).", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createEventIn) (*mcp.CallToolResult, *eventOut, error) {
			tz := in.TimeZone
			if tz == "" {
				tz = t.loc.String()
			}
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			ev := &pb.Event{Title: in.Title, AllDay: in.AllDay, Location: in.Location, Description: in.Description, TimeZone: tz, Rrule: in.RRule,
				RemindBeforeMinutes: in.RemindBeforeMinutes, Notify: in.Notify, HolidayPolicy: in.HolidayPolicy, Region: in.Region,
				OriginalText: in.OriginalText, Space: sp, ProjectId: in.ProjectID}
			if in.Schedule != nil {
				if ev.AlarmSchedule, err = t.scheduleIn(in.Schedule); err != nil {
					return nil, nil, err
				}
				if ev.AlarmSchedule.TimeZone == "" {
					ev.AlarmSchedule.TimeZone = tz
				}
			} else {
				if in.Start == "" {
					return nil, nil, fmt.Errorf("start is required (or an alarm-style schedule)")
				}
				if ev.StartTime, err = t.when(in.Start); err != nil {
					return nil, nil, err
				}
				if ev.EndTime, err = t.optWhen(in.End); err != nil {
					return nil, nil, err
				}
			}
			r, err := t.cal.CreateEvent(ctx, &pb.CreateEventRequest{Event: ev})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.event(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "update_event", Description: "Reschedule or edit a schedule item; omitted fields stay as they are. notify changes how it reaches the person (alarm / reminder / notification / none), remind_before_minutes how far ahead (0 = at the start; clear_reminder for none at all), enabled false pauses it. " +
		"On a to-do's item (kind task) title, start, remind_before_minutes, notify, project and space go to the to-do; use update_task for the rest.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateEventIn) (*mcp.CallToolResult, *eventOut, error) {
			e, mask := &pb.Event{Id: in.ID}, []string{}
			str := func(v *string, path string, dst *string) {
				if v != nil {
					*dst, mask = *v, append(mask, path)
				}
			}
			str(in.Title, "title", &e.Title)
			str(in.Location, "location", &e.Location)
			str(in.Description, "description", &e.Description)
			str(in.RRule, "rrule", &e.Rrule)
			var err error
			if in.Start != "" {
				if e.StartTime, err = t.when(in.Start); err != nil {
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
			if in.ProjectID != nil {
				e.ProjectId, mask = *in.ProjectID, append(mask, "project_id")
			}
			if in.Space != nil {
				sp, err := spaceOf(*in.Space)
				if err != nil || sp == pb.Space_SPACE_UNSPECIFIED {
					return nil, nil, fmt.Errorf("space must be work or life")
				}
				e.Space, mask = sp, append(mask, "space")
			}
			if in.RemindBeforeMinutes != nil || in.ClearReminder {
				if !in.ClearReminder {
					e.RemindBeforeMinutes = in.RemindBeforeMinutes
				}
				mask = append(mask, "remind_before_minutes")
			}
			str(in.Notify, "notify", &e.Notify)
			str(in.HolidayPolicy, "holiday_policy", &e.HolidayPolicy)
			str(in.OriginalText, "original_text", &e.OriginalText)
			if in.Region != nil {
				e.Region, mask = in.Region, append(mask, "region")
			}
			if in.Enabled != nil {
				e.Enabled, mask = in.Enabled, append(mask, "enabled")
			}
			if in.Schedule != nil {
				if e.AlarmSchedule, err = t.scheduleIn(in.Schedule); err != nil {
					return nil, nil, err
				}
				mask = append(mask, "alarm_schedule")
			}
			if len(mask) == 0 {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field")
			}
			r, err := t.cal.UpdateEvent(ctx, &pb.UpdateEventRequest{Event: e, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.event(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_event", Description: "Permanently delete a schedule item (all occurrences if it recurs); on a to-do's item this deletes the to-do.", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			if _, err := t.cal.DeleteEvent(ctx, &pb.DeleteEventRequest{Id: in.ID}); err != nil {
				return nil, nil, err
			}
			return deleted("event", "event", in.ID)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_tasks", Description: "List to-do items, earliest due first. A repeating one shows its rrule.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listTasksIn) (*mcp.CallToolResult, *listOut[*taskOut], error) {
			f := pb.ListTasksRequest_FILTER_OPEN
			switch in.State {
			case "", "open":
			case "done":
				f = pb.ListTasksRequest_FILTER_COMPLETED
			case "all":
				f = pb.ListTasksRequest_FILTER_ALL
			default:
				return nil, nil, fmt.Errorf("state must be open, done or all")
			}
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.cal.ListTasks(ctx, &pb.ListTasksRequest{Filter: f, Tag: in.Tag, PageSize: in.Limit, Space: sp, ProjectId: in.ProjectID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &listOut[*taskOut]{Items: items(r.Tasks, t.task), NextPageToken: r.NextPageToken})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "create_task", Description: "Create a to-do item, optionally with a due time and a reminder (remind_at; notify says how it reaches the person: reminder by default, alarm to ring the phone, notification for a plain heads-up, none for nobody). A to-do with a time is also on the schedule (list_events); one without stays unscheduled until it gets a due. With rrule it repeats: the rule is anchored on the due time (required), and completing the task keeps it as done and adds the next one at the rule's next occurrence, its reminder moved along by the same amount, until COUNT or UNTIL ends the series. Supported rrule subset: FREQ=DAILY|WEEKLY|MONTHLY|YEARLY, INTERVAL, COUNT, UNTIL, BYDAY (weekly only).", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createTaskIn) (*mcp.CallToolResult, *taskOut, error) {
			due, err := t.optWhen(in.Due)
			if err != nil {
				return nil, nil, err
			}
			rem, err := t.optWhen(in.RemindAt)
			if err != nil {
				return nil, nil, err
			}
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.cal.CreateTask(ctx, &pb.CreateTaskRequest{Task: &pb.Task{
				Title: in.Title, Notes: in.Notes, DueTime: due, RemindTime: rem, Priority: pb.Priority(in.Priority), Tags: in.Tags,
				Space: sp, ProjectId: in.ProjectID, Rrule: in.RRule, Notify: in.Notify}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.task(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "complete_task", Description: "Mark a to-do item as done. If it repeats (rrule), the next one is created; find it with list_tasks.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *taskOut, error) {
			r, err := t.cal.UpdateTask(ctx, &pb.UpdateTaskRequest{Task: &pb.Task{Id: in.ID, Completed: true},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"completed"}}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.task(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_task", Description: "Permanently delete a to-do item.", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			if _, err := t.cal.DeleteTask(ctx, &pb.DeleteTaskRequest{Id: in.ID}); err != nil {
				return nil, nil, err
			}
			return deleted("task", "task", in.ID)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_reminders", Description: "Reminders that fired recently (default: last 24 hours).", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, *listOut[*reminderOut], error) {
			r, err := t.cal.ListReminders(ctx, &pb.ListRemindersRequest{})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &listOut[*reminderOut]{Items: items(r.Reminders, t.reminder)})
		})
}

// registerAI adds the tools that need the server's LLM.
func (t *tools) registerAI(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "ask_assistant", Description: "Ask noted's own built-in assistant, which can read and write the user's notes, events and tasks from plain language.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in askIn) (*mcp.CallToolResult, *askOut, error) {
			// The MCP client has its own confirmation step for tool calls, so what the
			// assistant prepares is carried out here; the result names the change so it can be undone.
			r, err := t.ai.Ask(ctx, &pb.AskRequest{Message: in.Message, SessionId: in.SessionID, AutoApply: true})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.ask(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "summarize_note", Description: "Summarize a note with the server's LLM.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *summaryOut, error) {
			r, err := t.ai.SummarizeNote(ctx, &pb.SummarizeNoteRequest{NoteId: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return sayWith(&summaryOut{NoteID: in.ID, Summary: r.Summary}, "%s", r.Summary)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "suggest_tags", Description: "Suggest up to 5 topic tags for a note with the server's LLM; with apply=true they are also added to the note.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in suggestTagsIn) (*mcp.CallToolResult, *tagsOut, error) {
			r, err := t.ai.SuggestTags(ctx, &pb.SuggestTagsRequest{NoteId: in.ID, Apply: in.Apply})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &tagsOut{NoteID: in.ID, Tags: items(r.Tags, func(s string) string { return s }), Applied: in.Apply})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "daily_briefing", Description: "A short briefing of the day's events, due tasks and pinned notes.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in briefingIn) (*mcp.CallToolResult, *briefingOut, error) {
			day, err := t.optWhen(in.Day)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.ai.DailyBriefing(ctx, &pb.DailyBriefingRequest{Day: day, TimeZone: in.TimeZone})
			if err != nil {
				return nil, nil, err
			}
			return sayWith(&briefingOut{Briefing: r.Briefing}, "%s", r.Briefing)
		})
}
