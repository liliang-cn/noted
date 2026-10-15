package ai

import (
	"context"
	"time"

	"github.com/liliang-cn/agent-go/v3/pkg/agent"
	"github.com/liliang-cn/noted/internal/store"
)

func (a *Agent) projectView(v store.ProjectView) map[string]any {
	out := map[string]any{
		"id": v.Project.ID, "title": v.Project.Title, "space": v.Project.Space, "pinned": v.Project.Pinned,
		"tasks_done": v.Progress.TasksDone, "tasks_total": v.Progress.TasksTotal, "tasks_overdue": v.Progress.TasksOverdue,
	}
	if v.Project.Start != nil {
		out["start"] = a.fmtTime(*v.Project.Start)
	}
	if v.Project.Due != nil {
		out["due"] = a.fmtTime(*v.Project.Due)
	}
	if v.Progress.DaysLeft != nil {
		out["days_left"] = *v.Progress.DaysLeft
	}
	if v.Progress.Next != nil {
		out["next"] = map[string]any{"title": v.Progress.NextTitle, "time": a.fmtTime(*v.Progress.Next)}
	}
	return out
}

func (a *Agent) goalView(v store.GoalView) map[string]any {
	p := v.Progress
	out := map[string]any{
		"id": v.Goal.ID, "title": v.Goal.Title, "space": v.Goal.Space, "period": v.Goal.Period, "unit": v.Goal.Unit,
		"target": v.Goal.Target, "done": p.Done, "remaining": p.Remaining, "achieved": p.Achieved,
		"behind": p.Behind, "streak": p.Streak,
	}
	if v.Goal.CounterTarget > 0 {
		out["counter"] = map[string]any{"unit": v.Goal.CounterUnit, "done": p.CounterDone, "target": v.Goal.CounterTarget}
	}
	return out
}

// registerPlanningTools gives the assistant projects and goals.
func (a *Agent) registerPlanningTools() {
	read := agent.ToolMetadata{ReadOnly: true, ConcurrencySafe: true, InterruptBehavior: agent.InterruptBehaviorCancel}

	a.svc.AddToolWithMetadata("list_projects",
		"List the user's projects (finite efforts such as a trip) with progress, days left and the next step. Pinned first.",
		schema(map[string]any{"pinned_only": flag("Only pinned projects")}),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			vs, err := a.store.ListProjects(ctx, u.ID, store.ProjectFilter{PinnedOnly: args(in).boolean("pinned_only"), Space: ctxSpace(ctx)}, time.Now(), a.loc)
			if err != nil {
				return fail(err), nil
			}
			out := make([]map[string]any, 0, len(vs))
			for _, v := range vs {
				remember(ctx, Ref{Kind: "project", ID: v.Project.ID, Title: v.Project.Title})
				out = append(out, a.projectView(v))
			}
			return ok(out), nil
		}, read)

	a.svc.AddToolWithMetadata("list_goals",
		"List recurring goals (for example 3 workouts a week) with this period's progress, whether the user is behind pace, and the streak.",
		schema(map[string]any{}),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			vs, err := a.store.ListGoals(ctx, u.ID, false, time.Now(), ctxSpace(ctx))
			if err != nil {
				return fail(err), nil
			}
			out := make([]map[string]any, 0, len(vs))
			for _, v := range vs {
				remember(ctx, Ref{Kind: "goal", ID: v.Goal.ID, Title: v.Goal.Title})
				out = append(out, a.goalView(v))
			}
			return ok(out), nil
		}, read)

	a.svc.AddToolWithMetadata("time_summary",
		"How much time the user logged (时间记录), totalled by activity (title), project, goal or day, for entries that started in [from, to).",
		schema(map[string]any{
			"from":     str("Window start, RFC 3339; default 7 days before to"),
			"to":       str("Window end, RFC 3339; default now"),
			"group_by": str("title (default), project, goal or day"),
		}),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			q := args(in)
			to := time.Now()
			if q.str("to") != "" {
				if to, err = parseTime(q.str("to"), a.loc); err != nil {
					return fail(err), nil
				}
			}
			from := to.AddDate(0, 0, -7)
			if q.str("from") != "" {
				if from, err = parseTime(q.str("from"), a.loc); err != nil {
					return fail(err), nil
				}
			}
			sum, err := a.store.SummarizeTime(ctx, u.ID, store.TimeFilter{From: &from, To: &to, Space: ctxSpace(ctx)}, q.str("group_by"), a.loc)
			if err != nil {
				return fail(err), nil
			}
			rows := make([]map[string]any, 0, len(sum.Rows))
			for _, r := range sum.Rows {
				rows = append(rows, map[string]any{"key": r.Key, "label": r.Label, "minutes": r.Minutes, "count": r.Count})
			}
			return ok(map[string]any{"from": a.fmtTime(from), "to": a.fmtTime(to), "total_minutes": sum.Minutes, "entries": sum.Count, "rows": rows}), nil
		}, read)

}
