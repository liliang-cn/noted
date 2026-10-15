package plan

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/liliang-cn/noted/internal/store"
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

// dueAt is a task due on a date the user gave without a time of day: 09:00 that
// day, the same hour the assistant and the dates counted from a start use. A
// bare date would be midnight, and the task overdue the whole day it is due.
func dueAt(t time.Time) string {
	return time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, t.Location()).Format(time.RFC3339)
}

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
			start := map[string]any{"input": "start"}
			if !needsStart {
				start["optional"] = true // left empty, the project simply has no start
			}
			args["start"] = start
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
			args["due"] = dueAt(*t.Due)
			label += " · " + dateStr(*t.Due)
		case t.DaysBeforeStart != nil && hasProject:
			if spec.Start != nil {
				due := spec.Start.AddDate(0, 0, -*t.DaysBeforeStart)
				args["due"] = dueAt(due)
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
			args["due"] = dueAt(*t.Due)
			label += " · " + dateStr(*t.Due)
		}
		d.Ops = append(d.Ops, Op{Type: OpCreateTask, Label: label, Args: args})
	}
	return d
}

// BuildAsk wraps what the assistant staged during one reply. The draft belongs
// to the space its steps land in: one space when they all share it, both ("")
// when they span work and life. space, the mode it was asked from, is used
// only when no step says.
func BuildAsk(ops []Op, space string, l Locale) Draft {
	return Draft{Kind: KindAsk, Space: OpsSpace(ops, nil, space), Title: l.t("ask.title", len(ops)), Ops: ops}
}

// SpaceLookup reports the space of an existing project, task, event or goal
// (kind "project", "task", "event", "goal"), or "" when it is not known.
type SpaceLookup func(kind, id string) string

// StoreLookup answers a SpaceLookup from the user's data. A failed read counts
// as not known.
func StoreLookup(ctx context.Context, st *store.Store, userID string) SpaceLookup {
	return func(kind, id string) string {
		sp, err := st.SpaceOf(ctx, userID, kind, id)
		if err != nil {
			return ""
		}
		return sp
	}
}

// OpsSpace is the space a list of operations touches: the one they all share,
// or "" (both) when they span work and life. A step that names no space lands
// where the store would put it: in its project's space, else life; a change to
// something that exists is in that thing's space (asked of lookup, which may
// be nil). fallback is returned when no step's space can be told.
func OpsSpace(ops []Op, lookup SpaceLookup, fallback string) string {
	look := func(kind, id string) string {
		if lookup == nil || id == "" {
			return ""
		}
		return lookup(kind, id)
	}
	spaces := make([]string, len(ops))
	// ref is the space of the thing an id argument names: an earlier step's or an existing one's.
	ref := func(i int, kind, key string) string {
		id, _ := ops[i].Args[key].(string)
		if n, err := strconv.Atoi(strings.TrimPrefix(id, "$op")); err == nil && strings.HasPrefix(id, "$op") {
			if n >= 0 && n < i {
				return spaces[n]
			}
			return ""
		}
		return look(kind, id)
	}
	seen := map[string]bool{}
	for i, op := range ops {
		s, _ := op.Args["space"].(string)
		if s != store.SpaceWork && s != store.SpaceLife {
			s = ""
			switch op.Type {
			case OpCreateProject:
				s = store.SpaceLife
			case OpCreateTask, OpCreateEvent, OpCreateNote:
				if pid, _ := op.Args["project_id"].(string); pid == "" {
					s = store.SpaceLife
				} else {
					s = ref(i, "project", "project_id")
				}
			case OpUpdateTask, OpCompleteTask:
				s = ref(i, "task", "id")
			case OpUpdateEvent:
				s = ref(i, "event", "id")
			case OpUpdateProject:
				s = ref(i, "project", "id")
			case OpCheckIn:
				s = ref(i, "goal", "goal_id")
			case OpLogTime:
				switch pid, _ := op.Args["project_id"].(string); {
				case pid != "":
					s = ref(i, "project", "project_id")
				case op.Args["goal_id"] != nil && op.Args["goal_id"] != "":
					s = ref(i, "goal", "goal_id")
				default:
					s = store.SpaceLife
				}
			}
		}
		spaces[i] = s
		if s != "" {
			seen[s] = true
		}
	}
	switch {
	case seen[store.SpaceWork] && seen[store.SpaceLife]:
		return ""
	case seen[store.SpaceWork]:
		return store.SpaceWork
	case seen[store.SpaceLife]:
		return store.SpaceLife
	}
	return fallback
}
