package plan

import (
	"context"
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

// Review is a look back at one week.
type Review struct {
	From, To   time.Time
	TasksDone  int
	TasksTotal int // finished this week plus open ones that were due this week
	Goals      []store.GoalView
	Carried    []store.Task // open and due this week: they slipped
	Next       *Draft       // suggested plan for the following week; nil if there is nothing to suggest
}

// WeeklyReview summarises the Monday-to-Sunday week containing weekOf, for the
// given space ("" for both), and drafts a plan for the week after.
func (s Suggester) WeeklyReview(ctx context.Context, userID string, weekOf time.Time, space string, now time.Time) (Review, error) {
	loc, lc := s.loc(), s.locale()
	w := weekOf.In(loc)
	ds := dayStart(w)
	from := ds.AddDate(0, 0, -((int(ds.Weekday()) + 6) % 7))
	to := from.AddDate(0, 0, 7)
	r := Review{From: from, To: to}

	done, err := s.Store.CountCompleted(ctx, userID, from, to, space)
	if err != nil {
		return r, err
	}
	r.TasksDone = done

	open, err := s.Store.ListTasks(ctx, userID, store.TaskFilter{State: "open", DueBefore: &to, Space: space, Limit: 200})
	if err != nil {
		return r, err
	}
	for _, t := range open {
		if t.Due != nil && !t.Due.Before(from) {
			r.Carried = append(r.Carried, t)
		}
	}
	r.TasksTotal = done + len(r.Carried)

	// Goals as they stood at the end of that week.
	asOf := to.Add(-time.Second)
	if now.Before(asOf) {
		asOf = now
	}
	r.Goals, err = s.Store.ListGoals(ctx, userID, false, asOf, space)
	if err != nil {
		return r, err
	}

	// Next week: carry the slipped tasks to Monday morning, and find a slot for each goal that fell short.
	next := Draft{Kind: KindWeekly, Title: lc.t("week.title"), Reason: lc.t("week.reason"), Space: space,
		Fingerprint: "weekly:" + from.Format("2006-01-02") + ":" + space}
	monday := time.Date(to.Year(), to.Month(), to.Day(), 9, 0, 0, 0, loc)
	for _, t := range r.Carried {
		next.Ops = append(next.Ops, Op{Type: OpUpdateTask, Label: lc.t("week.carry", t.Title),
			Args: map[string]any{"id": t.ID, "due": rfc(monday)}})
	}
	occ, err := s.Store.ListEvents(ctx, userID, to, to.AddDate(0, 0, 7), "")
	if err != nil {
		return r, err
	}
	blocks := busyFrom(occ)
	for _, v := range r.Goals {
		if v.Progress.Achieved {
			continue
		}
		dur := goalDuration(v.Goal, v.Progress)
		start, ok := freeSlot(blocks, loc, to, 7, dur, 9, 21)
		if !ok {
			continue
		}
		end := start.Add(dur)
		blocks = append(blocks, busy{start, end})
		next.Ops = append(next.Ops, Op{Type: OpCreateEvent, Label: lc.t("goal.op", v.Goal.Title, lc.when(start)), Args: map[string]any{
			"title": v.Goal.Title, "start": rfc(start), "end": rfc(end), "space": v.Goal.Space, "remind_before_minutes": 15}})
	}
	if len(next.Ops) > 0 {
		r.Next = &next
	}
	return r, nil
}
