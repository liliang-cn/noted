import Foundation
import SwiftProtobuf

// Conveniences on the server's messages, shared by every screen.
//
// Conventions the server does not encode itself:
// - A task due at exactly 00:00 local time is a date-only task ("全天 · 未定时"); any other
//   due time is a timed task. Create date-only tasks with `day.startOfDay`.
// - A task's reminder is stored as an absolute remind_time; the UI shows it as a lead before the
//   due time (ReminderLead).
// - A task repeats with an RRULE like an event's (it needs a due time); completing one keeps it
//   done and the server creates the next.

// MARK: - tasks

extension Noted_V1_Task {
    /// The due date and time, nil when the task has none.
    var due: Date? { hasDueTime ? dueTime.date : nil }
    /// Due at a clock time rather than just on a day.
    var hasClockTime: Bool { hasDueTime && dueTime.date.minuteOfDay != 0 }
    var repeatRule: RepeatRule { RepeatRule(rrule: rrule) }
    var repeats: Bool { !rrule.isEmpty }
    /// Open and due on an earlier day, or today at a time that has passed.
    func isOverdue(now: Date = .now) -> Bool {
        guard !completed, let d = due else { return false }
        if d.startOfDay < now.startOfDay { return true }
        return hasClockTime && d.isSameDay(as: now) && d < now
    }
    /// "!", "!!", "!!!" or "" for the priority.
    var priorityMarks: String {
        switch priority {
        case .low: "!"
        case .medium: "!!"
        case .high: "!!!"
        default: ""
        }
    }
    /// Has work logged on it (shows in 进行中).
    var isInProgress: Bool { !completed && workSessions > 0 }
    /// The reminder as a lead before the due time.
    var reminderLead: ReminderLead {
        guard hasRemindTime, let d = due else { return .none }
        return ReminderLead(minutesBefore: Int((d.timeIntervalSince(remindTime.date) / 60).rounded()))
    }
    /// Sets remind_time from a lead before the current due time (clears it without a due time).
    mutating func setReminder(_ lead: ReminderLead) {
        if let d = due, let m = lead.minutes {
            remindTime = Google_Protobuf_Timestamp(date: d.addingTimeInterval(-Double(m) * 60))
        } else {
            clearRemindTime()
        }
    }
}

extension Noted_V1_Priority {
    var label: String {
        switch self {
        case .low: "低"
        case .medium: "中"
        case .high: "高"
        default: "无"
        }
    }
    static let choices: [Noted_V1_Priority] = [.unspecified, .low, .medium, .high]
}

// MARK: - events

extension Noted_V1_Event {
    var start: Date { startTime.date }
    var end: Date { endTime.date }
    var repeatRule: RepeatRule { RepeatRule(rrule: rrule) }
    var reminderLead: ReminderLead { hasRemindBeforeMinutes ? ReminderLead(minutesBefore: Int(remindBeforeMinutes)) : .none }
    mutating func setReminder(_ lead: ReminderLead) {
        if let m = lead.minutes { remindBeforeMinutes = Int32(m) } else { clearRemindBeforeMinutes() }
    }
}

extension Noted_V1_Occurrence {
    var start: Date { startTime.date }
    var end: Date { endTime.date }
    /// Unique per instance of a recurring event.
    var key: String { "\(event.id)@\(Int(startTime.seconds))" }
    func isPast(now: Date = .now) -> Bool { !event.allDay && end <= now }
    func isHappening(now: Date = .now) -> Bool { !event.allDay && start <= now && end > now }
    var minutes: Int { max(0, Int(end.timeIntervalSince(start) / 60)) }
}

/// 提醒: none, at the time, or a lead before it.
enum ReminderLead: Hashable, CaseIterable, Identifiable, Sendable {
    case none, atTime, min5, min15, min30, hour1, day1, custom(Int)

    static let allCases: [ReminderLead] = [.none, .atTime, .min5, .min15, .min30, .hour1, .day1]
    var id: String { label }

    init(minutesBefore m: Int) {
        switch m {
        case 0: self = .atTime
        case 5: self = .min5
        case 15: self = .min15
        case 30: self = .min30
        case 60: self = .hour1
        case 1440: self = .day1
        default: self = m < 0 ? .none : .custom(m)
        }
    }

    var minutes: Int? {
        switch self {
        case .none: nil
        case .atTime: 0
        case .min5: 5
        case .min15: 15
        case .min30: 30
        case .hour1: 60
        case .day1: 1440
        case .custom(let m): m
        }
    }

    var label: String {
        switch self {
        case .none: "无"
        case .atTime: "准时"
        case .min5: "提前 5 分钟"
        case .min15: "提前 15 分钟"
        case .min30: "提前 30 分钟"
        case .hour1: "提前 1 小时"
        case .day1: "提前 1 天"
        case .custom(let m): m % 60 == 0 ? "提前 \(m / 60) 小时" : "提前 \(m) 分钟"
        }
    }
}

/// 重复, as the RRULE subset the server takes: FREQ=DAILY|WEEKLY|MONTHLY|YEARLY, BYDAY for weekly.
struct RepeatRule: Hashable, Sendable {
    enum Frequency: String, CaseIterable, Sendable {
        case never, daily, weekly, monthly, yearly
        var label: String {
            switch self {
            case .never: "永不"
            case .daily: "每天"
            case .weekly: "每周"
            case .monthly: "每月"
            case .yearly: "每年"
            }
        }
    }

    var frequency: Frequency = .never
    /// Calendar weekdays (1 = Sunday … 7 = Saturday), for weekly rules.
    var weekdays: Set<Int> = []

    /// Monday-first order for weekday pickers.
    static let weekOrder = [2, 3, 4, 5, 6, 7, 1]
    private static let codes = [1: "SU", 2: "MO", 3: "TU", 4: "WE", 5: "TH", 6: "FR", 7: "SA"]

    init(frequency: Frequency = .never, weekdays: Set<Int> = []) {
        self.frequency = frequency
        self.weekdays = weekdays
    }

    init(rrule: String) {
        var parts: [String: String] = [:]
        for p in rrule.uppercased().replacingOccurrences(of: "RRULE:", with: "").split(separator: ";") {
            let kv = p.split(separator: "=", maxSplits: 1)
            if kv.count == 2 { parts[String(kv[0])] = String(kv[1]) }
        }
        switch parts["FREQ"] {
        case "DAILY": frequency = .daily
        case "WEEKLY": frequency = .weekly
        case "MONTHLY": frequency = .monthly
        case "YEARLY": frequency = .yearly
        default: frequency = .never
        }
        if let by = parts["BYDAY"] {
            let back = Dictionary(uniqueKeysWithValues: Self.codes.map { ($1, $0) })
            weekdays = Set(by.split(separator: ",").compactMap { back[String($0.suffix(2))] })
        }
    }

    var rrule: String {
        switch frequency {
        case .never: return ""
        case .daily: return "FREQ=DAILY"
        case .monthly: return "FREQ=MONTHLY"
        case .yearly: return "FREQ=YEARLY"
        case .weekly:
            let days = Self.weekOrder.filter { weekdays.contains($0) }.compactMap { Self.codes[$0] }
            return days.isEmpty ? "FREQ=WEEKLY" : "FREQ=WEEKLY;BYDAY=" + days.joined(separator: ",")
        }
    }

    /// "永不", "每天", "每周五", "每周一、三".
    var label: String {
        guard frequency == .weekly, !weekdays.isEmpty else { return frequency.label }
        return "每周" + Self.weekOrder.filter { weekdays.contains($0) }.map(Fmt.weekdayChar).joined(separator: "、")
    }
}

// MARK: - goals

extension Noted_V1_Goal {
    /// Counted in minutes rather than times.
    var isMinutes: Bool { ["min", "mins", "minute", "minutes", "分钟", "分"].contains(unit.lowercased()) }
    /// "分钟" or "次".
    var unitLabel: String { isMinutes ? "分钟" : (unit.isEmpty || unit == "times" ? "次" : unit) }
    var done: Double { progress.done }
    var goalTarget: Double { progress.target > 0 ? progress.target : target }
    var isAchieved: Bool { progress.achieved || (goalTarget > 0 && done >= goalTarget) }
    /// 0…1 for rings and bars.
    var fraction: Double { goalTarget > 0 ? min(done / goalTarget, 1) : 0 }
    var hasCounter: Bool { counterTarget > 0 }
    /// What one tap of the check-in button adds: 1, or up to 30 minutes.
    var quickAmount: Double { isMinutes ? min(target, 30) : 1 }
}

extension Noted_V1_GoalPeriod {
    /// 今天 / 本周 / 本月
    var current: String {
        switch self {
        case .day: "今天"
        case .month: "本月"
        default: "本周"
        }
    }
    /// 每天 / 每周 / 每月
    var every: String {
        switch self {
        case .day: "每天"
        case .month: "每月"
        default: "每周"
        }
    }
    /// 天 / 周 / 个月, for streaks ("4周").
    var unitName: String {
        switch self {
        case .day: "天"
        case .month: "个月"
        default: "周"
        }
    }
}

extension Noted_V1_Objective {
    var hasMetric: Bool { !metricUnit.isEmpty || !metricName.isEmpty }
    var fraction: Double { min(max(progress.percent, 0), 1) }
    /// Longest streak among the dimensions, shown next to the title.
    var streak: Int32 { goals.map(\.progress.streak).max() ?? 0 }
    /// Dimensions that met this period's target, out of all.
    var dimsDone: Int { goals.filter(\.isAchieved).count }
}

// MARK: - projects

extension Noted_V1_Project {
    var fraction: Double { min(max(progress.percent, 0), 1) }
    /// Days until the due date (the start date when there is none); nil without either.
    func daysLeft(now: Date = .now) -> Int? {
        if hasDueTime { return Fmt.dayDiff(dueTime.date, from: now) }
        return progress.hasDaysLeft ? Int(progress.daysLeft) : nil
    }
    /// "1/3 待办 · 还剩 41 天"
    var summary: String {
        var s = "\(progress.tasksDone)/\(progress.tasksTotal) 待办"
        if let n = daysLeft() { s += " · " + Fmt.daysLeft(n) }
        return s
    }
}

// MARK: - holdings

extension Noted_V1_Holding {
    var price: Double? { hasLastPrice ? lastPrice : nil }
    var hasPlan: Bool { dcaDay > 0 }
}

/// Something that can belong to a project.
enum ItemRef: Hashable, Sendable {
    case task(String), event(String), note(String), goal(String), objective(String)
}
