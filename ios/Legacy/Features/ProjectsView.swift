import SwiftUI

struct ProjectsView: View {
    @Environment(Session.self) private var session
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @State private var projects: [Noted_V1_Project] = []
    @State private var goals: [Noted_V1_Goal] = []
    @State private var objectives: [Noted_V1_Objective] = []
    @State private var holdings: [Noted_V1_Holding] = []
    @State private var newHolding = false
    @State private var newObjective = false
    @State private var error: String?
    #if DEBUG
    @State private var tab = Int(ProcessInfo.processInfo.environment["NOTED_PTAB"] ?? "") ?? 0
    #else
    @State private var tab = 0
    #endif
    @State private var showArchived = false
    @State private var naming = false
    @State private var newTitle = ""
    @State private var pendingDelete: PendingDelete?

    var body: some View {
        TabScreen(title: "项目") {
            ModeSwitch(mini: true)
        } content: {
            UnderlineTabs(items: ["项目", "目标", "标的"], selected: tab) { tab = $0 }
            ErrorLine(text: error)
            if tab == 0 {
                if projects.isEmpty {
                    Text("还没有项目").font(.ui(14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 80).card()
                }
                let live = projects.filter { !$0.archived }
                let pinned = live.filter(\.pinned), rest = live.filter { !$0.pinned }
                if !pinned.isEmpty {
                    EyebrowSection(title: "置顶") {
                        VStack(spacing: 14) {
                            ForEach(pinned, id: \.id) { pr in
                                ProjectCard(project: pr)
                                    .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: p.radius))
                                    .deleteMenu { pendingDelete = .project(pr) }
                            }
                        }
                    }
                }
                if !rest.isEmpty {
                    EyebrowSection(title: "进行中") { rows(rest) }
                }
                if showArchived {
                    EyebrowSection(title: "已归档") { rows(projects.filter(\.archived)) }
                }
                HStack(spacing: 10) {
                    chip("+ 新建项目") { newTitle = ""; naming = true }
                    let n = projects.filter(\.archived).count
                    if n > 0 { chip(showArchived ? "收起已归档" : "已归档 \(n)", muted: true) { showArchived.toggle() } }
                }
            } else if tab == 2 {
                if holdings.isEmpty {
                    Text("还没有标的").font(.ui(14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 80).card()
                }
                ForEach(holdings, id: \.id) { h in
                    HoldingCard(holding: h) { Task { await load() } }
                        .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: p.radius))
                        .deleteMenu { pendingDelete = .holding(h) }
                }
                chip("+ 新建标的") { newHolding = true }.accessibilityIdentifier("new-holding")
            } else {
                let loose = goals.filter { $0.objectiveID.isEmpty }
                if loose.isEmpty && objectives.isEmpty {
                    Text("还没有目标").font(.ui(14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 80).card()
                }
                ForEach(objectives, id: \.id) { o in
                    ObjectiveCard(objective: o) { Task { await load() } }
                        .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: p.radius))
                        .deleteMenu { pendingDelete = .objective(o) }
                }
                ForEach(loose, id: \.id) { g in
                    GoalRow(goal: g) { Task { await load() } }
                        .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: p.radius))
                        .deleteMenu { pendingDelete = .goal(g) }
                }
                HStack(spacing: 8) {
                    chip("+ 新建目标") { router.push(.create(CreateSpec(kind: CaptureView.Kind.goal.rawValue, space: session.mode.space))) }
                        .accessibilityIdentifier("new-goal")
                    chip("+ 多维目标") { newObjective = true }.accessibilityIdentifier("new-objective")
                }
            }
        }
        .task(id: session.mode.rawValue) { await load() }
        .reloadOnReturn { await load() }
        .refreshable { await load() }
        .sheet(isPresented: $newHolding, onDismiss: { Task { await load() } }) { NewHoldingView() }
        .sheet(isPresented: $newObjective, onDismiss: { Task { await load() } }) { NewObjectiveView() }
        .confirmDelete($pendingDelete) { err in error = err; await load() }
        .alert("新建项目", isPresented: $naming) {
            TextField("项目名称", text: $newTitle)
            Button("取消", role: .cancel) {}
            Button("创建") { Task { await create() } }
        }
    }

    private func rows(_ list: [Noted_V1_Project]) -> some View {
        VStack(spacing: 0) {
            ForEach(Array(list.enumerated()), id: \.element.id) { i, pr in
                if i > 0 { Rectangle().fill(p.line).frame(height: 1) }
                ProjectRow(project: pr)
                    .deleteMenu { pendingDelete = .project(pr) }
            }
        }
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
        .clipShape(RoundedRectangle(cornerRadius: p.radius))
        .overlay(RoundedRectangle(cornerRadius: p.radius).strokeBorder(p.line))
    }

    /// The design's small ghost button.
    private func chip(_ label: String, muted: Bool = false, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label).font(.ui(13, .bold)).foregroundStyle(muted ? p.ink2 : p.ink).padding(.horizontal, 12).frame(height: 32)
                .overlay(RoundedRectangle(cornerRadius: p.radiusSm).strokeBorder(p.line))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func create() async {
        let title = newTitle.trimmingCharacters(in: .whitespaces)
        guard let api = session.api, !title.isEmpty else { return }
        var r = Noted_V1_CreateProjectRequest()
        r.project.title = title
        r.project.space = session.mode == .work ? .work : .life
        do { _ = try await api.projects.createProject(r); await load() } catch { self.error = describe(error) }
    }

    private func load() async {
        guard let api = session.api else { return }
        var pr = Noted_V1_ListProjectsRequest(); pr.space = session.mode.space; pr.includeArchived = true
        var gr = Noted_V1_ListGoalsRequest(); gr.space = session.mode.space
        do {
            projects = try await api.projects.listProjects(pr).projects
            goals = try await api.goals.listGoals(gr).goals
            var orq = Noted_V1_ListObjectivesRequest(); orq.space = session.mode.space
            objectives = try await api.objectives.listObjectives(orq).objectives
            holdings = try await api.holdings.listHoldings(Noted_V1_ListHoldingsRequest()).holdings
            error = nil
        } catch { if !(error is CancellationError) { self.error = describe(error) } }
    }
}

struct GoalRow: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    let goal: Noted_V1_Goal
    let onChange: () -> Void

    private var periodName: String {
        switch goal.period {
        case .day: "每天"
        case .month: "每月"
        default: "每周"
        }
    }

    var body: some View {
        let g = goal.progress
        let tint = goal.space.color(p)
        let week = goal.period == .week && g.days.count == 7
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Text(goal.title).font(.ui(15, .bold)).foregroundStyle(p.ink).lineLimit(1)
                Spacer()
                Text("\(periodName) \(Fmt.number(goal.target)) \(goal.unit)").font(.ui(11, .semibold))
                    .padding(.horizontal, 8).frame(height: 22)
                    .background(goal.space == .work ? p.workBg : p.lifeBg, in: RoundedRectangle(cornerRadius: 6))
                    .foregroundStyle(tint)
            }
            HStack(alignment: .bottom, spacing: 10) {
                (Text(Fmt.number(g.done)).font(.mono(week ? 44 : 30, .semibold)).foregroundColor(g.behind ? p.bad : p.ink)
                    + Text("/\(Fmt.number(g.target)) \(goal.unit)").font(.mono(week ? 22 : 16, .semibold)).foregroundColor(p.ink2))
                    .tracking(-1)
                VStack(alignment: .leading, spacing: 3) {
                    if g.achieved {
                        Text("本期已达标").font(.ui(13, .bold)).foregroundStyle(p.ok)
                    } else if g.remaining > 0 {
                        Text("再来 \(Fmt.number(g.remaining)) \(goal.unit)就达标").font(.ui(13, .bold)).foregroundStyle(g.behind ? p.bad : p.ok)
                    }
                    if g.streak > 0 { Text("连续 \(g.streak) 个周期").font(.ui(13)).foregroundStyle(p.ink2) }
                }
                .padding(.bottom, 3)
                Spacer(minLength: 0)
            }
            if week {
                HStack(spacing: 6) {
                    ForEach(Array(g.days.enumerated()), id: \.offset) { i, d in
                        let today = d.date == Fmt.ymd(.now)
                        let past = d.date < Fmt.ymd(.now)
                        VStack(spacing: 5) {
                            Group {
                                if d.amount > 0 {
                                    RoundedRectangle(cornerRadius: 6).fill(tint)
                                        .overlay { Image(systemName: "checkmark").font(.system(size: 12, weight: .heavy)).foregroundStyle(.white) }
                                } else if today {
                                    RoundedRectangle(cornerRadius: 6).strokeBorder(tint, lineWidth: 1.5)
                                } else if past {
                                    RoundedRectangle(cornerRadius: 6).fill(p.surface2)
                                } else {
                                    RoundedRectangle(cornerRadius: 6).strokeBorder(p.ink3, style: StrokeStyle(lineWidth: 1, dash: [3, 2]))
                                }
                            }
                            .frame(height: 30)
                            Text(["一", "二", "三", "四", "五", "六", "日"][i]).font(.mono(13, today ? .semibold : .medium))
                                .foregroundStyle(today ? tint : p.ink2)
                        }
                        .frame(maxWidth: .infinity)
                    }
                }
            } else {
                LineBar(value: g.percent, tint: g.achieved ? p.ok : tint)
            }
            if goal.counterTarget > 0 {
                Text("\(Fmt.number(g.counterDone))/\(Fmt.number(goal.counterTarget)) \(goal.counterUnit)")
                    .font(.mono(13)).foregroundStyle(p.ink2)
            }
            Button {
                Task {
                    var req = Noted_V1_RecordCheckInRequest()
                    req.goalID = goal.id; req.amount = 1
                    _ = try? await session.api?.goals.recordCheckIn(req)
                    onChange()
                }
            } label: {
                Text("打卡 +1").font(.ui(15, .bold)).frame(maxWidth: .infinity).frame(height: 46)
                    .background(tint, in: RoundedRectangle(cornerRadius: p.radiusSm)).foregroundStyle(.white)
            }
            .buttonStyle(.plain)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 2)
        .card()
    }
}

struct ProjectRow: View {
    @Environment(\.palette) private var p
    @Environment(Router.self) private var router
    let project: Noted_V1_Project

    var body: some View {
        let pr = project.progress
        let left = pr.tasksTotal - pr.tasksDone
        let when = project.hasDueTime ? Fmt.md(project.dueTime.date) + "截止" : (project.hasStartTime ? Fmt.md(project.startTime.date) : "没有日期")
        Button { router.push(.project(project.id)) } label: {
            HStack(spacing: 12) {
                Circle().fill(project.space.color(p)).frame(width: 8, height: 8)
                VStack(alignment: .leading, spacing: 3) {
                    Text(project.title).font(.ui(15, .bold)).foregroundStyle(p.ink).lineLimit(1)
                    Text("\(pr.tasksDone)/\(pr.tasksTotal) 待办 · \(when)").font(.ui(13)).foregroundStyle(p.ink2).lineLimit(1)
                }
                Spacer()
                LineBar(value: pr.tasksTotal > 0 ? Double(pr.tasksDone) / Double(pr.tasksTotal) : 0, tint: project.space.color(p)).frame(width: 48)
                    .opacity(left < 0 ? 0 : 1)
            }
            .padding(.horizontal, 14).padding(.vertical, 8).frame(minHeight: 58)
            .background(p.surface)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
