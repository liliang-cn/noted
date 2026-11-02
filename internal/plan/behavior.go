package plan

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

// BehaviorRow is what one week did for one active objective or goal.
type BehaviorRow struct {
	Kind        string // "objective" or "goal"
	ID, Title   string
	Target      float64 // goal: the week's share of its target; 0 for an objective
	Unit        string
	Done        float64 // check-in amount this week
	CheckIns    int
	TimeMinutes int
	TasksDone   int
	Short       bool // behind the target, or nothing at all was done
}

// Unlinked is time that went to something no objective or goal is linked to.
type Unlinked struct {
	Title   string
	Minutes int
	Count   int
}

// Behavior sets what was done in a week against what was said: the targets of
// the active objectives and goals. It is data only; nothing here asks a model.
type Behavior struct {
	Rows            []BehaviorRow
	Unlinked        []Unlinked // biggest first, at most topActivities
	UnlinkedMinutes int
	Mismatches      []string // short sentences, in the review's language
}

// minUnlinked is the least time (minutes) an unlinked place needs to be named.
const minUnlinked = 60

type owner struct {
	row      BehaviorRow
	goalIDs  map[string]bool
	projects map[string]bool
}

func (o *owner) addProject(id string) {
	if id != "" {
		o.projects[id] = true
	}
}

func (o *owner) hasTime(e store.TimeEntry) bool {
	return (e.GoalID != "" && o.goalIDs[e.GoalID]) || (e.ProjectID != "" && o.projects[e.ProjectID])
}

// weekTarget is what a goal asks for in one week, from its own period.
func weekTarget(g store.GoalView) float64 {
	switch g.Goal.Period {
	case "day":
		return g.Goal.Target * 7
	case "month":
		return g.Progress.Target // month to date: it is judged by the month's pace
	}
	return g.Goal.Target
}

func (s Suggester) behavior(ctx context.Context, userID string, from, to, asOf time.Time, space string) (Behavior, error) {
	lc := s.locale()
	var b Behavior
	objs, err := s.Store.ListObjectives(ctx, userID, false, asOf, space)
	if err != nil {
		return b, err
	}
	goals, err := s.Store.ListGoals(ctx, userID, false, asOf, space)
	if err != nil {
		return b, err
	}
	entries, err := s.Store.ListTimeEntries(ctx, userID, store.TimeFilter{From: &from, To: &to, Space: space, Limit: 100000})
	if err != nil {
		return b, err
	}
	tasks, err := s.Store.CompletedTasks(ctx, userID, from, to, space)
	if err != nil {
		return b, err
	}

	var owners []*owner
	goalOwner := map[string]*owner{}
	newGoal := func(g store.GoalView) *owner {
		o := &owner{goalIDs: map[string]bool{g.Goal.ID: true}, projects: map[string]bool{}}
		o.addProject(g.Goal.ProjectID)
		o.row = BehaviorRow{Kind: "goal", ID: g.Goal.ID, Title: g.Goal.Title, Target: weekTarget(g), Unit: g.Goal.Unit}
		goalOwner[g.Goal.ID] = o
		return o
	}
	seen := map[string]bool{}
	for _, ov := range objs {
		obj := &owner{goalIDs: map[string]bool{}, projects: map[string]bool{}}
		obj.row = BehaviorRow{Kind: "objective", ID: ov.Objective.ID, Title: ov.Objective.Title}
		obj.addProject(ov.Objective.ProjectID)
		owners = append(owners, obj)
		for _, g := range ov.Goals {
			if g.Goal.Archived {
				continue
			}
			seen[g.Goal.ID] = true
			obj.goalIDs[g.Goal.ID] = true
			obj.addProject(g.Goal.ProjectID)
			owners = append(owners, newGoal(g))
		}
	}
	for _, g := range goals {
		if !seen[g.Goal.ID] {
			owners = append(owners, newGoal(g))
		}
	}

	// Check-ins: only goals have them; an objective sums its goals.
	for _, o := range owners {
		if o.row.Kind != "goal" {
			continue
		}
		cs, err := s.Store.ListCheckIns(ctx, userID, o.row.ID, &from, &to, 10000)
		if err != nil {
			return b, err
		}
		for _, c := range cs {
			o.row.Done += c.Amount
			o.row.CheckIns++
		}
	}
	for _, o := range owners {
		if o.row.Kind != "objective" {
			continue
		}
		for id := range o.goalIDs {
			o.row.Done += goalOwner[id].row.Done
			o.row.CheckIns += goalOwner[id].row.CheckIns
		}
	}

	byTitle := map[string]*Unlinked{}
	for _, e := range entries {
		if e.End == nil {
			continue // a running timer has no minutes yet
		}
		linked := false
		for _, o := range owners {
			if o.hasTime(e) {
				o.row.TimeMinutes += e.Minutes
				linked = true
			}
		}
		if linked {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(e.Title))
		u := byTitle[key]
		if u == nil {
			u = &Unlinked{Title: e.Title}
			byTitle[key] = u
		}
		u.Minutes += e.Minutes
		u.Count++
		b.UnlinkedMinutes += e.Minutes
	}
	for _, t := range tasks {
		if t.ProjectID == "" {
			continue
		}
		for _, o := range owners {
			if o.projects[t.ProjectID] {
				o.row.TasksDone++
			}
		}
	}
	for _, u := range byTitle {
		b.Unlinked = append(b.Unlinked, *u)
	}
	sort.Slice(b.Unlinked, func(i, j int) bool {
		a, c := b.Unlinked[i], b.Unlinked[j]
		if a.Minutes != c.Minutes {
			return a.Minutes > c.Minutes
		}
		return a.Title < c.Title
	})
	b.Unlinked = b.Unlinked[:min(len(b.Unlinked), topActivities)]

	for _, o := range owners {
		r := &o.row
		switch r.Kind {
		case "goal":
			r.Short = r.Target > 0 && r.Done < r.Target
			if r.Short {
				b.Mismatches = append(b.Mismatches, lc.t("behav.goal", r.Title, num(r.Done), num(r.Target), unitOr(lc, r.Unit)))
			}
		default:
			r.Short = r.CheckIns == 0 && r.TimeMinutes == 0 && r.TasksDone == 0
			if r.Short {
				b.Mismatches = append(b.Mismatches, lc.t("behav.idle", r.Title))
			}
		}
		b.Rows = append(b.Rows, *r)
	}
	for _, u := range b.Unlinked {
		if u.Minutes >= minUnlinked {
			b.Mismatches = append(b.Mismatches, lc.t("behav.unlinked", u.Title, hours(u.Minutes)))
		}
	}
	return b, nil
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func unitOr(lc Locale, u string) string {
	if u != "" {
		return u
	}
	if lc == ZH {
		return "次"
	}
	return "times"
}

// hours writes minutes the short way: 9h, 1.5h, 40m.
func hours(m int) string {
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	return trimZero(float64(m)/60) + "h"
}

func trimZero(h float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(h, 'f', 1, 64), ".0")
}
