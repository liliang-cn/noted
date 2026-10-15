import SwiftUI

/// The "@" panel above the formatting bar: projects, each followed by its to-dos, events, notes
/// and goals, filtered by what was typed after the "@". Picking a row inserts a reference.
struct NoteMentionPanel: View {
    @Environment(AppData.self) private var data
    let query: String
    /// The note being edited, left out of the list.
    let selfID: String
    var onPick: (Item) -> Void

    struct Item: Identifiable {
        var kind: String
        var id: String
        var title: String
        var symbol: String
        var detail: String
        var tint: Color?
        var child = false
        var key: String { "\(kind)/\(id)" }
    }

    var body: some View {
        let items = self.items
        VStack(alignment: .leading, spacing: 0) {
            Text(query.isEmpty ? "引用" : "引用 “\(query)”")
                .font(.footnote).foregroundStyle(.secondaryText)
                .padding(.horizontal, 16).padding(.top, 10).padding(.bottom, 4)
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(items, id: \.key) { it in row(it) }
                    if items.isEmpty {
                        Text("没有匹配").font(.subheadline).foregroundStyle(.secondaryText)
                            .frame(maxWidth: .infinity, minHeight: 44)
                    }
                }
            }
            .scrollIndicators(.hidden)
            .frame(maxHeight: 220)
            .fixedSize(horizontal: false, vertical: items.count < 5)
        }
        .background {
            Rectangle().fill(.white)
                .overlay(alignment: .top) { Rectangle().fill(.hairline).frame(height: 0.5) }
        }
        .accessibilityIdentifier("mention-panel")
    }

    private func row(_ it: Item) -> some View {
        Button { onPick(it) } label: {
            HStack(spacing: 12) {
                Image(systemName: it.symbol)
                    .font(.system(size: it.child ? 15 : 17))
                    .foregroundStyle(it.tint ?? .secondaryText)
                    .frame(width: 24)
                VStack(alignment: .leading, spacing: 1) {
                    Text(it.title.isEmpty ? "无标题" : it.title).font(.body).lineLimit(1)
                    Text(it.detail).font(.caption).foregroundStyle(.secondaryText).lineLimit(1)
                }
                Spacer(minLength: 0)
            }
            .padding(.leading, it.child ? 44 : 16).padding(.trailing, 16)
            .frame(minHeight: 46)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("引用 \(it.detail) \(it.title)")
        .accessibilityIdentifier("mention-\(it.kind)")
    }

    private var items: [Item] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        let match: (String) -> Bool = { q.isEmpty || $0.lowercased().contains(q) }
        var out: [Item] = []
        let events = projectEvents
        for p in data.projects where !p.archived {
            let kids = children(of: p, events: events[p.id] ?? [])
            let projectMatches = match(p.title)
            let shown = projectMatches ? kids : kids.filter { match($0.title) }
            guard projectMatches || !shown.isEmpty else { continue }
            out.append(Item(kind: "project", id: p.id, title: p.title, symbol: "folder",
                            detail: "项目 · \(kids.count) 项", tint: p.space == .work ? Color.work : Color.personal))
            out += shown.prefix(8)
        }
        // Things outside any project only once something is typed, so the panel stays short.
        if !q.isEmpty {
            var loose: [Item] = []
            loose += data.tasks.filter { !$0.completed && $0.projectID.isEmpty && match($0.title) }
                .map { Item(kind: "task", id: $0.id, title: $0.title, symbol: "checklist", detail: "待办") }
            loose += data.notes.filter { $0.projectID.isEmpty && $0.id != selfID && match($0.title) }
                .map { Item(kind: "note", id: $0.id, title: $0.title, symbol: "note.text", detail: "笔记") }
            loose += data.goals.filter { $0.projectID.isEmpty && $0.objectiveID.isEmpty && match($0.title) }
                .map { Item(kind: "goal", id: $0.id, title: $0.title, symbol: "target", detail: "目标") }
            loose += data.objectives.filter { match($0.title) }
                .map { Item(kind: "objective", id: $0.id, title: $0.title, symbol: "target", detail: "目标") }
            out += loose.prefix(10)
        }
        return out
    }

    /// Events of each project: from the loaded project pages, else the loaded months.
    private var projectEvents: [String: [Noted_V1_Event]] {
        var out: [String: [Noted_V1_Event]] = [:]
        var seen: Set<String> = []
        for o in data.occurrences where !o.event.projectID.isEmpty && seen.insert(o.event.id).inserted {
            out[o.event.projectID, default: []].append(o.event)
        }
        for (pid, d) in data.projectDetails { out[pid] = d.events }
        return out
    }

    private func children(of p: Noted_V1_Project, events: [Noted_V1_Event]) -> [Item] {
        var kids: [Item] = []
        kids += data.tasks.filter { $0.projectID == p.id && !$0.completed }
            .map { Item(kind: "task", id: $0.id, title: $0.title, symbol: "checklist", detail: "待办", child: true) }
        kids += events.map { Item(kind: "event", id: $0.id, title: $0.title, symbol: "calendar", detail: "日程", child: true) }
        kids += data.notes.filter { $0.projectID == p.id && $0.id != selfID }
            .map { Item(kind: "note", id: $0.id, title: $0.title, symbol: "note.text", detail: "笔记", child: true) }
        kids += data.goals.filter { $0.projectID == p.id }
            .map { Item(kind: "goal", id: $0.id, title: $0.title, symbol: "target", detail: "目标", child: true) }
        return kids
    }
}
