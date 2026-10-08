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
						if !n.Archived && a.visible(ctx, n.Space) {
							remember(ctx, Ref{Kind: "note", ID: n.ID, Title: n.Title})
							out = append(out, a.noteView(n, store.Snippet(n.Content, q.str("query"), 80)))
						}
					}
				}
			}
			if len(out) == 0 { // no index, or nothing semantic: keyword search
				hits, err := a.store.SearchNotes(ctx, u.ID, q.str("query"), limit, false, ctxSpace(ctx))
				if err != nil {
					return fail(err), nil
				}
				for _, h := range hits {
					remember(ctx, Ref{Kind: "note", ID: h.Note.ID, Title: h.Note.Title})
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
			if err == nil && !a.visible(ctx, n.Space) {
				err = store.ErrNotFound
			}
			if err != nil {
				return fail(err), nil
			}
			remember(ctx, Ref{Kind: "note", ID: n.ID, Title: n.Title})
			v := a.noteView(n, "")
			v["content"] = n.Content
			return ok(v), nil
		}, read)

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
			occ, err := a.store.ListEvents(ctx, u.ID, from, to, ctxSpace(ctx))
			if err != nil {
				return fail(err), nil
			}
			out := make([]map[string]any, 0, len(occ))
			for _, o := range occ {
				st := o.Start
				remember(ctx, Ref{Kind: "event", ID: o.Event.ID, Title: o.Event.Title, Time: &st})
				out = append(out, a.eventView(o.Event, o.Start, o.End))
			}
			return ok(out), nil
		}, read)

	a.svc.AddToolWithMetadata("list_tasks", "List the user's to-do items.",
		schema(map[string]any{"include_done": flag("Also include completed tasks"), "tag": str("Only tasks with this tag"), "project_id": str("Only tasks of this project")}),
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
			ts, err := a.store.ListTasks(ctx, u.ID, store.TaskFilter{State: state, Tag: q.str("tag"), ProjectID: q.str("project_id"), Space: ctxSpace(ctx), Limit: 50})
			if err != nil {
				return fail(err), nil
			}
			out := make([]map[string]any, 0, len(ts))
			for _, t := range ts {
				remember(ctx, Ref{Kind: "task", ID: t.ID, Title: t.Title, Time: t.Due})
				out = append(out, a.taskView(t))
			}
			return ok(out), nil
		}, read)

}

type spaceKey struct{}

// withSpace records the app's current mode for the duration of one assistant run.
func withSpace(ctx context.Context, space string) context.Context {
	return context.WithValue(ctx, spaceKey{}, space)
}

func ctxSpace(ctx context.Context) string {
	s, _ := ctx.Value(spaceKey{}).(string)
	return s
}

// spaceArg is the space a created item gets: what the model said, else the
// current mode. Empty lets the store inherit from the project or fall back to life.
func spaceArg(ctx context.Context, q args) string {
	if s := q.str("space"); s != "" {
		return s
	}
	return ctxSpace(ctx)
}

const (
	spaceDesc   = "work or life; omit to use the current mode"
	projectDesc = "Id of the project this belongs to (from list_projects)"
)

// visible reports whether the assistant may look at something in the given
// space during this run. The scope was set by the service from the user's
// AI-access setting; a read by id must honour it exactly like a list does.
func (a *Agent) visible(ctx context.Context, space string) bool {
	f := ctxSpace(ctx)
	return f == "" || f == space
}
