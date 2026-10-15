import SwiftUI

// Rows and cards that appear on more than one screen (今天, 日历, 计划, project pages). Each one
// opens its own page through the Router in the environment and writes through AppData.

// MARK: - timeline rows (今天, the calendar's day list)

/// A to-do in a timeline: time column, round check, title with priority marks, a gray line
/// (已逾期 / day · project · progress · repeat · bell) and the space dot. Tapping opens the to-do.
struct TodoTimelineRow: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let task: Noted_V1_Task
    /// Show the due day in the gray line (lists that span several days).
    var showsDate = false
    /// Show the time column (off in project pages).
    var showsTime = true
    var now: Date = .now

    var body: some View {
        let overdue = task.isOverdue(now: now)
        Button { router.push(.todo(task.id)) } label: {
            HStack(spacing: 12) {
                if showsTime {
                    Text(task.hasClockTime ? Fmt.hm(task.dueTime.date) : "")
                        .font(.subheadline.weight(.medium)).monospacedDigit()
                        .foregroundStyle(overdue ? AnyShapeStyle(Color.danger) : AnyShapeStyle(.primary))
                        .lineLimit(1).fixedSize()
                        .frame(width: 44, alignment: .leading)
                        .frame(maxHeight: .infinity, alignment: .top)
                        .padding(.top, 2)
                }
                CheckButton(isOn: task.completed) { toggle() }
                    .buttonStyle(.borderless)
                    .padding(.horizontal, -10).padding(.vertical, -10)
                VStack(alignment: .leading, spacing: 1) {
                    title
                    if let sub = subtitle(overdue: overdue) { sub.font(.footnote).foregroundStyle(.secondaryText).lineLimit(1) }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                SpaceDot(space: task.space)
            }
            .padding(.vertical, 6)
            .frame(minHeight: 46)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("todo-\(task.id)")
    }

    private var title: some View {
        (Text(task.priorityMarks.isEmpty ? "" : task.priorityMarks + " ").foregroundColor(.danger).bold()
            + Text(task.title).foregroundColor(task.completed ? .secondaryText : .primary))
            .strikethrough(task.completed, color: .tertiaryText)
            .lineLimit(1)
    }

    private func subtitle(overdue: Bool) -> Text? {
        var parts: [Text] = []
        if overdue, let d = task.due {
            let past = d.startOfDay < now.startOfDay
            parts.append(Text(past ? "\(Fmt.day(d, now: now)) · 已逾期" : "已逾期").foregroundColor(.danger))
        } else if showsDate, let d = task.due {
            parts.append(Text(Fmt.day(d, now: now)))
        }
        if let p = data.projectTitle(task.projectID) { parts.append(Text("\(Image(systemName: "folder")) \(p)")) }
        if task.workSessions > 0 || task.progress > 0 { parts.append(Text("\(task.progress)%").monospacedDigit()) }
        if task.repeats { parts.append(Text("\(Image(systemName: "repeat")) \(task.repeatRule.label)")) }
        if task.hasRemindTime && !task.completed { parts.append(Text(Image(systemName: "bell"))) }
        guard !parts.isEmpty else { return nil }
        return parts.dropFirst().reduce(parts[0]) { $0 + Text(" · ") + $1 }
    }

    private func toggle() {
        let done = !task.completed
        Task {
            if await data.setCompleted(task.id, done), done { toaster.show("已完成") }
        }
    }
}

/// An event in a timeline: start/end column, a 4 pt bar in the space color, title, and a gray line
/// (进行中 · location · duration · project). Past events are dimmed and struck through.
struct EventTimelineRow: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    let occurrence: Noted_V1_Occurrence
    var now: Date = .now

    var body: some View {
        let e = occurrence.event
        let past = occurrence.isPast(now: now)
        Button { router.push(.event(e.id, occurrence: occurrence.start)) } label: {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 0) {
                    if e.allDay {
                        Text("全天").font(.subheadline.weight(.medium))
                    } else {
                        Text(Fmt.hm(occurrence.start)).font(.subheadline.weight(.medium))
                        Text(Fmt.hm(occurrence.end)).font(.caption).foregroundStyle(.secondaryText)
                    }
                }
                .monospacedDigit()
                .lineLimit(1).fixedSize()
                .frame(width: 44, alignment: .leading)
                .frame(maxHeight: .infinity, alignment: .top)
                .padding(.top, 2)
                RoundedRectangle(cornerRadius: 2).fill(e.space.tint).frame(width: 4).padding(.vertical, 1)
                VStack(alignment: .leading, spacing: 1) {
                    Text(e.title).lineLimit(1)
                        .strikethrough(past, color: .tertiaryText)
                    if let sub = subtitle { sub.font(.footnote).foregroundStyle(.secondaryText).lineLimit(1) }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.vertical, 6)
            .frame(minHeight: 46)
            .opacity(past ? 0.45 : 1)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("event-\(e.id)")
    }

    private var subtitle: Text? {
        let e = occurrence.event
        var parts: [Text] = []
        if occurrence.isHappening(now: now) { parts.append(Text("进行中").foregroundColor(.danger).fontWeight(.semibold)) }
        if !e.location.isEmpty { parts.append(Text(e.location)) }
        if !e.allDay, occurrence.minutes > 0 { parts.append(Text(Fmt.duration(minutes: occurrence.minutes))) }
        if let p = data.projectTitle(e.projectID) { parts.append(Text("\(Image(systemName: "folder")) \(p)")) }
        guard !parts.isEmpty else { return nil }
        return parts.dropFirst().reduce(parts[0]) { $0 + Text(" · ") + $1 }
    }
}

/// The red "now" line between timeline rows, with the time ("11:00 ———").
struct NowLine: View {
    var now: Date = .now
    var body: some View {
        HStack(spacing: 6) {
            Text(Fmt.hm(now)).font(.caption.weight(.semibold)).monospacedDigit()
            Circle().frame(width: 8, height: 8).padding(.trailing, -10)
            Rectangle().frame(height: 1.5)
        }
        .foregroundStyle(.danger)
        .frame(height: 22)
        .accessibilityLabel("现在 \(Fmt.hm(now))")
    }
}

// MARK: - swipe actions for tasks

extension View {
    /// The to-do swipes: right for 完成 (green), left for 推迟 (orange) and 删除 (red, full swipe).
    func taskSwipeActions(_ task: Noted_V1_Task) -> some View { modifier(TaskSwipeActions(task: task)) }
}

private struct TaskSwipeActions: ViewModifier {
    @Environment(AppData.self) private var data
    @Environment(Toaster.self) private var toaster
    let task: Noted_V1_Task

    func body(content: Content) -> some View {
        content
            .swipeActions(edge: .leading, allowsFullSwipe: true) {
                if !task.completed {
                    Button {
                        Task { if await data.setCompleted(task.id, true) { toaster.show("已完成") } }
                    } label: { Label("完成", systemImage: "checkmark") }
                    .tint(.success)
                }
            }
            .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                Button(role: .destructive) {
                    Task { if await data.deleteTask(task.id) { toaster.show("已删除", systemImage: "trash") } }
                } label: { Label("删除", systemImage: "trash") }
                if !task.completed {
                    Button {
                        Task { if let d = await data.postpone(task.id) { toaster.show("已推迟到\(Fmt.day(d))", systemImage: "clock") } }
                    } label: { Label("推迟", systemImage: "clock") }
                    .tint(.warning)
                }
            }
    }
}

// MARK: - project card (今天's 置顶项目, 计划, 项目 · 全部)

/// A white card: dot + title, a bar in the space color, "1/3 待办 · 还剩 41 天" and the next step.
struct ProjectCard: View {
    @Environment(Router.self) private var router
    let project: Noted_V1_Project
    /// 236 in the horizontal strip on 今天; nil: full width.
    var width: CGFloat?
    var now: Date = .now

    var body: some View {
        Button { router.push(.project(project.id)) } label: {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 7) {
                    SpaceDot(space: project.space)
                    Text(project.title).font(.headline).lineLimit(1)
                    Spacer(minLength: 0)
                    if project.archived { Text("已归档").font(.caption.weight(.medium)).foregroundStyle(.secondaryText) }
                }
                ProgressBar(value: project.fraction, tint: project.space.tint)
                Text(project.summary).font(.footnote).foregroundStyle(.secondaryText).monospacedDigit()
                if !project.progress.nextTitle.isEmpty {
                    HStack(spacing: 6) {
                        Image(systemName: "chevron.right").font(.system(size: 10, weight: .semibold))
                        Text(nextLine).lineLimit(1)
                    }
                    .font(.footnote)
                } else {
                    Text("没有待办").font(.footnote).foregroundStyle(.secondaryText)
                }
            }
            .padding(14)
            .frame(width: width, alignment: .leading)
            .frame(maxWidth: width == nil ? .infinity : nil, alignment: .leading)
            .background(.white, in: RoundedRectangle(cornerRadius: 14))
            .contentShape(RoundedRectangle(cornerRadius: 14))
        }
        .buttonStyle(CardPressStyle())
        .accessibilityIdentifier("project-\(project.id)")
    }

    private var nextLine: String {
        let p = project.progress
        return p.hasNextTime ? "\(p.nextTitle) · \(Fmt.day(p.nextTime.date, now: now))" : p.nextTitle
    }
}

/// Cards darken slightly while pressed, like a list row.
struct CardPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .overlay(RoundedRectangle(cornerRadius: 14).fill(.black.opacity(configuration.isPressed ? 0.06 : 0)))
    }
}

// MARK: - goal rows (今天打卡, 计划's 目标, 目标 · 全部)

/// A goal: progress ring, title with streak, "2/3 本周 · 课时 19/40", and the check-in button.
/// Tapping +1 checks in at once; a minutes goal asks how long first.
struct GoalRow: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let goal: Noted_V1_Goal
    @State private var askMinutes = false

    var body: some View {
        HStack(spacing: 12) {
            Button { router.push(.goal(goal.id)) } label: {
                HStack(spacing: 12) {
                    GoalRing(fraction: goal.fraction, done: goal.isAchieved)
                    VStack(alignment: .leading, spacing: 1) {
                        HStack(spacing: 5) {
                            Text(goal.title).lineLimit(1)
                            if goal.progress.streak > 0 { StreakLabel(count: Int(goal.progress.streak), unit: goal.period.unitName) }
                        }
                        Text(subtitle).font(.footnote).foregroundStyle(.secondaryText).lineLimit(1).monospacedDigit()
                    }
                    Spacer(minLength: 0)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            CheckInButton(title: goal.isMinutes ? "+\(Fmt.number(goal.quickAmount))" : "+1", done: goal.isAchieved) {
                if goal.isMinutes { askMinutes = true } else { Task { await checkIn(amount: 1) } }
            }
            .buttonStyle(.borderless)
        }
        .padding(.vertical, 3)
        .sheet(isPresented: $askMinutes) {
            MinutesCheckInSheet(goal: goal) { minutes, count in Task { await checkIn(amount: Double(minutes), count: count) } }
        }
        .accessibilityIdentifier("goal-\(goal.id)")
    }

    private var subtitle: String {
        var s = "\(Fmt.number(goal.done))/\(Fmt.number(goal.goalTarget))"
        if goal.isMinutes { s += " 分钟" }
        s += " \(goal.period.current)"
        if goal.hasCounter {
            let name = goal.counterUnit.isEmpty ? "累计" : goal.counterUnit
            s += " · \(name) \(Fmt.number(goal.progress.counterDone))/\(Fmt.number(goal.counterTarget))"
        }
        return s
    }

    private func checkIn(amount: Double, count: Double? = nil) async {
        let c = count ?? (goal.hasCounter ? 1 : 0)
        guard await data.checkIn(goal.id, amount: amount, count: c) != nil else { return }
        toaster.show(goal.isMinutes ? "\(goal.title) +\(Fmt.number(amount)) 分钟" : "\(goal.title) +1")
    }
}

/// An objective (a goal with dimensions and maybe a metric) in the same row shape: its ring,
/// "3/5 本周 · 2 个维度", a sparkline of the metric, and a "+" that asks which dimension.
struct ObjectiveRow: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let objective: Noted_V1_Objective
    @State private var choose = false

    var body: some View {
        let dims = objective.goals
        let done = dims.reduce(0.0) { $0 + min($1.done, $1.goalTarget) }
        let total = dims.reduce(0.0) { $0 + $1.goalTarget }
        let achieved = total > 0 && done >= total
        HStack(spacing: 12) {
            Button { router.push(.objective(objective.id)) } label: {
                HStack(spacing: 12) {
                    GoalRing(fraction: total > 0 ? done / total : objective.fraction, done: achieved)
                    VStack(alignment: .leading, spacing: 1) {
                        HStack(spacing: 5) {
                            Text(objective.title).lineLimit(1)
                            if objective.streak > 0 { StreakLabel(count: Int(objective.streak), unit: dims.first?.period.unitName ?? "周") }
                        }
                        Text(subtitle(done: done, total: total)).font(.footnote).foregroundStyle(.secondaryText).lineLimit(1).monospacedDigit()
                    }
                    Spacer(minLength: 0)
                    if objective.hasMetric, let values = readings, values.count > 1 {
                        VStack(alignment: .trailing, spacing: 1) {
                            Sparkline(values: values).frame(width: 48, height: 22)
                            Text("\(objective.metricName) \(Fmt.number(values.last!))")
                                .font(.system(size: 11, design: .monospaced)).foregroundStyle(.secondaryText)
                        }
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if !dims.isEmpty {
                CheckInButton(title: nil, done: achieved) { choose = true }
                    .buttonStyle(.borderless)
            }
        }
        .padding(.vertical, 3)
        .task(id: objective.id) {
            if objective.hasMetric && data.measurements[objective.id] == nil { await data.loadMeasurements(objective.id) }
        }
        .confirmationDialog("\(objective.title) · 打卡", isPresented: $choose, titleVisibility: .visible) {
            ForEach(dims, id: \.id) { g in
                Button("\(g.title) +1　（\(Fmt.number(g.done))/\(Fmt.number(g.goalTarget))）") {
                    Task {
                        if await data.checkIn(g.id, amount: 1) != nil { toaster.show("\(objective.title) · \(g.title) +1") }
                    }
                }
            }
        }
        .accessibilityIdentifier("objective-\(objective.id)")
    }

    private var readings: [Double]? {
        data.measurements[objective.id].map { $0.suffix(12).map(\.value) }
    }

    private func subtitle(done: Double, total: Double) -> String {
        guard !objective.goals.isEmpty else {
            return objective.hasMetric && objective.progress.hasReading_p
                ? "\(objective.metricName) \(Fmt.number(objective.progress.metricCurrent))\(objective.metricUnit)"
                : "\(Int((objective.fraction * 100).rounded()))%"
        }
        let period = objective.goals.first?.period.current ?? "本周"
        return "\(Fmt.number(done))/\(Fmt.number(total)) \(period) · \(objective.goals.count) 个维度"
    }
}

/// The 40 pt ring used by goal rows: accent while open, green with a check once met.
struct GoalRing: View {
    let fraction: Double
    let done: Bool
    var size: CGFloat = 40
    var body: some View {
        ProgressRing(value: fraction, lineWidth: 4.5, tint: done ? .success : nil) {
            if done { Image(systemName: "checkmark").font(.system(size: 13, weight: .bold)).foregroundStyle(.success) }
        }
        .frame(width: size, height: size)
    }
}

/// How long, for a minutes goal: 15 / 30 / 45 / 60 and, with a running total, whether this counts as one (课时 +1).
struct MinutesCheckInSheet: View {
    @Environment(\.dismiss) private var dismiss
    let goal: Noted_V1_Goal
    let onSave: (Int, Double) -> Void
    @State private var minutes = 30
    @State private var countsOne = true

    var body: some View {
        NavigationStack {
            List {
                Section {
                    HStack(spacing: 8) {
                        ForEach([15, 30, 45, 60], id: \.self) { m in
                            Chip(title: "\(m) 分钟", isOn: minutes == m) { minutes = m }
                        }
                    }
                    .frame(maxWidth: .infinity)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 10, leading: 0, bottom: 12, trailing: 0))
                }
                if goal.hasCounter {
                    Section {
                        Toggle("\(goal.counterUnit.isEmpty ? "累计" : goal.counterUnit) +1", isOn: $countsOne).tint(.switchOn)
                    }
                }
            }
            .listSectionSpacing(.compact)
            .navigationTitle("\(goal.title) · 记录时长")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("记录") {
                        onSave(minutes, goal.hasCounter && countsOne ? 1 : 0)
                        dismiss()
                    }
                    .fontWeight(.semibold)
                }
            }
        }
        .presentationDetents([.height(goal.hasCounter ? 290 : 230)])
    }
}
