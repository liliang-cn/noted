// Package export writes everything a user has stored as a zip they can keep:
// noted.json (all records), notes/*.md (one Markdown file per note) and
// calendar.ics (events and dated tasks, readable by any calendar app).
package export

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liliang-cn/noted/internal/store"
)

const pageSize = 500

type Note struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Content  string    `json:"content"`
	Tags     []string  `json:"tags"`
	Pinned   bool      `json:"pinned"`
	Archived bool      `json:"archived"`
	Project  string    `json:"project_id,omitempty"`
	Space    string    `json:"space"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

type Event struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description,omitempty"`
	Location     string    `json:"location,omitempty"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	AllDay       bool      `json:"all_day"`
	TimeZone     string    `json:"time_zone,omitempty"`
	RRule        string    `json:"rrule,omitempty"`
	RemindBefore *int      `json:"remind_before_minutes,omitempty"`
	Project      string    `json:"project_id,omitempty"`
	Space        string    `json:"space"`
}

type Task struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Notes     string     `json:"notes,omitempty"`
	Due       *time.Time `json:"due,omitempty"`
	Priority  int        `json:"priority"`
	Done      bool       `json:"done"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
	Remind    *time.Time `json:"remind,omitempty"`
	Tags      []string   `json:"tags"`
	Project   string     `json:"project_id,omitempty"`
	Space     string     `json:"space"`
	CreatedAt time.Time  `json:"created"`
}

type Project struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Notes    string     `json:"notes,omitempty"`
	Pinned   bool       `json:"pinned"`
	Archived bool       `json:"archived"`
	Start    *time.Time `json:"start,omitempty"`
	Due      *time.Time `json:"due,omitempty"`
	Space    string     `json:"space"`
}

type CheckIn struct {
	Time   time.Time `json:"time"`
	Date   string    `json:"date"`
	Amount float64   `json:"amount"`
	Tally  float64   `json:"tally,omitempty"`
	Note   string    `json:"note,omitempty"`
}

type Goal struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Notes         string    `json:"notes,omitempty"`
	Period        string    `json:"period"`
	Target        float64   `json:"target"`
	Unit          string    `json:"unit,omitempty"`
	CounterUnit   string    `json:"counter_unit,omitempty"`
	CounterTarget float64   `json:"counter_target,omitempty"`
	Archived      bool      `json:"archived"`
	Space         string    `json:"space"`
	Objective     string    `json:"objective_id,omitempty"`
	CheckIns      []CheckIn `json:"check_ins"`
}

type Reading struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
	Note  string    `json:"note,omitempty"`
}

type Objective struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Notes        string     `json:"notes,omitempty"`
	Space        string     `json:"space"`
	Start        *time.Time `json:"start,omitempty"`
	Due          *time.Time `json:"due,omitempty"`
	MetricName   string     `json:"metric_name,omitempty"`
	MetricUnit   string     `json:"metric_unit,omitempty"`
	MetricStart  float64    `json:"metric_start,omitempty"`
	MetricTarget float64    `json:"metric_target,omitempty"`
	Archived     bool       `json:"archived"`
	Readings     []Reading  `json:"readings"`
}

type Trade struct {
	Time   time.Time `json:"time"`
	Side   string    `json:"side"`
	Shares float64   `json:"shares"`
	Price  float64   `json:"price"`
	Fee    float64   `json:"fee,omitempty"`
	Note   string    `json:"note,omitempty"`
}

type Holding struct {
	ID        string     `json:"id"`
	Symbol    string     `json:"symbol"`
	Name      string     `json:"name,omitempty"`
	Currency  string     `json:"currency"`
	Notes     string     `json:"notes,omitempty"`
	LastPrice *float64   `json:"last_price,omitempty"`
	PriceTime *time.Time `json:"last_price_time,omitempty"`
	DCAAmount float64    `json:"dca_amount,omitempty"`
	DCADay    int        `json:"dca_day,omitempty"`
	DCATime   string     `json:"dca_time,omitempty"`
	TimeZone  string     `json:"time_zone"`
	Archived  bool       `json:"archived"`
	Trades    []Trade    `json:"trades"`
}

type Data struct {
	Format     string            `json:"format"`
	ExportedAt time.Time         `json:"exported_at"`
	User       string            `json:"user"`
	Notes      []Note            `json:"notes"`
	Events     []Event           `json:"events"`
	Tasks      []Task            `json:"tasks"`
	Projects   []Project         `json:"projects"`
	Goals      []Goal            `json:"goals"`
	Objectives []Objective       `json:"objectives"`
	Holdings   []Holding         `json:"holdings"`
	Settings   map[string]string `json:"settings"`
}

// Collect reads everything the user owns, archived and finished items included.
func Collect(ctx context.Context, st *store.Store, userID, userName string, now time.Time) (*Data, error) {
	d := &Data{Format: "noted-export/1", ExportedAt: now.UTC(), User: userName,
		Notes: []Note{}, Events: []Event{}, Tasks: []Task{}, Projects: []Project{}, Goals: []Goal{}, Objectives: []Objective{}, Holdings: []Holding{}, Settings: map[string]string{}}

	for off := 0; ; off += pageSize {
		ns, err := st.ListNotes(ctx, userID, store.NoteFilter{IncludeArchived: true, Limit: pageSize, Offset: off})
		if err != nil {
			return nil, err
		}
		for _, n := range ns {
			d.Notes = append(d.Notes, Note{n.ID, n.Title, n.Content, nonNil(n.Tags), n.Pinned, n.Archived, n.ProjectID, n.Space, n.Created.UTC(), n.Updated.UTC()})
		}
		if len(ns) < pageSize {
			break
		}
	}
	evs, err := st.AllEvents(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, e := range evs {
		d.Events = append(d.Events, Event{e.ID, e.Title, e.Description, e.Location, e.Start.UTC(), e.End.UTC(), e.AllDay, e.TimeZone, e.RRule, e.RemindBefore, e.ProjectID, e.Space})
	}
	for off := 0; ; off += pageSize {
		ts, err := st.ListTasks(ctx, userID, store.TaskFilter{State: "all", Limit: pageSize, Offset: off})
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			d.Tasks = append(d.Tasks, Task{t.ID, t.Title, t.Notes, utc(t.Due), t.Priority, t.Done, utc(t.DoneAt), utc(t.Remind), nonNil(t.Tags), t.ProjectID, t.Space, t.Created.UTC()})
		}
		if len(ts) < pageSize {
			break
		}
	}
	ps, err := st.ListProjects(ctx, userID, store.ProjectFilter{IncludeArchived: true}, now, time.UTC)
	if err != nil {
		return nil, err
	}
	for _, v := range ps {
		p := v.Project
		d.Projects = append(d.Projects, Project{p.ID, p.Title, p.Notes, p.Pinned, p.Archived, utc(p.Start), utc(p.Due), p.Space})
	}
	gs, err := st.ListGoals(ctx, userID, true, now, "")
	if err != nil {
		return nil, err
	}
	for _, v := range gs {
		g := v.Goal
		cs, err := st.ListCheckIns(ctx, userID, g.ID, nil, nil, 1_000_000)
		if err != nil {
			return nil, err
		}
		out := Goal{ID: g.ID, Title: g.Title, Notes: g.Notes, Period: g.Period, Target: g.Target, Unit: g.Unit,
			CounterUnit: g.CounterUnit, CounterTarget: g.CounterTarget, Archived: g.Archived, Space: g.Space, Objective: g.ObjectiveID, CheckIns: []CheckIn{}}
		for _, c := range cs {
			out.CheckIns = append(out.CheckIns, CheckIn{c.Time.UTC(), c.Date, c.Amount, c.Tally, c.Note})
		}
		sort.SliceStable(out.CheckIns, func(i, j int) bool { return out.CheckIns[i].Time.Before(out.CheckIns[j].Time) })
		d.Goals = append(d.Goals, out)
	}
	obs, err := st.ListObjectives(ctx, userID, true, now, "")
	if err != nil {
		return nil, err
	}
	for _, v := range obs {
		o := v.Objective
		out := Objective{ID: o.ID, Title: o.Title, Notes: o.Notes, Space: o.Space, Start: utc(o.Start), Due: utc(o.Due), MetricName: o.MetricName,
			MetricUnit: o.MetricUnit, MetricStart: o.MetricStart, MetricTarget: o.MetricTarget, Archived: o.Archived, Readings: []Reading{}}
		if o.HasMetric() {
			ms, err := st.ListMeasurements(ctx, userID, o.ID, nil, nil, 1_000_000)
			if err != nil {
				return nil, err
			}
			for _, m := range ms {
				out.Readings = append(out.Readings, Reading{m.Time.UTC(), m.Value, m.Note})
			}
		}
		d.Objectives = append(d.Objectives, out)
	}
	hs, err := st.ListHoldings(ctx, userID, true, now)
	if err != nil {
		return nil, err
	}
	for _, v := range hs {
		h := v.Holding
		out := Holding{ID: h.ID, Symbol: h.Symbol, Name: h.Name, Currency: h.Currency, Notes: h.Notes, LastPrice: h.LastPrice, PriceTime: utc(h.PriceTime),
			DCAAmount: h.DCAAmount, DCADay: h.DCADay, DCATime: h.DCATime, TimeZone: h.TimeZone, Archived: h.Archived, Trades: []Trade{}}
		ts, err := st.ListTrades(ctx, userID, h.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			out.Trades = append(out.Trades, Trade{t.Time.UTC(), t.Side, t.Shares, t.Price, t.Fee, t.Note})
		}
		d.Holdings = append(d.Holdings, out)
	}
	prefs, err := st.ListPreferences(ctx, userID, "")
	if err != nil {
		return nil, err
	}
	for _, p := range prefs {
		d.Settings[p.Key] = p.Value
	}
	return d, nil
}

// Write streams the zip for one user.
func Write(ctx context.Context, st *store.Store, userID, userName string, now time.Time, w io.Writer) error {
	d, err := Collect(ctx, st, userID, userName, now)
	if err != nil {
		return err
	}
	return WriteZip(d, w)
}

func WriteZip(d *Data, w io.Writer) error {
	zw := zip.NewWriter(w)
	add := func(name string, body []byte, mod time.Time) error {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: mod})
		if err != nil {
			return err
		}
		_, err = f.Write(body)
		return err
	}
	js, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if err := add("noted.json", js, d.ExportedAt); err != nil {
		return err
	}
	if err := add("calendar.ics", []byte(ICS(d)), d.ExportedAt); err != nil {
		return err
	}
	if err := add("trades.csv", []byte(TradesCSV(d)), d.ExportedAt); err != nil {
		return err
	}
	titles := map[string]string{}
	for _, p := range d.Projects {
		titles[p.ID] = p.Title
	}
	used := map[string]bool{}
	for _, n := range d.Notes {
		name := "notes/" + NoteFileName(n, used)
		if err := add(name, []byte(Markdown(n, titles[n.Project])), n.Updated); err != nil {
			return err
		}
	}
	return zw.Close()
}

// TradesCSV lists every buy and sell, ready for a spreadsheet.
func TradesCSV(d *Data) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"symbol", "currency", "time", "side", "shares", "price", "fee", "note"})
	for _, h := range d.Holdings {
		for _, t := range h.Trades {
			_ = w.Write([]string{h.Symbol, h.Currency, t.Time.Format(time.RFC3339), t.Side,
				strconv.FormatFloat(t.Shares, 'f', -1, 64), strconv.FormatFloat(t.Price, 'f', -1, 64), strconv.FormatFloat(t.Fee, 'f', -1, 64), csvSafe(t.Note)})
		}
	}
	w.Flush()
	return b.String()
}

// csvSafe stops a spreadsheet from running a note that starts like a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

var unsafe = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

// NoteFileName is a readable, unique file name such as 2026-10-08 行程想法.md.
func NoteFileName(n Note, used map[string]bool) string {
	title := strings.TrimSpace(unsafe.ReplaceAllString(n.Title, " "))
	if title == "" {
		title = "无标题"
	}
	if utf8.RuneCountInString(title) > 60 {
		title = string([]rune(title)[:60])
	}
	base := n.Created.Format("2006-01-02") + " " + title
	name := base + ".md"
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s (%d).md", base, i)
	}
	used[name] = true
	return name
}

// Markdown is the note with its metadata as front matter.
func Markdown(n Note, project string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", yamlString(n.Title))
	if len(n.Tags) > 0 {
		q := make([]string, len(n.Tags))
		for i, t := range n.Tags {
			q[i] = yamlString(t)
		}
		fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(q, ", "))
	}
	fmt.Fprintf(&b, "space: %s\n", n.Space)
	if project != "" {
		fmt.Fprintf(&b, "project: %s\n", yamlString(project))
	}
	if n.Pinned {
		b.WriteString("pinned: true\n")
	}
	if n.Archived {
		b.WriteString("archived: true\n")
	}
	fmt.Fprintf(&b, "created: %s\nupdated: %s\n---\n\n", n.Created.Format(time.RFC3339), n.Updated.Format(time.RFC3339))
	b.WriteString(n.Content)
	if !strings.HasSuffix(n.Content, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func yamlString(s string) string {
	b, _ := json.Marshal(s) // a JSON string is a valid YAML double-quoted scalar
	return string(b)
}

// ICS renders events and dated tasks as an iCalendar file.
func ICS(d *Data) string {
	var lines []string
	add := func(s string) { lines = append(lines, s) }
	stamp := d.ExportedAt.UTC().Format("20060102T150405Z")
	add("BEGIN:VCALENDAR")
	add("VERSION:2.0")
	add("PRODID:-//noted//export//EN")
	add("CALSCALE:GREGORIAN")
	for _, e := range d.Events {
		add("BEGIN:VEVENT")
		add("UID:" + e.ID + "@noted")
		add("DTSTAMP:" + stamp)
		if e.AllDay {
			add("DTSTART;VALUE=DATE:" + e.Start.Format("20060102"))
			add("DTEND;VALUE=DATE:" + e.End.Format("20060102"))
		} else {
			add("DTSTART:" + e.Start.UTC().Format("20060102T150405Z"))
			add("DTEND:" + e.End.UTC().Format("20060102T150405Z"))
		}
		add("SUMMARY:" + icsText(e.Title))
		if e.Description != "" {
			add("DESCRIPTION:" + icsText(e.Description))
		}
		if e.Location != "" {
			add("LOCATION:" + icsText(e.Location))
		}
		if e.RRule != "" {
			add("RRULE:" + strings.TrimPrefix(e.RRule, "RRULE:"))
		}
		if e.RemindBefore != nil {
			add("BEGIN:VALARM")
			add("ACTION:DISPLAY")
			add("DESCRIPTION:" + icsText(e.Title))
			add(fmt.Sprintf("TRIGGER:-PT%dM", *e.RemindBefore))
			add("END:VALARM")
		}
		add("END:VEVENT")
	}
	for _, t := range d.Tasks {
		add("BEGIN:VTODO")
		add("UID:" + t.ID + "@noted")
		add("DTSTAMP:" + stamp)
		add("SUMMARY:" + icsText(t.Title))
		if t.Notes != "" {
			add("DESCRIPTION:" + icsText(t.Notes))
		}
		if t.Due != nil {
			add("DUE:" + t.Due.UTC().Format("20060102T150405Z"))
		}
		switch t.Priority {
		case 3:
			add("PRIORITY:1")
		case 2:
			add("PRIORITY:5")
		case 1:
			add("PRIORITY:9")
		}
		if len(t.Tags) > 0 {
			add("CATEGORIES:" + strings.Join(escAll(t.Tags), ","))
		}
		if t.Done {
			add("STATUS:COMPLETED")
			if t.DoneAt != nil {
				add("COMPLETED:" + t.DoneAt.UTC().Format("20060102T150405Z"))
			}
		} else {
			add("STATUS:NEEDS-ACTION")
		}
		add("END:VTODO")
	}
	add("END:VCALENDAR")
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(fold(l))
	}
	return b.String()
}

func escAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = icsText(s)
	}
	return out
}

func icsText(s string) string {
	s = strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`).Replace(s)
	return s
}

// fold wraps a content line at 75 octets without splitting a character.
func fold(l string) string {
	var b strings.Builder
	n := 0
	for _, r := range l {
		w := utf8.RuneLen(r)
		if n+w > 75 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += w
	}
	b.WriteString("\r\n")
	return b.String()
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
