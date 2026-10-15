package mcpserver

// Structured results. Next to the readable text every tool has always returned,
// a tool also returns a JSON object (MCP structuredContent) described by its
// outputSchema, so a client can draw cards and buttons without parsing text.
// docs/mcp-structured-output.md is the contract. Field names follow the
// protojson text (camelCase); times are RFC 3339 in the MCP server's zone;
// percents are fractions (0.5 is half); money carries its currency; every item
// has kind, id and link (noted://<kind>/<id>).

import (
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// schemas is shared by every server New builds: the HTTP transport builds one
// per session, and the schemas only depend on the Go types.
var schemas = mcp.NewSchemaCache()

// reply returns m as readable JSON, exactly the text the tools have always
// given, with v as the structured result next to it.
func reply[T any](m proto.Message, v T) (*mcp.CallToolResult, T, error) {
	b, err := protojson.MarshalOptions{Multiline: true, Indent: " "}.Marshal(m)
	if err != nil {
		var zero T
		return nil, zero, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, v, nil
}

// sayWith returns a sentence as the text, with v as the structured result.
func sayWith[T any](v T, format string, a ...any) (*mcp.CallToolResult, T, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, a...)}}}, v, nil
}

// deleted answers a delete_* tool.
func deleted(kind, what, id string) (*mcp.CallToolResult, *deletedOut, error) {
	return sayWith(&deletedOut{Deleted: id, Kind: kind}, "deleted %s %s", what, id)
}

func link(kind, id string) string {
	if id == "" {
		return ""
	}
	return "noted://" + kind + "/" + id
}

// ts formats a time in the server's zone; unset is "".
func (t *tools) ts(p *timestamppb.Timestamp) string {
	if p == nil {
		return ""
	}
	return p.AsTime().In(t.loc).Format(time.RFC3339)
}

func spaceName(s pb.Space) string {
	switch s {
	case pb.Space_SPACE_WORK:
		return "work"
	case pb.Space_SPACE_LIFE:
		return "life"
	}
	return ""
}

func periodName(p pb.GoalPeriod) string {
	switch p {
	case pb.GoalPeriod_GOAL_PERIOD_DAY:
		return "day"
	case pb.GoalPeriod_GOAL_PERIOD_WEEK:
		return "week"
	case pb.GoalPeriod_GOAL_PERIOD_MONTH:
		return "month"
	}
	return ""
}

func groupByName(g pb.TimeGroupBy) string {
	switch g {
	case pb.TimeGroupBy_TIME_GROUP_BY_PROJECT:
		return "project"
	case pb.TimeGroupBy_TIME_GROUP_BY_GOAL:
		return "goal"
	case pb.TimeGroupBy_TIME_GROUP_BY_DAY:
		return "day"
	}
	return "title"
}

// ---- shapes ----

type money struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency" jsonschema:"ISO 4217 code, e.g. USD"`
}

type deletedOut struct {
	Deleted string   `json:"deleted" jsonschema:"the id of what was deleted"`
	Kind    string   `json:"kind" jsonschema:"task, event, note, project, goal, objective, holding, time, trade, reading, taskWork or alarm"`
	Task    *taskOut `json:"task,omitempty" jsonschema:"delete_task_work only: the task with its totals recomputed"`
}

type taskOut struct {
	Kind           string   `json:"kind" jsonschema:"always task"`
	ID             string   `json:"id"`
	Link           string   `json:"link"`
	Title          string   `json:"title"`
	Notes          string   `json:"notes,omitempty"`
	DueTime        string   `json:"dueTime,omitempty"`
	RemindTime     string   `json:"remindTime,omitempty"`
	Priority       int32    `json:"priority" jsonschema:"0 none, 1 low, 2 medium, 3 high"`
	Completed      bool     `json:"completed"`
	CompleteTime   string   `json:"completeTime,omitempty"`
	Overdue        bool     `json:"overdue" jsonschema:"open and due before now (get_overview: before its window)"`
	Tags           []string `json:"tags,omitempty"`
	Space          string   `json:"space,omitempty" jsonschema:"work or life"`
	ProjectID      string   `json:"projectId,omitempty"`
	ProjectTitle   string   `json:"projectTitle,omitempty" jsonschema:"get_overview only"`
	RRule          string   `json:"rrule,omitempty"`
	Progress       int32    `json:"progress" jsonschema:"0-100"`
	WorkedMinutes  int32    `json:"workedMinutes"`
	WorkSessions   int32    `json:"workSessions"`
	LastWorkedTime string   `json:"lastWorkedTime,omitempty"`
	CreateTime     string   `json:"createTime,omitempty"`
	UpdateTime     string   `json:"updateTime,omitempty"`
}

type eventOut struct {
	Kind                string `json:"kind" jsonschema:"always event"`
	ID                  string `json:"id"`
	Link                string `json:"link"`
	Title               string `json:"title"`
	Description         string `json:"description,omitempty"`
	Location            string `json:"location,omitempty"`
	StartTime           string `json:"startTime" jsonschema:"for list_events and get_overview: this occurrence's"`
	EndTime             string `json:"endTime,omitempty"`
	AllDay              bool   `json:"allDay"`
	TimeZone            string `json:"timeZone,omitempty"`
	RRule               string `json:"rrule,omitempty"`
	RemindBeforeMinutes *int32 `json:"remindBeforeMinutes,omitempty"`
	Space               string `json:"space,omitempty"`
	ProjectID           string `json:"projectId,omitempty"`
	ProjectTitle        string `json:"projectTitle,omitempty" jsonschema:"get_overview only"`
	CreateTime          string `json:"createTime,omitempty"`
	UpdateTime          string `json:"updateTime,omitempty"`
}

type noteOut struct {
	Kind       string   `json:"kind" jsonschema:"always note"`
	ID         string   `json:"id"`
	Link       string   `json:"link"`
	Title      string   `json:"title"`
	Content    string   `json:"content" jsonschema:"Markdown"`
	Tags       []string `json:"tags,omitempty"`
	Pinned     bool     `json:"pinned"`
	Archived   bool     `json:"archived"`
	Space      string   `json:"space,omitempty"`
	ProjectID  string   `json:"projectId,omitempty"`
	CreateTime string   `json:"createTime,omitempty"`
	UpdateTime string   `json:"updateTime,omitempty"`
	Snippet    string   `json:"snippet,omitempty" jsonschema:"search_notes only"`
	Score      *float64 `json:"score,omitempty" jsonschema:"search_notes only"`
}

type projectProgressOut struct {
	TasksTotal     int32   `json:"tasksTotal"`
	TasksDone      int32   `json:"tasksDone"`
	Percent        float64 `json:"percent"`
	EventsUpcoming int32   `json:"eventsUpcoming"`
	NotesCount     int32   `json:"notesCount"`
	NextTime       string  `json:"nextTime,omitempty"`
	NextTitle      string  `json:"nextTitle,omitempty"`
	DaysLeft       *int32  `json:"daysLeft,omitempty" jsonschema:"days to the start, else the due date; negative once past"`
	TasksOverdue   int32   `json:"tasksOverdue"`
	TimeMinutes    int32   `json:"timeMinutes"`
}

type projectOut struct {
	Kind       string              `json:"kind" jsonschema:"always project"`
	ID         string              `json:"id"`
	Link       string              `json:"link"`
	Title      string              `json:"title"`
	Notes      string              `json:"notes,omitempty"`
	Pinned     bool                `json:"pinned"`
	Archived   bool                `json:"archived"`
	StartTime  string              `json:"startTime,omitempty"`
	DueTime    string              `json:"dueTime,omitempty"`
	Space      string              `json:"space,omitempty"`
	CreateTime string              `json:"createTime,omitempty"`
	UpdateTime string              `json:"updateTime,omitempty"`
	Progress   *projectProgressOut `json:"progress,omitempty"`
}

type projectDetailOut struct {
	Project     *projectOut     `json:"project"`
	Tasks       []*taskOut      `json:"tasks"`
	Events      []*eventOut     `json:"events"`
	Notes       []*noteOut      `json:"notes"`
	Goals       []*goalOut      `json:"goals"`
	Objectives  []*objectiveOut `json:"objectives"`
	TimeEntries []*timeOut      `json:"timeEntries" jsonschema:"the latest 20"`
}

type milestoneOut struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Done     bool   `json:"done"`
	DoneTime string `json:"doneTime,omitempty"`
}

type dayTotalOut struct {
	Date   string  `json:"date" jsonschema:"YYYY-MM-DD in the goal's zone"`
	Amount float64 `json:"amount"`
}

type goalProgressOut struct {
	PeriodStart      string        `json:"periodStart,omitempty"`
	PeriodEnd        string        `json:"periodEnd,omitempty"`
	Done             float64       `json:"done"`
	Target           float64       `json:"target"`
	Remaining        float64       `json:"remaining"`
	Percent          float64       `json:"percent" jsonschema:"done / target; can exceed 1"`
	Achieved         bool          `json:"achieved"`
	Behind           bool          `json:"behind"`
	Streak           int32         `json:"streak"`
	Days             []dayTotalOut `json:"days,omitempty"`
	Recent           []dayTotalOut `json:"recent,omitempty"`
	MilestonesDone   int32         `json:"milestonesDone"`
	MilestonesTotal  int32         `json:"milestonesTotal"`
	MilestonePercent float64       `json:"milestonePercent"`
	CounterDone      float64       `json:"counterDone"`
	CounterPercent   float64       `json:"counterPercent"`
}

type goalOut struct {
	Kind          string           `json:"kind" jsonschema:"always goal"`
	ID            string           `json:"id"`
	Link          string           `json:"link"`
	Title         string           `json:"title"`
	Notes         string           `json:"notes,omitempty"`
	Period        string           `json:"period" jsonschema:"day, week or month"`
	Target        float64          `json:"target"`
	Unit          string           `json:"unit"`
	TimeZone      string           `json:"timeZone,omitempty"`
	EventID       string           `json:"eventId,omitempty"`
	Milestones    []milestoneOut   `json:"milestones,omitempty"`
	Archived      bool             `json:"archived"`
	Space         string           `json:"space,omitempty"`
	CounterUnit   string           `json:"counterUnit,omitempty"`
	CounterTarget float64          `json:"counterTarget,omitempty"`
	ObjectiveID   string           `json:"objectiveId,omitempty"`
	ProjectID     string           `json:"projectId,omitempty"`
	CreateTime    string           `json:"createTime,omitempty"`
	UpdateTime    string           `json:"updateTime,omitempty"`
	Progress      *goalProgressOut `json:"progress,omitempty"`
	CheckIn       *checkInOut      `json:"checkIn,omitempty" jsonschema:"check_in only: the check-in just recorded"`
}

type checkInOut struct {
	Kind        string  `json:"kind" jsonschema:"always checkIn"`
	ID          string  `json:"id"`
	Link        string  `json:"link" jsonschema:"its goal"`
	GoalID      string  `json:"goalId"`
	Time        string  `json:"time"`
	Amount      float64 `json:"amount"`
	Count       float64 `json:"count"`
	Note        string  `json:"note,omitempty"`
	TimeEntryID string  `json:"timeEntryId,omitempty"`
}

type objectiveProgressOut struct {
	GoalsTotal      int32   `json:"goalsTotal"`
	GoalsAchieved   int32   `json:"goalsAchieved"`
	GoalsPercent    float64 `json:"goalsPercent"`
	Behind          bool    `json:"behind"`
	HasMetric       bool    `json:"hasMetric"`
	HasReading      bool    `json:"hasReading"`
	MetricCurrent   float64 `json:"metricCurrent"`
	MetricChange    float64 `json:"metricChange"`
	MetricPercent   float64 `json:"metricPercent"`
	LastReadingTime string  `json:"lastReadingTime,omitempty"`
	Percent         float64 `json:"percent"`
	DaysLeft        *int32  `json:"daysLeft,omitempty"`
	OnTrack         bool    `json:"onTrack"`
}

type objectiveOut struct {
	Kind         string                `json:"kind" jsonschema:"always objective"`
	ID           string                `json:"id"`
	Link         string                `json:"link"`
	Title        string                `json:"title"`
	Notes        string                `json:"notes,omitempty"`
	Space        string                `json:"space,omitempty"`
	StartTime    string                `json:"startTime,omitempty"`
	DueTime      string                `json:"dueTime,omitempty"`
	MetricName   string                `json:"metricName,omitempty"`
	MetricUnit   string                `json:"metricUnit,omitempty" jsonschema:"empty: no metric"`
	MetricStart  float64               `json:"metricStart"`
	MetricTarget float64               `json:"metricTarget"`
	Archived     bool                  `json:"archived"`
	ProjectID    string                `json:"projectId,omitempty"`
	CreateTime   string                `json:"createTime,omitempty"`
	UpdateTime   string                `json:"updateTime,omitempty"`
	Progress     *objectiveProgressOut `json:"progress,omitempty"`
	Goals        []*goalOut            `json:"goals,omitempty" jsonschema:"its dimensions"`
}

type readingOut struct {
	Kind        string  `json:"kind" jsonschema:"always reading"`
	ID          string  `json:"id"`
	Link        string  `json:"link" jsonschema:"its objective"`
	ObjectiveID string  `json:"objectiveId"`
	Time        string  `json:"time"`
	Value       float64 `json:"value"`
	Note        string  `json:"note,omitempty"`
}

type holdingPlanOut struct {
	Amount            money  `json:"amount" jsonschema:"per month"`
	Day               int32  `json:"day" jsonschema:"day of the month, 1-28"`
	Time              string `json:"time" jsonschema:"HH:MM in timeZone"`
	TimeZone          string `json:"timeZone,omitempty"`
	EventID           string `json:"eventId,omitempty" jsonschema:"the monthly reminder event"`
	Active            bool   `json:"active"`
	NextTime          string `json:"nextTime,omitempty"`
	DoneThisMonth     bool   `json:"doneThisMonth"`
	InvestedThisMonth money  `json:"investedThisMonth"`
	StreakMonths      int32  `json:"streakMonths"`
}

type positionOut struct {
	Shares            float64  `json:"shares"`
	AvgCost           money    `json:"avgCost" jsonschema:"per share"`
	CostBasis         money    `json:"costBasis"`
	RealizedPnl       money    `json:"realizedPnl"`
	TotalBought       money    `json:"totalBought"`
	Trades            int32    `json:"trades"`
	HasPrice          bool     `json:"hasPrice"`
	MarketValue       *money   `json:"marketValue,omitempty" jsonschema:"set when hasPrice"`
	UnrealizedPnl     *money   `json:"unrealizedPnl,omitempty" jsonschema:"set when hasPrice"`
	UnrealizedPercent *float64 `json:"unrealizedPercent,omitempty" jsonschema:"of costBasis; set when hasPrice"`
}

type holdingOut struct {
	Kind          string          `json:"kind" jsonschema:"always holding"`
	ID            string          `json:"id"`
	Link          string          `json:"link"`
	Symbol        string          `json:"symbol"`
	Name          string          `json:"name,omitempty"`
	Currency      string          `json:"currency"`
	Notes         string          `json:"notes,omitempty"`
	LastPrice     *money          `json:"lastPrice,omitempty" jsonschema:"entered by the user; noted fetches no prices"`
	LastPriceTime string          `json:"lastPriceTime,omitempty"`
	Plan          *holdingPlanOut `json:"plan,omitempty" jsonschema:"the monthly buying plan, when there is one"`
	Archived      bool            `json:"archived"`
	CreateTime    string          `json:"createTime,omitempty"`
	UpdateTime    string          `json:"updateTime,omitempty"`
	Position      *positionOut    `json:"position,omitempty"`
}

type tradeOut struct {
	Kind      string  `json:"kind" jsonschema:"always trade"`
	ID        string  `json:"id"`
	Link      string  `json:"link" jsonschema:"its holding"`
	HoldingID string  `json:"holdingId"`
	Time      string  `json:"time"`
	Side      string  `json:"side" jsonschema:"buy or sell"`
	Shares    float64 `json:"shares"`
	Price     money   `json:"price" jsonschema:"per share"`
	Fee       money   `json:"fee"`
	Note      string  `json:"note,omitempty"`
}

type timeOut struct {
	Kind       string `json:"kind" jsonschema:"always time"`
	ID         string `json:"id"`
	Link       string `json:"link"`
	Title      string `json:"title" jsonschema:"the activity"`
	Minutes    int32  `json:"minutes" jsonschema:"while running: the whole minutes so far"`
	Day        string `json:"day" jsonschema:"the day it started, YYYY-MM-DD in the server's zone"`
	StartTime  string `json:"startTime"`
	EndTime    string `json:"endTime,omitempty" jsonschema:"unset while running"`
	Timed      bool   `json:"timed" jsonschema:"true when startTime and endTime are what the person gave or a timer measured; false when only minutes were given and the start was defaulted, so show the day and minutes, not the range"`
	Running    bool   `json:"running"`
	Note       string `json:"note,omitempty"`
	ProjectID  string `json:"projectId,omitempty"`
	GoalID     string `json:"goalId,omitempty"`
	CheckInID  string `json:"checkInId,omitempty"`
	Space      string `json:"space,omitempty"`
	CreateTime string `json:"createTime,omitempty"`
	UpdateTime string `json:"updateTime,omitempty"`
}

type taskWorkOut struct {
	Kind     string `json:"kind" jsonschema:"always taskWork"`
	ID       string `json:"id"`
	Link     string `json:"link" jsonschema:"its task"`
	TaskID   string `json:"taskId"`
	Time     string `json:"time"`
	Minutes  int32  `json:"minutes"`
	Progress *int32 `json:"progress,omitempty" jsonschema:"0-100, when the sitting gave one"`
	Note     string `json:"note,omitempty"`
}

type reminderOut struct {
	Kind     string `json:"kind" jsonschema:"always reminder"`
	ID       string `json:"id"`
	Link     string `json:"link" jsonschema:"the task or event it is for"`
	RefKind  string `json:"refKind" jsonschema:"task or event"`
	RefID    string `json:"refId"`
	Title    string `json:"title"`
	DueTime  string `json:"dueTime,omitempty" jsonschema:"when the event starts or the task is due"`
	FireTime string `json:"fireTime"`
	Space    string `json:"space,omitempty"`
}

type timeGroupOut struct {
	Kind    string `json:"kind" jsonschema:"always timeGroup"`
	ID      string `json:"id" jsonschema:"the group's key: the activity in lower case, the project or goal id (empty for none), or YYYY-MM-DD"`
	Link    string `json:"link,omitempty" jsonschema:"the project or goal, when grouped by one"`
	Label   string `json:"label"`
	Date    string `json:"date,omitempty" jsonschema:"YYYY-MM-DD, when grouped by day"`
	Minutes int32  `json:"minutes"`
	Count   int32  `json:"count" jsonschema:"entries"`
}

type operationOut struct {
	Type  string         `json:"type"`
	Label string         `json:"label"`
	Args  map[string]any `json:"args,omitempty"`
}

type proposalInputOut struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Type     string `json:"type" jsonschema:"date or text"`
	Required bool   `json:"required"`
}

type suggestionOut struct {
	Kind       string             `json:"kind" jsonschema:"always suggestion"`
	ID         string             `json:"id"`
	Type       string             `json:"type" jsonschema:"goal_slot, project_dates, conflict, overdue, schedule, weekly, plan, extract or ask"`
	Status     string             `json:"status" jsonschema:"pending, accepted or dismissed"`
	Title      string             `json:"title"`
	Reason     string             `json:"reason,omitempty"`
	Space      string             `json:"space,omitempty"`
	Source     string             `json:"source" jsonschema:"rules or ai"`
	Operations []operationOut     `json:"operations"`
	Inputs     []proposalInputOut `json:"inputs,omitempty" jsonschema:"values accept_suggestion needs"`
	CreateTime string             `json:"createTime,omitempty"`
	DecideTime string             `json:"decideTime,omitempty"`
}

type changeOut struct {
	Kind         string `json:"kind" jsonschema:"always change"`
	ID           string `json:"id"`
	SuggestionID string `json:"suggestionId,omitempty"`
	Summary      string `json:"summary"`
	Entries      int32  `json:"entries"`
	CreateTime   string `json:"createTime,omitempty"`
	Undone       bool   `json:"undone"`
	UndoTime     string `json:"undoTime,omitempty"`
}

// listOut is every plain list: the spec wants structuredContent to be an object.
type listOut[T any] struct {
	Items         []T    `json:"items"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}

type eventsOut struct {
	Items []*eventOut `json:"items" jsonschema:"occurrences, by start time"`
	From  string      `json:"from"`
	To    string      `json:"to"`
}

type searchOut struct {
	Items []*noteOut `json:"items"`
	Mode  string     `json:"mode" jsonschema:"fulltext or semantic"`
}

type runningOut struct {
	Running bool     `json:"running"`
	Entry   *timeOut `json:"entry,omitempty"`
}

type timeSummaryOut struct {
	From         string          `json:"from"`
	To           string          `json:"to"`
	GroupBy      string          `json:"groupBy" jsonschema:"title, project, goal or day"`
	TotalMinutes int32           `json:"totalMinutes"`
	TotalCount   int32           `json:"totalCount"`
	Items        []*timeGroupOut `json:"items" jsonschema:"most minutes first; by date for day"`
}

type overviewOut struct {
	Horizon        string        `json:"horizon" jsonschema:"today, upcoming, week or month"`
	From           string        `json:"from"`
	To             string        `json:"to"`
	Space          string        `json:"space,omitempty"`
	Items          []any         `json:"items" jsonschema:"tasks and events, by time"`
	Goals          []*goalOut    `json:"goals"`
	PinnedProjects []*projectOut `json:"pinnedProjects"`
	RunningTimer   *timeOut      `json:"runningTimer,omitempty"`
}

type weeklyReviewOut struct {
	From          string          `json:"from"`
	To            string          `json:"to"`
	TasksDone     int32           `json:"tasksDone"`
	TasksTotal    int32           `json:"tasksTotal"`
	TimeMinutes   int32           `json:"timeMinutes"`
	Goals         []*goalOut      `json:"goals"`
	CarriedTasks  []*taskOut      `json:"carriedTasks" jsonschema:"open and due this week: they slipped"`
	TopActivities []*timeGroupOut `json:"topActivities"`
	NextWeek      *suggestionOut  `json:"nextWeek,omitempty" jsonschema:"a pending suggestion for next week"`
}

type acceptOut struct {
	Suggestion *suggestionOut `json:"suggestion"`
	Change     *changeOut     `json:"change,omitempty"`
}

type referenceOut struct {
	Kind  string `json:"kind" jsonschema:"event, task, note, project or goal"`
	ID    string `json:"id"`
	Link  string `json:"link"`
	Title string `json:"title"`
	Time  string `json:"time,omitempty"`
}

type askOut struct {
	Reply      string         `json:"reply"`
	SessionID  string         `json:"sessionId"`
	ToolsUsed  []string       `json:"toolsUsed,omitempty"`
	Suggestion *suggestionOut `json:"suggestion,omitempty"`
	Change     *changeOut     `json:"change,omitempty"`
	References []referenceOut `json:"references,omitempty"`
}

type summaryOut struct {
	NoteID  string `json:"noteId"`
	Summary string `json:"summary"`
}

type tagsOut struct {
	NoteID  string   `json:"noteId"`
	Tags    []string `json:"tags"`
	Applied bool     `json:"applied"`
}

type briefingOut struct {
	Briefing string `json:"briefing"`
}

// overviewSchema is overviewOut's, with its items spelled out as tasks or events.
var overviewSchema = func() *jsonschema.Schema {
	s := must(jsonschema.For[overviewOut](nil))
	task, event := must(jsonschema.For[taskOut](nil)), must(jsonschema.For[eventOut](nil))
	task.Properties["kind"].Const = jsonschema.Ptr[any]("task")
	event.Properties["kind"].Const = jsonschema.Ptr[any]("event")
	s.Properties["items"].Items = &jsonschema.Schema{AnyOf: []*jsonschema.Schema{task, event}}
	return s
}()

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// ---- conversions ----

func items[P, T any](in []P, f func(P) T) []T {
	out := make([]T, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}

func (t *tools) task(k *pb.Task) *taskOut {
	if k == nil {
		return nil
	}
	return &taskOut{Kind: "task", ID: k.Id, Link: link("task", k.Id), Title: k.Title, Notes: k.Notes,
		DueTime: t.ts(k.DueTime), RemindTime: t.ts(k.RemindTime), Priority: int32(k.Priority),
		Completed: k.Completed, CompleteTime: t.ts(k.CompleteTime),
		Overdue: !k.Completed && k.DueTime != nil && k.DueTime.AsTime().Before(time.Now()),
		Tags:    k.Tags, Space: spaceName(k.Space), ProjectID: k.ProjectId, RRule: k.Rrule,
		Progress: k.Progress, WorkedMinutes: k.WorkedMinutes, WorkSessions: k.WorkSessions, LastWorkedTime: t.ts(k.LastWorkedTime),
		CreateTime: t.ts(k.CreateTime), UpdateTime: t.ts(k.UpdateTime)}
}

func (t *tools) event(e *pb.Event) *eventOut {
	if e == nil {
		return nil
	}
	return &eventOut{Kind: "event", ID: e.Id, Link: link("event", e.Id), Title: e.Title, Description: e.Description,
		Location: e.Location, StartTime: t.ts(e.StartTime), EndTime: t.ts(e.EndTime), AllDay: e.AllDay, TimeZone: e.TimeZone,
		RRule: e.Rrule, RemindBeforeMinutes: e.RemindBeforeMinutes, Space: spaceName(e.Space), ProjectID: e.ProjectId,
		CreateTime: t.ts(e.CreateTime), UpdateTime: t.ts(e.UpdateTime)}
}

// occurrence is an event at one of its occurrences' times.
func (t *tools) occurrence(o *pb.Occurrence) *eventOut {
	e := t.event(o.Event)
	if e == nil {
		e = &eventOut{Kind: "event"}
	}
	e.StartTime, e.EndTime = t.ts(o.StartTime), t.ts(o.EndTime)
	return e
}

func (t *tools) note(n *pb.Note) *noteOut {
	if n == nil {
		return nil
	}
	return &noteOut{Kind: "note", ID: n.Id, Link: link("note", n.Id), Title: n.Title, Content: n.Content, Tags: n.Tags,
		Pinned: n.Pinned, Archived: n.Archived, Space: spaceName(n.Space), ProjectID: n.ProjectId,
		CreateTime: t.ts(n.CreateTime), UpdateTime: t.ts(n.UpdateTime)}
}

func (t *tools) noteHit(h *pb.NoteHit) *noteOut {
	n := t.note(h.Note)
	if n == nil {
		n = &noteOut{Kind: "note"}
	}
	n.Snippet, n.Score = h.Snippet, &h.Score
	return n
}

func (t *tools) project(p *pb.Project) *projectOut {
	if p == nil {
		return nil
	}
	out := &projectOut{Kind: "project", ID: p.Id, Link: link("project", p.Id), Title: p.Title, Notes: p.Notes,
		Pinned: p.Pinned, Archived: p.Archived, StartTime: t.ts(p.StartTime), DueTime: t.ts(p.DueTime),
		Space: spaceName(p.Space), CreateTime: t.ts(p.CreateTime), UpdateTime: t.ts(p.UpdateTime)}
	if g := p.Progress; g != nil {
		out.Progress = &projectProgressOut{TasksTotal: g.TasksTotal, TasksDone: g.TasksDone, Percent: g.Percent,
			EventsUpcoming: g.EventsUpcoming, NotesCount: g.NotesCount, NextTime: t.ts(g.NextTime), NextTitle: g.NextTitle,
			DaysLeft: g.DaysLeft, TasksOverdue: g.TasksOverdue, TimeMinutes: g.TimeMinutes}
	}
	return out
}

func (t *tools) projectDetail(d *pb.ProjectDetail) *projectDetailOut {
	return &projectDetailOut{Project: t.project(d.Project), Tasks: items(d.Tasks, t.task), Events: items(d.Events, t.event),
		Notes: items(d.Notes, t.note), Goals: items(d.Goals, t.goal), Objectives: items(d.Objectives, t.objective),
		TimeEntries: items(d.TimeEntries, t.timeEntry)}
}

func days(in []*pb.DayTotal) []dayTotalOut {
	return items(in, func(d *pb.DayTotal) dayTotalOut { return dayTotalOut{Date: d.Date, Amount: d.Amount} })
}

func (t *tools) goal(g *pb.Goal) *goalOut {
	if g == nil {
		return nil
	}
	out := &goalOut{Kind: "goal", ID: g.Id, Link: link("goal", g.Id), Title: g.Title, Notes: g.Notes, Period: periodName(g.Period),
		Target: g.Target, Unit: g.Unit, TimeZone: g.TimeZone, EventID: g.EventId, Archived: g.Archived, Space: spaceName(g.Space),
		CounterUnit: g.CounterUnit, CounterTarget: g.CounterTarget, ObjectiveID: g.ObjectiveId, ProjectID: g.ProjectId,
		CreateTime: t.ts(g.CreateTime), UpdateTime: t.ts(g.UpdateTime)}
	for _, m := range g.Milestones {
		out.Milestones = append(out.Milestones, milestoneOut{ID: m.Id, Title: m.Title, Done: m.Done, DoneTime: t.ts(m.DoneTime)})
	}
	if p := g.Progress; p != nil {
		out.Progress = &goalProgressOut{PeriodStart: t.ts(p.PeriodStart), PeriodEnd: t.ts(p.PeriodEnd), Done: p.Done, Target: p.Target,
			Remaining: p.Remaining, Percent: p.Percent, Achieved: p.Achieved, Behind: p.Behind, Streak: p.Streak,
			Days: days(p.Days), Recent: days(p.Recent), MilestonesDone: p.MilestonesDone, MilestonesTotal: p.MilestonesTotal,
			MilestonePercent: p.MilestonePercent, CounterDone: p.CounterDone, CounterPercent: p.CounterPercent}
	}
	return out
}

func (t *tools) checkIn(c *pb.CheckIn) *checkInOut {
	if c == nil {
		return nil
	}
	return &checkInOut{Kind: "checkIn", ID: c.Id, Link: link("goal", c.GoalId), GoalID: c.GoalId, Time: t.ts(c.Time),
		Amount: c.Amount, Count: c.Count, Note: c.Note, TimeEntryID: c.TimeEntryId}
}

func (t *tools) objective(o *pb.Objective) *objectiveOut {
	if o == nil {
		return nil
	}
	out := &objectiveOut{Kind: "objective", ID: o.Id, Link: link("objective", o.Id), Title: o.Title, Notes: o.Notes,
		Space: spaceName(o.Space), StartTime: t.ts(o.StartTime), DueTime: t.ts(o.DueTime), MetricName: o.MetricName,
		MetricUnit: o.MetricUnit, MetricStart: o.MetricStart, MetricTarget: o.MetricTarget, Archived: o.Archived,
		ProjectID: o.ProjectId, CreateTime: t.ts(o.CreateTime), UpdateTime: t.ts(o.UpdateTime)}
	if p := o.Progress; p != nil {
		out.Progress = &objectiveProgressOut{GoalsTotal: p.GoalsTotal, GoalsAchieved: p.GoalsAchieved, GoalsPercent: p.GoalsPercent,
			Behind: p.Behind, HasMetric: p.HasMetric, HasReading: p.HasReading, MetricCurrent: p.MetricCurrent,
			MetricChange: p.MetricChange, MetricPercent: p.MetricPercent, LastReadingTime: t.ts(p.LastReadingTime),
			Percent: p.Percent, DaysLeft: p.DaysLeft, OnTrack: p.OnTrack}
	}
	if len(o.Goals) > 0 {
		out.Goals = items(o.Goals, t.goal)
	}
	return out
}

func (t *tools) reading(m *pb.Measurement) *readingOut {
	return &readingOut{Kind: "reading", ID: m.Id, Link: link("objective", m.ObjectiveId), ObjectiveID: m.ObjectiveId,
		Time: t.ts(m.Time), Value: m.Value, Note: m.Note}
}

func (t *tools) holding(h *pb.Holding) *holdingOut {
	if h == nil {
		return nil
	}
	cur := func(v float64) money { return money{Amount: v, Currency: h.Currency} }
	out := &holdingOut{Kind: "holding", ID: h.Id, Link: link("holding", h.Id), Symbol: h.Symbol, Name: h.Name,
		Currency: h.Currency, Notes: h.Notes, LastPriceTime: t.ts(h.LastPriceTime), Archived: h.Archived,
		CreateTime: t.ts(h.CreateTime), UpdateTime: t.ts(h.UpdateTime)}
	if h.LastPrice != nil {
		out.LastPrice = ptr(cur(*h.LastPrice))
	}
	if h.DcaDay > 0 {
		out.Plan = &holdingPlanOut{Amount: cur(h.DcaAmount), Day: h.DcaDay, Time: h.DcaTime, TimeZone: h.TimeZone,
			EventID: h.DcaEventId, InvestedThisMonth: cur(0)}
	}
	if p := h.Position; p != nil {
		out.Position = &positionOut{Shares: p.Shares, AvgCost: cur(p.AvgCost), CostBasis: cur(p.CostBasis),
			RealizedPnl: cur(p.RealizedPnl), TotalBought: cur(p.TotalBought), Trades: p.Trades, HasPrice: p.HasPrice}
		if p.HasPrice {
			out.Position.MarketValue, out.Position.UnrealizedPnl = ptr(cur(p.MarketValue)), ptr(cur(p.UnrealizedPnl))
			out.Position.UnrealizedPercent = ptr(p.UnrealizedPercent)
		}
		if d := p.Dca; d != nil && out.Plan != nil {
			out.Plan.Active, out.Plan.NextTime, out.Plan.DoneThisMonth = d.Active, t.ts(d.NextTime), d.DoneThisMonth
			out.Plan.InvestedThisMonth, out.Plan.StreakMonths = cur(d.InvestedThisMonth), d.StreakMonths
		}
	}
	return out
}

func (t *tools) trade(r *pb.Trade, currency string) *tradeOut {
	return &tradeOut{Kind: "trade", ID: r.Id, Link: link("holding", r.HoldingId), HoldingID: r.HoldingId, Time: t.ts(r.Time),
		Side: r.Side, Shares: r.Shares, Price: money{Amount: r.Price, Currency: currency}, Fee: money{Amount: r.Fee, Currency: currency}, Note: r.Note}
}

func (t *tools) timeEntry(e *pb.TimeEntry) *timeOut {
	if e == nil {
		return nil
	}
	day := ""
	if e.StartTime != nil {
		day = e.StartTime.AsTime().In(t.loc).Format("2006-01-02")
	}
	return &timeOut{Kind: "time", ID: e.Id, Link: link("time", e.Id), Title: e.Title, Minutes: e.Minutes, Day: day,
		StartTime: t.ts(e.StartTime), EndTime: t.ts(e.EndTime), Timed: e.Timed, Running: e.Running, Note: e.Note, ProjectID: e.ProjectId,
		GoalID: e.GoalId, CheckInID: e.CheckInId, Space: spaceName(e.Space), CreateTime: t.ts(e.CreateTime), UpdateTime: t.ts(e.UpdateTime)}
}

func (t *tools) taskWork(w *pb.TaskWork) *taskWorkOut {
	out := &taskWorkOut{Kind: "taskWork", ID: w.Id, Link: link("task", w.TaskId), TaskID: w.TaskId, Time: t.ts(w.Time),
		Minutes: w.Minutes, Note: w.Note}
	if w.Progress >= 0 {
		out.Progress = ptr(w.Progress)
	}
	return out
}

func (t *tools) reminder(r *pb.Reminder) *reminderOut {
	ref := "event"
	if r.Kind == pb.Reminder_KIND_TASK {
		ref = "task"
	}
	return &reminderOut{Kind: "reminder", ID: r.Id, Link: link(ref, r.RefId), RefKind: ref, RefID: r.RefId, Title: r.Title,
		DueTime: t.ts(r.DueTime), FireTime: t.ts(r.FireTime), Space: spaceName(r.Space)}
}

func timeGroups(rows []*pb.TimeSummaryRow, by pb.TimeGroupBy) []*timeGroupOut {
	kind := map[pb.TimeGroupBy]string{pb.TimeGroupBy_TIME_GROUP_BY_PROJECT: "project", pb.TimeGroupBy_TIME_GROUP_BY_GOAL: "goal"}[by]
	return items(rows, func(r *pb.TimeSummaryRow) *timeGroupOut {
		out := &timeGroupOut{Kind: "timeGroup", ID: r.Key, Label: r.Label, Minutes: r.Minutes, Count: r.Count}
		if kind != "" {
			out.Link = link(kind, r.Key)
		}
		if by == pb.TimeGroupBy_TIME_GROUP_BY_DAY {
			out.Date = r.Key
		}
		return out
	})
}

func (t *tools) timeSummary(s *pb.TimeSummary) *timeSummaryOut {
	return &timeSummaryOut{From: t.ts(s.From), To: t.ts(s.To), GroupBy: groupByName(s.GroupBy), TotalMinutes: s.TotalMinutes,
		TotalCount: s.TotalCount, Items: timeGroups(s.Rows, s.GroupBy)}
}

func (t *tools) suggestion(p *pb.Proposal) *suggestionOut {
	if p == nil {
		return nil
	}
	out := &suggestionOut{Kind: "suggestion", ID: p.Id, Type: p.Kind, Status: p.Status, Title: p.Title, Reason: p.Reason,
		Space: spaceName(p.Space), Source: p.Source, Operations: []operationOut{}, CreateTime: t.ts(p.CreateTime), DecideTime: t.ts(p.DecideTime)}
	for _, op := range p.Operations {
		out.Operations = append(out.Operations, operationOut{Type: op.Type, Label: op.Label, Args: op.Args.AsMap()})
	}
	for _, in := range p.Inputs {
		out.Inputs = append(out.Inputs, proposalInputOut{Name: in.Name, Label: in.Label, Type: in.Type, Required: in.Required})
	}
	return out
}

func (t *tools) change(c *pb.Change) *changeOut {
	if c == nil {
		return nil
	}
	return &changeOut{Kind: "change", ID: c.Id, SuggestionID: c.ProposalId, Summary: c.Summary, Entries: c.Entries,
		CreateTime: t.ts(c.CreateTime), Undone: c.Undone, UndoTime: t.ts(c.UndoTime)}
}

func (t *tools) overview(r *pb.GetFocusResponse, horizon string) *overviewOut {
	out := &overviewOut{Horizon: horizon, From: t.ts(r.From), To: t.ts(r.To), Space: spaceName(r.Space), Items: []any{},
		Goals: items(r.Goals, t.goal), PinnedProjects: items(r.PinnedProjects, t.project), RunningTimer: t.timeEntry(r.RunningTimer)}
	for _, it := range r.Items {
		switch {
		case it.GetTask() != nil:
			k := t.task(it.GetTask())
			k.Overdue = k.Overdue || it.Overdue
			k.ProjectTitle = it.ProjectTitle
			out.Items = append(out.Items, k)
		case it.GetEvent() != nil:
			e := t.occurrence(it.GetEvent())
			e.ProjectTitle = it.ProjectTitle
			out.Items = append(out.Items, e)
		}
	}
	return out
}

func (t *tools) weeklyReview(w *pb.WeeklyReview) *weeklyReviewOut {
	return &weeklyReviewOut{From: t.ts(w.From), To: t.ts(w.To), TasksDone: w.TasksDone, TasksTotal: w.TasksTotal,
		TimeMinutes: w.TimeMinutes, Goals: items(w.Goals, t.goal), CarriedTasks: items(w.CarriedTasks, t.task),
		TopActivities: timeGroups(w.TopActivities, pb.TimeGroupBy_TIME_GROUP_BY_TITLE), NextWeek: t.suggestion(w.NextWeek)}
}

func (t *tools) ask(r *pb.AskResponse) *askOut {
	out := &askOut{Reply: r.Reply, SessionID: r.SessionId, ToolsUsed: r.ToolsUsed, Suggestion: t.suggestion(r.Proposal), Change: t.change(r.Change)}
	for _, ref := range r.References {
		out.References = append(out.References, referenceOut{Kind: ref.Kind, ID: ref.Id, Link: link(ref.Kind, ref.Id), Title: ref.Title, Time: t.ts(ref.Time)})
	}
	return out
}
