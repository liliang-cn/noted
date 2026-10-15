package alarm

import (
	"fmt"
	"strings"

	"github.com/liliang-cn/noted/internal/holiday"
)

var (
	dayZH = map[string]string{"MON": "一", "TUE": "二", "WED": "三", "THU": "四", "FRI": "五", "SAT": "六", "SUN": "日"}
	dayEN = map[string]string{"MON": "Mon", "TUE": "Tue", "WED": "Wed", "THU": "Thu", "FRI": "Fri", "SAT": "Sat", "SUN": "Sun"}
)

// Describe says when a schedule rings in a few words, in lang "zh" (default)
// or "en": 每隔一周的周一 15:15, 每天 08:00、12:00、18:00.
func Describe(s Schedule, lang string) string {
	en := lang == "en"
	if s.Kind == KindOneShot {
		if s.At == nil {
			return ""
		}
		at := s.At.Format("2006-01-02 15:04")
		if en {
			return "once at " + at
		}
		return at + "（一次）"
	}
	rc := s.Recurrence
	if rc == nil {
		return ""
	}
	n := max(rc.Interval, 1)
	at := s.Time
	if len(rc.ByTime) > 0 {
		sep := "、"
		if en {
			sep = ", "
		}
		at = strings.Join(rc.ByTime, sep)
	}
	var out string
	switch rc.Freq {
	case FreqMinutely, FreqHourly:
		end := rc.EndTime
		if end == "" {
			end = DefaultEndTime
		}
		if en {
			u := "minutes"
			if rc.Freq == FreqHourly {
				u = "hours"
			}
			out = fmt.Sprintf("every %d %s from %s to %s", n, u, s.Time, end)
		} else {
			u := "分钟"
			if rc.Freq == FreqHourly {
				u = "小时"
			}
			out = fmt.Sprintf("每 %d %s（%s–%s）", n, u, s.Time, end)
		}
		if rc.Until == "" && rc.Count == 0 {
			if en {
				out += " on " + s.Anchor + " only"
			} else {
				out += "，仅 " + s.Anchor + " 当天"
			}
		}
	case FreqDaily:
		switch {
		case en && n == 1:
			out = "every day at " + at
		case en:
			out = fmt.Sprintf("every %d days at %s", n, at)
		case n == 1:
			out = "每天 " + at
		case n == 2:
			out = "每隔一天 " + at
		default:
			out = fmt.Sprintf("每 %d 天 %s", n, at)
		}
	case FreqWeekly:
		var days []string
		for _, d := range rc.ByDay {
			if en {
				days = append(days, dayEN[d])
			} else {
				days = append(days, dayZH[d])
			}
		}
		switch {
		case en && n == 1:
			out = fmt.Sprintf("every %s at %s", strings.Join(days, ", "), at)
		case en && n == 2:
			out = fmt.Sprintf("every other week on %s at %s", strings.Join(days, ", "), at)
		case en:
			out = fmt.Sprintf("every %d weeks on %s at %s", n, strings.Join(days, ", "), at)
		case n == 1:
			out = "每周" + strings.Join(days, "、") + " " + at
		case n == 2:
			out = "每隔一周的周" + strings.Join(days, "、") + " " + at
		default:
			out = fmt.Sprintf("每 %d 周的周%s %s", n, strings.Join(days, "、"), at)
		}
	case FreqMonthly:
		switch {
		case en && n == 1:
			out = fmt.Sprintf("day %d of every month at %s", rc.ByMonthDay, at)
		case en:
			out = fmt.Sprintf("day %d of every %d months at %s", rc.ByMonthDay, n, at)
		case n == 1:
			out = fmt.Sprintf("每月 %d 日 %s", rc.ByMonthDay, at)
		default:
			out = fmt.Sprintf("每 %d 个月的 %d 日 %s", n, rc.ByMonthDay, at)
		}
	}
	if rc.Until != "" {
		if en {
			out += ", until " + rc.Until
		} else {
			out += "，到 " + rc.Until + " 为止"
		}
	}
	if rc.Count > 0 {
		if en {
			out += fmt.Sprintf(", %d times", rc.Count)
		} else {
			out += fmt.Sprintf("，共 %d 次", rc.Count)
		}
	}
	return out
}

// DescribePolicy says what a holiday policy does in the region; "" for none.
func DescribePolicy(policy, region, lang string) string {
	en := lang == "en"
	cn := region == holiday.RegionCN
	switch policy {
	case PolicySkipHoliday:
		switch {
		case en && cn:
			return "skips statutory holidays"
		case en:
			return "skips holidays (no holiday data outside CN: rings as usual)"
		case cn:
			return "法定节假日不响"
		}
		return "节假日不响（无当地节假日数据，照常响）"
	case PolicyWorkdayOnly, PolicyFollowStatutory:
		switch {
		case en && cn:
			return "working days only (statutory holidays and make-up workdays)"
		case en:
			return "Monday to Friday only"
		case cn:
			return "仅工作日（按法定节假日和调休补班）"
		}
		return "仅周一至周五"
	}
	return ""
}
