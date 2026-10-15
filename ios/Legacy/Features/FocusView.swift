import SwiftUI

struct FocusView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @State private var horizon: Noted_V1_Horizon = .today
    @State private var data: Noted_V1_GetFocusResponse?
    @State private var error: String?
    @Environment(Router.self) private var router
    @State private var pending = 0
    @State private var briefing = ""
    @State private var recent: [Noted_V1_Note] = []
    @State private var pendingDelete: PendingDelete?

    private let horizons: [(Noted_V1_Horizon, String)] = [(.today, "今天"), (.upcoming, "接下来"), (.week, "这周"), (.month, "这个月")]

    var body: some View {
        TabScreen(eyebrow: Fmt.eyebrow(.now), title: "概览") {
            avatar
        } content: {
            ModeSwitch()
            UnderlineTabs(items: horizons.map(\.1), selected: horizons.firstIndex { $0.0 == horizon } ?? 0) { horizon = horizons[$0].0 }
            ErrorLine(text: error)
            if pending > 0 {
                Button { router.push(.suggestions) } label: {
                    Text("\(pending) 条建议 ›").font(.ui(13, .bold)).foregroundStyle(session.mode.accent(p))
                        .frame(minHeight: 24).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .padding(.top, -2)
            }
            if let d = data {
                ForEach(session.layout.modules(for: session.mode).filter(\.on), id: \.id) { m in
                    module(m.id, d)
                }
            }
        }
        .task(id: "\(horizon.rawValue)-\(session.mode.rawValue)-\(session.aiChat)-\(session.layout.modules(for: session.mode).filter(\.on).map(\.id).joined(separator: ","))") { await load() }
        .refreshable { await load() }
        .reloadOnReturn { await load() }
        .confirmDelete($pendingDelete) { err in error = err; await load() }
        .onAppear(perform: openFromLaunch)
    }

    /// Screenshot and UI-test runs can start on a given screen.
    private func openFromLaunch() {
        #if DEBUG
        guard router.path.isEmpty, !FocusView.launched else { return }
        FocusView.launched = true
        switch ProcessInfo.processInfo.environment["NOTED_SHEET"] ?? "" {
        case "aisettings", "themeeditor", "modules", "paywall", "settings": router.push(.settings)
        case "suggestions": router.push(.suggestions)
        case "review": router.push(.review)
        case "ask", "plan": router.push(.ask)
        default: break
        }
        #endif
    }
    #if DEBUG
    @MainActor private static var launched = false
    #endif

    @ViewBuilder private func module(_ id: String, _ d: Noted_V1_GetFocusResponse) -> some View {
        switch id {
        case "pinned": if !d.pinnedProjects.isEmpty { pinned(d.pinnedProjects) }
        case "items": items(d.items)
        case "goals": if !d.goals.isEmpty { goals(d.goals) }
        case "briefing": if session.aiChat, !briefing.isEmpty { briefingBlock }
        case "notes": if !recent.isEmpty { recentNotes }
        case "heat": if !d.goals.isEmpty { heat(d.goals) }
        default: EmptyView()
        }
    }

    private var briefingBlock: some View {
        EyebrowSection(title: "今日简报") {
            Text(briefing).font(.ui(15)).foregroundStyle(p.ink).frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 2).card()
        }
        .padding(.top, 2)
    }

    private var recentNotes: some View {
        EyebrowSection(title: "最近笔记") {
            VStack(spacing: 0) {
                ForEach(Array(recent.enumerated()), id: \.element.id) { i, n in
                    if i > 0 { Rectangle().fill(p.line).frame(height: 1) }
                    HStack(spacing: 12) {
                        Text(n.title.isEmpty ? "无标题" : n.title).font(.ui(15)).foregroundStyle(p.ink).lineLimit(1).layoutPriority(1)
                        Text(n.content).font(.ui(13)).foregroundStyle(p.ink2).lineLimit(1)
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                    .background(p.surface)
                    .contentShape(Rectangle())
                    .onTapGesture { router.push(.note(n)) }
                    .deleteMenu { pendingDelete = .note(n) }
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
            .clipShape(RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).strokeBorder(p.line))
        }
        .padding(.top, 2)
    }

    private func heat(_ goals: [Noted_V1_Goal]) -> some View {
        EyebrowSection(title: "近 14 天") {
            VStack(spacing: 10) {
                ForEach(goals, id: \.id) { g in
                    HStack(spacing: 3) {
                        Text(g.title).font(.ui(13, .bold)).foregroundStyle(p.ink).frame(width: 56, alignment: .leading).lineLimit(1)
                            .padding(.trailing, 6)
                        ForEach(Array(g.progress.recent.enumerated()), id: \.offset) { _, d in
                            Rectangle().fill(d.amount > 0 ? g.space.color(p) : p.surface2).frame(height: 14)
                        }
                    }
                }
            }
            .padding(.horizontal, 2).card()
        }
        .padding(.top, 2)
    }

    private var avatar: some View {
        HStack(spacing: 8) {
            if session.aiChat {
                Button { router.push(.ask) } label: {
                    Image(systemName: "text.bubble").font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink)
                        .frame(width: 38, height: 38).background(p.surface, in: Circle()).overlay(Circle().stroke(p.line))
                }.accessibilityLabel("提问")
            }
            profile
        }
    }

    private var profile: some View {
        Button { router.push(.settings) } label: {
            Image(systemName: "person").font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink)
                .frame(width: 38, height: 38).background(p.surface, in: Circle()).overlay(Circle().stroke(p.line))
        }.accessibilityLabel("设置")
    }

    private func load() async {
        guard let api = session.api else { return }
        var req = Noted_V1_GetFocusRequest()
        req.horizon = horizon
        req.space = session.mode.space
        req.timeZone = TimeZone.current.identifier
        do {
            data = try await api.focus.getFocus(req)
            var pr = Noted_V1_ListProposalsRequest()
            pr.space = session.mode.space
            pending = (try? await api.suggest.listProposals(pr).proposals.count) ?? 0
            let on = session.layout.modules(for: session.mode).filter(\.on).map(\.id)
            if on.contains("notes") {
                var nr = Noted_V1_ListNotesRequest(); nr.pageSize = 3; nr.space = session.mode.space
                recent = (try? await api.notes.listNotes(nr).notes) ?? []
            }
            if on.contains("briefing"), session.aiChat {
                var br = Noted_V1_DailyBriefingRequest(); br.space = session.mode.space; br.timeZone = TimeZone.current.identifier
                briefing = (try? await api.ai.dailyBriefing(br).briefing) ?? ""
            }
            error = nil
            Task { await session.syncExtras() }
        } catch { if !(error is CancellationError) { self.error = describe(error) } }
    }

    private func pinned(_ projects: [Noted_V1_Project]) -> some View {
        EyebrowSection(title: "置顶项目") {
            ScrollView(.horizontal) {
                HStack(spacing: 10) {
                    ForEach(projects, id: \.id) { pr in
                        ProjectCard(project: pr).frame(width: 250)
                            .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: p.radius))
                            .deleteMenu { pendingDelete = .project(pr) }
                    }
                }
                .padding(.trailing, 16)
            }
            .scrollIndicators(.hidden)
            .padding(.trailing, -16)
        }
        .padding(.top, 2)
    }

    /// On today, the event that is on now or comes next: the row the day is at.
    private func currentIndex(_ items: [Noted_V1_FocusItem]) -> Int? {
        guard horizon == .today else { return nil }
        return items.firstIndex {
            if case .event(let o) = $0.item { return !o.event.allDay && o.endTime.date > .now }
            return false
        }
    }

    private func items(_ items: [Noted_V1_FocusItem]) -> some View {
        let now = currentIndex(items)
        return EyebrowSection(title: horizons.first { $0.0 == horizon }?.1 ?? "") {
            Text("\(items.count)").font(.mono(13)).foregroundStyle(p.ink2)
        } content: {
            if items.isEmpty {
                Text("没有安排").font(.ui(14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 60)
                    .card()
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(items.enumerated()), id: \.offset) { i, it in
                        if i > 0 { Rectangle().fill(p.line).frame(height: 1) }
                        FocusRow(item: it, current: i == now, onToggle: { Task { await load() } })
                            .contentShape(Rectangle())
                            .onTapGesture { open(it) }
                            .contextMenu {
                                if let d = deletable(it) {
                                    Button(role: .destructive) { pendingDelete = d } label: { Label("删除", systemImage: "trash") }
                                }
                            }
                    }
                }
                .background(p.surface)
                .clipShape(RoundedRectangle(cornerRadius: p.radius))
                .overlay(RoundedRectangle(cornerRadius: p.radius).strokeBorder(p.line, lineWidth: 1))
            }
        }
        .padding(.top, 2)
    }

    private func goals(_ goals: [Noted_V1_Goal]) -> some View {
        EyebrowSection(title: "目标") {
            Button { router.push(.review) } label: {
                Text("回顾").font(.ui(13, .bold)).foregroundStyle(session.mode.accent(p))
            }
            .buttonStyle(.plain)
        } content: {
            if goals.count <= 3 {
                HStack(spacing: 8) {
                    ForEach(goals, id: \.id) { g in goalCard(g).frame(maxWidth: .infinity) }
                }
            } else {
                ScrollView(.horizontal) {
                    HStack(spacing: 8) {
                        ForEach(goals, id: \.id) { g in goalCard(g).frame(width: 140) }
                    }
                    .padding(.trailing, 16)
                }
                .scrollIndicators(.hidden)
                .padding(.trailing, -16)
            }
        }
        .padding(.top, 2)
    }

    private func goalCard(_ g: Noted_V1_Goal) -> some View {
        GoalCard(goal: g) { Task { await load() } }
            .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: p.radius))
            .deleteMenu { pendingDelete = .goal(g) }
    }

    /// An event opens its own page, a to-do its page.
    private func open(_ it: Noted_V1_FocusItem) {
        switch it.item {
        case .event(let o): router.push(.event(o.event))
        case .task(let t): router.push(.task(t))
        case nil: break
        }
    }

    private func deletable(_ it: Noted_V1_FocusItem) -> PendingDelete? {
        switch it.item {
        case .event(let o): .event(o.event)
        case .task(let t): .task(t)
        case nil: nil
        }
    }
}

struct FocusRow: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.mode) private var mode
    let item: Noted_V1_FocusItem
    /// The row the day is at now: shaded, its time in the accent.
    var current = false
    let onToggle: () -> Void

    private var past: Bool {
        if case .event(let occ) = item.item { return !occ.event.allDay && occ.endTime.date < .now }
        return false
    }

    var body: some View {
        HStack(spacing: 12) {
            switch item.item {
            case .event(let occ):
                Text(occ.event.allDay ? "全天" : Fmt.hm(occ.startTime.date))
                    .font(.mono(12, current ? .semibold : .medium)).foregroundStyle(current ? mode.accent(p) : p.ink2)
                    .lineLimit(1).fixedSize().frame(width: 42, alignment: .leading)
                Circle().fill(occ.event.space.color(p)).frame(width: 8, height: 8)
                Text(occ.event.title).font(.ui(15, current ? .bold : .medium)).foregroundStyle(p.ink)
                    .strikethrough(past, color: p.ink3).lineLimit(1)
                Spacer(minLength: 0)
                if past {
                    CheckBox(done: true)
                } else if !occ.event.allDay, let d = Fmt.duration(from: occ.startTime.date, to: occ.endTime.date) {
                    Text(d).font(.mono(13)).foregroundStyle(p.ink2)
                }
            case .task(let t):
                let dueToday = t.hasDueTime && Calendar.current.isDateInToday(t.dueTime.date)
                Button { toggle(t) } label: {
                    CheckBox(hot: item.overdue || dueToday).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("完成 \(t.title)")
                Circle().fill(t.space.color(p)).frame(width: 8, height: 8)
                Text(t.title).font(.ui(15)).foregroundStyle(p.ink).lineLimit(1)
                Spacer(minLength: 0)
                if item.overdue {
                    badTag("已逾期")
                } else if dueToday {
                    badTag("今天到期")
                } else if t.hasDueTime {
                    Text(Fmt.md(t.dueTime.date)).font(.mono(13)).foregroundStyle(p.ink2)
                }
            case nil:
                EmptyView()
            }
        }
        .padding(.horizontal, 14).frame(minHeight: p.rowHeight)
        .background(current ? p.surface2 : p.surface)
        .opacity(past ? 0.55 : 1)
    }

    private func badTag(_ text: String) -> some View {
        Text(text).font(.ui(11, .semibold)).padding(.horizontal, 8).frame(height: 22)
            .background(p.badBg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(p.bad)
    }

    private func toggle(_ t: Noted_V1_Task) {
        Task {
            var req = Noted_V1_UpdateTaskRequest()
            req.task = t
            req.task.completed = true
            req.updateMask = fieldMask("completed")
            _ = try? await session.api?.calendar.updateTask(req)
            onToggle()
        }
    }
}

struct ProjectCard: View {
    @Environment(\.palette) private var p
    let project: Noted_V1_Project
    @Environment(Router.self) private var router
    private var autoOpen: Bool { ProcessInfo.processInfo.environment["NOTED_SHEET"] == "project" && project.space == .life }

    var body: some View {
        card.contentShape(Rectangle()).onTapGesture { router.push(.project(project.id)) }
            .onAppear { if autoOpen && router.path.isEmpty { router.push(.project(project.id)) } }
    }

    /// "1/3 · 下一步 办理签证 10月18日": the count, then what comes next and when.
    private var summary: String {
        let pr = project.progress
        var s = "\(pr.tasksDone)/\(pr.tasksTotal)"
        guard !pr.nextTitle.isEmpty else { return s }
        s += " · 下一步 \(pr.nextTitle)"
        if pr.hasNextTime {
            let t = pr.nextTime.date
            s += Calendar.current.isDateInToday(t) ? " 今天 \(Fmt.hm(t))" : " \(Fmt.md(t))"
        }
        return s
    }

    @ViewBuilder private var card: some View {
        let pr = project.progress
        let done = pr.tasksTotal > 0 ? Double(pr.tasksDone) / Double(pr.tasksTotal) : 0
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                SpaceTag(space: project.space)
                Spacer()
                if pr.hasDaysLeft { Text("\(pr.daysLeft) 天").font(.mono(13, .semibold)).foregroundStyle(p.ink2) }
            }
            Text(project.title).font(.ui(17, .bold)).tracking(-0.17).foregroundStyle(p.ink).lineLimit(1)
            LineBar(value: done, tint: project.space.color(p))
            Text(summary).font(.ui(13)).foregroundStyle(p.ink2).lineLimit(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 2)
        .card(padding: 14)
    }
}

struct GoalCard: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    let goal: Noted_V1_Goal
    let onChange: () -> Void

    var body: some View {
        let g = goal.progress
        Button {
            Task {
                var req = Noted_V1_RecordCheckInRequest()
                req.goalID = goal.id
                req.amount = 1
                _ = try? await session.api?.goals.recordCheckIn(req)
                onChange()
            }
        } label: {
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 4) {
                    Text(goal.title).font(.ui(13, .bold)).foregroundStyle(p.ink).lineLimit(1)
                    Spacer(minLength: 4)
                    Text("\(Fmt.number(g.done))/\(Fmt.number(g.target))")
                        .font(.mono(13)).foregroundStyle(g.behind ? p.bad : p.ink2).lineLimit(1)
                }
                LineBar(value: g.percent, tint: g.achieved ? p.ok : goal.space.color(p))
            }
            .padding(.horizontal, 12).padding(.vertical, 10)
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
            .overlay(RoundedRectangle(cornerRadius: p.radius).strokeBorder(g.behind ? p.bad : p.line, lineWidth: 1))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
