import SwiftUI

/// 关联已有: pick to-dos, events, notes or goals that are not in this project and put them in it.
struct LinkExistingView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let projectID: String

    enum Kind: String, CaseIterable, Identifiable {
        case todo, event, note, goal
        var id: String { rawValue }
        var label: String {
            switch self {
            case .todo: "待办"
            case .event: "日程"
            case .note: "笔记"
            case .goal: "目标"
            }
        }
    }

    /// One candidate row.
    struct Candidate: Identifiable {
        let ref: ItemRef
        let title: String
        let subtitle: String
        let space: Noted_V1_Space
        var id: ItemRef { ref }
    }

    @State private var kind: Kind = .todo
    @State private var query = ""
    @State private var selected: Set<ItemRef> = []
    @State private var saving = false

    var body: some View {
        let items = candidates
        List {
            Section {
                Picker("类型", selection: $kind) {
                    ForEach(Kind.allCases) { Text($0.label).tag($0) }
                }
                .pickerStyle(.segmented)
                .planCardRow(bottom: 0)
                .accessibilityIdentifier("link-kind")
            }
            Section {
                if items.isEmpty { EmptyState(text: query.isEmpty ? "没有可关联的内容" : "没有找到") }
                ForEach(items) { c in
                    let on = selected.contains(c.ref)
                    Button {
                        if on { selected.remove(c.ref) } else { selected.insert(c.ref) }
                    } label: {
                        HStack(spacing: 12) {
                            CheckMark(isOn: on)
                            VStack(alignment: .leading, spacing: 1) {
                                Text(c.title).lineLimit(1)
                                if !c.subtitle.isEmpty {
                                    Text(c.subtitle).font(.footnote).foregroundStyle(.secondaryText).lineLimit(1)
                                }
                            }
                            .frame(maxWidth: .infinity, minHeight: 40, alignment: .leading)
                            SpaceDot(space: c.space)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
            }
        }
        .listStyle(.insetGrouped)
        .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "搜索")
        .navigationTitle("关联已有")
        .navigationBarTitleDisplayMode(.inline)
        .safeAreaInset(edge: .bottom, spacing: 0) {
            BottomBar {
                Button(selected.isEmpty ? "关联" : "关联 \(selected.count) 项") { link() }
                    .buttonStyle(.wide)
                    .disabled(selected.isEmpty || saving)
                    .accessibilityIdentifier("link-confirm")
            }
        }
        .task {
            await data.loadMonth(.now)
            await data.loadMonth(Fmt.calendar.date(byAdding: .month, value: 1, to: .now) ?? .now)
        }
    }

    private func link() {
        let refs = Array(selected)
        saving = true
        Task {
            let n = await data.setProject(refs, projectID: projectID)
            saving = false
            if n > 0 {
                router.pop()
                toaster.show("已关联 \(n) 项", systemImage: "link")
            }
        }
    }

    // MARK: candidates

    private var candidates: [Candidate] {
        let all: [Candidate]
        switch kind {
        case .todo:
            all = data.tasks
                .filter { !$0.completed && $0.projectID != projectID && data.inSpace($0.space) }
                .sorted { a, b in
                    switch (a.due, b.due) {
                    case let (x?, y?): return x < y
                    case (_?, nil): return true
                    case (nil, _?): return false
                    default: return a.createTime.date > b.createTime.date
                    }
                }
                .map { t in
                    Candidate(ref: .task(t.id), title: t.title,
                              subtitle: line(t.due.map { Fmt.day($0) }, t.projectID), space: t.space)
                }
        case .event:
            var seen: Set<String> = []
            let today = Date.now.startOfDay
            all = data.occurrences
                .filter { $0.end >= today || $0.start >= today }
                .compactMap { o -> Candidate? in
                    let e = o.event
                    guard e.projectID != projectID, data.inSpace(e.space), seen.insert(e.id).inserted else { return nil }
                    let when = Fmt.mdw(o.start) + (e.allDay ? "" : " \(Fmt.hm(o.start))")
                    return Candidate(ref: .event(e.id), title: e.title, subtitle: line(when, e.projectID), space: e.space)
                }
        case .note:
            all = data.notes
                .filter { $0.projectID != projectID && data.inSpace($0.space) }
                .map { n in
                    Candidate(ref: .note(n.id), title: n.title.isEmpty ? "无标题" : n.title,
                              subtitle: line(Fmt.stamp(n.updateTime.date), n.projectID), space: n.space)
                }
        case .goal:
            let goals = data.standaloneGoals.filter { $0.projectID != projectID }.map { g in
                Candidate(ref: .goal(g.id), title: g.title, subtitle: line(g.planRule, g.projectID), space: g.space)
            }
            let objectives = data.visibleObjectives.filter { $0.projectID != projectID }.map { o in
                Candidate(ref: .objective(o.id), title: o.title, subtitle: line("\(o.goals.count) 个维度", o.projectID), space: o.space)
            }
            all = goals + objectives
        }
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return q.isEmpty ? all : all.filter { $0.title.lowercased().contains(q) }
    }

    /// "10月20日 · 在「Q4 产品发布」"
    private func line(_ first: String?, _ otherProject: String) -> String {
        var parts: [String] = []
        if let first, !first.isEmpty { parts.append(first) }
        if let p = data.projectTitle(otherProject) { parts.append("在「\(p)」") }
        return parts.joined(separator: " · ")
    }
}
