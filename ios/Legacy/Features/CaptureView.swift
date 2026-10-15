import SwiftUI

struct CaptureView: View {
    enum Kind: String, CaseIterable, Identifiable {
        case task = "待办", event = "日程", remindOnce = "提醒", remindRepeat = "周期", goal = "目标", project = "项目", note = "笔记"
        var id: String { rawValue }
        var icon: String {
            switch self {
            case .event: "calendar"
            case .task: "checkmark.circle"
            case .remindOnce: "bell"
            case .remindRepeat: "repeat"
            case .goal: "target"
            case .project: "flag"
            case .note: "doc.text"
            }
        }
        var action: String {
            switch self {
            case .event: "添加到日历"
            case .task: "添加待办"
            case .remindOnce, .remindRepeat: "添加提醒"
            case .goal: "添加目标"
            case .project: "创建项目"
            case .note: "保存笔记"
            }
        }
    }

    enum Repeat: String, CaseIterable, Identifiable {
        case none = "不重复", daily = "每天", weekly = "每周", monthly = "每月", yearly = "每年"
        var id: String { rawValue }
    }

    let spec: CreateSpec
    @State private var projectID = ""
    @State private var projects: [Noted_V1_Project] = []
    @Environment(Session.self) private var session
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var kind: Kind = .task
    @State private var title = ""
    @State private var content = ""
    @State private var location = ""
    @State private var when = CaptureView.defaultTime()
    @State private var until = CaptureView.defaultTime().addingTimeInterval(3600)
    @State private var hasWhen = false
    @State private var remindMinutes = -1   // events: -1 none, 0 at start, else minutes before
    @State private var remindTask = false
    @State private var repeatRule: Repeat = .none
    @State private var weekdays: Set<Int> = []   // 1 = Monday … 7 = Sunday
    @State private var space: Noted_V1_Space = .life
    @State private var error: String?
    @State private var saving = false
    @State private var restored = false
    @State private var hasStart = false
    @State private var startDate = Calendar.current.startOfDay(for: .now)
    @State private var hasDue = false
    @State private var dueDate = Calendar.current.startOfDay(for: .now).addingTimeInterval(14 * 86400)
    @State private var pinned = false
    @State private var goalPeriod: Noted_V1_GoalPeriod = .week
    @State private var goalTarget = "3"
    @State private var goalUnit = "次"
    @State private var hasCounter = false
    @State private var counterTarget = ""
    @State private var counterUnit = "课时"
    @State private var ready = false
    @FocusState private var titleFocused: Bool

    private var cleanTitle: String { title.trimmingCharacters(in: .whitespaces) }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            CreateHeader(selected: kind) { choose($0) }
            if restored { restoredBanner }

            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    VStack(spacing: 0) { form }
                        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                        .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))
                    ErrorLine(text: error)
                }
            }
            .scrollIndicators(.hidden)
            .scrollDismissesKeyboard(.interactively)

            Button { Task { await save() } } label: {
                Text(saving ? "保存中…" : kind.action)
                    .font(.ui(16, .semibold)).frame(maxWidth: .infinity).frame(height: 50)
                    .background(session.mode.accent(p).opacity(cleanTitle.isEmpty || saving ? 0.4 : 1), in: RoundedRectangle(cornerRadius: p.radiusSm))
                    .foregroundStyle(.white)
            }
            .disabled(cleanTitle.isEmpty || saving)
            .accessibilityIdentifier("capture-save")
            .padding(.bottom, 8)
        }
        .padding(.horizontal, 16)
        .background(p.bg.ignoresSafeArea())
        .onAppear(perform: start)
        .onChange(of: draft) { _, d in if ready { CaptureDraft.store(d) } }
    }

    // MARK: draft

    private var draft: CaptureDraft {
        CaptureDraft(kind: kind.rawValue, title: title, location: location, when: when, until: until, hasWhen: hasWhen,
                     remindMinutes: remindMinutes, remindTask: remindTask, repeatRule: repeatRule.rawValue,
                     weekdays: weekdays.sorted(), space: space.rawValue, projectID: projectID)
    }

    private func start() {
        guard !ready else { return }
        if spec.space == .work || spec.space == .life { space = spec.space } else if session.mode == .work { space = .work }
        if let k = Kind(rawValue: spec.kind) { kind = k }
        projectID = spec.projectID
        title = spec.title
        if let s = spec.start { when = s; until = s.addingTimeInterval(3600); hasWhen = true }
        Task { projects = (try? await session.api?.projects.listProjects(Noted_V1_ListProjectsRequest()).projects) ?? [] }
        if spec.title.isEmpty, let d = CaptureDraft.load(), d.projectID == spec.projectID, !d.title.trimmingCharacters(in: .whitespaces).isEmpty {
            kind = Kind(rawValue: d.kind) ?? kind
            title = d.title; location = d.location; hasWhen = d.hasWhen
            if d.when > .now { when = d.when; until = max(d.until, d.when) }
            remindMinutes = d.remindMinutes; remindTask = d.remindTask
            repeatRule = Repeat(rawValue: d.repeatRule) ?? .none; weekdays = Set(d.weekdays)
            space = Noted_V1_Space(rawValue: d.space) ?? space
            restored = true
        } else {
            titleFocused = true
        }
        ready = true
    }

    private var restoredBanner: some View {
        HStack(spacing: 10) {
            Image(systemName: "arrow.uturn.backward.circle").foregroundStyle(p.warn)
            Text("恢复了上次没添加的内容").font(.ui(13)).foregroundStyle(p.ink)
            Spacer()
            Button("放弃") {
                CaptureDraft.clear()
                title = ""; location = ""; hasWhen = false; remindMinutes = -1; remindTask = false; repeatRule = .none; weekdays = []
                restored = false
            }
            .font(.ui(13, .semibold)).foregroundStyle(p.ink2)
        }
        .padding(.horizontal, 14).padding(.vertical, 10)
        .background(Color(hex: 0xFFF1CC), in: RoundedRectangle(cornerRadius: p.radiusSm))
    }

    private func choose(_ k: Kind) {
        if k == .note {
            // A note is written on its own page; this page becomes it.
            var n = Noted_V1_Note()
            n.title = cleanTitle
            n.space = space
            n.projectID = projectID
            CaptureDraft.clear()
            var t = Transaction(); t.disablesAnimations = true
            withTransaction(t) { router.replaceTop(.note(n)) }
            return
        }
        kind = k
        if k == .remindRepeat, repeatRule == .none { repeatRule = .daily }
    }

    // MARK: form rows

    @ViewBuilder private var form: some View {
        row("标题") {
            TextField(kind == .project ? "项目名称" : kind == .goal ? "比如 健身、学德语" : kind == .note ? "标题" : "要做什么", text: $title)
                .font(.ui(16, .semibold)).foregroundStyle(p.ink)
                .focused($titleFocused)
        }
        switch kind {
        case .goal:
            divider
            row("周期") {
                Picker("", selection: $goalPeriod) {
                    Text("每天").tag(Noted_V1_GoalPeriod.day)
                    Text("每周").tag(Noted_V1_GoalPeriod.week)
                    Text("每月").tag(Noted_V1_GoalPeriod.month)
                }
                .pickerStyle(.segmented).frame(maxWidth: 200)
                .accessibilityIdentifier("goal-period")
                Spacer()
            }
            divider
            row("目标") {
                TextField("3", text: $goalTarget).keyboardType(.decimalPad).font(.mono(16, .semibold))
                    .frame(width: 64).multilineTextAlignment(.trailing)
                    .accessibilityIdentifier("goal-target")
                TextField("次", text: $goalUnit).font(.ui(15)).frame(width: 72)
                    .accessibilityIdentifier("goal-unit")
                Spacer()
            }
            divider
            row("累计") {
                if hasCounter {
                    TextField("40", text: $counterTarget).keyboardType(.decimalPad).font(.mono(16, .semibold))
                        .frame(width: 64).multilineTextAlignment(.trailing)
                    TextField("课时", text: $counterUnit).font(.ui(15)).frame(width: 72)
                } else {
                    Text("不设").font(.ui(15)).foregroundStyle(p.ink3)
                }
                Spacer()
                Toggle("", isOn: $hasCounter).labelsHidden().accessibilityLabel("设置累计目标")
            }
        case .project:
            divider
            row("开始") {
                if hasStart { DatePicker("", selection: $startDate, displayedComponents: .date).labelsHidden().environment(\.locale, Fmt.zh) }
                else { Text("无").font(.ui(15)).foregroundStyle(p.ink3) }
                Spacer()
                Toggle("", isOn: $hasStart).labelsHidden().accessibilityLabel("设置开始日期")
            }
            divider
            row("截止") {
                if hasDue { DatePicker("", selection: $dueDate, displayedComponents: .date).labelsHidden().environment(\.locale, Fmt.zh) }
                else { Text("无").font(.ui(15)).foregroundStyle(p.ink3) }
                Spacer()
                Toggle("", isOn: $hasDue).labelsHidden().accessibilityLabel("设置截止日期")
            }
            divider
            row("置顶") {
                Text("在概览里显示").font(.ui(15)).foregroundStyle(p.ink2)
                Spacer()
                Toggle("", isOn: $pinned).labelsHidden().accessibilityLabel("置顶")
            }
        case .note:
            divider
            VStack(alignment: .leading, spacing: 6) {
                Text("内容").font(.ui(13, .medium)).foregroundStyle(p.ink2)
                TextEditor(text: $content).scrollContentBackground(.hidden).frame(minHeight: 150)
                    .font(.ui(15)).foregroundStyle(p.ink)
            }.padding(.horizontal, 14).padding(.vertical, 12)
        case .task:
            divider
            row("时间") {
                if hasWhen { DatePicker("", selection: $when).labelsHidden().environment(\.locale, Fmt.zh) }
                else { Text("无").font(.ui(15)).foregroundStyle(p.ink3) }
                Spacer()
                Toggle("", isOn: $hasWhen).labelsHidden().accessibilityLabel("设置截止时间")
            }
            if hasWhen {
                divider
                row("提醒") {
                    Picker("", selection: $remindTask) {
                        Text("不提醒").tag(false)
                        Text("到期时").tag(true)
                    }.labelsHidden().accessibilityLabel("到期时提醒")
                    Spacer()
                }
            }
        case .event:
            divider
            row("开始") { DatePicker("", selection: $when).labelsHidden().environment(\.locale, Fmt.zh) }
            divider
            row("结束") { DatePicker("", selection: $until, in: when...).labelsHidden().environment(\.locale, Fmt.zh) }
            divider
            row("地点") { TextField("无", text: $location).font(.ui(15)).foregroundStyle(p.ink) }
            divider
            row("提醒") {
                Picker("", selection: $remindMinutes) {
                    Text("不提醒").tag(-1)
                    Text("开始时").tag(0)
                    Text("提前 5 分钟").tag(5)
                    Text("提前 10 分钟").tag(10)
                    Text("提前 30 分钟").tag(30)
                    Text("提前 1 小时").tag(60)
                }.labelsHidden().accessibilityIdentifier("remind-picker")
                Spacer()
            }
            divider
            repeatRow(allowNone: true)
            if repeatRule == .weekly { divider; weekdayRow }
        case .remindOnce:
            divider
            row("时间") { DatePicker("", selection: $when).labelsHidden().environment(\.locale, Fmt.zh) }
        case .remindRepeat:
            divider
            row("首次") { DatePicker("", selection: $when).labelsHidden().environment(\.locale, Fmt.zh) }
            divider
            repeatRow(allowNone: false)
            if repeatRule == .weekly { divider; weekdayRow }
        }
        if kind != .project {
        divider
        row("项目") {
            Menu {
                Button { projectID = "" } label: { Label("不属于项目", systemImage: projectID.isEmpty ? "checkmark" : "") }
                ForEach(projects, id: \.id) { pr in
                    Button { projectID = pr.id } label: { Label(pr.title, systemImage: projectID == pr.id ? "checkmark" : "") }
                }
            } label: {
                HStack(spacing: 6) {
                    Text(projects.first { $0.id == projectID }?.title ?? "无").font(.ui(15))
                        .foregroundStyle(projectID.isEmpty ? p.ink3 : p.ink)
                    Image(systemName: "chevron.up.chevron.down").font(.system(size: 11, weight: .semibold)).foregroundStyle(p.ink3)
                }
            }
            .accessibilityIdentifier("project-picker")
            Spacer()
        }
        }
        divider
        row("空间") {
            SpacePicker(selection: $space)
            Spacer()
        }
    }

    private var divider: some View { Divider().overlay(p.line) }

    private func row<C: View>(_ label: String, @ViewBuilder _ content: () -> C) -> some View {
        HStack(spacing: 10) {
            Text(label).font(.ui(13, .medium)).foregroundStyle(p.ink2).frame(width: 44, alignment: .leading)
            content()
        }
        .padding(.horizontal, 14).frame(minHeight: p.rowHeight)
    }

    private func repeatRow(allowNone: Bool) -> some View {
        row("重复") {
            Picker("", selection: $repeatRule) {
                ForEach(Repeat.allCases.filter { allowNone || $0 != .none }) { Text($0.rawValue).tag($0) }
            }.labelsHidden().accessibilityIdentifier("repeat-picker")
            Spacer()
        }
    }

    private var weekdayRow: some View {
        row("星期") {
            HStack(spacing: 6) {
                ForEach(Array(["一", "二", "三", "四", "五", "六", "日"].enumerated()), id: \.offset) { i, d in
                    let on = weekdays.contains(i + 1)
                    Button { if on { weekdays.remove(i + 1) } else { weekdays.insert(i + 1) } } label: {
                        Text(d).font(.ui(13, .semibold))
                            .frame(width: 30, height: 30)
                            .background(on ? session.mode.accent(p) : p.surface2, in: Circle())
                            .foregroundStyle(on ? Color.white : p.ink2)
                    }
                    .accessibilityLabel("周\(d)")
                }
            }
            Spacer()
        }
    }


    /// An hour from now. UI tests ask for less, so they do not depend on the time of day.
    static func defaultTime() -> Date {
        var minutes = 60.0
        #if DEBUG
        if let m = ProcessInfo.processInfo.environment["NOTED_CAPTURE_MINUTES"], let v = Double(m) { minutes = v }
        #endif
        return Date().addingTimeInterval(minutes * 60)
    }

    // MARK: saving

    private static let days = ["MO", "TU", "WE", "TH", "FR", "SA", "SU"]

    private var rrule: String {
        switch repeatRule {
        case .none: return ""
        case .daily: return "FREQ=DAILY"
        case .monthly: return "FREQ=MONTHLY"
        case .yearly: return "FREQ=YEARLY"
        case .weekly:
            // No day chosen means the weekday of the start.
            let picked = weekdays.isEmpty ? [(Calendar.current.component(.weekday, from: when) + 5) % 7 + 1] : weekdays.sorted()
            return "FREQ=WEEKLY;BYDAY=" + picked.map { Self.days[$0 - 1] }.joined(separator: ",")
        }
    }

    private func save() async {
        guard let api = session.api else { return }
        saving = true
        defer { saving = false }
        let t = cleanTitle
        do {
            switch kind {
            case .note:
                var r = Noted_V1_CreateNoteRequest()
                r.note.title = t; r.note.content = content; r.note.space = space; r.note.projectID = projectID
                _ = try await api.notes.createNote(r)
            case .task:
                var r = Noted_V1_CreateTaskRequest()
                r.task.title = t; r.task.space = space; r.task.projectID = projectID
                if hasWhen {
                    r.task.dueTime = .init(when)
                    if remindTask { r.task.remindTime = .init(when) }
                }
                _ = try await api.calendar.createTask(r)
            case .remindOnce:
                // A reminder is a to-do that fires at its time.
                var r = Noted_V1_CreateTaskRequest()
                r.task.title = t; r.task.space = space; r.task.projectID = projectID
                r.task.dueTime = .init(when); r.task.remindTime = .init(when)
                _ = try await api.calendar.createTask(r)
            case .event:
                var r = Noted_V1_CreateEventRequest()
                r.event.title = t; r.event.space = space; r.event.projectID = projectID
                r.event.location = location.trimmingCharacters(in: .whitespaces)
                r.event.startTime = .init(when)
                r.event.endTime = .init(max(until, when.addingTimeInterval(60)))
                r.event.timeZone = TimeZone.current.identifier
                if remindMinutes >= 0 { r.event.remindBeforeMinutes = Int32(remindMinutes) }
                r.event.rrule = rrule
                _ = try await api.calendar.createEvent(r)
            case .goal:
                guard let target = Double(goalTarget.trimmingCharacters(in: .whitespaces)), target > 0 else {
                    error = "目标次数要大于 0"; return
                }
                var r = Noted_V1_CreateGoalRequest()
                r.goal.title = t; r.goal.space = space; r.goal.projectID = projectID
                r.goal.period = goalPeriod; r.goal.target = target
                r.goal.unit = goalUnit.trimmingCharacters(in: .whitespaces).isEmpty ? "次" : goalUnit.trimmingCharacters(in: .whitespaces)
                r.goal.timeZone = TimeZone.current.identifier
                if hasCounter, let c = Double(counterTarget.trimmingCharacters(in: .whitespaces)), c > 0 {
                    r.goal.counterTarget = c
                    r.goal.counterUnit = counterUnit.trimmingCharacters(in: .whitespaces)
                }
                _ = try await api.goals.createGoal(r)
            case .project:
                var r = Noted_V1_CreateProjectRequest()
                r.project.title = t; r.project.space = space; r.project.pinned = pinned
                if hasStart { r.project.startTime = .init(startDate) }
                if hasDue { r.project.dueTime = .init(dueDate) }
                let made = try await api.projects.createProject(r)
                CaptureDraft.clear()
                router.replaceTop(.project(made.id))
                return
            case .remindRepeat:
                // A recurring event short enough to read as a reminder, fired when it starts.
                var r = Noted_V1_CreateEventRequest()
                r.event.title = t; r.event.space = space; r.event.projectID = projectID
                r.event.startTime = .init(when)
                r.event.endTime = .init(when.addingTimeInterval(15 * 60))
                r.event.timeZone = TimeZone.current.identifier
                r.event.remindBeforeMinutes = 0
                r.event.rrule = rrule
                _ = try await api.calendar.createEvent(r)
            }
            CaptureDraft.clear()
            dismiss()
        } catch { self.error = describe(error) }
    }
}

/// What was typed on the create page and not added yet, kept on the device.
struct CaptureDraft: Codable, Equatable {
    var kind, title, location: String
    var when, until: Date
    var hasWhen: Bool
    var remindMinutes: Int
    var remindTask: Bool
    var repeatRule: String
    var weekdays: [Int]
    var space: Int
    var projectID: String

    private static let key = "draft.capture"

    static func load() -> CaptureDraft? {
        guard let d = UserDefaults.standard.data(forKey: key) else { return nil }
        return try? JSONDecoder().decode(CaptureDraft.self, from: d)
    }

    static func store(_ d: CaptureDraft) {
        if d.title.trimmingCharacters(in: .whitespaces).isEmpty { clear(); return }
        if let data = try? JSONEncoder().encode(d) { UserDefaults.standard.set(data, forKey: key) }
    }

    static func clear() { UserDefaults.standard.removeObject(forKey: key) }
}

/// The top of the create page: back, the title, and the kinds of thing you can make. The note
/// page shows the same header while the note is new, so switching kinds feels like one page.
struct CreateHeader: View {
    @Environment(\.palette) private var p
    let selected: CaptureView.Kind
    let onSelect: (CaptureView.Kind) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ZStack {
                HStack { BackButton(title: "返回"); Spacer() }
                Text("新建").font(.ui(17, .semibold)).foregroundStyle(p.ink)
            }
            UnderlineTabs(items: CaptureView.Kind.allCases.map(\.rawValue),
                          selected: CaptureView.Kind.allCases.firstIndex(of: selected) ?? 0) { onSelect(CaptureView.Kind.allCases[$0]) }
                .accessibilityIdentifier("kind-tabs")
        }
    }
}
