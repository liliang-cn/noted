package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liliang-cn/noted/internal/plan"
	"github.com/liliang-cn/noted/internal/store"
)

const (
	maxPlanTasks    = 8
	maxExtractTasks = 10
	maxTaskTitle    = 200
)

func (a *Agent) todayLine(now time.Time) string {
	n := now.In(a.loc)
	return fmt.Sprintf("Today is %s (%s), time zone %s.", n.Format("2006-01-02"), n.Format("Monday"), a.loc)
}

// Plan reads a free-form request such as "next month I'm going to the
// Philippines, I need a visa, flights and a hotel" and breaks it into a project
// with tasks. It only keeps dates the user actually stated.
func (a *Agent) Plan(ctx context.Context, user store.User, text, space string) (plan.PlanSpec, error) {
	out, err := a.generate(ctx, a.todayLine(time.Now())+`
Read the request and break it into ONE project and the tasks it needs. Output ONLY a JSON object, no commentary:
{"project": "short name, or empty", "start": "YYYY-MM-DD or null", "due": "YYYY-MM-DD or null", "space": "work" or "life" or null,
 "tasks": [{"title": "...", "due": "YYYY-MM-DD or null", "days_before_start": integer or null, "priority": 0 to 3}]}
Rules:
- Give "start", "due" or a task's "due" ONLY when the user stated an exact date, or one you can compute exactly from today (for example "next Friday"). "Next month" or "in spring" is not exact: use null.
- If a task should happen a fixed time before the start (a visa takes weeks), set days_before_start and leave its due null. Never invent a date.
- Use only tasks the user mentioned or that what they mentioned plainly requires. At most 8. Titles short, in the language the user wrote in.
- A request that is one single action is not a project: leave "project" empty and return that one task.
- "space" is work for jobs, colleagues and deadlines, life for everything else; null if unclear.`, text, tokensLong)
	if err != nil {
		return plan.PlanSpec{}, err
	}
	return parsePlan(out, a.loc, space)
}

// Extract finds the to-dos inside a note.
func (a *Agent) Extract(ctx context.Context, user store.User, n store.Note) ([]plan.ExtractSpec, error) {
	out, err := a.generate(ctx, a.todayLine(time.Now())+`
Find the actions the author of this note still has to do. Output ONLY a JSON object, no commentary:
{"tasks": [{"title": "...", "due": "YYYY-MM-DD or null"}]}
Rules:
- Only things clearly written as something to do. Skip anything already done (a checked box, struck through, in the past tense).
- "due" only when the note states a date or one you can compute exactly from today; otherwise null.
- At most 10. Titles short, in the note's language.`, noteText(n), tokensMedium)
	if err != nil {
		return nil, err
	}
	return parseExtract(out, a.loc)
}

func jsonObject(out string) (string, error) {
	i, j := strings.Index(out, "{"), strings.LastIndex(out, "}")
	if i < 0 || j < i {
		return "", fmt.Errorf("the model did not return JSON: %.80q", out)
	}
	return out[i : j+1], nil
}

func parseDate(s *string, loc *time.Location) *time.Time {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(*s), loc)
	if err != nil {
		return nil // an unreadable date is no date, not a guess
	}
	return &t
}

func cleanTitle(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > maxTaskTitle {
		return "", false
	}
	return s, true
}

func parsePlan(out string, loc *time.Location, defaultSpace string) (plan.PlanSpec, error) {
	raw, err := jsonObject(out)
	if err != nil {
		return plan.PlanSpec{}, err
	}
	var in struct {
		Project string  `json:"project"`
		Start   *string `json:"start"`
		Due     *string `json:"due"`
		Space   *string `json:"space"`
		Tasks   []struct {
			Title           string  `json:"title"`
			Due             *string `json:"due"`
			DaysBeforeStart *int    `json:"days_before_start"`
			Priority        int     `json:"priority"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return plan.PlanSpec{}, fmt.Errorf("the model returned malformed JSON: %w", err)
	}
	spec := plan.PlanSpec{Start: parseDate(in.Start, loc), Due: parseDate(in.Due, loc), Space: defaultSpace}
	if p, ok := cleanTitle(in.Project); ok {
		spec.Project = p
	}
	if in.Space != nil && (*in.Space == store.SpaceWork || *in.Space == store.SpaceLife) && defaultSpace == "" {
		spec.Space = *in.Space
	}
	for _, t := range in.Tasks {
		title, ok := cleanTitle(t.Title)
		if !ok {
			continue
		}
		ts := plan.PlanTaskSpec{Title: title, Due: parseDate(t.Due, loc)}
		if t.DaysBeforeStart != nil && *t.DaysBeforeStart >= 0 && *t.DaysBeforeStart <= 365 {
			ts.DaysBeforeStart = t.DaysBeforeStart
		}
		if t.Priority >= 1 && t.Priority <= 3 {
			ts.Priority = t.Priority
		}
		spec.Tasks = append(spec.Tasks, ts)
		if len(spec.Tasks) == maxPlanTasks {
			break
		}
	}
	if spec.Project == "" && len(spec.Tasks) == 0 {
		return plan.PlanSpec{}, fmt.Errorf("could not find anything to plan in that")
	}
	// A relative offset needs a project to count from; without one, drop it.
	if spec.Project == "" {
		for i := range spec.Tasks {
			spec.Tasks[i].DaysBeforeStart = nil
		}
	}
	return spec, nil
}

func parseExtract(out string, loc *time.Location) ([]plan.ExtractSpec, error) {
	raw, err := jsonObject(out)
	if err != nil {
		return nil, err
	}
	var in struct {
		Tasks []struct {
			Title string  `json:"title"`
			Due   *string `json:"due"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, fmt.Errorf("the model returned malformed JSON: %w", err)
	}
	var tasks []plan.ExtractSpec
	seen := map[string]bool{}
	for _, t := range in.Tasks {
		title, ok := cleanTitle(t.Title)
		if !ok || seen[strings.ToLower(title)] {
			continue
		}
		seen[strings.ToLower(title)] = true
		tasks = append(tasks, plan.ExtractSpec{Title: title, Due: parseDate(t.Due, loc)})
		if len(tasks) == maxExtractTasks {
			break
		}
	}
	return tasks, nil
}
