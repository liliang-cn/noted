package plan

import (
	"fmt"
	"time"
)

// Locale chooses the language of the sentences proposals carry.
type Locale string

const (
	ZH Locale = "zh"
	EN Locale = "en"
)

var texts = map[string]map[Locale]string{
	"goal.title":     {ZH: "%s还差 %s,%s 有空档", EN: "%s is %s short; %s is free"},
	"goal.reason":    {ZH: "这个周期已过去一部分,进度落在节奏后面。", EN: "This period is partly gone and progress is behind pace."},
	"goal.op":        {ZH: "加入日历:%s %s", EN: "Add to calendar: %s %s"},
	"dates.title":    {ZH: "「%s」有 %d 项待办没有日期", EN: "%q has %d tasks with no due date"},
	"dates.reason":   {ZH: "项目还有 %d 天;建议提前完成,留出余量。", EN: "The project has %d days left; finishing early leaves slack."},
	"dates.op":       {ZH: "「%s」设为 %s", EN: "Set %q to %s"},
	"conflict.title": {ZH: "「%s」和「%s」时间重叠", EN: "%q overlaps %q"},
	"conflict.op":    {ZH: "把「%s」移到 %s", EN: "Move %q to %s"},
	"overdue.title":  {ZH: "「%s」已逾期 %d 天", EN: "%q is %d days overdue"},
	"overdue.op":     {ZH: "改到 %s", EN: "Move to %s"},
	"sched.title":    {ZH: "把 %d 项待办排进空档", EN: "Schedule %d tasks into free time"},
	"sched.op":       {ZH: "「%s」%s", EN: "%q %s"},
	"sched.none":     {ZH: "接下来几天没有足够的空档", EN: "There is not enough free time in the next few days"},
	"week.title":     {ZH: "下周建议", EN: "Next week"},
	"week.carry":     {ZH: "顺延「%s」到下周一", EN: "Carry %q to Monday"},
	"plan.title":     {ZH: "拟创建「%s」和 %d 项待办", EN: "Create %q with %d tasks"},
	"plan.titleNP":   {ZH: "拟创建 %d 项待办", EN: "Create %d tasks"},
	"plan.start":     {ZH: "出发日期", EN: "Start date"},
	"plan.reason":    {ZH: "按你说的拆出来的,日期是建议,可以改。", EN: "Broken down from what you said; the dates are suggestions."},
	"plan.rel":       {ZH: "出发前 %d 天", EN: "%d days before the start"},
	"extract.title":  {ZH: "笔记里有 %d 件事", EN: "%d things in this note"},
	"extract.op":     {ZH: "加入待办:%s", EN: "Add task: %s"},
	"ask.title":      {ZH: "助手准备做 %d 项改动", EN: "The assistant would make %d changes"},
	"week.reason":    {ZH: "本周没做完的事和没达标的目标。", EN: "What was left undone this week and the goals that fell short."},
}

func (l Locale) t(key string, a ...any) string {
	m := texts[key]
	f, ok := m[l]
	if !ok {
		f = m[EN]
	}
	return fmt.Sprintf(f, a...)
}

var zhWeekdays = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// when formats a moment for people: "10月10日 周六 09:00" or "Oct 10 Sat 09:00".
func (l Locale) when(t time.Time) string {
	if l == ZH {
		return fmt.Sprintf("%d月%d日 %s %s", int(t.Month()), t.Day(), zhWeekdays[t.Weekday()], t.Format("15:04"))
	}
	return t.Format("Jan 2 Mon 15:04")
}

func (l Locale) span(a, b time.Time) string {
	return l.when(a) + "–" + b.Format("15:04")
}
