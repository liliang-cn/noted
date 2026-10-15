import SwiftUI

/// 今天 (tab root): 日程与待办 (a timeline with the now line), 进行中, 今天打卡 and 置顶项目.
struct TodayView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router

    var body: some View {
        TimelineView(.everyMinute) { ctx in
            TodayList(now: ctx.date)
        }
        .navigationTitle("今天")
        .largeSubtitle(Fmt.mdw(.now))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { SpaceFilterMenu() }
            ToolbarItem(placement: .topBarTrailing) { SettingsAvatarButton() }
        }
        .task { await data.loadMonth(.now) }
    }
}

private struct TodayList: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let now: Date
    @State private var confirmDelete: Noted_V1_Task?

    var body: some View {
        ScrollViewReader { proxy in
            List {
                agenda
                inProgress
                checkIns
                pinned
            }
            .listStyle(.insetGrouped)
            .environment(\.defaultMinListRowHeight, 10)
            .refreshable { await data.refreshAll() }
            .onChange(of: router.scrollToTopRequest) {
                withAnimation(.snappy) { proxy.scrollTo(TodayList.topID, anchor: .top) }
            }
        }
        .confirmationDialog("删除待办？", isPresented: Binding(get: { confirmDelete != nil }, set: { if !$0 { confirmDelete = nil } }),
                            titleVisibility: .visible, presenting: confirmDelete) { t in
            Button("删除待办", role: .destructive) {
                Task { if await data.deleteTask(t.id) { toaster.show("已删除", systemImage: "trash") } }
            }
            .accessibilityIdentifier("today-delete-confirm")
        } message: { t in
            Text("“\(t.title)”")
        }
        .accessibilityIdentifier("today-list")
    }

    static let topID = "today-top"

    // MARK: 日程与待办

    /// One line of the timeline.
    private enum Line: Identifiable {
        case event(Noted_V1_Occurrence)
        case todo(Noted_V1_Task)
        case now
        case untimedLabel

        var id: String {
            switch self {
            case .event(let o): "e-\(o.key)"
            case .todo(let t): "t-\(t.id)"
            case .now: "now"
            case .untimedLabel: "untimed"
            }
        }
    }

    /// Overdue to-dos from earlier days, then events and timed to-dos by time with the now line among
    /// them, then 全天 · 未定时: all-day events and date-only to-dos (open ones first).
    private var lines: [Line] {
        let today = now.startOfDay
        let occs = data.occurrences(on: now)
        let tasks = data.tasks(on: now)
        var out: [Line] = data.overdueTasks(now: now).map { .todo($0) }

        var timed: [(minute: Int, line: Line)] = []
        for o in occs where !o.event.allDay {
            timed.append((o.start < today ? 0 : o.start.minuteOfDay, .event(o)))
        }
        for t in tasks where t.hasClockTime {
            timed.append((t.dueTime.date.minuteOfDay, .todo(t)))
        }
        timed.sort { $0.minute < $1.minute }
        let nowMinute = now.minuteOfDay
        var placed = false
        for x in timed {
            if !placed && x.minute > nowMinute { out.append(.now); placed = true }
            out.append(x.line)
        }
        if !placed && !timed.isEmpty { out.append(.now) }

        let untimed: [Line] = occs.filter(\.event.allDay).map { .event($0) }
            + tasks.filter { !$0.hasClockTime }.sorted { !$0.completed && $1.completed }.map { .todo($0) }
        if !untimed.isEmpty {
            out.append(.untimedLabel)
            out += untimed
        }
        return out
    }

    private var agenda: some View {
        let lines = self.lines
        return Section {
            if lines.isEmpty {
                Group {
                    if data.isLoaded { EmptyState(text: "今天没有安排") }
                    else { ProgressView().frame(maxWidth: .infinity).padding(.vertical, 22) }
                }
                .id(Self.topID)
            }
            ForEach(Array(lines.enumerated()), id: \.element.id) { i, line in
                row(line)
                    .id(i == 0 ? Self.topID : line.id)
            }
        } header: {
            VStack(alignment: .leading, spacing: 0) {
                if #unavailable(iOS 26.0) {
                    Text(Fmt.mdw(now)).font(.subheadline).foregroundStyle(.secondaryText)
                        .textCase(nil).padding(.bottom, 10)
                }
                SectionHeader("日程与待办", action: "日历") { router.select(.calendar) }
            }
            .listRowInsets(EdgeInsets(top: 8, leading: 4, bottom: 8, trailing: 0))
        }
    }

    @ViewBuilder
    private func row(_ line: Line) -> some View {
        switch line {
        case .event(let o):
            EventTimelineRow(occurrence: o, now: now)
        case .todo(let t):
            TodoTimelineRow(task: t, now: now)
                .todaySwipes(t, onDelete: { confirmDelete = t })
        case .now:
            NowLine(now: now)
                .listRowInsets(EdgeInsets(top: 0, leading: 12, bottom: 0, trailing: 0))
                .listRowSeparator(.hidden)
        case .untimedLabel:
            Text("全天 · 未定时")
                .font(.footnote).foregroundStyle(.secondaryText)
                .listRowInsets(EdgeInsets(top: 10, leading: 16, bottom: 4, trailing: 16))
                .listRowSeparator(.hidden)
        }
    }

    // MARK: 进行中

    @ViewBuilder
    private var inProgress: some View {
        let tasks = data.inProgressTasks
        if !tasks.isEmpty {
            Section {
                ForEach(tasks, id: \.id) { t in
                    InProgressRow(task: t, now: now)
                        .todaySwipes(t, onDelete: { confirmDelete = t })
                }
            } header: {
                SectionHeader("进行中")
            }
        }
    }

    // MARK: 今天打卡

    @ViewBuilder
    private var checkIns: some View {
        let goals = data.standaloneGoals
        let objectives = data.visibleObjectives
        if !goals.isEmpty || !objectives.isEmpty {
            Section {
                ForEach(goals, id: \.id) { GoalRow(goal: $0) }
                ForEach(objectives, id: \.id) { ObjectiveRow(objective: $0) }
            } header: {
                SectionHeader("今天打卡", action: "全部") { router.push(.goals) }
            }
        }
    }

    // MARK: 置顶项目

    @ViewBuilder
    private var pinned: some View {
        let projects = data.pinnedProjects
        if !projects.isEmpty {
            Section {
                ScrollView(.horizontal) {
                    HStack(spacing: 12) {
                        ForEach(projects, id: \.id) { ProjectCard(project: $0, width: 236, now: now) }
                    }
                    .padding(.horizontal, 16)
                    .scrollTargetLayout()
                }
                .scrollIndicators(.hidden)
                .scrollTargetBehavior(.viewAligned)
                .listRowInsets(EdgeInsets())
                .listRowBackground(Color.clear)
            } header: {
                SectionHeader("置顶项目", action: "全部") { router.push(.projects) }
            }
        }
    }
}

/// A to-do with sittings: dot, title, "60%", a bar and "已做 3 次 · 共 2 小时 10 分 · 上次 今天".
private struct InProgressRow: View {
    @Environment(Router.self) private var router
    let task: Noted_V1_Task
    let now: Date

    var body: some View {
        Button { router.push(.todo(task.id)) } label: {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 8) {
                    SpaceDot(space: task.space)
                    Text(task.title).lineLimit(1)
                    Spacer(minLength: 8)
                    Text("\(task.progress)%").font(.system(.subheadline, design: .monospaced).weight(.bold))
                }
                ProgressBar(value: Double(task.progress) / 100)
                Text(summary).font(.footnote).foregroundStyle(.secondaryText).lineLimit(1).monospacedDigit()
            }
            .padding(.vertical, 6)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("inprogress-\(task.id)")
    }

    private var summary: String {
        var s = "已做 \(task.workSessions) 次 · 共 \(Fmt.duration(minutes: Int(task.workedMinutes)))"
        if task.hasLastWorkedTime { s += " · 上次 \(Fmt.day(task.lastWorkedTime.date, now: now))" }
        return s
    }
}

// MARK: - swipes

extension View {
    /// Right: 完成 (green). Left: 删除 (red, asks first) and 推迟 (to tomorrow).
    fileprivate func todaySwipes(_ task: Noted_V1_Task, onDelete: @escaping () -> Void) -> some View {
        modifier(TodaySwipes(task: task, onDelete: onDelete))
    }
}

private struct TodaySwipes: ViewModifier {
    @Environment(AppData.self) private var data
    @Environment(Toaster.self) private var toaster
    let task: Noted_V1_Task
    let onDelete: () -> Void

    func body(content: Content) -> some View {
        content
            .swipeActions(edge: .leading, allowsFullSwipe: true) {
                Button {
                    let done = !task.completed
                    Task { if await data.setCompleted(task.id, done), done { toaster.show("已完成") } }
                } label: {
                    task.completed ? Label("未完成", systemImage: "arrow.uturn.backward") : Label("完成", systemImage: "checkmark")
                }
                .tint(task.completed ? .tabInactive : .success)
                .accessibilityIdentifier("swipe-complete")
            }
            .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                Button(action: onDelete) { Label("删除", systemImage: "trash") }
                    .tint(.danger)
                    .accessibilityIdentifier("swipe-delete")
                if !task.completed {
                    Button {
                        Task { if let d = await data.postpone(task.id) { toaster.show("已推迟到\(Fmt.day(d))", systemImage: "clock") } }
                    } label: { Label("推迟", systemImage: "clock") }
                    .tint(.warning)
                    .accessibilityIdentifier("swipe-postpone")
                }
            }
    }
}
