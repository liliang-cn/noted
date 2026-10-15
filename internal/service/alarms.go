package service

import (
	"context"
	"strings"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/alarm"
	"github.com/liliang-cn/noted/internal/holiday"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// nextFires is how many upcoming rings an alarm carries.
const nextFires = 5

// Alarms keeps alarm definitions (闹钟); the phone rings them.
type Alarms struct {
	pb.UnimplementedAlarmServiceServer
	Store    *store.Store
	Location *time.Location // the zone of a schedule that names none
}

func (s *Alarms) zone() string {
	if s.Location == nil {
		return "UTC"
	}
	return s.Location.String()
}

func alarmToPB(a store.Alarm, now time.Time) *pb.Alarm {
	sc := a.Schedule
	out := &pb.Alarm{
		Id: a.ID, Label: a.Label, Mode: a.Mode, Enabled: &a.Enabled, HolidayPolicy: a.HolidayPolicy, Region: &a.Region, Note: a.Note,
		ProjectId: a.ProjectID, Space: spaceOut(a.Space), OriginalText: a.OriginalText,
		CreateTime: timestamppb.New(a.Created), UpdateTime: timestamppb.New(a.Updated),
		Schedule: &pb.AlarmSchedule{Kind: sc.Kind, At: tsPtr(sc.At), Anchor: sc.Anchor, Time: sc.Time, TimeZone: sc.TimeZone},
	}
	if r := sc.Recurrence; r != nil {
		out.Schedule.Recurrence = &pb.AlarmRecurrence{Freq: r.Freq, Interval: int32(r.Interval), ByDay: r.ByDay, ByMonthDay: int32(r.ByMonthDay),
			ByTime: r.ByTime, Until: r.Until, Count: int32(r.Count), EndTime: r.EndTime}
	}
	for _, t := range a.NextFires(now, nextFires) {
		out.NextFires = append(out.NextFires, timestamppb.New(t))
	}
	return out
}

// scheduleIn reads a wire schedule; a missing zone is the server's.
func (s *Alarms) scheduleIn(in *pb.AlarmSchedule) (alarm.Schedule, error) {
	if in == nil {
		return alarm.Schedule{}, status.Error(codes.InvalidArgument, "schedule is required")
	}
	out := alarm.Schedule{Kind: in.Kind, At: timePtr(in.At), Anchor: in.Anchor, Time: in.Time, TimeZone: in.TimeZone}
	if r := in.Recurrence; r != nil {
		out.Recurrence = &alarm.Recurrence{Freq: r.Freq, Interval: int(r.Interval), ByDay: r.ByDay, ByMonthDay: int(r.ByMonthDay),
			ByTime: r.ByTime, Until: r.Until, Count: int(r.Count), EndTime: r.EndTime}
	}
	return out, nil
}

// chinaZones are the zones whose alarms follow the CN holiday calendar unless told otherwise.
var chinaZones = map[string]bool{"Asia/Shanghai": true, "Asia/Chongqing": true, "Asia/Chungking": true, "Asia/Harbin": true,
	"Asia/Urumqi": true, "Asia/Kashgar": true, "PRC": true}

// DefaultRegion is the holiday region of an alarm in zone tz that names none.
func DefaultRegion(tz string) string {
	if chinaZones[tz] {
		return holiday.RegionCN
	}
	return ""
}

func (s *Alarms) CreateAlarm(ctx context.Context, req *pb.CreateAlarmRequest) (*pb.Alarm, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetAlarm()
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "alarm is required")
	}
	sc, err := s.scheduleIn(in.Schedule)
	if err != nil {
		return nil, err
	}
	if sc.TimeZone == "" {
		sc.TimeZone = s.zone()
	}
	a := store.Alarm{UserID: u, Label: in.Label, Mode: in.Mode, Enabled: in.Enabled == nil || *in.Enabled, Schedule: sc,
		HolidayPolicy: in.HolidayPolicy, Note: in.Note, ProjectID: in.ProjectId, Space: spaceIn(in.Space), OriginalText: in.OriginalText}
	if in.Region != nil {
		a.Region = *in.Region
	} else {
		a.Region = DefaultRegion(sc.TimeZone)
	}
	out, err := s.Store.CreateAlarm(ctx, a)
	if err != nil {
		return nil, toStatus(err)
	}
	return alarmToPB(out, time.Now()), nil
}

func (s *Alarms) GetAlarm(ctx context.Context, req *pb.GetAlarmRequest) (*pb.Alarm, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.Store.GetAlarm(ctx, u, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return alarmToPB(a, time.Now()), nil
}

func (s *Alarms) UpdateAlarm(ctx context.Context, req *pb.UpdateAlarmRequest) (*pb.Alarm, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetAlarm()
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "alarm.id is required")
	}
	paths, err := maskPaths(req.UpdateMask, "label", "mode", "enabled", "schedule", "holiday_policy", "region", "note", "project_id", "space", "original_text")
	if err != nil {
		return nil, err
	}
	var p store.AlarmPatch
	str := func(path string, v string) *string {
		if paths[path] {
			return &v
		}
		return nil
	}
	p.Label, p.Mode, p.HolidayPolicy = str("label", in.Label), str("mode", in.Mode), str("holiday_policy", in.HolidayPolicy)
	p.Region, p.Note, p.OriginalText = str("region", in.GetRegion()), str("note", in.Note), str("original_text", in.OriginalText)
	p.ProjectID = str("project_id", in.ProjectId)
	if paths["enabled"] {
		v := in.GetEnabled()
		p.Enabled = &v
	}
	if paths["schedule"] {
		sc, err := s.scheduleIn(in.Schedule)
		if err != nil {
			return nil, err
		}
		p.Schedule = &sc
	}
	if paths["space"] {
		sp := spaceIn(in.Space)
		if sp == "" {
			return nil, status.Error(codes.InvalidArgument, "space must be WORK or LIFE")
		}
		p.Space = &sp
	}
	a, err := s.Store.UpdateAlarm(ctx, u, in.Id, p)
	if err != nil {
		return nil, toStatus(err)
	}
	return alarmToPB(a, time.Now()), nil
}

func (s *Alarms) DeleteAlarm(ctx context.Context, req *pb.DeleteAlarmRequest) (*emptypb.Empty, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteAlarm(ctx, u, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Alarms) ListAlarms(ctx context.Context, req *pb.ListAlarmsRequest) (*pb.ListAlarmsResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	as, version, err := s.Store.ListAlarms(ctx, u, store.AlarmFilter{Enabled: req.Enabled, Space: spaceIn(req.Space), ProjectID: req.ProjectId})
	if err != nil {
		return nil, toStatus(err)
	}
	now := time.Now()
	out := &pb.ListAlarmsResponse{Version: version}
	for _, a := range as {
		out.Alarms = append(out.Alarms, alarmToPB(a, now))
	}
	return out, nil
}

func (s *Alarms) GetAlarmsVersion(ctx context.Context, _ *pb.GetAlarmsVersionRequest) (*pb.GetAlarmsVersionResponse, error) {
	u, err := uid(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.Store.AlarmsVersion(ctx, u)
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.GetAlarmsVersionResponse{Version: v}, nil
}

// Holidays serves the statutory holiday calendar alarms follow.
type Holidays struct {
	pb.UnimplementedHolidayServiceServer
	Location *time.Location // "this year" is read here
}

func (s *Holidays) GetHolidays(ctx context.Context, req *pb.GetHolidaysRequest) (*pb.GetHolidaysResponse, error) {
	if _, err := uid(ctx); err != nil {
		return nil, err
	}
	region := strings.ToUpper(strings.TrimSpace(req.Region))
	if region == "" {
		region = holiday.RegionCN
	}
	cal := holiday.ForRegion(region)
	if cal == nil {
		return nil, status.Errorf(codes.InvalidArgument, "region %q has no holiday calendar; only CN (mainland China) has one", req.Region)
	}
	year := int(req.Year)
	if year == 0 {
		loc := s.Location
		if loc == nil {
			loc = time.UTC
		}
		year = time.Now().In(loc).Year()
	}
	if year < 1900 || year > 2200 {
		return nil, status.Error(codes.InvalidArgument, "year must be 1900-2200")
	}
	out := &pb.GetHolidaysResponse{Year: int32(year), Region: region, Version: cal.Version()}
	for _, d := range cal.Year(year) {
		out.Days = append(out.Days, &pb.HolidayDay{Date: d.Date, Kind: d.Kind, Name: d.Name})
	}
	for _, y := range cal.Years() {
		out.AvailableYears = append(out.AvailableYears, int32(y))
	}
	return out, nil
}
