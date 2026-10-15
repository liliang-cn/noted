// Package cortexsync copies what a user has in noted into a CortexDB brain,
// one way, so the brain knows what exists and how it relates while noted
// stays the record of the exact numbers, times and states.
//
// Every item becomes one knowledge record in the collection "noted"
// (knowledge_save: searchable text, replaced on update, removed on delete)
// that also declares the item as a typed graph entity and its relations to
// other items. The ids are stable, so a re-send overwrites:
//
//	knowledge id   noted:<user>:<kind>:<id>
//	entity node    entity:noted:<user>:<kind>:<id>
//	source url     noted://<kind>/<id>
//
// Kinds: project, task, event, note, goal, objective, holding, alarm, and
// time, whose id is an ISO week (time entries are summed per week, not sent
// one by one). Numbers that change (shares, average cost, a metric's latest
// reading, minutes, an alarm's next rings) are written only as dated
// snapshots marked derived.
package cortexsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liliang-cn/cortexdb/v2/pkg/cortexdb"
	"github.com/liliang-cn/noted/internal/alarm"
	"github.com/liliang-cn/noted/internal/store"
)

// DefaultCollection is the knowledge collection items are saved in. Unscoped
// recall searches every collection; this one lets a caller ask noted only.
const DefaultCollection = "noted"

// Item kinds; the first part of an item key.
const (
	KindProject   = "project"
	KindTask      = "task"
	KindEvent     = "event"
	KindNote      = "note"
	KindGoal      = "goal"
	KindObjective = "objective"
	KindHolding   = "holding"
	KindAlarm     = "alarm"
	KindTime      = "time"
)

// kindOrder is the order a run sends kinds in, the ones others point at first.
var kindOrder = []string{KindObjective, KindProject, KindGoal, KindHolding, KindTask, KindEvent, KindNote, KindAlarm, KindTime}

// Relation types between entities. "mentions" is not one of them: CortexDB
// keeps that name for its own chunk-to-entity bookkeeping edges, which graph
// listings hide, so a note's links to other items are "references".
const (
	RelPartOf      = "part_of"      // task, event, note, goal, objective, alarm -> project
	RelReferences  = "references"   // note -> anything it links to with noted://kind/id
	RelDimensionOf = "dimension_of" // goal -> objective
	RelCountToward = "count_toward" // a week of time entries -> goal
	RelSpentOn     = "spent_on"     // a week of time entries -> project
	RelHeldIn      = "held_in"      // holding -> currency
)

// Holding is a holding with the position its trades add up to.
type Holding struct {
	store.Holding
	Position  store.Position
	LastTrade *time.Time
}

// Alarm is an alarm with its next rings as of the snapshot.
type Alarm struct {
	store.Alarm
	NextFires []time.Time
}

// Snapshot is everything of one user the projection reads.
type Snapshot struct {
	Now        time.Time      // when it was read; dates the alarms' next rings
	User       string         // user name; part of every id
	Location   *time.Location // dates and ISO weeks are read here
	Lang       string         // "zh" or "en"
	Projects   []store.Project
	Tasks      []store.Task
	Events     []store.Event
	Notes      []store.Note
	Goals      []store.Goal
	Objectives []store.ObjectiveView
	Holdings   []Holding
	Time       []store.TimeEntry
	Alarms     []Alarm
}

// Item is one record for the brain.
type Item struct {
	Key     string // "<kind>:<id>", the ledger key
	Save    cortexdb.KnowledgeSaveRequest
	Targets []string // keys of the items its relations point at
	Hash    string   // of Save, to tell whether it changed since it was sent
}

func (it Item) Kind() string { k, _, _ := strings.Cut(it.Key, ":"); return k }

// Projection is what a snapshot becomes.
type Projection struct {
	Items []Item
	// Entities holds each item's entity by key, without chunk links. A record
	// whose relations point at other items carries their entities too, so it
	// never depends on the order records reach the brain.
	Entities map[string]cortexdb.ToolEntityInput
}

func key(kind, id string) string { return kind + ":" + id }

// KnowledgeID is the stable knowledge id of an item.
func KnowledgeID(user, key string) string { return "noted:" + user + ":" + key }

// EntityID is the graph node id of an item's entity.
func EntityID(user, key string) string {
	return cortexdb.EntityNodeIDPrefix + "noted:" + user + ":" + key
}

// Link is the noted:// address of an item, the way the App writes references.
func Link(key string) string {
	kind, id, _ := strings.Cut(key, ":")
	return "noted://" + kind + "/" + url.PathEscape(id)
}

// firstChunkID is the node CortexDB gives the first chunk of a knowledge
// record. An item's entity is linked to it, so a text hit on the record leads
// to the entity and on through its relations.
func firstChunkID(knowledgeID string) string { return fmt.Sprintf("chunk:%s:%03d", knowledgeID, 0) }

func currencyEntity(code string) cortexdb.ToolEntityInput {
	return cortexdb.ToolEntityInput{
		ID:       cortexdb.EntityNodeIDPrefix + "noted:currency:" + strings.ToLower(code),
		Name:     code,
		Type:     "noted_currency",
		Metadata: map[string]string{"source": "noted"},
	}
}

// builder collects items while it projects a snapshot.
type builder struct {
	s     Snapshot
	l     labels
	out   Projection
	exist map[string]bool   // keys present in the snapshot
	title map[string]string // key -> title, for "project: X" lines
}

// Project turns a snapshot into the records a brain should hold.
func Project(s Snapshot) Projection {
	if s.Location == nil {
		s.Location = time.UTC
	}
	b := &builder{s: s, l: labelsFor(s.Lang), out: Projection{Entities: map[string]cortexdb.ToolEntityInput{}},
		exist: map[string]bool{}, title: map[string]string{}}
	for _, o := range s.Objectives {
		b.see(KindObjective, o.Objective.ID, o.Objective.Title)
	}
	for _, p := range s.Projects {
		b.see(KindProject, p.ID, p.Title)
	}
	for _, g := range s.Goals {
		b.see(KindGoal, g.ID, g.Title)
	}
	for _, h := range s.Holdings {
		b.see(KindHolding, h.ID, h.Symbol)
	}
	for _, t := range s.Tasks {
		b.see(KindTask, t.ID, t.Title)
	}
	for _, e := range s.Events {
		b.see(KindEvent, e.ID, e.Title)
	}
	for _, n := range s.Notes {
		b.see(KindNote, n.ID, n.Title)
	}
	for _, a := range s.Alarms {
		b.see(KindAlarm, a.ID, a.Label)
	}
	for _, o := range s.Objectives {
		b.objective(o)
	}
	for _, p := range s.Projects {
		b.project(p)
	}
	for _, g := range s.Goals {
		b.goal(g)
	}
	for _, h := range s.Holdings {
		b.holding(h)
	}
	for _, t := range s.Tasks {
		b.task(t)
	}
	for _, e := range s.Events {
		b.event(e)
	}
	for _, n := range s.Notes {
		b.note(n)
	}
	for _, a := range s.Alarms {
		b.alarm(a)
	}
	b.weeks()
	order := map[string]int{}
	for i, k := range kindOrder {
		order[k] = i
	}
	sort.SliceStable(b.out.Items, func(i, j int) bool {
		a, c := b.out.Items[i], b.out.Items[j]
		if a.Kind() != c.Kind() {
			return order[a.Kind()] < order[c.Kind()]
		}
		return a.Key < c.Key
	})
	return b.out
}

func (b *builder) see(kind, id, title string) {
	k := key(kind, id)
	b.exist[k] = true
	b.title[k] = title
}

// rel is a relation from an item to another item.
type rel struct {
	to, typ string
	meta    map[string]string
}

// record is what one item says before it becomes an Item.
type record struct {
	kind, id string
	title    string // the record's title
	name     string // the entity's name
	desc     string // one line about the entity
	content  []string
	meta     map[string]string
	rels     []rel
	extra    []cortexdb.ToolEntityInput // entities that are not items, such as a currency
	extraRel []cortexdb.ToolRelationInput
}

func (b *builder) emit(r record) {
	k := key(r.kind, r.id)
	kid := KnowledgeID(b.s.User, k)
	meta := map[string]string{
		"source":           "noted",
		"system_of_record": "noted",
		"noted_user":       b.s.User,
		"noted_kind":       r.kind,
		"noted_id":         r.id,
		"link":             Link(k),
	}
	// Empty values are written too: CortexDB keeps an entity's earlier
	// properties unless a write names them, so a cleared field must be named.
	for mk, mv := range r.meta {
		meta[mk] = mv
	}
	ent := cortexdb.ToolEntityInput{
		ID: EntityID(b.s.User, k), Name: r.name, Type: "noted_" + r.kind, Description: r.desc, Metadata: meta,
	}
	b.out.Entities[k] = ent
	own := ent
	own.ChunkIDs = []string{firstChunkID(kid)}

	save := cortexdb.KnowledgeSaveRequest{
		KnowledgeID: kid,
		Title:       r.title,
		Content:     strings.TrimSpace(strings.Join(r.content, "\n")) + "\n\n" + fmt.Sprintf(b.l.source, Link(k)),
		SourceURL:   Link(k),
		Author:      "noted",
		Collection:  DefaultCollection,
		Metadata:    meta,
		Entities:    append([]cortexdb.ToolEntityInput{own}, r.extra...),
	}
	var targets []string
	seen := map[string]bool{}
	for _, x := range r.rels {
		if x.to == k || !b.exist[x.to] || seen[x.typ+" "+x.to] {
			continue
		}
		seen[x.typ+" "+x.to] = true
		save.Relations = append(save.Relations, cortexdb.ToolRelationInput{
			From: ent.ID, To: EntityID(b.s.User, x.to), Type: x.typ, Metadata: x.meta,
		})
		if !contains(targets, x.to) {
			targets = append(targets, x.to)
		}
	}
	save.Relations = append(save.Relations, r.extraRel...)
	sort.Strings(targets)
	body, _ := json.Marshal(save)
	sum := sha256.Sum256(body)
	b.out.Items = append(b.out.Items, Item{Key: k, Save: save, Targets: targets, Hash: hex.EncodeToString(sum[:16])})
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func (b *builder) date(t time.Time) string { return t.In(b.s.Location).Format("2006-01-02") }

func (b *builder) datePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return b.date(*t)
}

func (b *builder) space(s string) string {
	if s == store.SpaceWork {
		return b.l.work
	}
	return b.l.life
}

// line joins the non-empty parts with " · ".
func line(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func labeled(label, v string) string {
	if v == "" {
		return ""
	}
	return label + v
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func money(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

func (b *builder) projectLine(projectID string) (string, []rel) {
	if projectID == "" || !b.exist[key(KindProject, projectID)] {
		return "", nil
	}
	k := key(KindProject, projectID)
	return b.l.project + b.title[k], []rel{{to: k, typ: RelPartOf}}
}

func (b *builder) project(p store.Project) {
	status := b.l.active
	if p.Archived {
		status = b.l.archived
	}
	head := line(b.l.status+status, b.l.space+b.space(p.Space), labeled(b.l.start, b.datePtr(p.Start)), labeled(b.l.due, b.datePtr(p.Due)))
	b.emit(record{
		kind: KindProject, id: p.ID, title: b.l.kindProject + "：" + p.Title, name: p.Title,
		desc:    line(b.l.kindProject, status, b.space(p.Space), labeled(b.l.due, b.datePtr(p.Due))),
		content: []string{b.l.kindProject + "：" + p.Title, head, p.Notes},
		meta: map[string]string{"status": statusKey(p.Archived, "active"), "space": p.Space, "pinned": boolStr(p.Pinned),
			"start": b.datePtr(p.Start), "due": b.datePtr(p.Due)},
	})
}

func statusKey(archived bool, otherwise string) string {
	if archived {
		return "archived"
	}
	return otherwise
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return ""
}

func (b *builder) task(t store.Task) {
	status, sk := b.l.open, "open"
	if t.Done {
		status, sk = b.l.done, "done"
		if t.DoneAt != nil {
			status += "（" + b.date(*t.DoneAt) + "）"
		}
	}
	progress := ""
	if t.Progress > 0 && !t.Done {
		progress = b.l.progress + strconv.Itoa(t.Progress) + "%"
	}
	proj, rels := b.projectLine(t.ProjectID)
	head := line(b.l.status+status, labeled(b.l.priority, b.l.prio[clampPrio(t.Priority)]), labeled(b.l.due, b.datePtr(t.Due)),
		progress, labeled(b.l.repeat, t.RRule), b.l.space+b.space(t.Space))
	b.emit(record{
		kind: KindTask, id: t.ID, title: b.l.kindTask + "：" + t.Title, name: t.Title,
		desc:    line(b.l.kindTask, status, labeled(b.l.due, b.datePtr(t.Due))),
		content: []string{b.l.kindTask + "：" + t.Title, head, proj, labeled(b.l.tags, strings.Join(t.Tags, ", ")), t.Notes},
		meta: map[string]string{"status": sk, "space": t.Space, "due": b.datePtr(t.Due), "done_at": b.datePtr(t.DoneAt),
			"priority": strconv.Itoa(t.Priority), "tags": strings.Join(t.Tags, ","), "project_id": t.ProjectID, "repeat": t.RRule},
		rels: rels,
	})
}

func clampPrio(p int) int {
	if p < 0 || p > 3 {
		return 0
	}
	return p
}

func (b *builder) event(e store.Event) {
	loc := b.s.Location
	if z, err := time.LoadLocation(e.TimeZone); err == nil && e.TimeZone != "" {
		loc = z
	}
	when := ""
	if e.AllDay {
		when = e.Start.In(loc).Format("2006-01-02") + " " + b.l.allDay
		if last := e.End.Add(-time.Nanosecond).In(loc); last.Format("2006-01-02") != e.Start.In(loc).Format("2006-01-02") {
			when = e.Start.In(loc).Format("2006-01-02") + " – " + last.Format("2006-01-02") + " " + b.l.allDay
		}
	} else {
		when = e.Start.In(loc).Format("2006-01-02 15:04") + " – " + e.End.In(loc).Format("15:04")
		if e.End.In(loc).Format("2006-01-02") != e.Start.In(loc).Format("2006-01-02") {
			when = e.Start.In(loc).Format("2006-01-02 15:04") + " – " + e.End.In(loc).Format("2006-01-02 15:04")
		}
		when += "（" + loc.String() + "）"
	}
	proj, rels := b.projectLine(e.ProjectID)
	b.emit(record{
		kind: KindEvent, id: e.ID, title: b.l.kindEvent + "：" + e.Title, name: e.Title,
		desc:    line(b.l.kindEvent, when),
		content: []string{b.l.kindEvent + "：" + e.Title, line(b.l.when+when, labeled(b.l.repeat, e.RRule), labeled(b.l.location, e.Location), b.l.space+b.space(e.Space)), proj, e.Description},
		meta: map[string]string{"start": e.Start.UTC().Format(time.RFC3339), "end": e.End.UTC().Format(time.RFC3339), "all_day": boolStr(e.AllDay),
			"time_zone": e.TimeZone, "repeat": e.RRule, "location": e.Location, "space": e.Space, "project_id": e.ProjectID},
		rels: rels,
	})
}

// refRE finds noted://kind/id addresses, such as the [@title](noted://task/<id>)
// references the App writes into notes.
var refRE = regexp.MustCompile(`noted://([a-z]+)/([^\s()\[\]<>"'，。；：！？、）】]+)`)

var refKinds = map[string]bool{KindProject: true, KindTask: true, KindEvent: true, KindNote: true, KindGoal: true, KindObjective: true, KindHolding: true, KindAlarm: true}

// References returns the keys of the items a Markdown text links to, in order
// of first appearance, each once.
func References(markdown string) []string {
	var out []string
	for _, m := range refRE.FindAllStringSubmatch(markdown, -1) {
		if !refKinds[m[1]] {
			continue
		}
		id := strings.TrimRight(m[2], ".,;:!?") // a bare link at the end of a sentence
		if u, err := url.PathUnescape(id); err == nil {
			id = u
		}
		if k := key(m[1], id); !contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

func (b *builder) note(n store.Note) {
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = b.l.untitled
	}
	proj, rels := b.projectLine(n.ProjectID)
	for _, r := range References(n.Content) {
		rels = append(rels, rel{to: r, typ: RelReferences})
	}
	status := ""
	if n.Archived {
		status = "archived"
	}
	b.emit(record{
		kind: KindNote, id: n.ID, title: title, name: title,
		desc:    line(b.l.kindNote, labeled(b.l.tags, strings.Join(n.Tags, ", ")), b.l.updated+b.date(n.Updated)),
		content: []string{"# " + title, "", n.Content, "", line(labeled(b.l.tags, strings.Join(n.Tags, ", ")), proj, b.l.space+b.space(n.Space), b.l.updated+b.date(n.Updated))},
		meta: map[string]string{"tags": strings.Join(n.Tags, ","), "project_id": n.ProjectID, "space": n.Space, "pinned": boolStr(n.Pinned),
			"status": status, "updated": n.Updated.UTC().Format(time.RFC3339)},
		rels: rels,
	})
}

func (b *builder) goal(g store.Goal) {
	target := fmt.Sprintf(b.l.goalTarget, b.l.period(g.Period), num(g.Target), g.Unit)
	if g.CounterTarget > 0 {
		target += fmt.Sprintf(b.l.goalCounter, num(g.CounterTarget), g.CounterUnit)
	}
	var rels []rel
	objective := ""
	if k := key(KindObjective, g.ObjectiveID); g.ObjectiveID != "" && b.exist[k] {
		objective = b.l.objective + b.title[k]
		rels = append(rels, rel{to: k, typ: RelDimensionOf})
	}
	proj, prels := b.projectLine(g.ProjectID)
	rels = append(rels, prels...)
	var ms []string
	for _, m := range g.Milestones {
		if m.Done {
			ms = append(ms, m.Title+" ✓")
		} else {
			ms = append(ms, m.Title)
		}
	}
	status := b.l.active
	if g.Archived {
		status = b.l.archived
	}
	b.emit(record{
		kind: KindGoal, id: g.ID, title: b.l.kindGoal + "：" + g.Title, name: g.Title,
		desc:    line(b.l.kindGoal, target, status),
		content: []string{b.l.kindGoal + "：" + g.Title, line(b.l.target+target, b.l.status+status, b.l.space+b.space(g.Space)), objective, proj, labeled(b.l.milestones, strings.Join(ms, ", ")), g.Notes},
		meta: map[string]string{"status": statusKey(g.Archived, "active"), "space": g.Space, "period": g.Period, "target": num(g.Target), "unit": g.Unit,
			"counter_target": numIf(g.CounterTarget), "counter_unit": g.CounterUnit, "objective_id": g.ObjectiveID, "project_id": g.ProjectID},
		rels: rels,
	})
}

func numIf(v float64) string {
	if v == 0 {
		return ""
	}
	return num(v)
}

func (b *builder) objective(v store.ObjectiveView) {
	o := v.Objective
	status := b.l.active
	if o.Archived {
		status = b.l.archived
	}
	metric, snapshot := "", ""
	meta := map[string]string{"status": statusKey(o.Archived, "active"), "space": o.Space, "start": b.datePtr(o.Start), "due": b.datePtr(o.Due), "project_id": o.ProjectID,
		"metric": "", "metric_unit": "", "metric_start": "", "metric_target": "", "metric_current": "", "as_of": "", "derived": ""}
	if o.HasMetric() {
		metric = fmt.Sprintf(b.l.metric, o.MetricName, num(o.MetricStart), num(o.MetricTarget), o.MetricUnit)
		meta["metric"], meta["metric_unit"], meta["metric_start"], meta["metric_target"] = o.MetricName, o.MetricUnit, num(o.MetricStart), num(o.MetricTarget)
		if v.Progress.HasReading && v.Progress.LastReading != nil {
			asOf := b.date(*v.Progress.LastReading)
			snapshot = fmt.Sprintf(b.l.metricSnapshot, asOf, o.MetricName, num(v.Progress.MetricCurrent), o.MetricUnit)
			meta["metric_current"], meta["as_of"], meta["derived"] = num(v.Progress.MetricCurrent), asOf, "true"
		}
	}
	proj, rels := b.projectLine(o.ProjectID)
	b.emit(record{
		kind: KindObjective, id: o.ID, title: b.l.kindObjective + "：" + o.Title, name: o.Title,
		desc:    line(b.l.kindObjective, status, labeled(b.l.due, b.datePtr(o.Due))),
		content: []string{b.l.kindObjective + "：" + o.Title, line(b.l.status+status, b.l.space+b.space(o.Space), labeled(b.l.start, b.datePtr(o.Start)), labeled(b.l.due, b.datePtr(o.Due))), metric, snapshot, proj, o.Notes},
		meta:    meta,
		rels:    rels,
	})
}

func (b *builder) holding(h Holding) {
	status := b.l.active
	if h.Archived {
		status = b.l.archived
	}
	name := h.Symbol
	if h.Name != "" {
		name = h.Symbol + "（" + h.Name + "）"
	}
	meta := map[string]string{"status": statusKey(h.Archived, "active"), "symbol": h.Symbol, "currency": h.Currency,
		"shares": "", "avg_cost": "", "as_of": "", "derived": "", "last_price": "", "last_price_date": ""}
	snapshot := b.l.noTrades
	if h.LastTrade != nil {
		asOf := b.date(*h.LastTrade)
		snapshot = fmt.Sprintf(b.l.position, asOf, h.Symbol, num(h.Position.Shares), money(h.Position.AvgCost))
		meta["shares"], meta["avg_cost"], meta["as_of"], meta["derived"] = num(h.Position.Shares), money(h.Position.AvgCost), asOf, "true"
	}
	price := ""
	if h.LastPrice != nil {
		price = fmt.Sprintf(b.l.lastPrice, num(*h.LastPrice), b.datePtr(h.PriceTime))
		meta["last_price"], meta["last_price_date"] = num(*h.LastPrice), b.datePtr(h.PriceTime)
	}
	dca := ""
	if h.DCADay > 0 {
		dca = fmt.Sprintf(b.l.dca, h.DCADay, num(h.DCAAmount), h.Currency)
	}
	var extra []cortexdb.ToolEntityInput
	var extraRel []cortexdb.ToolRelationInput
	if h.Currency != "" {
		c := currencyEntity(h.Currency)
		extra = append(extra, c)
		extraRel = append(extraRel, cortexdb.ToolRelationInput{From: EntityID(b.s.User, key(KindHolding, h.ID)), To: c.ID, Type: RelHeldIn})
	}
	b.emit(record{
		kind: KindHolding, id: h.ID, title: b.l.kindHolding + "：" + name, name: h.Symbol,
		desc:    line(b.l.kindHolding, h.Name, h.Currency, status),
		content: []string{b.l.kindHolding + "：" + name, line(b.l.currency+h.Currency, b.l.status+status), snapshot, price, dca, h.Notes},
		meta:    meta, extra: extra, extraRel: extraRel,
	})
}

// alarm writes an alarm: its rule in words and, dated, its next rings.
func (b *builder) alarm(a Alarm) {
	loc := b.s.Location
	if z, err := time.LoadLocation(a.Schedule.TimeZone); err == nil && a.Schedule.TimeZone != "" {
		loc = z
	}
	lang := "zh"
	if b.s.Lang == "en" {
		lang = "en"
	}
	rule := alarm.Describe(a.Schedule, lang)
	policy := alarm.DescribePolicy(a.HolidayPolicy, a.Region, lang)
	mode := b.l.modeAlarm
	if a.Mode == alarm.ModeReminder {
		mode = b.l.modeReminder
	}
	status, sk := b.l.alarmOn, "enabled"
	if !a.Enabled {
		status, sk = b.l.alarmOff, "disabled"
	}
	meta := map[string]string{"status": sk, "mode": a.Mode, "space": a.Space, "project_id": a.ProjectID, "schedule_kind": a.Schedule.Kind,
		"freq": "", "time_zone": a.Schedule.TimeZone, "holiday_policy": a.HolidayPolicy, "region": a.Region, "rule": rule,
		"next_fire": "", "next_fires": "", "as_of": "", "derived": ""}
	if a.Schedule.Recurrence != nil {
		meta["freq"] = a.Schedule.Recurrence.Freq
	}
	next := b.l.noNext
	if len(a.NextFires) > 0 {
		var words, stamps []string
		for _, t := range a.NextFires {
			words = append(words, t.In(loc).Format("2006-01-02 15:04"))
			stamps = append(stamps, t.UTC().Format(time.RFC3339))
		}
		asOf := b.date(b.s.Now)
		next = fmt.Sprintf(b.l.nextFires, asOf, strings.Join(words, b.l.listSep))
		meta["next_fire"], meta["next_fires"], meta["as_of"], meta["derived"] = stamps[0], strings.Join(stamps, ","), asOf, "true"
	}
	proj, rels := b.projectLine(a.ProjectID)
	b.emit(record{
		kind: KindAlarm, id: a.ID, title: b.l.kindAlarm + "：" + a.Label, name: a.Label,
		desc: line(b.l.kindAlarm, rule, policy, status),
		content: []string{b.l.kindAlarm + "：" + a.Label,
			line(b.l.rule+rule, labeled(b.l.holidays, policy), b.l.mode+mode, b.l.status+status, b.l.space+b.space(a.Space)),
			next, proj, labeled(b.l.said, a.OriginalText), a.Note},
		meta: meta,
		rels: rels,
	})
}

// weeks writes one record per ISO week with finished time entries.
func (b *builder) weeks() {
	type activity struct {
		label    string
		minutes  int
		projects []string
		goals    []string
	}
	type week struct {
		id       string
		monday   time.Time
		last     time.Time // the end of its latest entry
		minutes  int
		entries  int
		acts     map[string]*activity
		goalMin  map[string]int
		projMin  map[string]int
		actOrder []string
		list     []store.TimeEntry // its entries, by start
	}
	entries := append([]store.TimeEntry(nil), b.s.Time...)
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].Start.Equal(entries[j].Start) {
			return entries[i].Start.Before(entries[j].Start)
		}
		return entries[i].ID < entries[j].ID
	})
	weeks := map[string]*week{}
	var ids []string
	for _, e := range entries {
		if e.Running() || e.Minutes <= 0 {
			continue
		}
		t := e.Start.In(b.s.Location)
		y, w := t.ISOWeek()
		id := fmt.Sprintf("%04d-W%02d", y, w)
		wk := weeks[id]
		if wk == nil {
			day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, b.s.Location)
			off := (int(day.Weekday()) + 6) % 7 // days since Monday
			wk = &week{id: id, monday: day.AddDate(0, 0, -off), acts: map[string]*activity{}, goalMin: map[string]int{}, projMin: map[string]int{}}
			weeks[id] = wk
			ids = append(ids, id)
		}
		wk.minutes += e.Minutes
		wk.entries++
		wk.list = append(wk.list, e)
		if e.End.After(wk.last) {
			wk.last = *e.End
		}
		ak := strings.ToLower(strings.TrimSpace(e.Title))
		a := wk.acts[ak]
		if a == nil {
			a = &activity{label: strings.TrimSpace(e.Title)}
			wk.acts[ak] = a
			wk.actOrder = append(wk.actOrder, ak)
		}
		a.minutes += e.Minutes
		if k := key(KindProject, e.ProjectID); e.ProjectID != "" && b.exist[k] {
			wk.projMin[k] += e.Minutes
			if !contains(a.projects, b.title[k]) {
				a.projects = append(a.projects, b.title[k])
			}
		}
		if k := key(KindGoal, e.GoalID); e.GoalID != "" && b.exist[k] {
			wk.goalMin[k] += e.Minutes
			if !contains(a.goals, b.title[k]) {
				a.goals = append(a.goals, b.title[k])
			}
		}
	}
	for _, id := range ids {
		wk := weeks[id]
		acts := make([]*activity, 0, len(wk.acts))
		for _, k := range wk.actOrder {
			acts = append(acts, wk.acts[k])
		}
		sort.SliceStable(acts, func(i, j int) bool { return acts[i].minutes > acts[j].minutes })
		from, to := wk.monday.Format("2006-01-02"), wk.monday.AddDate(0, 0, 6).Format("2006-01-02")
		name := fmt.Sprintf(b.l.weekName, id)
		content := []string{
			fmt.Sprintf(b.l.weekHead, name, from, to),
			fmt.Sprintf(b.l.weekTotal, wk.minutes, float64(wk.minutes)/60, wk.entries),
		}
		for _, a := range acts {
			content = append(content, "- "+line(fmt.Sprintf(b.l.weekAct, a.label, a.minutes),
				labeled(b.l.project, strings.Join(a.projects, ", ")), labeled(b.l.goal, strings.Join(a.goals, ", "))))
		}
		content = append(content, b.l.weekEntries)
		for _, e := range wk.list {
			content = append(content, "- "+b.entryLine(e))
		}
		content = append(content, b.l.weekFoot)
		var rels []rel
		for _, k := range sortedKeys(wk.goalMin) {
			rels = append(rels, rel{to: k, typ: RelCountToward, meta: map[string]string{"minutes": strconv.Itoa(wk.goalMin[k]), "derived": "true"}})
		}
		for _, k := range sortedKeys(wk.projMin) {
			rels = append(rels, rel{to: k, typ: RelSpentOn, meta: map[string]string{"minutes": strconv.Itoa(wk.projMin[k]), "derived": "true"}})
		}
		b.emit(record{
			kind: KindTime, id: id, title: name, name: name,
			desc:    line(name, fmt.Sprintf(b.l.weekShort, wk.minutes)),
			content: content,
			meta: map[string]string{"week": id, "from": from, "to": to, "minutes": strconv.Itoa(wk.minutes), "entries": strconv.Itoa(wk.entries),
				"derived": "true", "as_of": b.date(wk.last)},
			rels: rels,
		})
	}
}

// entryLine is one finished time entry under its day: "10月13日 周二 · 游泳
// 30 分钟", with its range when the start and end are real: "10月14日 周三
// 13:00–13:30 · 游泳 30 分钟". An entry logged with only minutes has a made-up
// start, so it shows none.
func (b *builder) entryLine(e store.TimeEntry) string {
	start := e.Start.In(b.s.Location)
	day := fmt.Sprintf(b.l.entryDay, start.Format(b.l.dayLayout), b.l.weekdays[start.Weekday()])
	if e.Timed && e.End != nil {
		day += " " + start.Format("15:04") + "–" + e.End.In(b.s.Location).Format("15:04")
	}
	return line(day, fmt.Sprintf(b.l.entryMin, strings.TrimSpace(e.Title), e.Minutes))
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
