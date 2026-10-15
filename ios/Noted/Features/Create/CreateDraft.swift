import Foundation

/// Everything the 新建 page holds for one kind. Each kind keeps its own draft in UserDefaults
/// until it is added or discarded.
struct CreateDraft: Codable, Equatable {
    var title = ""
    var notes = ""
    var projectID = ""
    var spaceRaw = Noted_V1_Space.life.rawValue

    // 待办 (due at 00:00 = date only)
    var due: Date?
    var remindMinutes: Int?
    var priorityRaw = 0

    // 日程
    var allDay = false
    var start = Date.now
    var end = Date.now.addingTimeInterval(3600)
    var location = ""
    var repeatFrequency = RepeatRule.Frequency.never.rawValue
    var weekdays: [Int] = []

    // 项目
    var projectStart = Date.now.startOfDay
    var projectDue = Date.now.startOfDay.adding(days: 30)
    var pinned = false

    // 目标
    var periodRaw = Noted_V1_GoalPeriod.week.rawValue
    var target = "3"
    var unit = GoalUnit.times.rawValue
    var customUnit = ""
    var hasCounter = false
    var counterTarget = "40"
    var counterUnit = "课时"
    var dimensions: [Dimension] = []
    var hasMetric = false
    var metricName = ""
    var metricUnit = ""
    var metricStart = ""
    var metricTarget = ""

    /// A dimension of a goal that becomes an objective.
    struct Dimension: Codable, Equatable, Identifiable {
        var id = UUID()
        var title: String
        var periodRaw: Int
        var target: Double
        /// Server unit: "times", "minutes", or the unit's own name.
        var unit: String

        var period: Noted_V1_GoalPeriod { Noted_V1_GoalPeriod(rawValue: periodRaw) ?? .week }
        /// "每周 2 次"
        var summary: String { "\(period.every) \(Fmt.number(target)) \(GoalUnit.label(forServerUnit: unit))" }
    }

    /// Nothing typed yet: not worth keeping.
    var isEmpty: Bool {
        title.trimmedText.isEmpty && notes.trimmedText.isEmpty && location.trimmedText.isEmpty && dimensions.isEmpty
    }

    var space: Noted_V1_Space {
        get { Noted_V1_Space(rawValue: spaceRaw) ?? .life }
        set { spaceRaw = (newValue == .work ? Noted_V1_Space.work : .life).rawValue }
    }
    var priority: Noted_V1_Priority {
        get { Noted_V1_Priority(rawValue: priorityRaw) ?? .unspecified }
        set { priorityRaw = newValue.rawValue }
    }
    var remind: ReminderLead {
        get { remindMinutes.map { ReminderLead(minutesBefore: $0) } ?? .none }
        set { remindMinutes = newValue.minutes }
    }
    var repeatRule: RepeatRule {
        get { RepeatRule(frequency: RepeatRule.Frequency(rawValue: repeatFrequency) ?? .never, weekdays: Set(weekdays)) }
        set { repeatFrequency = newValue.frequency.rawValue; weekdays = newValue.weekdays.sorted() }
    }
    var period: Noted_V1_GoalPeriod {
        get { Noted_V1_GoalPeriod(rawValue: periodRaw) ?? .week }
        set { periodRaw = newValue.rawValue }
    }
    var goalUnit: GoalUnit {
        get { GoalUnit(rawValue: unit) ?? .custom }
        set { unit = newValue.rawValue }
    }
    /// What the server stores as the goal's unit.
    var serverUnit: String { goalUnit == .custom ? customUnit.trimmedText : goalUnit.rawValue }
    /// 次 / 分钟 / 页 / 公里 / the custom name.
    var unitLabel: String { goalUnit == .custom ? (customUnit.trimmedText.isEmpty ? "单位" : customUnit.trimmedText) : goalUnit.label }
}

/// 计量 units offered for a goal's target.
enum GoalUnit: String, CaseIterable, Identifiable {
    case times, minutes, pages = "页", km = "公里", custom
    var id: String { rawValue }
    var label: String {
        switch self {
        case .times: "次"
        case .minutes: "分钟"
        case .pages: "页"
        case .km: "公里"
        case .custom: "自定义…"
        }
    }
    /// One tap of − / +.
    var step: Double { self == .minutes ? 5 : 1 }

    static func label(forServerUnit u: String) -> String {
        switch u {
        case "", "times": "次"
        case "minutes": "分钟"
        default: u
        }
    }
}

/// Drafts in UserDefaults, one per kind.
enum DraftStore {
    private static func key(_ kind: CreateKind) -> String { "create.draft.\(kind.rawValue)" }

    static func load(_ kind: CreateKind) -> CreateDraft? {
        guard let raw = UserDefaults.standard.data(forKey: key(kind)),
              let d = try? JSONDecoder().decode(CreateDraft.self, from: raw), !d.isEmpty else { return nil }
        return d
    }

    /// Saves `draft`, or forgets the kind's draft when nothing is typed.
    static func save(_ draft: CreateDraft, for kind: CreateKind) {
        guard !draft.isEmpty, let raw = try? JSONEncoder().encode(draft) else { clear(kind); return }
        UserDefaults.standard.set(raw, forKey: key(kind))
    }

    static func clear(_ kind: CreateKind) { UserDefaults.standard.removeObject(forKey: key(kind)) }
}

extension String {
    var trimmedText: String { trimmingCharacters(in: .whitespacesAndNewlines) }
    /// "75", "75.5", "75,5" → a number.
    var parsedNumber: Double? { Double(trimmedText.replacingOccurrences(of: ",", with: ".")) }
}
