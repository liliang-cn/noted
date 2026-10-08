// Package plan turns suggestions into changes safely. A proposal is a list of
// operations that nobody has applied; Apply carries out the ones the user
// selected and records how to take them back, and Undo does that.
package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/liliang-cn/noted/internal/store"
)

// Operation types.
const (
	OpCreateProject = "create_project"
	OpCreateTask    = "create_task"
	OpCreateEvent   = "create_event"
	OpUpdateTask    = "update_task"
	OpUpdateEvent   = "update_event"
	OpUpdateProject = "update_project"
	OpCheckIn       = "check_in"
	OpCreateNote    = "create_note"
	OpCompleteTask  = "complete_task"
)

// Op is one change. Args depend on Type; ids of things an earlier op in the
// same proposal creates are written "$op0", "$op1", ... and times are either
// an RFC 3339 string, a YYYY-MM-DD date, or {"input":"name","days_before":n}
// to count from a value the user supplies when accepting.
type Op struct {
	Type  string         `json:"type"`
	Label string         `json:"label"`
	Args  map[string]any `json:"args"`
}

// Input is a value the proposal cannot know and the user must give on accept.
type Input struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Type     string `json:"type"` // "date" or "text"
	Required bool   `json:"required"`
}

// Entry records one effect of Apply, enough to reverse it.
type Entry struct {
	Action string         `json:"action"` // "created" or "updated"
	Kind   string         `json:"kind"`   // project, task, event, check_in
	ID     string         `json:"id"`
	Before map[string]any `json:"before,omitempty"`
}

func EncodeOps(ops []Op) json.RawMessage {
	b, _ := json.Marshal(ops)
	return b
}
func EncodeInputs(in []Input) json.RawMessage {
	if in == nil {
		in = []Input{}
	}
	b, _ := json.Marshal(in)
	return b
}
func EncodeEntries(e []Entry) json.RawMessage {
	if e == nil {
		e = []Entry{}
	}
	b, _ := json.Marshal(e)
	return b
}
func DecodeOps(raw json.RawMessage) ([]Op, error) {
	var ops []Op
	return ops, json.Unmarshal(raw, &ops)
}
func DecodeInputs(raw json.RawMessage) ([]Input, error) {
	var in []Input
	return in, json.Unmarshal(raw, &in)
}
func DecodeEntries(raw json.RawMessage) ([]Entry, error) {
	var e []Entry
	return e, json.Unmarshal(raw, &e)
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", store.ErrInvalid, fmt.Sprintf(format, a...))
}

type Applier struct {
	Store *store.Store
	Loc   *time.Location
	Now   func() time.Time
}

func (a Applier) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a Applier) loc() *time.Location {
	if a.Loc != nil {
		return a.Loc
	}
	return time.UTC
}

// ---- argument helpers ----

type args map[string]any

func (g args) str(k string) string   { s, _ := g[k].(string); return strings.TrimSpace(s) }
func (g args) boolean(k string) bool { b, _ := g[k].(bool); return b }
func (g args) num(k string) float64  { f, _ := g[k].(float64); return f }
func (g args) strs(k string) []string {
	var out []string
	if raw, ok := g[k].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func (a Applier) parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, a.loc()); err == nil {
		return t, nil
	}
	return time.Time{}, invalid("cannot read %q as a time", s)
}

// timeArg reads a time argument: absent gives nil.
func (a Applier) timeArg(g args, key string, inputs map[string]string) (*time.Time, error) {
	v, ok := g[key]
	if !ok || v == nil {
		return nil, nil
	}
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "" {
			return nil, nil
		}
		t, err := a.parseTime(strings.TrimSpace(x))
		return &t, err
	case map[string]any:
		name, _ := x["input"].(string)
		raw := strings.TrimSpace(inputs[name])
		if raw == "" {
			return nil, invalid("%q is needed to work out %s", name, key)
		}
		base, err := a.parseTime(raw)
		if err != nil {
			return nil, err
		}
		before, _ := x["days_before"].(float64)
		after, _ := x["days_after"].(float64)
		t := base.AddDate(0, 0, int(after)-int(before))
		if h, ok := x["hour"].(float64); ok {
			l := t.In(a.loc())
			t = time.Date(l.Year(), l.Month(), l.Day(), int(h), 0, 0, 0, a.loc())
		}
		return &t, nil
	}
	return nil, invalid("%s must be a time", key)
}

// ---- apply ----

// Apply carries out the selected operations in order. selected nil means all.
// If one fails, those already done are undone and the error is returned, so a
// proposal is applied completely or not at all.
func (a Applier) Apply(ctx context.Context, userID string, ops []Op, selected map[int]bool, inputs map[string]string) ([]Entry, error) {
	var done []Entry
	created := map[int]string{}
	fail := func(i int, err error) ([]Entry, error) {
		if uerr := a.Undo(ctx, userID, done); uerr != nil {
			err = errors.Join(err, fmt.Errorf("rolling back: %w", uerr))
		}
		return nil, fmt.Errorf("step %d (%s): %w", i+1, ops[i].Label, err)
	}
	for i, op := range ops {
		if selected != nil && !selected[i] {
			continue
		}
		g := args(op.Args)
		// Resolve "$opN" references to ids created earlier in this run.
		resolved := func(key string) (string, error) {
			s := g.str(key)
			if strings.HasPrefix(s, "$op") {
				n, err := strconv.Atoi(strings.TrimPrefix(s, "$op"))
				id, ok := created[n]
				if err != nil || !ok {
					return "", invalid("%s refers to step %s, which was not applied", key, s)
				}
				return id, nil
			}
			return s, nil
		}
		entries, id, err := a.applyOne(ctx, userID, op, g, resolved, inputs)
		done = append(done, entries...)
		if err != nil {
			return fail(i, err)
		}
		if id != "" {
			created[i] = id
		}
	}
	return done, nil
}

func (a Applier) applyOne(ctx context.Context, userID string, op Op, g args, ref func(string) (string, error), inputs map[string]string) (entries []Entry, createdID string, err error) {
	st := a.Store
	switch op.Type {
	case OpCreateProject:
		start, err := a.timeArg(g, "start", inputs)
		if err != nil {
			return nil, "", err
		}
		due, err := a.timeArg(g, "due", inputs)
		if err != nil {
			return nil, "", err
		}
		p, err := st.CreateProject(ctx, store.Project{UserID: userID, Title: g.str("title"), Notes: g.str("notes"),
			Pinned: g.boolean("pinned"), Start: start, Due: due, Space: g.str("space")})
		if err != nil {
			return nil, "", err
		}
		return []Entry{{Action: "created", Kind: "project", ID: p.ID}}, p.ID, nil

	case OpCreateTask:
		due, err := a.timeArg(g, "due", inputs)
		if err != nil {
			return nil, "", err
		}
		pid, err := ref("project_id")
		if err != nil {
			return nil, "", err
		}
		t, err := st.CreateTask(ctx, store.Task{UserID: userID, Title: g.str("title"), Notes: g.str("notes"), Due: due,
			Priority: int(g.num("priority")), ProjectID: pid, Space: g.str("space"), Tags: g.strs("tags")})
		if err != nil {
			return nil, "", err
		}
		return []Entry{{Action: "created", Kind: "task", ID: t.ID}}, t.ID, nil

	case OpCreateEvent:
		start, err := a.timeArg(g, "start", inputs)
		if err != nil {
			return nil, "", err
		}
		if start == nil {
			return nil, "", invalid("start is required")
		}
		end, err := a.timeArg(g, "end", inputs)
		if err != nil {
			return nil, "", err
		}
		pid, err := ref("project_id")
		if err != nil {
			return nil, "", err
		}
		e := store.Event{UserID: userID, Title: g.str("title"), Start: *start, Location: g.str("location"),
			Description: g.str("description"), ProjectID: pid, Space: g.str("space"), TimeZone: a.loc().String(),
			AllDay: g.boolean("all_day"), RRule: g.str("rrule")}
		if end != nil {
			e.End = *end
		}
		if m, ok := g["remind_before_minutes"].(float64); ok {
			v := int(m)
			e.RemindBefore = &v
		}
		out, err := st.CreateEvent(ctx, e)
		if err != nil {
			return nil, "", err
		}
		return []Entry{{Action: "created", Kind: "event", ID: out.ID}}, out.ID, nil

	case OpUpdateTask:
		id, err := ref("id")
		if err != nil {
			return nil, "", err
		}
		old, err := st.GetTask(ctx, userID, id)
		if err != nil {
			return nil, "", err
		}
		due, err := a.timeArg(g, "due", inputs)
		if err != nil {
			return nil, "", err
		}
		entry := Entry{Action: "updated", Kind: "task", ID: id, Before: map[string]any{"due": timeJSON(old.Due)}}
		if _, err := st.UpdateTask(ctx, userID, id, store.TaskPatch{Due: &due}); err != nil {
			return nil, "", err
		}
		return []Entry{entry}, "", nil

	case OpUpdateEvent:
		id, err := ref("id")
		if err != nil {
			return nil, "", err
		}
		old, err := st.GetEvent(ctx, userID, id)
		if err != nil {
			return nil, "", err
		}
		start, err := a.timeArg(g, "start", inputs)
		if err != nil {
			return nil, "", err
		}
		end, err := a.timeArg(g, "end", inputs)
		if err != nil {
			return nil, "", err
		}
		if start == nil {
			return nil, "", invalid("start is required")
		}
		if end == nil { // keep the duration
			e := start.Add(old.End.Sub(old.Start))
			end = &e
		}
		entry := Entry{Action: "updated", Kind: "event", ID: id,
			Before: map[string]any{"start": old.Start.Format(time.RFC3339), "end": old.End.Format(time.RFC3339)}}
		if _, err := st.UpdateEvent(ctx, userID, id, store.EventPatch{Start: start, End: end}); err != nil {
			return nil, "", err
		}
		return []Entry{entry}, "", nil

	case OpUpdateProject:
		id, err := ref("id")
		if err != nil {
			return nil, "", err
		}
		old, err := st.GetProject(ctx, userID, id, a.now(), a.loc())
		if err != nil {
			return nil, "", err
		}
		patch := store.ProjectPatch{}
		for _, k := range []string{"start", "due"} {
			if _, has := g[k]; !has {
				continue
			}
			t, err := a.timeArg(g, k, inputs)
			if err != nil {
				return nil, "", err
			}
			if k == "start" {
				patch.Start = &t
			} else {
				patch.Due = &t
			}
		}
		p := old.View.Project
		entry := Entry{Action: "updated", Kind: "project", ID: id, Before: map[string]any{"start": timeJSON(p.Start), "due": timeJSON(p.Due)}}
		if _, err := st.UpdateProject(ctx, userID, id, patch, a.now(), a.loc()); err != nil {
			return nil, "", err
		}
		return []Entry{entry}, "", nil

	case OpCreateNote:
		pid, err := ref("project_id")
		if err != nil {
			return nil, "", err
		}
		n, err := st.CreateNote(ctx, store.Note{UserID: userID, Title: g.str("title"), Content: g.str("content"),
			Tags: g.strs("tags"), ProjectID: pid, Space: g.str("space")})
		if err != nil {
			return nil, "", err
		}
		return []Entry{{Action: "created", Kind: "note", ID: n.ID}}, n.ID, nil

	case OpCompleteTask:
		id, err := ref("id")
		if err != nil {
			return nil, "", err
		}
		old, err := st.GetTask(ctx, userID, id)
		if err != nil {
			return nil, "", err
		}
		yes := true
		if _, err := st.UpdateTask(ctx, userID, id, store.TaskPatch{Done: &yes}); err != nil {
			return nil, "", err
		}
		return []Entry{{Action: "updated", Kind: "task_done", ID: id, Before: map[string]any{"done": old.Done}}}, "", nil

	case OpCheckIn:
		gid, err := ref("goal_id")
		if err != nil {
			return nil, "", err
		}
		at, err := a.timeArg(g, "time", inputs)
		if err != nil {
			return nil, "", err
		}
		var when time.Time
		if at != nil {
			when = *at
		}
		c, err := st.RecordCheckIn(ctx, userID, gid, g.num("amount"), g.num("count"), when, g.str("note"), a.now())
		if err != nil {
			return nil, "", err
		}
		return []Entry{{Action: "created", Kind: "check_in", ID: c.ID}}, c.ID, nil
	}
	return nil, "", invalid("unknown operation %q", op.Type)
}

func timeJSON(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}

func beforeTime(m map[string]any, k string) *time.Time {
	s, _ := m[k].(string)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

// Undo reverses entries, newest first. Things that no longer exist are skipped:
// the user has already removed them, which is the outcome undo wants.
func (a Applier) Undo(ctx context.Context, userID string, entries []Entry) error {
	st := a.Store
	var errs []error
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		var err error
		switch {
		case e.Action == "created" && e.Kind == "task":
			err = st.DeleteTask(ctx, userID, e.ID)
		case e.Action == "created" && e.Kind == "event":
			err = st.DeleteEvent(ctx, userID, e.ID)
		case e.Action == "created" && e.Kind == "project":
			err = st.DeleteProject(ctx, userID, e.ID, false)
		case e.Action == "created" && e.Kind == "check_in":
			_, err = st.DeleteCheckIn(ctx, userID, e.ID)
		case e.Action == "created" && e.Kind == "note":
			err = st.DeleteNote(ctx, userID, e.ID)
		case e.Action == "updated" && e.Kind == "task_done":
			was, _ := e.Before["done"].(bool)
			_, err = st.UpdateTask(ctx, userID, e.ID, store.TaskPatch{Done: &was})
		case e.Action == "updated" && e.Kind == "task":
			due := beforeTime(e.Before, "due")
			_, err = st.UpdateTask(ctx, userID, e.ID, store.TaskPatch{Due: &due})
		case e.Action == "updated" && e.Kind == "event":
			s, en := beforeTime(e.Before, "start"), beforeTime(e.Before, "end")
			if s != nil && en != nil {
				_, err = st.UpdateEvent(ctx, userID, e.ID, store.EventPatch{Start: s, End: en})
			}
		case e.Action == "updated" && e.Kind == "project":
			s, d := beforeTime(e.Before, "start"), beforeTime(e.Before, "due")
			_, err = st.UpdateProject(ctx, userID, e.ID, store.ProjectPatch{Start: &s, Due: &d}, a.now(), a.loc())
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
