package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/liliang-cn/agent-go/v3/pkg/agent"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/store"
)

func withUser(ctx context.Context, u store.User) context.Context { return auth.WithUser(ctx, u) }

// callerOf returns the user the current assistant run acts for. Tools never
// take a user id from the model: identity comes only from the request.
func callerOf(ctx context.Context) (store.User, error) {
	u, ok := auth.User(ctx)
	if !ok {
		return store.User{}, errors.New("no user bound to this run")
	}
	return u, nil
}

type args map[string]any

func (a args) str(k string) string {
	s, _ := a[k].(string)
	return strings.TrimSpace(s)
}

func (a args) has(k string) bool { _, ok := a[k]; return ok }

func (a args) integer(k string) (int, bool) {
	switch v := a[k].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

func (a args) boolean(k string) bool { b, _ := a[k].(bool); return b }

func (a args) strs(k string) []string {
	var out []string
	if raw, ok := a[k].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// parseTime accepts RFC 3339, or a zone-less local time that is read in loc.
func parseTime(s string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot read %q as a time; use RFC 3339 such as 2026-03-05T15:00:00+08:00", s)
}

func ok(data any) map[string]any { return map[string]any{"ok": true, "data": data} }

// fail reports a problem to the model as data so it can correct itself and
// retry, instead of aborting the run.
func fail(err error) map[string]any {
	msg := err.Error()
	if errors.Is(err, store.ErrNotFound) {
		msg = "not found"
	}
	return map[string]any{"ok": false, "error": strings.TrimPrefix(msg, store.ErrInvalid.Error()+": ")}
}

func (a *Agent) fmtTime(t time.Time) string { return t.In(a.loc).Format(time.RFC3339) }

func (a *Agent) eventView(e store.Event, start, end time.Time) map[string]any {
	v := map[string]any{"id": e.ID, "title": e.Title, "start": a.fmtTime(start), "end": a.fmtTime(end)}
	if e.Location != "" {
		v["location"] = e.Location
	}
	if e.AllDay {
		v["all_day"] = true
	}
	if e.RRule != "" {
		v["rrule"] = e.RRule
	}
	return v
}

func (a *Agent) taskView(t store.Task) map[string]any {
	v := map[string]any{"id": t.ID, "title": t.Title, "done": t.Done, "priority": t.Priority}
	if t.Due != nil {
		v["due"] = a.fmtTime(*t.Due)
	}
	if len(t.Tags) > 0 {
		v["tags"] = t.Tags
	}
	return v
}

func (a *Agent) noteView(n store.Note, snippet string) map[string]any {
	v := map[string]any{"id": n.ID, "title": n.Title, "tags": n.Tags, "updated": a.fmtTime(n.Updated)}
	if snippet != "" {
		v["snippet"] = snippet
	}
	return v
}

func schema(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any  { return map[string]any{"type": "integer", "description": desc} }
func flag(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
func strList(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func (a *Agent) registerTools() {
	read := agent.ToolMetadata{ReadOnly: true, ConcurrencySafe: true, InterruptBehavior: agent.InterruptBehaviorCancel}
	write := agent.ToolMetadata{InterruptBehavior: agent.InterruptBehaviorBlock}

	agent.RegisterDateTimeTool(a.svc)

	a.svc.AddToolWithMetadata("search_notes",
		"Search the user's notes by keywords or meaning. Returns titles and snippets; use get_note for the full text.",
		schema(map[string]any{"query": str("What to look for"), "limit": num("Max results, default 8")}, "query"),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			q := args(in)
			limit, has := q.integer("limit")
			if !has || limit <= 0 || limit > 20 {
				limit = 8
			}
			var out []map[string]any
			if a.index != nil {
				if ms, err := a.Search(ctx, u.ID, q.str("query"), limit); err == nil && len(ms) > 0 {
					ids := make([]string, len(ms))
					for i, m := range ms {
						ids[i] = m.NoteID
					}
					notes, err := a.store.NotesByIDs(ctx, u.ID, ids)
					if err != nil {
						return fail(err), nil
					}
					for _, n := range notes {
						if !n.Archived {
							out = append(out, a.noteView(n, store.Snippet(n.Content, q.str("query"), 80)))
						}
					}
				}
			}
			if len(out) == 0 { // no index, or nothing semantic: keyword search
				hits, err := a.store.SearchNotes(ctx, u.ID, q.str("query"), limit, false)
				if err != nil {
					return fail(err), nil
				}
				for _, h := range hits {
					out = append(out, a.noteView(h.Note, h.Snippet))
				}
			}
			return ok(out), nil
		}, read)

	a.svc.AddToolWithMetadata("get_note", "Read one note in full by id.",
		schema(map[string]any{"id": str("Note id")}, "id"),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			n, err := a.store.GetNote(ctx, u.ID, args(in).str("id"))
			if err != nil {
				return fail(err), nil
			}
			v := a.noteView(n, "")
			v["content"] = n.Content
			return ok(v), nil
		}, read)

	a.svc.AddToolWithMetadata("create_note", "Save a new note.",
		schema(map[string]any{"title": str("Short title"), "content": str("Markdown body"), "tags": strList("Topic tags")}, "content"),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			q := args(in)
			n, err := a.store.CreateNote(ctx, store.Note{UserID: u.ID, Title: q.str("title"), Content: q.str("content"), Tags: q.strs("tags")})
			if err != nil {
				return fail(err), nil
			}
			return ok(a.noteView(n, "")), nil
		}, write)

	a.svc.AddToolWithMetadata("list_events",
		"List calendar events (recurring ones expanded) that overlap a time window.",
		schema(map[string]any{"from": str("Window start, RFC 3339"), "to": str("Window end, RFC 3339; at most 366 days after from")}, "from", "to"),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			q := args(in)
			from, err := parseTime(q.str("from"), a.loc)
			if err != nil {
				return fail(err), nil
			}
			to, err := parseTime(q.str("to"), a.loc)
			if err != nil {
				return fail(err), nil
			}
			occ, err := a.store.ListEvents(ctx, u.ID, from, to)
			if err != nil {
				return fail(err), nil
			}
			out := make([]map[string]any, 0, len(occ))
			for _, o := range occ {
				out = append(out, a.eventView(o.Event, o.Start, o.End))
			}
			return ok(out), nil
		}, read)

	eventProps := map[string]any{
		"title":                 str("Event title"),
		"start":                 str("Start, RFC 3339 (from resolve_datetime)"),
		"end":                   str("End, RFC 3339; default one hour after start"),
		"location":              str("Where"),
		"description":           str("Details"),
		"all_day":               flag("All-day event"),
		"rrule":                 str("Repeat rule, e.g. FREQ=WEEKLY;BYDAY=MO,WE or FREQ=DAILY;COUNT=5"),
		"remind_before_minutes": num("Remind this many minutes before the start"),
	}

	a.svc.AddToolWithMetadata("create_event", "Create a calendar event.",
		schema(eventProps, "title", "start"),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			q := args(in)
			start, err := parseTime(q.str("start"), a.loc)
			if err != nil {
				return fail(err), nil
			}
			e := store.Event{UserID: u.ID, Title: q.str("title"), Start: start, Location: q.str("location"),
				Description: q.str("description"), AllDay: q.boolean("all_day"), RRule: q.str("rrule"), TimeZone: a.loc.String()}
			if s := q.str("end"); s != "" {
				if e.End, err = parseTime(s, a.loc); err != nil {
					return fail(err), nil
				}
			}
			if m, has := q.integer("remind_before_minutes"); has {
				e.RemindBefore = &m
			}
			out, err := a.store.CreateEvent(ctx, e)
			if err != nil {
				return fail(err), nil
			}
			return ok(a.eventView(out, out.Start, out.End)), nil
		}, write)

	updateProps := map[string]any{"id": str("Event id (from list_events)")}
	for k, v := range eventProps {
		updateProps[k] = v
	}
	a.svc.AddToolWithMetadata("update_event",
		"Change fields of an existing event (reschedule, rename, move). Only pass the fields to change; moving start keeps the duration unless end is also given.",
		schema(updateProps, "id"),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			q := args(in)
			var p store.EventPatch
			set := func(k string, dst **string) {
				if q.has(k) {
					s := q.str(k)
					*dst = &s
				}
			}
			set("title", &p.Title)
			set("location", &p.Location)
			set("description", &p.Description)
			set("rrule", &p.RRule)
			if q.has("all_day") {
				b := q.boolean("all_day")
				p.AllDay = &b
			}
			if q.has("start") {
				t, err := parseTime(q.str("start"), a.loc)
				if err != nil {
					return fail(err), nil
				}
				p.Start = &t
			}
			if q.has("end") {
				t, err := parseTime(q.str("end"), a.loc)
				if err != nil {
					return fail(err), nil
				}
				p.End = &t
			}
			if m, has := q.integer("remind_before_minutes"); has {
				mp := &m
				p.RemindBefore = &mp
			}
			out, err := a.store.UpdateEvent(ctx, u.ID, q.str("id"), p)
			if err != nil {
				return fail(err), nil
			}
			return ok(a.eventView(out, out.Start, out.End)), nil
		}, write)

	a.svc.AddToolWithMetadata("list_tasks", "List the user's to-do items.",
		schema(map[string]any{"include_done": flag("Also include completed tasks"), "tag": str("Only tasks with this tag")}),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			q := args(in)
			state := "open"
			if q.boolean("include_done") {
				state = "all"
			}
			ts, err := a.store.ListTasks(ctx, u.ID, store.TaskFilter{State: state, Tag: q.str("tag"), Limit: 50})
			if err != nil {
				return fail(err), nil
			}
			out := make([]map[string]any, 0, len(ts))
			for _, t := range ts {
				out = append(out, a.taskView(t))
			}
			return ok(out), nil
		}, read)

	a.svc.AddToolWithMetadata("create_task", "Create a to-do item, optionally with a due time and a reminder.",
		schema(map[string]any{
			"title":     str("What to do"),
			"due":       str("Due time, RFC 3339"),
			"remind_at": str("When to be reminded, RFC 3339"),
			"priority":  num("1 low, 2 medium, 3 high"),
			"tags":      strList("Tags"),
		}, "title"),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			q := args(in)
			t := store.Task{UserID: u.ID, Title: q.str("title"), Tags: q.strs("tags")}
			if s := q.str("due"); s != "" {
				d, err := parseTime(s, a.loc)
				if err != nil {
					return fail(err), nil
				}
				t.Due = &d
			}
			if s := q.str("remind_at"); s != "" {
				r, err := parseTime(s, a.loc)
				if err != nil {
					return fail(err), nil
				}
				t.Remind = &r
			}
			if p, has := q.integer("priority"); has {
				t.Priority = p
			}
			out, err := a.store.CreateTask(ctx, t)
			if err != nil {
				return fail(err), nil
			}
			return ok(a.taskView(out)), nil
		}, write)

	a.svc.AddToolWithMetadata("complete_task", "Mark a to-do item as done.",
		schema(map[string]any{"id": str("Task id (from list_tasks)")}, "id"),
		func(ctx context.Context, in map[string]any) (any, error) {
			u, err := callerOf(ctx)
			if err != nil {
				return nil, err
			}
			done := true
			out, err := a.store.UpdateTask(ctx, u.ID, args(in).str("id"), store.TaskPatch{Done: &done})
			if err != nil {
				return fail(err), nil
			}
			return ok(a.taskView(out)), nil
		}, write)
}
