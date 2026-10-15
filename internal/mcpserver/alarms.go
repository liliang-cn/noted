package mcpserver

import (
	"context"
	"fmt"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// The schedule an LLM writes is the shape get_alarm returns, so it can copy
// one, change a field and send it back.
type alarmRecurrenceIn struct {
	Freq       string   `json:"freq" jsonschema:"minutely, hourly, daily, weekly or monthly (yearly is not supported)"`
	Interval   int32    `json:"interval,omitempty" jsonschema:"every N units, default 1; weekly with 2 is every other week (每隔一周)"`
	ByDay      []string `json:"byDay,omitempty" jsonschema:"weekly only: MON TUE WED THU FRI SAT SUN; default the anchor's weekday. For workdays use holiday_policy workdayOnly, not byDay"`
	ByMonthDay int32    `json:"byMonthDay,omitempty" jsonschema:"monthly only: 1-31, default the anchor's day; months without that day are skipped"`
	ByTime     []string `json:"byTime,omitempty" jsonschema:"daily only: several HH:MM a day (早中晚 08:00 12:00 18:00), instead of schedule.time"`
	Until      string   `json:"until,omitempty" jsonschema:"YYYY-MM-DD, the last day it may ring (inclusive)"`
	Count      int32    `json:"count,omitempty" jsonschema:"rings in all, counted from the anchor"`
	EndTime    string   `json:"endTime,omitempty" jsonschema:"minutely/hourly only: HH:MM, the day's last possible ring; default 22:00"`
}

type alarmScheduleIn struct {
	Kind       string             `json:"kind" jsonschema:"oneShot (rings once: give at) or recurring (give anchor, time and recurrence)"`
	At         string             `json:"at,omitempty" jsonschema:"oneShot: when it rings, RFC 3339, e.g. 2026-10-14T07:00:00+08:00"`
	Anchor     string             `json:"anchor,omitempty" jsonschema:"recurring: YYYY-MM-DD, the first day it may ring (usually today); every-N-days/weeks/months count from it"`
	Time       string             `json:"time,omitempty" jsonschema:"recurring: HH:MM in timeZone; minutely/hourly: the first ring of each day"`
	TimeZone   string             `json:"timeZone,omitempty" jsonschema:"IANA name; default the server's zone"`
	Recurrence *alarmRecurrenceIn `json:"recurrence,omitempty" jsonschema:"recurring only"`
}

type createAlarmIn struct {
	Label         string          `json:"label" jsonschema:"what it is for, short: 起床, 接三宝, 吃药"`
	Mode          string          `json:"mode,omitempty" jsonschema:"alarm (default): a real alarm that rings through silent mode and Focus, for 闹钟/叫我/叫醒/准时响/必须提醒到; reminder: just a notification, for 提醒一下/通知我就行/只要通知"`
	Schedule      alarmScheduleIn `json:"schedule"`
	HolidayPolicy string          `json:"holiday_policy,omitempty" jsonschema:"none (default); skipHoliday: 节假日不响; workdayOnly: 工作日/上班日 only, following 法定节假日 and 调休补班 in CN; followStatutory: 按调休, the same days as workdayOnly"`
	Region        *string         `json:"region,omitempty" jsonschema:"CN (mainland China holidays and 调休) or empty string for none (workdays are Mon-Fri); omit for the default: CN when the time zone is in mainland China, else none"`
	Enabled       *bool           `json:"enabled,omitempty" jsonschema:"default true"`
	Note          string          `json:"note,omitempty"`
	ProjectID     string          `json:"project_id,omitempty"`
	Space         string          `json:"space,omitempty" jsonschema:"work or life; omit to inherit from the project, else life"`
	OriginalText  string          `json:"original_text,omitempty" jsonschema:"the user's own words, e.g. 每隔一周的周一下午三点一刻接三宝"`
}

type updateAlarmIn struct {
	ID            string           `json:"id"`
	Label         *string          `json:"label,omitempty"`
	Mode          *string          `json:"mode,omitempty" jsonschema:"alarm or reminder"`
	Enabled       *bool            `json:"enabled,omitempty" jsonschema:"false turns it off (关闭闹钟) without deleting it; true turns it back on"`
	Schedule      *alarmScheduleIn `json:"schedule,omitempty" jsonschema:"replaces the whole schedule: send all of it, e.g. get_alarm's schedule with the time changed"`
	HolidayPolicy *string          `json:"holiday_policy,omitempty"`
	Region        *string          `json:"region,omitempty" jsonschema:"CN or empty string for none"`
	Note          *string          `json:"note,omitempty"`
	ProjectID     *string          `json:"project_id,omitempty" jsonschema:"empty string takes it out of its project"`
	Space         *string          `json:"space,omitempty" jsonschema:"work or life"`
	OriginalText  *string          `json:"original_text,omitempty"`
}

type listAlarmsIn struct {
	Enabled   *bool  `json:"enabled,omitempty" jsonschema:"true: only those that are on; false: only those that are off; omit for all"`
	Space     string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
	ProjectID string `json:"project_id,omitempty"`
}

type holidaysIn struct {
	Year   int32  `json:"year,omitempty" jsonschema:"default this year"`
	Region string `json:"region,omitempty" jsonschema:"default CN, the only region with data"`
}

// ---- results ----

type alarmRecurrenceOut struct {
	Freq       string   `json:"freq" jsonschema:"minutely, hourly, daily, weekly or monthly"`
	Interval   int32    `json:"interval" jsonschema:"1 or more"`
	ByDay      []string `json:"byDay,omitempty" jsonschema:"weekly: MON..SUN"`
	ByMonthDay int32    `json:"byMonthDay,omitempty" jsonschema:"monthly: 1-31"`
	ByTime     []string `json:"byTime,omitempty" jsonschema:"daily: HH:MM, several a day"`
	Until      string   `json:"until,omitempty" jsonschema:"YYYY-MM-DD, inclusive"`
	Count      int32    `json:"count,omitempty"`
	EndTime    string   `json:"endTime,omitempty" jsonschema:"minutely/hourly: HH:MM; absent means 22:00"`
}

type alarmScheduleOut struct {
	Kind       string              `json:"kind" jsonschema:"oneShot or recurring"`
	At         string              `json:"at,omitempty" jsonschema:"oneShot"`
	Anchor     string              `json:"anchor,omitempty" jsonschema:"recurring: YYYY-MM-DD"`
	Time       string              `json:"time,omitempty" jsonschema:"recurring: HH:MM; absent with byTime"`
	TimeZone   string              `json:"timeZone" jsonschema:"IANA"`
	Recurrence *alarmRecurrenceOut `json:"recurrence,omitempty"`
}

type alarmOut struct {
	Kind          string           `json:"kind" jsonschema:"always alarm"`
	ID            string           `json:"id"`
	Link          string           `json:"link"`
	Label         string           `json:"label"`
	Mode          string           `json:"mode" jsonschema:"alarm or reminder"`
	Enabled       bool             `json:"enabled"`
	Schedule      alarmScheduleOut `json:"schedule"`
	HolidayPolicy string           `json:"holidayPolicy" jsonschema:"none, skipHoliday, workdayOnly or followStatutory"`
	Region        string           `json:"region" jsonschema:"CN or empty"`
	Note          string           `json:"note,omitempty"`
	ProjectID     string           `json:"projectId,omitempty"`
	Space         string           `json:"space" jsonschema:"work or life"`
	OriginalText  string           `json:"originalText,omitempty"`
	CreateTime    string           `json:"createTime"`
	UpdateTime    string           `json:"updateTime"`
	NextFires     []string         `json:"nextFires" jsonschema:"the next rings (at most 5), holidays applied; empty when off or finished"`
}

type alarmsOut struct {
	Items   []*alarmOut `json:"items"`
	Version string      `json:"version" jsonschema:"opaque; changes whenever any of the user's alarms changes, deletes included"`
}

type holidayDayOut struct {
	Date string `json:"date" jsonschema:"YYYY-MM-DD"`
	Kind string `json:"kind" jsonschema:"holiday (a day off) or workday (a weekend day worked, 调休补班)"`
	Name string `json:"name"`
}

type holidaysOut struct {
	Year           int32           `json:"year"`
	Region         string          `json:"region"`
	Days           []holidayDayOut `json:"days" jsonschema:"by date; empty when the year has no data, and then every day is ordinary (Mon-Fri work)"`
	AvailableYears []int32         `json:"availableYears"`
	Version        string          `json:"version" jsonschema:"changes when the data does"`
}

func (t *tools) alarm(a *pb.Alarm) *alarmOut {
	if a == nil {
		return nil
	}
	sc := a.GetSchedule()
	out := &alarmOut{Kind: "alarm", ID: a.Id, Link: link("alarm", a.Id), Label: a.Label, Mode: a.Mode, Enabled: a.GetEnabled(),
		HolidayPolicy: a.HolidayPolicy, Region: a.GetRegion(), Note: a.Note, ProjectID: a.ProjectId, Space: spaceName(a.Space),
		OriginalText: a.OriginalText, CreateTime: t.ts(a.CreateTime), UpdateTime: t.ts(a.UpdateTime), NextFires: []string{},
		Schedule: alarmScheduleOut{Kind: sc.GetKind(), At: t.ts(sc.GetAt()), Anchor: sc.GetAnchor(), Time: sc.GetTime(), TimeZone: sc.GetTimeZone()}}
	if r := sc.GetRecurrence(); r != nil {
		out.Schedule.Recurrence = &alarmRecurrenceOut{Freq: r.Freq, Interval: r.Interval, ByDay: r.ByDay, ByMonthDay: r.ByMonthDay,
			ByTime: r.ByTime, Until: r.Until, Count: r.Count, EndTime: r.EndTime}
	}
	for _, f := range a.NextFires {
		out.NextFires = append(out.NextFires, t.ts(f))
	}
	return out
}

func (t *tools) scheduleIn(in *alarmScheduleIn) (*pb.AlarmSchedule, error) {
	out := &pb.AlarmSchedule{Kind: in.Kind, Anchor: in.Anchor, Time: in.Time, TimeZone: in.TimeZone}
	if in.At != "" {
		at, err := t.when(in.At)
		if err != nil {
			return nil, fmt.Errorf("schedule.at: %w", err)
		}
		out.At = at
	}
	if r := in.Recurrence; r != nil {
		out.Recurrence = &pb.AlarmRecurrence{Freq: r.Freq, Interval: r.Interval, ByDay: r.ByDay, ByMonthDay: r.ByMonthDay,
			ByTime: r.ByTime, Until: r.Until, Count: r.Count, EndTime: r.EndTime}
	}
	return out, nil
}

const alarmHelp = " Resolve relative dates yourself (今天/明天/下周一) before calling: the server reads nothing relative. " +
	"Shapes: 明天 7 点 → {kind: oneShot, at: <RFC 3339>}; 每天 7 点 → {kind: recurring, anchor: <today>, time: 07:00, recurrence: {freq: daily}}; " +
	"工作日 7 点 → daily plus holiday_policy workdayOnly; 每隔一周的周一 15:15 → {freq: weekly, interval: 2, byDay: [MON]} with anchor in the first week it rings; " +
	"每月 15 号 → {freq: monthly, byMonthDay: 15}; 早中晚 → {freq: daily, byTime: [08:00, 12:00, 18:00]} without time; " +
	"今天 9 点到 18 点每小时 → {freq: hourly, time: 09:00, endTime: 18:00} (a minutely/hourly alarm rings on its anchor day only unless until or count is given)."

// registerAlarms adds alarms (闹钟) and the holiday calendar they follow.
// noted keeps the definitions; the user's phone rings them.
func (t *tools) registerAlarms(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "create_alarm", Description: "Set an alarm (闹钟) or a reminder that the user's phone rings at the time, one-off or repeating, optionally skipping holidays or ringing on workdays only (CN 法定节假日 and 调休 included). " +
		"Use it whenever the user asks to be woken or called (闹钟/叫我/叫醒/准时响/必须提醒到) with mode alarm; when a notification is enough (提醒一下/通知我就行/只要通知) use mode reminder. " +
		"Returns the alarm with nextFires, the next times it will ring: check them against what the user meant." + alarmHelp, Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createAlarmIn) (*mcp.CallToolResult, *alarmOut, error) {
			sc, err := t.scheduleIn(&in.Schedule)
			if err != nil {
				return nil, nil, err
			}
			if sc.TimeZone == "" {
				sc.TimeZone = t.loc.String()
			}
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.alarms.CreateAlarm(ctx, &pb.CreateAlarmRequest{Alarm: &pb.Alarm{Label: in.Label, Mode: in.Mode, Enabled: in.Enabled,
				Schedule: sc, HolidayPolicy: in.HolidayPolicy, Region: in.Region, Note: in.Note, ProjectId: in.ProjectID, Space: sp,
				OriginalText: in.OriginalText}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.alarm(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_alarms", Description: "The user's alarms and reminders (闹钟), oldest first, each with its schedule and next rings, plus a version that changes whenever any alarm changes.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listAlarmsIn) (*mcp.CallToolResult, *alarmsOut, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.alarms.ListAlarms(ctx, &pb.ListAlarmsRequest{Enabled: in.Enabled, Space: sp, ProjectId: in.ProjectID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, &alarmsOut{Items: items(r.Alarms, t.alarm), Version: r.Version})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_alarm", Description: "One alarm with its schedule and next rings.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *alarmOut, error) {
			r, err := t.alarms.GetAlarm(ctx, &pb.GetAlarmRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.alarm(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "update_alarm", Description: "Change an alarm: turn it off or on (enabled), move its time (send the whole schedule, e.g. get_alarm's with time changed), rename it, switch alarm/reminder, change the holiday policy. Omitted fields stay as they are.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateAlarmIn) (*mcp.CallToolResult, *alarmOut, error) {
			a, mask := &pb.Alarm{Id: in.ID}, []string{}
			str := func(v *string, path string, dst *string) {
				if v != nil {
					*dst, mask = *v, append(mask, path)
				}
			}
			str(in.Label, "label", &a.Label)
			str(in.Mode, "mode", &a.Mode)
			str(in.HolidayPolicy, "holiday_policy", &a.HolidayPolicy)
			str(in.Note, "note", &a.Note)
			str(in.ProjectID, "project_id", &a.ProjectId)
			str(in.OriginalText, "original_text", &a.OriginalText)
			if in.Region != nil {
				a.Region, mask = in.Region, append(mask, "region")
			}
			if in.Enabled != nil {
				a.Enabled, mask = in.Enabled, append(mask, "enabled")
			}
			if in.Schedule != nil {
				sc, err := t.scheduleIn(in.Schedule)
				if err != nil {
					return nil, nil, err
				}
				a.Schedule, mask = sc, append(mask, "schedule")
			}
			if in.Space != nil {
				sp, err := spaceOf(*in.Space)
				if err != nil || sp == pb.Space_SPACE_UNSPECIFIED {
					return nil, nil, fmt.Errorf("space must be work or life")
				}
				a.Space, mask = sp, append(mask, "space")
			}
			if len(mask) == 0 {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field")
			}
			r, err := t.alarms.UpdateAlarm(ctx, &pb.UpdateAlarmRequest{Alarm: a, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.alarm(r))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_alarm", Description: "Permanently delete an alarm (删除闹钟). To pause one instead, update_alarm with enabled false.", Annotations: deleting},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, *deletedOut, error) {
			if _, err := t.alarms.DeleteAlarm(ctx, &pb.DeleteAlarmRequest{Id: in.ID}); err != nil {
				return nil, nil, err
			}
			return deleted("alarm", "alarm", in.ID)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_holidays", Description: "The statutory holiday calendar alarms follow (法定节假日 and 调休补班): the days off and the weekend days worked instead, for a year. Only CN (mainland China) has data.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in holidaysIn) (*mcp.CallToolResult, *holidaysOut, error) {
			r, err := t.hols.GetHolidays(ctx, &pb.GetHolidaysRequest{Year: in.Year, Region: in.Region})
			if err != nil {
				return nil, nil, err
			}
			out := &holidaysOut{Year: r.Year, Region: r.Region, Version: r.Version, Days: []holidayDayOut{}, AvailableYears: r.AvailableYears}
			if out.AvailableYears == nil {
				out.AvailableYears = []int32{}
			}
			for _, d := range r.Days {
				out.Days = append(out.Days, holidayDayOut{Date: d.Date, Kind: d.Kind, Name: d.Name})
			}
			return reply(r, out)
		})
}
