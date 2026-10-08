package ai

import (
	"strings"
	"testing"
	"time"
)

func TestParsePlanKeepsOnlyStatedDates(t *testing.T) {
	out := `Sure, here you go:
{"project": " 菲律宾旅行 ", "start": null, "due": null, "space": "life",
 "tasks": [
  {"title": "办理签证", "due": null, "days_before_start": 30, "priority": 3},
  {"title": "订机票", "due": "2026-10-20", "days_before_start": null, "priority": 0},
  {"title": "准备酒店", "due": "not a date", "days_before_start": null}
 ]}
Hope that helps!`
	spec, err := parsePlan(out, time.UTC, "")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Project != "菲律宾旅行" || spec.Start != nil || spec.Space != "life" || len(spec.Tasks) != 3 {
		t.Fatalf("%+v", spec)
	}
	visa, flights, hotel := spec.Tasks[0], spec.Tasks[1], spec.Tasks[2]
	if visa.DaysBeforeStart == nil || *visa.DaysBeforeStart != 30 || visa.Priority != 3 || visa.Due != nil {
		t.Fatalf("visa: %+v", visa)
	}
	if flights.Due == nil || flights.Due.Format("2006-01-02") != "2026-10-20" {
		t.Fatalf("flights: %+v", flights)
	}
	if hotel.Due != nil {
		t.Fatalf("an unreadable date must become no date, not a guess: %+v", hotel)
	}
}

func TestParsePlanTheCurrentModeBeatsTheModelsGuess(t *testing.T) {
	spec, err := parsePlan(`{"project":"X","space":"life","tasks":[{"title":"a"}]}`, time.UTC, "work")
	if err != nil || spec.Space != "work" {
		t.Fatalf("%+v %v", spec, err)
	}
}

func TestParsePlanBounds(t *testing.T) {
	var tasks []string
	for i := 0; i < 12; i++ {
		tasks = append(tasks, `{"title":"task `+string(rune('a'+i))+`"}`)
	}
	spec, err := parsePlan(`{"project":"P","tasks":[`+strings.Join(tasks, ",")+`,{"title":""},{"title":"x","days_before_start":9999},{"title":"y","days_before_start":-3}]}`, time.UTC, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Tasks) != maxPlanTasks {
		t.Fatalf("tasks = %d, want at most %d", len(spec.Tasks), maxPlanTasks)
	}
	spec, _ = parsePlan(`{"project":"P","tasks":[{"title":"x","days_before_start":9999},{"title":"y","days_before_start":-3},{"title":"z","priority":9}]}`, time.UTC, "")
	for _, tk := range spec.Tasks {
		if tk.DaysBeforeStart != nil || tk.Priority != 0 {
			t.Fatalf("out-of-range values must be dropped: %+v", tk)
		}
	}
}

func TestParsePlanWithoutAProjectDropsRelativeOffsets(t *testing.T) {
	spec, err := parsePlan(`{"project":"","tasks":[{"title":"Call the bank","days_before_start":5}]}`, time.UTC, "")
	if err != nil || spec.Project != "" || len(spec.Tasks) != 1 || spec.Tasks[0].DaysBeforeStart != nil {
		t.Fatalf("%+v %v", spec, err)
	}
}

func TestParsePlanRejectsNonsense(t *testing.T) {
	for name, out := range map[string]string{
		"prose":      "I cannot help with that",
		"broken":     `{"project": "x", "tasks": [`,
		"empty plan": `{"project":"","tasks":[]}`,
		"all blank":  `{"project":"  ","tasks":[{"title":" "}]}`,
	} {
		if _, err := parsePlan(out, time.UTC, ""); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseExtract(t *testing.T) {
	out := "```json\n" + `{"tasks":[{"title":"订大阪机票","due":"2026-10-31"},{"title":"订酒店","due":null},{"title":"订酒店"},{"title":""},{"title":"办电子签","due":"31/10"}]}` + "\n```"
	got, err := parseExtract(out, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("duplicates and blanks should go: %+v", got)
	}
	if got[0].Due == nil || got[0].Due.Format("2006-01-02") != "2026-10-31" || got[1].Due != nil || got[2].Due != nil {
		t.Fatalf("%+v", got)
	}
	if none, err := parseExtract(`{"tasks":[]}`, time.UTC); err != nil || len(none) != 0 {
		t.Fatalf("an empty list is a valid answer: %v %v", none, err)
	}
	if _, err := parseExtract("no json here", time.UTC); err == nil {
		t.Fatal("prose should be rejected")
	}
}
