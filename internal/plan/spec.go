package plan

import (
	"strings"
	"time"
)

// PlanSpec is what a language model understood from "I'm going to X next month,
// I need to do A, B and C": a project and its tasks, with only the dates the
// user actually gave. Nothing here is stored yet.
type PlanSpec struct {
	Project string
	Start   *time.Time // a date the user stated; nil when they did not
	Due     *time.Time
	Tasks   []PlanTaskSpec
	Space   string
}

type PlanTaskSpec struct {
	Title           string
	Due             *time.Time // a date the user stated
	DaysBeforeStart *int       // "30 days before leaving": counted once the start date is known
	Priority        int
}

// ExtractSpec is one to-do found inside a note.
type ExtractSpec struct {
	Title string
	Due   *time.Time
}

func dateStr(t time.Time) string { return t.Format("2006-01-02") }

// BuildPlan turns a PlanSpec into a draft. A start date the user did not give
// becomes an input to fill in rather than something guessed; it is required
// only when some task is dated relative to it.
func BuildPlan(spec PlanSpec, l Locale) Draft {
	d := Draft{Kind: KindPlan, Space: spec.Space, Reason: l.t("plan.reason")}
	hasProject := strings.TrimSpace(spec.Project) != ""
	needsStart := false

	if hasProject {
		args := map[string]any{"title": spec.Project, "pinned": true}
		if spec.Space != "" {
			args["space"] = spec.Space
		}
		switch {
		case spec.Start != nil:
			args["start"] = dateStr(*spec.Start)
		default:
			for _, t := range spec.Tasks {
				if t.DaysBeforeStart != nil && t.Due == nil {
					needsStart = true
				}
			}
			args["start"] = map[string]any{"input": "start"}
		}
		if spec.Due != nil {
			args["due"] = dateStr(*spec.Due)
		}
		d.Ops = append(d.Ops, Op{Type: OpCreateProject, Label: spec.Project, Args: args})
	}

	for _, t := range spec.Tasks {
		args := map[string]any{"title": t.Title}
		if hasProject {
			args["project_id"] = "$op0"
		}
		if spec.Space != "" {
			args["space"] = spec.Space
		}
		if t.Priority > 0 {
			args["priority"] = float64(t.Priority)
		}
		label := t.Title
		switch {
		case t.Due != nil:
			args["due"] = dateStr(*t.Due)
			label += " · " + dateStr(*t.Due)
		case t.DaysBeforeStart != nil && hasProject:
			if spec.Start != nil {
				due := spec.Start.AddDate(0, 0, -*t.DaysBeforeStart)
				args["due"] = dateStr(due)
				label += " · " + dateStr(due)
			} else {
				args["due"] = map[string]any{"input": "start", "days_before": float64(*t.DaysBeforeStart), "hour": 9.0}
				label += " · " + l.t("plan.rel", *t.DaysBeforeStart)
			}
		}
		d.Ops = append(d.Ops, Op{Type: OpCreateTask, Label: label, Args: args})
	}

	if hasProject && spec.Start == nil {
		d.Inputs = append(d.Inputs, Input{Name: "start", Label: l.t("plan.start"), Type: "date", Required: needsStart})
	}
	if hasProject {
		d.Title = l.t("plan.title", spec.Project, len(spec.Tasks))
	} else {
		d.Title = l.t("plan.titleNP", len(spec.Tasks))
	}
	return d
}

// BuildExtract turns to-dos found in a note into a draft. They join the note's
// project and space.
func BuildExtract(tasks []ExtractSpec, projectID, space string, l Locale) Draft {
	d := Draft{Kind: KindExtract, Space: space, Title: l.t("extract.title", len(tasks))}
	for _, t := range tasks {
		args := map[string]any{"title": t.Title}
		if projectID != "" {
			args["project_id"] = projectID
		}
		if space != "" {
			args["space"] = space
		}
		label := l.t("extract.op", t.Title)
		if t.Due != nil {
			args["due"] = dateStr(*t.Due)
			label += " · " + dateStr(*t.Due)
		}
		d.Ops = append(d.Ops, Op{Type: OpCreateTask, Label: label, Args: args})
	}
	return d
}

// BuildAsk wraps what the assistant staged during one reply.
func BuildAsk(ops []Op, space string, l Locale) Draft {
	return Draft{Kind: KindAsk, Space: space, Title: l.t("ask.title", len(ops)), Ops: ops}
}
