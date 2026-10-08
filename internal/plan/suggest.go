package plan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

// Proposal kinds.
const (
	KindGoalSlot     = "goal_slot"
	KindProjectDates = "project_dates"
	KindConflict     = "conflict"
	KindOverdue      = "overdue"
	KindSchedule     = "schedule"
	KindWeekly       = "weekly"
	KindPlan         = "plan"
	KindExtract      = "extract"
	KindAsk          = "ask"
)

// Sources of a proposal.
const (
	SourceRules = "rules"
	SourceAI    = "ai"
)

// Draft is a proposal before it is stored.
type Draft struct {
	Kind        string
	Title       string
	Reason      string
	Space       string
	Fingerprint string
	Ops         []Op
	Inputs      []Input
}

// ErrNoSlot means no stretch of free time was found.
var ErrNoSlot = errors.New("no free time found")

// Suggester computes suggestions from the user's own data with fixed rules.
// It needs no language model, so it works whether or not AI is enabled.
type Suggester struct {
	Store  *store.Store
	Loc    *time.Location
	Locale Locale
}

func (s Suggester) loc() *time.Location {
	if s.Loc != nil {
		return s.Loc
	}
	return time.UTC
}

func (s Suggester) locale() Locale {
	if s.Locale == "" {
		return ZH
	}
	return s.Locale
}

func shortHash(parts []string) string {
	sort.Strings(parts)
	h := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return hex.EncodeToString(h[:6])
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func rfc(t time.Time) string { return t.Format(time.RFC3339) }

// Rules returns what is worth suggesting right now.
func (s Suggester) Rules(ctx context.Context, userID string, now time.Time) ([]Draft, error) {
	loc, lc := s.loc(), s.locale()
	now = now.In(loc)
	occ, err := s.Store.ListEvents(ctx, userID, now.Add(-24*time.Hour), now.AddDate(0, 0, 8), "")
	if err != nil {
		return nil, err
	}
	var out []Draft

	g, err := s.goalSlots(ctx, userID, now, occ)
	if err != nil {
		return nil, err
	}
	out = append(out, g...)

	p, err := s.projectDates(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	out = append(out, p...)

	out = append(out, s.conflicts(now, occ)...)

	o, err := s.overdue(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	out = append(out, o...)
	_ = lc
	return out, nil
}

func goalDuration(g store.Goal, p store.GoalProgress) time.Duration {
	switch strings.ToLower(g.Unit) {
	case "分钟", "minutes", "minute", "mins", "min":
		m := p.Remaining
		if m < 20 {
			m = 20
		}
		if m > 90 {
			m = 90
		}
		return time.Duration(int(m/5)*5) * time.Minute
	}
	return time.Hour
}

func (l Locale) unit(u string) string {
	if l == ZH && (u == "times" || u == "") {
		return "次"
	}
	return u
}

func (s Suggester) goalSlots(ctx context.Context, userID string, now time.Time, occ []store.Occurrence) ([]Draft, error) {
	goals, err := s.Store.ListGoals(ctx, userID, false, now, "")
	if err != nil {
		return nil, err
	}
	loc, lc := s.loc(), s.locale()
	blocks := busyFrom(occ)
	var out []Draft
	for _, v := range goals {
		g, p := v.Goal, v.Progress
		if !p.Behind || p.Achieved {
			continue
		}
		dur := goalDuration(g, p)
		start, ok := freeSlot(blocks, loc, now, 3, dur, 9, 21)
		if !ok {
			continue
		}
		end := start.Add(dur)
		blocks = append(blocks, busy{start, end}) // do not offer the same hour to two goals
		remaining := fmt.Sprintf("%g%s", p.Remaining, lc.unit(g.Unit))
		out = append(out, Draft{
			Kind:        KindGoalSlot,
			Title:       lc.t("goal.title", g.Title, remaining, lc.span(start, end)),
			Reason:      lc.t("goal.reason"),
			Space:       g.Space,
			Fingerprint: "goal_slot:" + g.ID + ":" + p.PeriodStart.In(loc).Format("2006-01-02"),
			Ops: []Op{{Type: OpCreateEvent, Label: lc.t("goal.op", g.Title, lc.when(start)), Args: map[string]any{
				"title": g.Title, "start": rfc(start), "end": rfc(end), "space": g.Space, "remind_before_minutes": 15}}},
		})
	}
	return out, nil
}

func (s Suggester) projectDates(ctx context.Context, userID string, now time.Time) ([]Draft, error) {
	loc, lc := s.loc(), s.locale()
	projects, err := s.Store.ListProjects(ctx, userID, store.ProjectFilter{}, now, loc)
	if err != nil {
		return nil, err
	}
	var out []Draft
	for _, v := range projects {
		if v.Progress.DaysLeft == nil || *v.Progress.DaysLeft < 3 {
			continue
		}
		tasks, err := s.Store.ListTasks(ctx, userID, store.TaskFilter{State: "open", ProjectID: v.Project.ID, Limit: 100})
		if err != nil {
			return nil, err
		}
		var undated []store.Task
		for _, t := range tasks {
			if t.Due == nil {
				undated = append(undated, t)
			}
		}
		if len(undated) == 0 {
			continue
		}
		anchor := v.Project.Start
		if anchor == nil {
			anchor = v.Project.Due
		}
		target := time.Date(anchor.In(loc).Year(), anchor.In(loc).Month(), anchor.In(loc).Day()-14, 9, 0, 0, 0, loc)
		if earliest := dayStart(now).AddDate(0, 0, 1).Add(9 * time.Hour); target.Before(earliest) {
			target = earliest
		}
		var ops []Op
		var ids []string
		for _, t := range undated {
			ids = append(ids, t.ID)
			ops = append(ops, Op{Type: OpUpdateTask, Label: lc.t("dates.op", t.Title, lc.when(target)),
				Args: map[string]any{"id": t.ID, "due": rfc(target)}})
		}
		out = append(out, Draft{
			Kind:        KindProjectDates,
			Title:       lc.t("dates.title", v.Project.Title, len(undated)),
			Reason:      lc.t("dates.reason", *v.Progress.DaysLeft),
			Space:       v.Project.Space,
			Fingerprint: "project_dates:" + v.Project.ID + ":" + shortHash(ids),
			Ops:         ops,
		})
	}
	return out, nil
}

func (s Suggester) conflicts(now time.Time, occ []store.Occurrence) []Draft {
	loc, lc := s.loc(), s.locale()
	var timed []store.Occurrence
	for _, o := range occ {
		if !o.Event.AllDay && o.End.After(now) {
			timed = append(timed, o)
		}
	}
	sort.SliceStable(timed, func(i, j int) bool { return timed[i].Start.Before(timed[j].Start) })

	var out []Draft
	for i := range timed {
		for j := i + 1; j < len(timed); j++ {
			a, b := timed[i], timed[j]
			if !b.Start.Before(a.End) {
				break
			}
			if a.Event.ID == b.Event.ID {
				continue
			}
			// Move one that is not part of a repeating series, preferring the later one.
			mover, other := b, a
			if mover.Event.RRule != "" {
				mover, other = a, b
			}
			if mover.Event.RRule != "" {
				continue
			}
			var rest []store.Occurrence
			for _, o := range timed {
				if o.Event.ID != mover.Event.ID {
					rest = append(rest, o)
				}
			}
			dur := mover.End.Sub(mover.Start)
			slot, ok := freeSlot(busyFrom(rest), loc, maxTime(now, mover.Start), 4, dur, 9, 21)
			if !ok || slot.Equal(mover.Start) {
				continue
			}
			out = append(out, Draft{
				Kind:        KindConflict,
				Title:       lc.t("conflict.title", a.Event.Title, b.Event.Title),
				Space:       mover.Event.Space,
				Fingerprint: "conflict:" + a.Event.ID + ":" + b.Event.ID + ":" + a.Start.In(loc).Format("2006-01-02"),
				Ops: []Op{{Type: OpUpdateEvent, Label: lc.t("conflict.op", mover.Event.Title, lc.when(slot)),
					Args: map[string]any{"id": mover.Event.ID, "start": rfc(slot)}}},
			})
			_ = other
			if len(out) >= 5 {
				return out
			}
		}
	}
	return out
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (s Suggester) overdue(ctx context.Context, userID string, now time.Time) ([]Draft, error) {
	loc, lc := s.loc(), s.locale()
	today := dayStart(now)
	tasks, err := s.Store.ListTasks(ctx, userID, store.TaskFilter{State: "open", DueBefore: &today, Limit: 20})
	if err != nil {
		return nil, err
	}
	var out []Draft
	for _, t := range tasks {
		due := t.Due.In(loc)
		days := int(today.Sub(dayStart(due)).Hours()/24 + 0.5)
		hour, min := due.Hour(), due.Minute()
		if hour == 0 && min == 0 {
			hour = 9
		}
		target := time.Date(today.Year(), today.Month(), today.Day()+1, hour, min, 0, 0, loc)
		out = append(out, Draft{
			Kind:        KindOverdue,
			Title:       lc.t("overdue.title", t.Title, days),
			Space:       t.Space,
			Fingerprint: "overdue:" + t.ID + ":" + due.Format("2006-01-02"),
			Ops: []Op{{Type: OpUpdateTask, Label: lc.t("overdue.op", lc.when(target)),
				Args: map[string]any{"id": t.ID, "due": rfc(target)}}},
		})
	}
	return out, nil
}

// Schedule places tasks into free stretches of the calendar, one hour each by
// default, starting no earlier than from. The result is a proposal of events.
func (s Suggester) Schedule(ctx context.Context, userID string, taskIDs []string, dur time.Duration, days int, from time.Time) (Draft, error) {
	loc, lc := s.loc(), s.locale()
	if dur <= 0 {
		dur = time.Hour
	}
	if days <= 0 || days > 14 {
		days = 5
	}
	if len(taskIDs) == 0 {
		return Draft{}, fmt.Errorf("%w: choose at least one task", store.ErrInvalid)
	}
	from = from.In(loc)
	occ, err := s.Store.ListEvents(ctx, userID, from.Add(-24*time.Hour), from.AddDate(0, 0, days+1), "")
	if err != nil {
		return Draft{}, err
	}
	blocks := busyFrom(occ)
	d := Draft{Kind: KindSchedule, Space: ""}
	for _, id := range taskIDs {
		t, err := s.Store.GetTask(ctx, userID, id)
		if err != nil {
			return Draft{}, err
		}
		if t.Done {
			continue
		}
		start, ok := freeSlot(blocks, loc, from, days, dur, 9, 21)
		if !ok {
			continue
		}
		end := start.Add(dur)
		blocks = append(blocks, busy{start, end})
		args := map[string]any{"title": t.Title, "start": rfc(start), "end": rfc(end), "remind_before_minutes": 10, "space": t.Space}
		if t.ProjectID != "" {
			args["project_id"] = t.ProjectID
		}
		d.Ops = append(d.Ops, Op{Type: OpCreateEvent, Label: lc.t("sched.op", t.Title, lc.span(start, end)), Args: args})
		d.Space = t.Space
	}
	if len(d.Ops) == 0 {
		return Draft{}, ErrNoSlot
	}
	d.Title = lc.t("sched.title", len(d.Ops))
	return d, nil
}
