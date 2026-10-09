import SwiftUI

struct FocusView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @State private var horizon: Noted_V1_Horizon = .today
    @State private var data: Noted_V1_GetFocusResponse?
    @State private var error: String?
    @State private var settings = ["aisettings", "themeeditor", "modules", "paywall", "settings"].contains(ProcessInfo.processInfo.environment["NOTED_SHEET"] ?? "")
    @State private var suggestions = ProcessInfo.processInfo.environment["NOTED_SHEET"] == "suggestions"
    @State private var review = ProcessInfo.processInfo.environment["NOTED_SHEET"] == "review"
    @State private var pending = 0
    @State private var briefing = ""
    @State private var recent: [Noted_V1_Note] = []
    @State private var asking = ProcessInfo.processInfo.environment["NOTED_SHEET"] == "ask" || ProcessInfo.processInfo.environment["NOTED_SHEET"] == "plan"

    private let horizons: [(Noted_V1_Horizon, String)] = [(.today, "今天"), (.upcoming, "接下来"), (.week, "这周"), (.month, "这个月")]

    var body: some View {
        Screen(eyebrow: Fmt.eyebrow(.now), title: "概览", trailing: AnyView(avatar)) {
            ModeSwitch()
            HStack(spacing: 4) {
                ForEach(horizons, id: \.0) { h, label in
                    Button { horizon = h } label: {
                        Text(label).font(.system(size: 14, weight: horizon == h ? .semibold : .medium))
                            .foregroundStyle(horizon == h ? p.ink : p.ink3)
                            .padding(.vertical, 6).padding(.horizontal, 10)
                            .overlay(alignment: .bottom) {
                                if horizon == h { Rectangle().fill(session.mode.accent(p)).frame(height: 2) }
                            }
                    }
                }
                Spacer()
            }
            ErrorLine(text: error)
            if pending > 0 {
                Button { suggestions = true } label: {
                    HStack {
                        Text("\(pending) 条建议").font(.system(size: 14, weight: .semibold)).foregroundStyle(session.mode.accent(p))
                        Text("你不点,就不会执行").font(.system(size: 13)).foregroundStyle(p.ink2)
                        Spacer()
                        Image(systemName: "chevron.right").font(.system(size: 12)).foregroundStyle(p.ink3)
                    }
                    .padding(.horizontal, 14).frame(height: 44).card(padding: 0)
                }.buttonStyle(.plain)
            }
            if let d = data {
                ForEach(session.layout.modules(for: session.mode).filter(\.on), id: \.id) { m in
                    module(m.id, d)
                }
            }
        }
        .task(id: "\(horizon.rawValue)-\(session.mode.rawValue)-\(session.aiChat)-\(session.layout.modules(for: session.mode).filter(\.on).map(\.id).joined(separator: ","))") { await load() }
        .refreshable { await load() }
        .sheet(isPresented: $settings) { SettingsView() }
        .sheet(isPresented: $suggestions, onDismiss: { Task { await load() } }) { SuggestionsView() }
        .sheet(isPresented: $review) { ReviewView() }
        .sheet(isPresented: $asking, onDismiss: { Task { await load() } }) { AskView() }
    }

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
        VStack(alignment: .leading, spacing: 8) {
            Eyebrow(text: "今日简报")
            Text(briefing).font(.system(size: 14)).foregroundStyle(p.ink).frame(maxWidth: .infinity, alignment: .leading).card()
        }
    }

    private var recentNotes: some View {
        VStack(alignment: .leading, spacing: 8) {
            Eyebrow(text: "最近笔记")
            VStack(spacing: 0) {
                ForEach(Array(recent.enumerated()), id: \.element.id) { i, n in
                    if i > 0 { Divider().overlay(p.line) }
                    HStack(spacing: 10) {
                        Text(n.title.isEmpty ? "无标题" : n.title).font(.system(size: 15, weight: .medium)).foregroundStyle(p.ink)
                        Text(n.content).font(.system(size: 13)).foregroundStyle(p.ink2).lineLimit(1)
                        Spacer(minLength: 0)
                    }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
    }

    private func heat(_ goals: [Noted_V1_Goal]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Eyebrow(text: "近 14 天")
            VStack(spacing: 10) {
                ForEach(goals, id: \.id) { g in
                    HStack(spacing: 10) {
                        Text(g.title).font(.system(size: 13, weight: .semibold)).foregroundStyle(p.ink).frame(width: 56, alignment: .leading).lineLimit(1)
                        HStack(spacing: 3) {
                            ForEach(Array(g.progress.recent.enumerated()), id: \.offset) { _, d in
                                RoundedRectangle(cornerRadius: 3).fill(d.amount > 0 ? g.space.color(p) : p.surface2).frame(height: 18)
                            }
                        }
                    }
                }
            }.card()
        }
    }

    private var avatar: some View {
        HStack(spacing: 8) {
            if session.aiChat {
                Button { asking = true } label: {
                    Image(systemName: "text.bubble").font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink)
                        .frame(width: 38, height: 38).background(p.surface, in: Circle()).overlay(Circle().stroke(p.line))
                }.accessibilityLabel("提问")
            }
            profile
        }
    }

    private var profile: some View {
        Button { settings = true } label: {
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
        VStack(alignment: .leading, spacing: 8) {
            Eyebrow(text: "置顶项目")
            ScrollView(.horizontal) {
                HStack(spacing: 10) {
                    ForEach(projects, id: \.id) { pr in ProjectCard(project: pr).frame(width: 250) }
                }
            }.scrollIndicators(.hidden)
        }
    }

    private func items(_ items: [Noted_V1_FocusItem]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Eyebrow(text: horizons.first { $0.0 == horizon }?.1 ?? "")
                Spacer()
                Text("\(items.count)").font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2)
            }
            if items.isEmpty {
                Text("没有安排").font(.system(size: 14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 60)
                    .card()
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(items.enumerated()), id: \.offset) { i, it in
                        if i > 0 { Divider().overlay(p.line) }
                        FocusRow(item: it, onToggle: { Task { await load() } })
                    }
                }
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))
            }
        }
    }

    private func goals(_ goals: [Noted_V1_Goal]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Eyebrow(text: "目标")
                Spacer()
                Button("回顾") { review = true }.font(.system(size: 13, weight: .semibold))
            }
            ScrollView(.horizontal) {
                HStack(spacing: 8) {
                    ForEach(goals, id: \.id) { g in GoalCard(goal: g) { Task { await load() } }.frame(width: 140) }
                }
            }.scrollIndicators(.hidden)
        }
    }
}

struct FocusRow: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    let item: Noted_V1_FocusItem
    let onToggle: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            switch item.item {
            case .event(let occ):
                Text(occ.event.allDay ? "全天" : Fmt.hm(occ.startTime.date))
                    .font(.system(size: 12, weight: .medium, design: .monospaced)).foregroundStyle(p.ink2).lineLimit(1).fixedSize().frame(width: 46, alignment: .leading)
                Circle().fill(occ.event.space.color(p)).frame(width: 8, height: 8)
                Text(occ.event.title).font(.system(size: 15, weight: .medium)).foregroundStyle(occ.endTime.date < .now ? p.ink3 : p.ink)
                Spacer()
                if !occ.event.allDay, let d = Fmt.duration(from: occ.startTime.date, to: occ.endTime.date) {
                    Text(d).font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2)
                }
            case .task(let t):
                Button { toggle(t) } label: {
                    RoundedRectangle(cornerRadius: 6)
                        .strokeBorder(item.overdue ? p.bad : p.ink3, lineWidth: 1.75).frame(width: 20, height: 20)
                        .frame(width: 46, alignment: .leading)
                }
                .accessibilityLabel("完成 \(t.title)")
                Circle().fill(t.space.color(p)).frame(width: 8, height: 8)
                Text(t.title).font(.system(size: 15, weight: .medium)).foregroundStyle(p.ink)
                Spacer()
                if item.overdue {
                    Text("已逾期").font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
                        .background(p.badBg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(p.bad)
                } else if t.hasDueTime, Calendar.current.isDateInToday(t.dueTime.date) {
                    Text("今天到期").font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
                        .background(p.badBg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(p.bad)
                } else if t.hasDueTime {
                    Text(Fmt.md(t.dueTime.date)).font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2)
                }
            case nil:
                EmptyView()
            }
        }
        .padding(.horizontal, 14).frame(minHeight: p.rowHeight)
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
    @State private var open = false
    private var autoOpen: Bool { ProcessInfo.processInfo.environment["NOTED_SHEET"] == "project" && project.space == .life }

    var body: some View {
        card.onTapGesture { open = true }
            .onAppear { if autoOpen { open = true } }
            .sheet(isPresented: $open) { ProjectDetailView(id: project.id) }
    }

    @ViewBuilder private var card: some View {
        let pr = project.progress
        let done = pr.tasksTotal > 0 ? Double(pr.tasksDone) / Double(pr.tasksTotal) : 0
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                SpaceTag(space: project.space)
                Spacer()
                if pr.hasDaysLeft { Text("\(pr.daysLeft) 天").font(.system(size: 13, weight: .semibold, design: .monospaced)).foregroundStyle(p.ink) }
            }
            Text(project.title).font(.system(size: 17, weight: .semibold)).foregroundStyle(p.ink).lineLimit(1)
            ProgressBar(value: done, tint: project.space.color(p))
            Text(pr.nextTitle.isEmpty ? "\(pr.tasksDone)/\(pr.tasksTotal)" : "\(pr.tasksDone)/\(pr.tasksTotal) · 下一步 \(pr.nextTitle)")
                .font(.system(size: 13)).foregroundStyle(p.ink2).lineLimit(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
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
                HStack {
                    Text(goal.title).font(.system(size: 13, weight: .semibold)).foregroundStyle(p.ink).lineLimit(1)
                    Spacer(minLength: 4)
                    Text("\(Fmt.number(g.done))/\(Fmt.number(g.target))")
                        .font(.system(size: 12, design: .monospaced)).foregroundStyle(g.behind ? p.bad : p.ink2)
                }
                ProgressBar(value: g.percent, tint: g.achieved ? p.ok : goal.space.color(p))
            }
            .card(padding: 12)
            .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(g.behind ? p.bad : .clear, lineWidth: 1))
        }
        .buttonStyle(.plain)
    }
}
