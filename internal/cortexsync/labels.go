package cortexsync

// labels are the words the records are written in, following the server's
// language setting.
type labels struct {
	kindProject, kindTask, kindEvent, kindNote, kindGoal, kindObjective, kindHolding, kindAlarm string

	status, space, start, due, priority, progress, repeat, when, location, tags, updated string
	project, goal, objective, milestones, target, currency                               string
	active, archived, open, done, work, life, allDay, untitled                           string
	prio                                                                                 [4]string
	day, week, month                                                                     string

	goalTarget, goalCounter       string // period, number, unit; number, unit
	metric, metricSnapshot        string // name, start, target, unit; date, name, value, unit
	position, noTrades            string // date, symbol, shares, average cost
	lastPrice, dca                string // price, date; day, amount, currency
	weekName, weekHead            string // week id; name, from, to
	weekTotal, weekAct, weekShort string
	weekFoot                      string
	weekEntries                   string    // heading over the entries, day by day
	entryDay                      string    // date, weekday: 10月13日 周二
	dayLayout                     string    // Go layout of the date in entryDay
	weekdays                      [7]string // Sunday first, like time.Weekday
	entryMin                      string    // activity, minutes
	rule, holidays, mode, said    string    // alarm lines
	modeAlarm, modeReminder       string
	alarmOn, alarmOff, noNext     string
	nextFires, listSep            string // date, times
	source                        string // link
}

func (l labels) period(p string) string {
	switch p {
	case "day":
		return l.day
	case "month":
		return l.month
	}
	return l.week
}

func labelsFor(lang string) labels {
	if lang == "en" {
		return labelsEN
	}
	return labelsZH
}

var labelsZH = labels{
	kindProject: "项目", kindTask: "待办", kindEvent: "日程", kindNote: "笔记", kindGoal: "目标", kindObjective: "大目标", kindHolding: "标的", kindAlarm: "闹钟",
	status: "状态：", space: "空间：", start: "开始：", due: "截止：", priority: "优先级：", progress: "进度：", repeat: "重复：",
	when: "时间：", location: "地点：", tags: "标签：", updated: "更新于 ",
	project: "项目：", goal: "目标：", objective: "属于大目标：", milestones: "里程碑：", target: "目标值：", currency: "币种：",
	active: "进行中", archived: "已归档", open: "未完成", done: "已完成", work: "工作", life: "生活", allDay: "全天", untitled: "无标题",
	prio: [4]string{"", "低", "中", "高"},
	day:  "每天", week: "每周", month: "每月",
	goalTarget:     "%s %s %s",
	goalCounter:    "，累计 %s %s",
	metric:         "指标：%s 从 %s 到 %s %s",
	metricSnapshot: "截至 %s %s %s %s（推算快照，以 noted 为准）",
	position:       "截至 %s %s %s 股，均价 %s（推算快照，以 noted 为准）",
	noTrades:       "尚无交易记录",
	lastPrice:      "最近价格 %s（%s 录入，以 noted 为准）",
	dca:            "定投：每月 %d 日 %s %s",
	weekName:       "时间记录 %s",
	weekHead:       "%s（%s 至 %s）",
	weekTotal:      "合计 %d 分钟（%.1f 小时），%d 条记录",
	weekAct:        "%s：%d 分钟",
	weekShort:      "合计 %d 分钟",
	weekFoot:       "（由 noted 的时间记录按周汇总，推算快照，以 noted 为准）",
	weekEntries:    "明细：",
	entryDay:       "%s %s",
	dayLayout:      "1月2日",
	weekdays:       [7]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"},
	entryMin:       "%s %d 分钟",
	source:         "来源：%s（数据以 noted 为准）",
	rule:           "规则：", holidays: "节假日：", mode: "方式：", said: "原话：",
	modeAlarm: "闹钟", modeReminder: "提醒", alarmOn: "已开启", alarmOff: "已关闭", noNext: "暂无下次响铃",
	nextFires: "下次：截至 %s，接下来 %s（推算快照，以 noted 为准）", listSep: "、",
}

var labelsEN = labels{
	kindProject: "Project", kindTask: "To-do", kindEvent: "Event", kindNote: "Note", kindGoal: "Goal", kindObjective: "Objective", kindHolding: "Holding", kindAlarm: "Alarm",
	status: "Status: ", space: "Space: ", start: "Start: ", due: "Due: ", priority: "Priority: ", progress: "Progress: ", repeat: "Repeats: ",
	when: "When: ", location: "Location: ", tags: "Tags: ", updated: "Updated ",
	project: "Project: ", goal: "Goal: ", objective: "Part of objective: ", milestones: "Milestones: ", target: "Target: ", currency: "Currency: ",
	active: "active", archived: "archived", open: "open", done: "done", work: "work", life: "life", allDay: "all day", untitled: "Untitled",
	prio: [4]string{"", "low", "medium", "high"},
	day:  "per day", week: "per week", month: "per month",
	goalTarget:     "%[2]s %[3]s %[1]s",
	goalCounter:    ", %s %s in total",
	metric:         "Metric: %s from %s to %s %s",
	metricSnapshot: "As of %s: %s %s %s (derived snapshot; noted is authoritative)",
	position:       "As of %s: %s %s shares, average cost %s (derived snapshot; noted is authoritative)",
	noTrades:       "No trades yet",
	lastPrice:      "Last price %s (entered %s; noted is authoritative)",
	dca:            "Plan: buy on day %d of each month, %s %s",
	weekName:       "Time log %s",
	weekHead:       "%s (%s to %s)",
	weekTotal:      "Total %d minutes (%.1f hours), %d entries",
	weekAct:        "%s: %d minutes",
	weekShort:      "%d minutes in total",
	weekFoot:       "(Summed per week from noted's time entries; derived snapshot, noted is authoritative)",
	weekEntries:    "Entries:",
	entryDay:       "%[2]s %[1]s",
	dayLayout:      "Jan 2",
	weekdays:       [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
	entryMin:       "%s %d min",
	source:         "Source: %s (noted is authoritative)",
	rule:           "Rule: ", holidays: "Holidays: ", mode: "Rings as: ", said: "In the user's words: ",
	modeAlarm: "alarm", modeReminder: "reminder", alarmOn: "on", alarmOff: "off", noNext: "No upcoming rings",
	nextFires: "Next: as of %s, %s (derived snapshot; noted is authoritative)", listSep: ", ",
}
