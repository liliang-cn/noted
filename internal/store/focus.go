package store

import (
	"context"
	"sort"
	"time"
)

// Horizons for Focus.
const (
	HorizonToday    = "today"
	HorizonUpcoming = "upcoming"
	HorizonWeek     = "week"
	HorizonMonth    = "month"
)

// upcomingDays is how far "upcoming" looks: tomorrow through this many days ahead.
const upcomingDays = 14

type FocusItem struct {
	Event        *Occurrence
	Task         *Task
	SortTime     time.Time
	Overdue      bool
	ProjectID    string
	ProjectTitle string
}

type Focus struct {
	Horizon  string
	From, To time.Time
	Items    []FocusItem
	Goals    []GoalView
	Projects []ProjectView // pinned
}

// FocusWindow returns the [from, to) a horizon covers, in loc.
func FocusWindow(horizon string, now time.Time, loc *time.Location) (time.Time, time.Time, error) {
	t := now.In(loc)
	today := dayStart(t)
	switch horizon {
	case HorizonToday, "":
		return today, today.AddDate(0, 0, 1), nil
	case HorizonUpcoming:
		return today.AddDate(0, 0, 1), today.AddDate(0, 0, 1+upcomingDays), nil
	case HorizonWeek:
		from, to := periodBounds("week", t)
		return from, to, nil
	case HorizonMonth:
		from, to := periodBounds("month", t)
		return from, to, nil
	}
	return time.Time{}, time.Time{}, invalid("unknown horizon %q", horizon)
}

// Focus gathers what deserves attention in a horizon: the window's events
// and open dated tasks (today also lists overdue ones), every active goal
// with its progress, and the pinned projects.
func (s *Store) Focus(ctx context.Context, userID, horizon, space string, loc *time.Location, now time.Time) (Focus, error) {
	if err := checkSpaceFilter(space); err != nil {
		return Focus{}, err
	}
	from, to, err := FocusWindow(horizon, now, loc)
	if err != nil {
		return Focus{}, err
	}
	if horizon == "" {
		horizon = HorizonToday
	}
	f := Focus{Horizon: horizon, From: from.UTC(), To: to.UTC()}

	projects, err := s.ListProjects(ctx, userID, ProjectFilter{Space: space}, now, loc)
	if err != nil {
		return Focus{}, err
	}
	titles := map[string]string{}
	for _, p := range projects {
		titles[p.Project.ID] = p.Project.Title
		if p.Project.Pinned {
			f.Projects = append(f.Projects, p)
		}
	}

	occ, err := s.ListEvents(ctx, userID, from, to, space)
	if err != nil {
		return Focus{}, err
	}
	for _, o := range occ {
		o := o
		f.Items = append(f.Items, FocusItem{Event: &o, SortTime: o.Start, ProjectID: o.Event.ProjectID, ProjectTitle: titles[o.Event.ProjectID]})
	}

	tasks, err := s.ListTasks(ctx, userID, TaskFilter{State: "open", DueBefore: &to, Space: space, Limit: 500})
	if err != nil {
		return Focus{}, err
	}
	for _, t := range tasks {
		t := t
		if t.Due == nil {
			continue
		}
		overdue := t.Due.Before(from)
		if overdue && horizon != HorizonToday {
			continue // only "today" nags about the past
		}
		f.Items = append(f.Items, FocusItem{Task: &t, SortTime: *t.Due, Overdue: overdue, ProjectID: t.ProjectID, ProjectTitle: titles[t.ProjectID]})
	}
	sort.SliceStable(f.Items, func(i, j int) bool {
		if f.Items[i].Overdue != f.Items[j].Overdue {
			return f.Items[i].Overdue
		}
		return f.Items[i].SortTime.Before(f.Items[j].SortTime)
	})

	f.Goals, err = s.ListGoals(ctx, userID, false, now, space)
	return f, err
}
