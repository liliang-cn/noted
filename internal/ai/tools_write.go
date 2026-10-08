package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/liliang-cn/agent-go/v3/pkg/agent"
	"github.com/liliang-cn/noted/internal/plan"
	"github.com/liliang-cn/noted/internal/store"
)

// A run of the assistant never writes to the user's data. Its write tools
// validate their arguments and put an operation on the stage; the operations
// come back with the reply as a proposal and are carried out only if the user
// accepts. An id like "$op0" in a tool result stands for the thing operation 0
// would create, so later steps (a task in a new project) can refer to it.
type stage struct {
	mu   sync.Mutex
	ops  []plan.Op
	seen []Ref // everything the read tools returned this run, in order
}

type stageKey struct{}

func withStage(ctx context.Context) (context.Context, *stage) {
	s := &stage{}
	return context.WithValue(ctx, stageKey{}, s), s
}

func stageOf(ctx context.Context) *stage { s, _ := ctx.Value(stageKey{}).(*stage); return s }

func (s *stage) add(op plan.Op) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops = append(s.ops, op)
	return fmt.Sprintf("$op%d", len(s.ops)-1)
}

func (s *stage) list() []plan.Op {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]plan.Op(nil), s.ops...)
}

// say picks the wording for the configured language.
func (a *Agent) say(zh, en string) string {
	if a.locale == plan.EN {
		return en
	}
	return zh
}

const stagedNote = " The change is staged and only takes effect if the user confirms it."

func staged(ref, title string) map[string]any {
	return ok(map[string]any{"id": ref, "title": title, "staged": true})
}

func (a *Agent) iso(t time.Time) string { return t.In(a.loc).Format(time.RFC3339) }

func (a *Agent) registerWriteTools() {
	write := agent.ToolMetadata{InterruptBehavior: agent.InterruptBehaviorBlock}

	// withUser runs fn with the caller and the stage, turning setup problems into errors.
	run := func(fn func(ctx context.Context, u store.User, st *stage, q args) (any, error)) func(context.Context, map[string]any) (any, error) {
		return func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			st := stageOf(ctx)
			if st == nil {
				return fail(fmt.Errorf("changes cannot be made from here")), nil
			}
			return fn(ctx, u, st, args(in))
		}
	}
	timeArg := func(q args, key string) (string, error) {
		s := q.str(key)
		if s == "" {
			return "", nil
		}
		t, err := parseTime(s, a.loc)
		if err != nil {
			return "", err
		}
		return a.iso(t), nil
	}
	put := func(m map[string]any, k string, v any) {
		switch x := v.(type) {
		case string:
			if x != "" {
				m[k] = x
			}
		case []string:
			if len(x) > 0 {
				m[k] = x
			}
		default:
			m[k] = v
		}
	}

	a.svc.AddToolWithMetadata("create_note", "Save a new note."+stagedNote,
		schema(map[string]any{"title": str("Short title"), "content": str("Markdown body"), "tags": strList("Topic tags"),
			"space": str(spaceDesc), "project_id": str(projectDesc)}, "content"),
		run(func(ctx context.Context, u store.User, st *stage, q args) (any, error) {
			n := store.Note{Title: q.str("title"), Content: q.str("content"), Tags: q.strs("tags")}
			if err := n.Validate(); err != nil {
				return fail(err), nil
			}
			m := map[string]any{}
			put(m, "title", n.Title)
			put(m, "content", n.Content)
			put(m, "tags", q.strs("tags"))
			put(m, "space", spaceArg(ctx, q))
			put(m, "project_id", q.str("project_id"))
			title := n.Title
			if title == "" {
				title = "…"
			}
			return staged(st.add(plan.Op{Type: plan.OpCreateNote, Label: a.say("新建笔记:", "New note: ") + title, Args: m}), title), nil
		}), write)

	a.svc.AddToolWithMetadata("create_event", "Create a calendar event."+stagedNote,
		schema(map[string]any{
			"title":                 str("Event title"),
			"start":                 str("Start, RFC 3339 (from resolve_datetime)"),
			"end":                   str("End, RFC 3339; default one hour after start"),
			"location":              str("Where"),
			"description":           str("Details"),
			"all_day":               flag("All-day event"),
			"rrule":                 str("Repeat rule, e.g. FREQ=WEEKLY;BYDAY=MO,WE or FREQ=DAILY;COUNT=5"),
			"remind_before_minutes": num("Remind this many minutes before the start"),
			"space":                 str(spaceDesc),
			"project_id":            str(projectDesc),
		}, "title", "start"),
		run(func(ctx context.Context, u store.User, st *stage, q args) (any, error) {
			start, err := parseTime(q.str("start"), a.loc)
			if err != nil {
				return fail(err), nil
			}
			e := store.Event{Title: q.str("title"), Start: start, AllDay: q.boolean("all_day"), RRule: q.str("rrule"), TimeZone: a.loc.String()}
			m := map[string]any{"start": a.iso(start)}
			if s, err := timeArg(q, "end"); err != nil {
				return fail(err), nil
			} else if s != "" {
				m["end"] = s
				e.End, _ = time.Parse(time.RFC3339, s)
			}
			if mins, has := q.integer("remind_before_minutes"); has {
				e.RemindBefore = &mins
				m["remind_before_minutes"] = float64(mins)
			}
			if err := e.Validate(); err != nil {
				return fail(err), nil
			}
			put(m, "title", e.Title)
			put(m, "location", q.str("location"))
			put(m, "description", q.str("description"))
			put(m, "rrule", e.RRule)
			if e.AllDay {
				m["all_day"] = true
			}
			put(m, "space", spaceArg(ctx, q))
			put(m, "project_id", q.str("project_id"))
			label := a.say("新建日程:", "New event: ") + e.Title + " " + start.In(a.loc).Format("01-02 15:04")
			return staged(st.add(plan.Op{Type: plan.OpCreateEvent, Label: label, Args: m}), e.Title), nil
		}), write)

	a.svc.AddToolWithMetadata("reschedule_event", "Move an existing event to a new time. Moving the start keeps the duration unless end is also given."+stagedNote,
		schema(map[string]any{"id": str("Event id (from list_events)"), "start": str("New start, RFC 3339"), "end": str("New end, RFC 3339")}, "id", "start"),
		run(func(ctx context.Context, u store.User, st *stage, q args) (any, error) {
			ev, err := a.store.GetEvent(ctx, u.ID, q.str("id"))
			if err == nil && !a.visible(ctx, ev.Space) {
				err = store.ErrNotFound
			}
			if err != nil {
				return fail(err), nil
			}
			start, err := parseTime(q.str("start"), a.loc)
			if err != nil {
				return fail(err), nil
			}
			m := map[string]any{"id": ev.ID, "start": a.iso(start)}
			if s, err := timeArg(q, "end"); err != nil {
				return fail(err), nil
			} else if s != "" {
				m["end"] = s
			}
			label := a.say("改期:", "Reschedule: ") + ev.Title + " → " + start.In(a.loc).Format("01-02 15:04")
			return staged(st.add(plan.Op{Type: plan.OpUpdateEvent, Label: label, Args: m}), ev.Title), nil
		}), write)

	a.svc.AddToolWithMetadata("create_task", "Create a to-do item, optionally with a due time and a reminder."+stagedNote,
		schema(map[string]any{
			"title":      str("What to do"),
			"due":        str("Due time, RFC 3339. If the user gave only a date, use 09:00 on that date."),
			"priority":   num("1 low, 2 medium, 3 high"),
			"tags":       strList("Tags"),
			"space":      str(spaceDesc),
			"project_id": str(projectDesc),
		}, "title"),
		run(func(ctx context.Context, u store.User, st *stage, q args) (any, error) {
			t := store.Task{Title: q.str("title"), Tags: q.strs("tags")}
			m := map[string]any{}
			if s, err := timeArg(q, "due"); err != nil {
				return fail(err), nil
			} else if s != "" {
				m["due"] = s
			}
			if p, has := q.integer("priority"); has {
				t.Priority = p
				m["priority"] = float64(p)
			}
			if err := t.Validate(); err != nil {
				return fail(err), nil
			}
			put(m, "title", t.Title)
			put(m, "tags", t.Tags)
			put(m, "space", spaceArg(ctx, q))
			put(m, "project_id", q.str("project_id"))
			label := a.say("新建待办:", "New task: ") + t.Title
			if d, ok := m["due"].(string); ok {
				label += " · " + d[:10]
			}
			return staged(st.add(plan.Op{Type: plan.OpCreateTask, Label: label, Args: m}), t.Title), nil
		}), write)

	a.svc.AddToolWithMetadata("complete_task", "Mark a to-do item as done."+stagedNote,
		schema(map[string]any{"id": str("Task id (from list_tasks)")}, "id"),
		run(func(ctx context.Context, u store.User, st *stage, q args) (any, error) {
			t, err := a.store.GetTask(ctx, u.ID, q.str("id"))
			if err == nil && !a.visible(ctx, t.Space) {
				err = store.ErrNotFound
			}
			if err != nil {
				return fail(err), nil
			}
			label := a.say("完成待办:", "Complete task: ") + t.Title
			return staged(st.add(plan.Op{Type: plan.OpCompleteTask, Label: label, Args: map[string]any{"id": t.ID}}), t.Title), nil
		}), write)

	a.svc.AddToolWithMetadata("create_project",
		"Create a project. Afterwards add its tasks, events and notes by passing the returned id as project_id to the create_* tools. Do not invent dates the user did not give."+stagedNote,
		schema(map[string]any{
			"title":  str("Project name"),
			"start":  str("Start or departure date, RFC 3339. If the user gave only a date, use that date at 00:00 in their zone; never add a time of day they did not say."),
			"due":    str("Deadline or end date, RFC 3339. If the user gave only a date, use that date at 00:00 in their zone; never add a time of day they did not say."),
			"pinned": flag("Pin it to the overview"),
			"space":  str(spaceDesc),
		}, "title"),
		run(func(ctx context.Context, u store.User, st *stage, q args) (any, error) {
			p := store.Project{Title: q.str("title")}
			m := map[string]any{"pinned": q.boolean("pinned")}
			for _, k := range []string{"start", "due"} {
				s, err := timeArg(q, k)
				if err != nil {
					return fail(err), nil
				}
				if s != "" {
					m[k] = s
					t, _ := time.Parse(time.RFC3339, s)
					if k == "start" {
						p.Start = &t
					} else {
						p.Due = &t
					}
				}
			}
			if err := p.Validate(); err != nil {
				return fail(err), nil
			}
			put(m, "title", p.Title)
			put(m, "space", spaceArg(ctx, q))
			return staged(st.add(plan.Op{Type: plan.OpCreateProject, Label: a.say("新建项目:", "New project: ") + p.Title, Args: m}), p.Title), nil
		}), write)

	a.svc.AddToolWithMetadata("check_in", "Record progress on a goal, for example one workout or 20 minutes of study."+stagedNote,
		schema(map[string]any{
			"goal_id": str("Goal id (from list_goals)"),
			"amount":  map[string]any{"type": "number", "description": "How much, in the goal's unit; default 1"},
			"count":   map[string]any{"type": "number", "description": "Added to the goal's running total, such as one finished lesson; only for goals that have one"},
			"note":    str("Optional note"),
		}, "goal_id"),
		run(func(ctx context.Context, u store.User, st *stage, q args) (any, error) {
			g, err := a.store.GetGoal(ctx, u.ID, q.str("goal_id"))
			if err == nil && !a.visible(ctx, g.Space) {
				err = store.ErrNotFound
			}
			if err != nil {
				return fail(err), nil
			}
			amount, _ := q["amount"].(float64)
			count, _ := q["count"].(float64)
			if amount < 0 || count < 0 {
				return fail(fmt.Errorf("%w: amount and count must not be negative", store.ErrInvalid)), nil
			}
			if count > 0 && g.CounterTarget == 0 {
				return fail(fmt.Errorf("%w: this goal has no counter", store.ErrInvalid)), nil
			}
			if amount == 0 && count == 0 {
				amount = 1
			}
			m := map[string]any{"goal_id": g.ID}
			if amount > 0 {
				m["amount"] = amount
			}
			if count > 0 {
				m["count"] = count
			}
			put(m, "note", q.str("note"))
			label := a.say("打卡:", "Check in: ") + g.Title
			if amount > 0 {
				label += fmt.Sprintf(" %g%s", amount, strings.TrimSpace(g.Unit))
			}
			if count > 0 {
				label += fmt.Sprintf(" +%g%s", count, g.CounterUnit)
			}
			return staged(st.add(plan.Op{Type: plan.OpCheckIn, Label: label, Args: m}), g.Title), nil
		}), write)
}

// remember notes that a read tool showed the model something.
func remember(ctx context.Context, r Ref) {
	if st := stageOf(ctx); st != nil {
		st.mu.Lock()
		st.seen = append(st.seen, r)
		st.mu.Unlock()
	}
}

func (s *stage) refs() []Ref {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Ref(nil), s.seen...)
}

const maxRefs = 12

// pickRefs chooses which of the things the assistant read to show under its
// answer: those the answer names. When it names none (it summarised), a few of
// the most recently read are shown instead, so the answer is never left bare.
func pickRefs(seen []Ref, reply string) []Ref {
	lower := strings.ToLower(reply)
	have := map[string]bool{}
	var out, all []Ref
	for _, r := range seen {
		key := r.Kind + ":" + r.ID
		if have[key] {
			continue
		}
		have[key] = true
		all = append(all, r)
		if t := strings.ToLower(strings.TrimSpace(r.Title)); t != "" && strings.Contains(lower, t) {
			out = append(out, r)
		}
	}
	if len(out) == 0 && len(all) > 0 {
		out = all[max(0, len(all)-5):]
	}
	if len(out) > maxRefs {
		out = out[:maxRefs]
	}
	return out
}
