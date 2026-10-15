package cortexsync

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liliang-cn/noted/internal/alarm"
	"github.com/liliang-cn/noted/internal/store"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr(t time.Time) *time.Time { return &t }

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// fixture is one of everything, related the way the App relates them.
func fixture(t *testing.T) Snapshot {
	price := 712.5
	return Snapshot{
		User: "apple-b0d825f8", Location: shanghai(t), Lang: "zh",
		Objectives: []store.ObjectiveView{{
			Objective: store.Objective{ID: "o1", Title: "更健康", Space: "life", MetricName: "体重", MetricUnit: "kg", MetricStart: 80, MetricTarget: 70},
			Progress:  store.ObjectiveProgress{HasMetric: true, HasReading: true, MetricCurrent: 75.5, LastReading: ptr(at("2026-10-10T01:00:00Z"))},
		}},
		Projects: []store.Project{{ID: "p1", Title: "日本旅行", Space: "life", Due: ptr(at("2026-11-10T00:00:00Z"))}},
		Goals: []store.Goal{{ID: "g1", Title: "每周跑步", Period: "week", Target: 3, Unit: "次", ObjectiveID: "o1", Space: "life",
			Milestones: []store.Milestone{{Title: "5 公里", Done: true}}}},
		Holdings: []Holding{{
			Holding:   store.Holding{ID: "h1", Symbol: "VOO", Name: "Vanguard S&P 500", Currency: "USD", LastPrice: &price, PriceTime: ptr(at("2026-10-12T08:00:00Z")), DCADay: 3, DCAAmount: 1000},
			Position:  store.Position{Shares: 20, AvgCost: 700.3612},
			LastTrade: ptr(at("2026-10-13T02:00:00Z")),
		}},
		Tasks:  []store.Task{{ID: "t1", Title: "订酒店", ProjectID: "p1", Priority: 3, Due: ptr(at("2026-10-20T00:00:00Z")), Tags: []string{"travel"}, Space: "life"}},
		Events: []store.Event{{ID: "e1", Title: "出发", ProjectID: "p1", Start: at("2026-11-01T01:00:00Z"), End: at("2026-11-01T03:00:00Z"), TimeZone: "Asia/Shanghai"}},
		Notes: []store.Note{{ID: "n1", Title: "行程想法", ProjectID: "p1", Tags: []string{"trip"}, Updated: at("2026-10-12T00:00:00Z"),
			Content: "先 [@订酒店](noted://task/t1)，再看 [@更健康](noted://objective/o1)。旧链接 [@没了](noted://task/gone)。"}},
		Time: []store.TimeEntry{
			{ID: "x1", Title: "Running", Minutes: 30, Start: at("2026-10-06T00:00:00Z"), End: ptr(at("2026-10-06T00:30:00Z")), GoalID: "g1"},
			{ID: "x2", Title: "running", Minutes: 45, Start: at("2026-10-08T00:00:00Z"), End: ptr(at("2026-10-08T00:45:00Z")), GoalID: "g1"},
			{ID: "x3", Title: "Planning", Minutes: 60, Start: at("2026-10-07T00:00:00Z"), End: ptr(at("2026-10-07T01:00:00Z")), ProjectID: "p1"},
		},
	}
}

func itemByKey(t *testing.T, p Projection, k string) Item {
	t.Helper()
	for _, it := range p.Items {
		if it.Key == k {
			return it
		}
	}
	t.Fatalf("no item %s in %v", k, keys(p))
	return Item{}
}

func keys(p Projection) []string {
	var out []string
	for _, it := range p.Items {
		out = append(out, it.Key)
	}
	return out
}

func rels(it Item) []string {
	var out []string
	for _, r := range it.Save.Relations {
		out = append(out, r.Type+" "+r.To)
	}
	return out
}

func TestProjectionKeysIdsAndOrder(t *testing.T) {
	p := Project(fixture(t))
	want := []string{"objective:o1", "project:p1", "goal:g1", "holding:h1", "task:t1", "event:e1", "note:n1", "time:2026-W41"}
	if got := keys(p); !slices.Equal(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for _, it := range p.Items {
		s := it.Save
		if s.KnowledgeID != "noted:apple-b0d825f8:"+it.Key || s.Collection != "noted" || s.SourceURL != Link(it.Key) {
			t.Errorf("%s: id %q collection %q url %q", it.Key, s.KnowledgeID, s.Collection, s.SourceURL)
		}
		own := s.Entities[0]
		if own.ID != "entity:noted:apple-b0d825f8:"+it.Key || own.Type != "noted_"+it.Kind() {
			t.Errorf("%s: entity %q type %q", it.Key, own.ID, own.Type)
		}
		if !slices.Equal(own.ChunkIDs, []string{"chunk:" + s.KnowledgeID + ":000"}) {
			t.Errorf("%s: chunk links %v", it.Key, own.ChunkIDs)
		}
		if own.Metadata["source"] != "noted" || own.Metadata["link"] != s.SourceURL || own.Metadata["noted_id"] == "" {
			t.Errorf("%s: metadata %v", it.Key, own.Metadata)
		}
		if !strings.Contains(s.Content, s.SourceURL) || !strings.Contains(s.Content, "以 noted 为准") {
			t.Errorf("%s: content does not point back to noted:\n%s", it.Key, s.Content)
		}
		if len(p.Entities[it.Key].ChunkIDs) != 0 {
			t.Errorf("%s: the entity carried by other records must not link chunks", it.Key)
		}
	}
	if Link("note:a b/c") != "noted://note/a%20b%2Fc" {
		t.Errorf("link = %s", Link("note:a b/c"))
	}
}

func TestProjectionRelations(t *testing.T) {
	p := Project(fixture(t))
	e := func(k string) string { return "entity:noted:apple-b0d825f8:" + k }
	cases := map[string][]string{
		"task:t1":       {"part_of " + e("project:p1")},
		"event:e1":      {"part_of " + e("project:p1")},
		"goal:g1":       {"dimension_of " + e("objective:o1")},
		"note:n1":       {"part_of " + e("project:p1"), "references " + e("task:t1"), "references " + e("objective:o1")},
		"time:2026-W41": {"count_toward " + e("goal:g1"), "spent_on " + e("project:p1")},
		"holding:h1":    {"held_in entity:noted:currency:usd"},
		"project:p1":    nil,
	}
	for k, want := range cases {
		it := itemByKey(t, p, k)
		if got := rels(it); !slices.Equal(got, want) {
			t.Errorf("%s relations = %v, want %v", k, got, want)
		}
		for _, r := range it.Save.Relations {
			if r.From != e(k) {
				t.Errorf("%s: relation from %s", k, r.From)
			}
		}
	}
	note := itemByKey(t, p, "note:n1")
	if !slices.Equal(note.Targets, []string{"objective:o1", "project:p1", "task:t1"}) {
		t.Errorf("note targets = %v (a link to a deleted item is dropped)", note.Targets)
	}
	h := itemByKey(t, p, "holding:h1")
	if len(h.Save.Entities) != 2 || h.Save.Entities[1].ID != "entity:noted:currency:usd" || h.Save.Entities[1].Name != "USD" {
		t.Errorf("holding entities = %+v", h.Save.Entities)
	}
}

func TestNumbersAreDatedDerivedSnapshots(t *testing.T) {
	p := Project(fixture(t))
	h := itemByKey(t, p, "holding:h1")
	if !strings.Contains(h.Save.Content, "截至 2026-10-13 VOO 20 股，均价 700.36（推算快照，以 noted 为准）") {
		t.Errorf("holding content:\n%s", h.Save.Content)
	}
	if !strings.Contains(h.Save.Content, "最近价格 712.5（2026-10-12 录入") || !strings.Contains(h.Save.Content, "定投：每月 3 日 1000 USD") {
		t.Errorf("holding content:\n%s", h.Save.Content)
	}
	m := h.Save.Entities[0].Metadata
	if m["derived"] != "true" || m["as_of"] != "2026-10-13" || m["shares"] != "20" || m["avg_cost"] != "700.36" {
		t.Errorf("holding metadata = %v", m)
	}
	o := itemByKey(t, p, "objective:o1")
	if !strings.Contains(o.Save.Content, "截至 2026-10-10 体重 75.5 kg（推算快照，以 noted 为准）") {
		t.Errorf("objective content:\n%s", o.Save.Content)
	}
	if m := o.Save.Metadata; m["derived"] != "true" || m["as_of"] != "2026-10-10" || m["metric_current"] != "75.5" {
		t.Errorf("objective metadata = %v", m)
	}
	// A holding without trades states no position at all.
	s := fixture(t)
	s.Holdings[0].LastTrade = nil
	h = itemByKey(t, Project(s), "holding:h1")
	if strings.Contains(h.Save.Content, "股，均价") || h.Save.Metadata["derived"] != "" {
		t.Errorf("no trades, yet:\n%s", h.Save.Content)
	}
}

func TestNoteIsTheFullMarkdown(t *testing.T) {
	p := Project(fixture(t))
	n := itemByKey(t, p, "note:n1")
	if n.Save.Title != "行程想法" || !strings.HasPrefix(n.Save.Content, "# 行程想法\n\n先 [@订酒店](noted://task/t1)") {
		t.Errorf("note: %q\n%s", n.Save.Title, n.Save.Content)
	}
	if !strings.Contains(n.Save.Content, "标签：trip") || !strings.Contains(n.Save.Content, "项目：日本旅行") {
		t.Errorf("note lacks tags or project:\n%s", n.Save.Content)
	}
	if n.Save.Metadata["tags"] != "trip" || n.Save.Metadata["project_id"] != "p1" {
		t.Errorf("note metadata = %v", n.Save.Metadata)
	}
}

func TestProjectionIsStableAndChangesLocally(t *testing.T) {
	a, b := Project(fixture(t)), Project(fixture(t))
	for i := range a.Items {
		if a.Items[i].Hash != b.Items[i].Hash {
			t.Fatalf("%s hashes differ between identical snapshots", a.Items[i].Key)
		}
	}
	s := fixture(t)
	s.Notes[0].Content += "\n补一句"
	c := Project(s)
	for i := range a.Items {
		changed := a.Items[i].Hash != c.Items[i].Hash
		if changed != (a.Items[i].Key == "note:n1") {
			t.Errorf("%s changed = %v after editing the note", a.Items[i].Key, changed)
		}
	}
}

func TestEnglishLabels(t *testing.T) {
	s := fixture(t)
	s.Lang = "en"
	p := Project(s)
	if c := itemByKey(t, p, "holding:h1").Save.Content; !strings.Contains(c, "As of 2026-10-13: VOO 20 shares, average cost 700.36 (derived snapshot") {
		t.Errorf("holding:\n%s", c)
	}
	if c := itemByKey(t, p, "goal:g1").Save.Content; !strings.Contains(c, "3 次 per week") {
		t.Errorf("goal:\n%s", c)
	}
}

func TestReferences(t *testing.T) {
	md := `See [@Book \[hotel\]](noted://task/t%201) and [@trip](noted://project/p1).
Again [@trip](noted://project/p1), a bare noted://note/n2, a pairing link noted://pair?code=X,
an unknown noted://widget/w1 and [@x](noted://goal/g-9_a.b~c).
见 noted://event/e7。还有 noted://holding/h8，完。`
	got := References(md)
	want := []string{"task:t 1", "project:p1", "note:n2", "goal:g-9_a.b~c", "event:e7", "holding:h8"}
	if !slices.Equal(got, want) {
		t.Fatalf("References = %v, want %v", got, want)
	}
	if References("no links here") != nil {
		t.Fatal("want nil")
	}
}

func TestWeeklyTimeSummaries(t *testing.T) {
	loc := shanghai(t)
	local := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	entry := func(id, title string, start string, min int, goal, project string) store.TimeEntry {
		st := local(start)
		end := st.Add(time.Duration(min) * time.Minute)
		return store.TimeEntry{ID: id, Title: title, Minutes: min, Start: st, End: &end, GoalID: goal, ProjectID: project}
	}
	timed := func(e store.TimeEntry) store.TimeEntry { e.Timed = true; return e }
	s := Snapshot{User: "u", Location: loc, Lang: "zh",
		Projects: []store.Project{{ID: "p1", Title: "noted"}},
		Goals:    []store.Goal{{ID: "g1", Title: "写作", Period: "week", Target: 5}},
		Time: []store.TimeEntry{
			// Sunday night and Monday morning in Shanghai: two ISO weeks,
			// although both are Sunday in UTC.
			entry("a", "写代码", "2026-10-11 23:00", 40, "", "p1"),
			entry("b", "写代码", "2026-10-12 00:30", 20, "", "p1"),
			entry("c", "写作", "2026-10-13 09:00", 50, "g1", ""),
			timed(entry("d", "写代码", "2026-10-14 09:00", 30, "g1", "p1")),
			entry("e", "Ghost", "2026-10-14 10:00", 30, "deleted-goal", ""), // links to nothing that exists
		},
	}
	running := store.TimeEntry{ID: "r", Title: "写代码", Start: local("2026-10-14 11:00"), Timed: true}
	s.Time = append(s.Time, running)
	p := Project(s)
	var weeks []string
	for _, it := range p.Items {
		if it.Kind() == KindTime {
			weeks = append(weeks, it.Key)
		}
	}
	if !slices.Equal(weeks, []string{"time:2026-W41", "time:2026-W42"}) {
		t.Fatalf("weeks = %v", weeks)
	}
	w41 := itemByKey(t, p, "time:2026-W41")
	if !strings.Contains(w41.Save.Content, "合计 40 分钟") || w41.Save.Metadata["from"] != "2026-10-05" || w41.Save.Metadata["to"] != "2026-10-11" {
		t.Errorf("W41:\n%s\n%v", w41.Save.Content, w41.Save.Metadata)
	}
	w42 := itemByKey(t, p, "time:2026-W42")
	c := w42.Save.Content
	for _, want := range []string{
		"时间记录 2026-W42（2026-10-12 至 2026-10-18）",
		"合计 130 分钟（2.2 小时），4 条记录",
		"- 写作：50 分钟 · 目标：写作",
		"- 写代码：50 分钟 · 项目：noted · 目标：写作",
		"- Ghost：30 分钟",
		"明细：\n- 10月12日 周一 · 写代码 20 分钟\n- 10月13日 周二 · 写作 50 分钟\n- 10月14日 周三 09:00–09:30 · 写代码 30 分钟\n- 10月14日 周三 · Ghost 30 分钟\n",
		"推算快照，以 noted 为准",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("W42 lacks %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "11:00") {
		t.Errorf("a running timer is not an entry yet:\n%s", c)
	}

	// The same week in English.
	s.Lang = "en"
	c = itemByKey(t, Project(s), "time:2026-W42").Save.Content
	for _, want := range []string{
		"Time log 2026-W42 (2026-10-12 to 2026-10-18)",
		"Entries:\n- Mon Oct 12 · 写代码 20 min\n- Tue Oct 13 · 写作 50 min\n- Wed Oct 14 09:00–09:30 · 写代码 30 min\n- Wed Oct 14 · Ghost 30 min\n",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("W42 (en) lacks %q:\n%s", want, c)
		}
	}
	if m := w42.Save.Metadata; m["minutes"] != "130" || m["derived"] != "true" || m["as_of"] != "2026-10-14" || m["entries"] != "4" {
		t.Errorf("W42 metadata = %v", m)
	}
	got := map[string]string{}
	for _, r := range w42.Save.Relations {
		got[r.Type+" "+r.To] = r.Metadata["minutes"]
	}
	want := map[string]string{"count_toward entity:noted:u:goal:g1": "80", "spent_on entity:noted:u:project:p1": "50"}
	if len(got) != len(want) {
		t.Fatalf("W42 relations = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("W42 relation %s minutes = %q, want %s", k, got[k], v)
		}
	}
}

func TestDiff(t *testing.T) {
	items := []Item{{Key: "note:a", Hash: "1"}, {Key: "note:b", Hash: "2"}, {Key: "task:c", Hash: "3"}}
	ledger := map[string]string{"note:a": "1", "note:b": "old", "task:gone": "9", "event:gone": "8"}
	ups, dels, unchanged := Diff(items, ledger, false)
	var upKeys []string
	for _, it := range ups {
		upKeys = append(upKeys, it.Key)
	}
	if !slices.Equal(upKeys, []string{"note:b", "task:c"}) || !slices.Equal(dels, []string{"event:gone", "task:gone"}) || unchanged != 1 {
		t.Fatalf("ups %v dels %v unchanged %d", upKeys, dels, unchanged)
	}
	ups, dels, unchanged = Diff(items, ledger, true)
	if len(ups) != 3 || len(dels) != 2 || unchanged != 0 {
		t.Fatalf("full: ups %d dels %v unchanged %d", len(ups), dels, unchanged)
	}
	ups, dels, _ = Diff(nil, map[string]string{}, false)
	if ups != nil || dels != nil {
		t.Fatal("nothing to do")
	}
}

// withAlarms is the fixture plus two alarms: a biweekly one in the project,
// and one that is off.
func withAlarms(t *testing.T) Snapshot {
	s := fixture(t)
	s.Now = at("2026-10-13T02:00:00Z")
	on := store.Alarm{ID: "a1", Label: "接三宝", Mode: alarm.ModeAlarm, Enabled: true, ProjectID: "p1", Space: "life",
		HolidayPolicy: alarm.PolicyWorkdayOnly, Region: "CN", OriginalText: "每隔一周的周一下午三点一刻接三宝",
		Schedule: alarm.Schedule{Kind: alarm.KindRecurring, Anchor: "2026-10-12", Time: "15:15", TimeZone: "Asia/Shanghai",
			Recurrence: &alarm.Recurrence{Freq: alarm.FreqWeekly, Interval: 2, ByDay: []string{"MON"}}}}
	off := store.Alarm{ID: "a2", Label: "喝水", Mode: alarm.ModeReminder, Space: "life", HolidayPolicy: alarm.PolicyNone,
		Schedule: alarm.Schedule{Kind: alarm.KindRecurring, Anchor: "2026-10-13", Time: "10:00", TimeZone: "Asia/Shanghai",
			Recurrence: &alarm.Recurrence{Freq: alarm.FreqDaily, Interval: 1}}}
	s.Alarms = []Alarm{{Alarm: on, NextFires: on.NextFires(s.Now, 3)}, {Alarm: off, NextFires: off.NextFires(s.Now, 3)}}
	return s
}

func TestAlarmProjection(t *testing.T) {
	p := Project(withAlarms(t))
	want := []string{"objective:o1", "project:p1", "goal:g1", "holding:h1", "task:t1", "event:e1", "note:n1", "alarm:a1", "alarm:a2", "time:2026-W41"}
	if got := keys(p); !slices.Equal(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	a := itemByKey(t, p, "alarm:a1")
	if a.Save.Title != "闹钟：接三宝" || a.Save.Entities[0].Type != "noted_alarm" || a.Save.SourceURL != "noted://alarm/a1" {
		t.Fatalf("alarm record: %q %q %q", a.Save.Title, a.Save.Entities[0].Type, a.Save.SourceURL)
	}
	for _, w := range []string{
		"闹钟：接三宝",
		"规则：每隔一周的周一 15:15 · 节假日：仅工作日（按法定节假日和调休补班） · 方式：闹钟 · 状态：已开启 · 空间：生活",
		"下次：截至 2026-10-13，接下来 2026-10-26 15:15、2026-11-09 15:15、2026-11-23 15:15（推算快照，以 noted 为准）",
		"项目：日本旅行",
		"原话：每隔一周的周一下午三点一刻接三宝",
	} {
		if !strings.Contains(a.Save.Content, w) {
			t.Errorf("alarm lacks %q:\n%s", w, a.Save.Content)
		}
	}
	m := a.Save.Metadata
	if m["derived"] != "true" || m["as_of"] != "2026-10-13" || m["next_fire"] != "2026-10-26T07:15:00Z" || m["status"] != "enabled" ||
		m["freq"] != "weekly" || m["holiday_policy"] != "workdayOnly" || m["region"] != "CN" || m["rule"] != "每隔一周的周一 15:15" {
		t.Errorf("alarm metadata = %v", m)
	}
	if got := rels(a); !slices.Equal(got, []string{"part_of entity:noted:apple-b0d825f8:project:p1"}) {
		t.Errorf("alarm relations = %v", got)
	}
	// Off: no rings, so no dated snapshot to go stale.
	off := itemByKey(t, p, "alarm:a2")
	if !strings.Contains(off.Save.Content, "方式：提醒 · 状态：已关闭") || !strings.Contains(off.Save.Content, "暂无下次响铃") ||
		off.Save.Metadata["derived"] != "" || off.Save.Metadata["as_of"] != "" {
		t.Errorf("disabled alarm:\n%s\n%v", off.Save.Content, off.Save.Metadata)
	}
	// The same rings a day later change nothing but the date.
	s := withAlarms(t)
	s.Now = s.Now.Add(time.Hour)
	if b := itemByKey(t, Project(s), "alarm:a1"); b.Hash != a.Hash || itemByKey(t, Project(s), "alarm:a2").Hash != off.Hash {
		t.Error("an hour later with the same rings, the alarm changed")
	}
	// A note can link to an alarm.
	if got := References("见 noted://alarm/a1"); !slices.Equal(got, []string{"alarm:a1"}) {
		t.Errorf("References = %v", got)
	}

	s = withAlarms(t)
	s.Lang = "en"
	if c := itemByKey(t, Project(s), "alarm:a1").Save.Content; !strings.Contains(c, "Rule: every other week on Mon at 15:15 · Holidays: working days only") ||
		!strings.Contains(c, "Next: as of 2026-10-13, 2026-10-26 15:15, 2026-11-09 15:15") {
		t.Errorf("English alarm:\n%s", c)
	}
}
