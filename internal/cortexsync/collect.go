package cortexsync

import (
	"context"
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

const pageSize = 500

// Collect reads everything a user has, archived and finished items included,
// in the spaces the user lets AI read (AIService.SetAIAccess): the brain is AI
// memory, so a space closed to the assistant is closed to it too, and what
// was sent from it before is withdrawn on the next run. Holdings have no space
// and count as life.
func Collect(ctx context.Context, st *store.Store, userID, userName string, loc *time.Location, lang string, now time.Time) (Snapshot, error) {
	s := Snapshot{User: userName, Location: loc, Lang: lang, Now: now}
	access, err := st.GetAIAccess(ctx, userID)
	if err != nil {
		return s, err
	}
	for off := 0; ; off += pageSize {
		ns, err := st.ListNotes(ctx, userID, store.NoteFilter{IncludeArchived: true, Limit: pageSize, Offset: off})
		if err != nil {
			return s, err
		}
		s.Notes = append(s.Notes, ns...)
		if len(ns) < pageSize {
			break
		}
	}
	for off := 0; ; off += pageSize {
		ts, err := st.ListTasks(ctx, userID, store.TaskFilter{State: "all", Limit: pageSize, Offset: off})
		if err != nil {
			return s, err
		}
		s.Tasks = append(s.Tasks, ts...)
		if len(ts) < pageSize {
			break
		}
	}
	for off := 0; ; off += pageSize {
		es, err := st.ListTimeEntries(ctx, userID, store.TimeFilter{Limit: pageSize, Offset: off})
		if err != nil {
			return s, err
		}
		s.Time = append(s.Time, es...)
		if len(es) < pageSize {
			break
		}
	}
	if s.Events, err = st.AllEvents(ctx, userID); err != nil {
		return s, err
	}
	ps, err := st.ListProjects(ctx, userID, store.ProjectFilter{IncludeArchived: true}, now, loc)
	if err != nil {
		return s, err
	}
	for _, v := range ps {
		s.Projects = append(s.Projects, v.Project)
	}
	gs, err := st.ListGoals(ctx, userID, true, now, "")
	if err != nil {
		return s, err
	}
	for _, v := range gs {
		s.Goals = append(s.Goals, v.Goal)
	}
	if s.Objectives, err = st.ListObjectives(ctx, userID, true, now, ""); err != nil {
		return s, err
	}
	hs, err := st.ListHoldings(ctx, userID, true, now)
	if err != nil {
		return s, err
	}
	for _, v := range hs {
		h := Holding{Holding: v.Holding, Position: v.Position}
		trades, err := st.ListTrades(ctx, userID, v.Holding.ID)
		if err != nil {
			return s, err
		}
		for _, t := range trades {
			if h.LastTrade == nil || t.Time.After(*h.LastTrade) {
				tt := t.Time
				h.LastTrade = &tt
			}
		}
		s.Holdings = append(s.Holdings, h)
	}
	as, _, err := st.ListAlarms(ctx, userID, store.AlarmFilter{})
	if err != nil {
		return s, err
	}
	for _, a := range as {
		s.Alarms = append(s.Alarms, Alarm{Alarm: a, NextFires: a.NextFires(now, 5)})
	}
	return only(s, access), nil
}

// only keeps what access allows.
func only(s Snapshot, a store.AIAccess) Snapshot {
	s.Projects = keep(s.Projects, func(v store.Project) bool { return a.Allows(v.Space) })
	s.Tasks = keep(s.Tasks, func(v store.Task) bool { return a.Allows(v.Space) })
	s.Events = keep(s.Events, func(v store.Event) bool { return a.Allows(v.Space) })
	s.Notes = keep(s.Notes, func(v store.Note) bool { return a.Allows(v.Space) })
	s.Goals = keep(s.Goals, func(v store.Goal) bool { return a.Allows(v.Space) })
	s.Objectives = keep(s.Objectives, func(v store.ObjectiveView) bool { return a.Allows(v.Objective.Space) })
	s.Time = keep(s.Time, func(v store.TimeEntry) bool { return a.Allows(v.Space) })
	s.Alarms = keep(s.Alarms, func(v Alarm) bool { return a.Allows(v.Space) })
	if !a.Allows(store.SpaceLife) {
		s.Holdings = nil
	}
	return s
}

func keep[T any](xs []T, ok func(T) bool) []T {
	var out []T
	for _, x := range xs {
		if ok(x) {
			out = append(out, x)
		}
	}
	return out
}
