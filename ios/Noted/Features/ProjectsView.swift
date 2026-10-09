import SwiftUI

struct ProjectsView: View {
    @Environment(Session.self) private var session
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

    var body: some View {
        Screen(eyebrow: tab == 0 ? "\(projects.filter { !$0.archived }.count) 个进行中" : tab == 1 ? "\(goals.filter { $0.objectiveID.isEmpty }.count + objectives.count) 个目标" : "\(holdings.count) 个标的", title: ["项目", "目标", "标的"][tab], trailing: AnyView(ModeSwitch(mini: true))) {
            HStack(spacing: 4) {
                ForEach(Array(["项目", "目标", "标的"].enumerated()), id: \.offset) { i, label in
                    Button { tab = i } label: {
                        Text(label).font(.system(size: 14, weight: tab == i ? .semibold : .medium))
                            .foregroundStyle(tab == i ? p.ink : p.ink3).padding(.vertical, 6).padding(.horizontal, 10)
                            .overlay(alignment: .bottom) { if tab == i { Rectangle().fill(session.mode.accent(p)).frame(height: 2) } }
                    }
                }
                Spacer()
            }
            ErrorLine(text: error)
            if tab == 0 {
                if projects.isEmpty {
                    Text("还没有项目").font(.system(size: 14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 80).card()
                }
                let live = projects.filter { !$0.archived }
                let pinned = live.filter(\.pinned), rest = live.filter { !$0.pinned }
                if !pinned.isEmpty {
                    Eyebrow(text: "置顶")
                    ForEach(pinned, id: \.id) { ProjectCard(project: $0) }
                }
                if !rest.isEmpty {
                    Eyebrow(text: "进行中")
                    VStack(spacing: 0) {
                        ForEach(Array(rest.enumerated()), id: \.element.id) { i, pr in
                            if i > 0 { Divider().overlay(p.line) }
                            ProjectRow(project: pr)
                        }
                    }
                    .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                    .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
                }
                if showArchived {
                    Eyebrow(text: "已归档")
                    VStack(spacing: 0) {
                        ForEach(Array(projects.filter(\.archived).enumerated()), id: \.element.id) { i, pr in
                            if i > 0 { Divider().overlay(p.line) }
                            ProjectRow(project: pr)
                        }
                    }
                    .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                    .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
                }
                HStack(spacing: 8) {
                    chip("+ 新建项目") { newTitle = ""; naming = true }
                    let n = projects.filter(\.archived).count
                    if n > 0 { chip(showArchived ? "收起已归档" : "已归档 \(n)") { showArchived.toggle() } }
                }
            } else if tab == 2 {
                if holdings.isEmpty {
                    Text("还没有标的").font(.system(size: 14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 80).card()
                }
                ForEach(holdings, id: \.id) { h in HoldingCard(holding: h) { Task { await load() } } }
                chip("+ 新建标的") { newHolding = true }.accessibilityIdentifier("new-holding")
            } else {
                let loose = goals.filter { $0.objectiveID.isEmpty }
                if loose.isEmpty && objectives.isEmpty {
                    Text("还没有目标").font(.system(size: 14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 80).card()
                }
                ForEach(objectives, id: \.id) { o in ObjectiveCard(objective: o) { Task { await load() } } }
                ForEach(loose, id: \.id) { g in GoalRow(goal: g) { Task { await load() } } }
                chip("+ 新建目标") { newObjective = true }.accessibilityIdentifier("new-objective")
            }
        }
        .task(id: session.mode.rawValue) { await load() }
        .refreshable { await load() }
        .sheet(isPresented: $newHolding, onDismiss: { Task { await load() } }) { NewHoldingView() }
        .sheet(isPresented: $newObjective, onDismiss: { Task { await load() } }) { NewObjectiveView() }
        .alert("新建项目", isPresented: $naming) {
            TextField("项目名称", text: $newTitle)
            Button("取消", role: .cancel) {}
            Button("创建") { Task { await create() } }
        }
    }

    private func chip(_ label: String, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label).font(.system(size: 13, weight: .semibold)).foregroundStyle(p.ink).padding(.horizontal, 14).frame(height: 36)
                .background(p.surface, in: RoundedRectangle(cornerRadius: 10)).overlay(RoundedRectangle(cornerRadius: 10).stroke(p.line))
        }
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

    var body: some View {
        let g = goal.progress
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(goal.title).font(.system(size: 16, weight: .semibold)).foregroundStyle(p.ink)
                Spacer()
                Text("\(Fmt.number(g.done))/\(Fmt.number(g.target)) \(goal.unit)")
                    .font(.system(size: 13, design: .monospaced)).foregroundStyle(g.behind ? p.bad : p.ink2)
            }
            ProgressBar(value: g.percent, tint: g.achieved ? p.ok : goal.space.color(p))
            if goal.period == .week, g.days.count == 7 {
                HStack(spacing: 0) {
                    ForEach(Array(g.days.enumerated()), id: \.offset) { i, d in
                        VStack(spacing: 4) {
                            Circle().fill(d.amount > 0 ? goal.space.color(p) : p.surface2).frame(width: 14, height: 14)
                            Text(["一", "二", "三", "四", "五", "六", "日"][i]).font(.system(size: 11, design: .monospaced)).foregroundStyle(p.ink2)
                        }.frame(maxWidth: .infinity)
                    }
                }
            }
            HStack {
                if g.streak > 0 { Text("连续 \(g.streak) 个周期").font(.system(size: 12)).foregroundStyle(p.ink2) }
                if goal.counterTarget > 0 {
                    Text("\(Fmt.number(g.counterDone))/\(Fmt.number(goal.counterTarget)) \(goal.counterUnit)")
                        .font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2)
                }
                Spacer()
                Button {
                    Task {
                        var req = Noted_V1_RecordCheckInRequest()
                        req.goalID = goal.id; req.amount = 1
                        _ = try? await session.api?.goals.recordCheckIn(req)
                        onChange()
                    }
                } label: {
                    Text("打卡 +1").font(.system(size: 13, weight: .semibold)).padding(.horizontal, 12).frame(height: 30)
                        .background(p.surface2, in: RoundedRectangle(cornerRadius: 8)).foregroundStyle(p.ink)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }
}

struct ProjectRow: View {
    @Environment(\.palette) private var p
    let project: Noted_V1_Project
    @State private var open = false

    var body: some View {
        let pr = project.progress
        let left = pr.tasksTotal - pr.tasksDone
        let when = project.hasDueTime ? Fmt.md(project.dueTime.date) + "截止" : (project.hasStartTime ? Fmt.md(project.startTime.date) : "没有日期")
        Button { open = true } label: {
            HStack(spacing: 12) {
                Circle().fill(project.space.color(p)).frame(width: 8, height: 8)
                VStack(alignment: .leading, spacing: 2) {
                    Text(project.title).font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink).lineLimit(1)
                    Text("\(pr.tasksDone)/\(pr.tasksTotal) 待办 · \(when)").font(.system(size: 12)).foregroundStyle(p.ink2).lineLimit(1)
                }
                Spacer()
                ProgressBar(value: pr.tasksTotal > 0 ? Double(pr.tasksDone) / Double(pr.tasksTotal) : 0, tint: project.space.color(p)).frame(width: 48)
                    .opacity(left < 0 ? 0 : 1)
            }
            .padding(.horizontal, 14).frame(minHeight: 56).contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .sheet(isPresented: $open) { ProjectDetailView(id: project.id) }
    }
}
