package service

import (
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func tsPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

// timePtr converts an optional proto timestamp; unset or zero means "none".
func timePtr(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	v := t.AsTime()
	return &v
}

// spaceIn maps the wire enum to the store's string; UNSPECIFIED is "" (both / inherit).
func spaceIn(s pb.Space) string {
	switch s {
	case pb.Space_SPACE_WORK:
		return store.SpaceWork
	case pb.Space_SPACE_LIFE:
		return store.SpaceLife
	}
	return ""
}

func spaceOut(s string) pb.Space {
	switch s {
	case store.SpaceWork:
		return pb.Space_SPACE_WORK
	case store.SpaceLife:
		return pb.Space_SPACE_LIFE
	}
	return pb.Space_SPACE_UNSPECIFIED
}

func noteToPB(n store.Note) *pb.Note {
	return &pb.Note{
		Id: n.ID, Title: n.Title, Content: n.Content, Tags: n.Tags, Pinned: n.Pinned, Archived: n.Archived,
		ProjectId: n.ProjectID, Space: spaceOut(n.Space),
		CreateTime: timestamppb.New(n.Created), UpdateTime: timestamppb.New(n.Updated),
	}
}

func eventToPB(e store.Event) *pb.Event {
	out := &pb.Event{
		Id: e.ID, Title: e.Title, Description: e.Description, Location: e.Location,
		StartTime: timestamppb.New(e.Start), EndTime: timestamppb.New(e.End), AllDay: e.AllDay,
		TimeZone: e.TimeZone, Rrule: e.RRule, ProjectId: e.ProjectID, Space: spaceOut(e.Space),
		CreateTime: timestamppb.New(e.Created), UpdateTime: timestamppb.New(e.Updated),
	}
	if e.RemindBefore != nil {
		v := int32(*e.RemindBefore)
		out.RemindBeforeMinutes = &v
	}
	return out
}

func eventFromPB(e *pb.Event) store.Event {
	out := store.Event{
		Title: e.GetTitle(), Description: e.GetDescription(), Location: e.GetLocation(),
		AllDay: e.GetAllDay(), TimeZone: e.GetTimeZone(), RRule: e.GetRrule(),
		ProjectID: e.GetProjectId(), Space: spaceIn(e.GetSpace()),
	}
	if e.StartTime != nil {
		out.Start = e.StartTime.AsTime()
	}
	if e.EndTime != nil {
		out.End = e.EndTime.AsTime()
	}
	if e.RemindBeforeMinutes != nil {
		v := int(*e.RemindBeforeMinutes)
		out.RemindBefore = &v
	}
	return out
}

func taskToPB(t store.Task) *pb.Task {
	return &pb.Task{
		Id: t.ID, Title: t.Title, Notes: t.Notes, DueTime: tsPtr(t.Due), Priority: pb.Priority(t.Priority),
		Completed: t.Done, CompleteTime: tsPtr(t.DoneAt), RemindTime: tsPtr(t.Remind), Tags: t.Tags,
		ProjectId: t.ProjectID, Space: spaceOut(t.Space),
		CreateTime: timestamppb.New(t.Created), UpdateTime: timestamppb.New(t.Updated),
	}
}

func reminderToPB(r store.Reminder) *pb.Reminder {
	kind := pb.Reminder_KIND_UNSPECIFIED
	switch r.Kind {
	case store.KindEvent:
		kind = pb.Reminder_KIND_EVENT
	case store.KindTask:
		kind = pb.Reminder_KIND_TASK
	}
	return &pb.Reminder{
		Id: r.ID, Kind: kind, RefId: r.RefID, Title: r.Title,
		DueTime: timestamppb.New(r.Due), FireTime: timestamppb.New(r.Fired), Space: spaceOut(r.Space),
	}
}
