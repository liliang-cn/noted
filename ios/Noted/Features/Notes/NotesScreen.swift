import SwiftUI

/// 笔记 (tab root): search, tag chips, and the notes grouped into 置顶 / 今天 / 过去 7 天 / 更早.
struct NotesScreen: View {
    @Environment(AppData.self) private var data
    @Environment(Session.self) private var session
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster

    @State private var query = ""
    @State private var scope = SearchScope.literal
    @State private var tag: String?
    @State private var deleting: Noted_V1_Note?
    @State private var semanticHits: [Noted_V1_Note]?

    enum SearchScope: Hashable { case literal, meaning }

    private var trimmed: String { query.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var searchingByMeaning: Bool { session.aiChat && scope == .meaning && !trimmed.isEmpty }

    var body: some View {
        ScrollViewReader { proxy in
            List {
                tagChips.id("top")
                if searchingByMeaning {
                    meaningResults
                } else {
                    groupedNotes
                }
            }
            .listStyle(.insetGrouped)
            .onChange(of: router.scrollToTopRequest) { _, _ in
                withAnimation { proxy.scrollTo("top", anchor: .top) }
            }
        }
        .navigationTitle("笔记")
        // .always would force an inline title; the drawer shows the field under the large title.
        .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .automatic), prompt: "搜索")
        .modifier(MeaningScope(enabled: session.aiChat, scope: $scope))
        .task(id: searchingByMeaning ? "\(trimmed)|\(session.mode.rawValue)" : "") { await searchByMeaning() }
        .refreshable { await data.refreshNotes() }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { SpaceFilterMenu() }
            ToolbarItem(placement: .topBarTrailing) {
                Button { router.push(.note("")) } label: { Image(systemName: "square.and.pencil") }
                    .accessibilityLabel("新建笔记")
                    .accessibilityIdentifier("notes-new")
            }
        }
        .confirmationDialog("删除笔记？", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { n in
            Button("删除", role: .destructive) { Task { await delete(n) } }
            Button("取消", role: .cancel) {}
        } message: { n in
            Text("“\(NoteText.title(n))”")
        }
        .onChange(of: availableTags) { _, tags in if let t = tag, !tags.contains(t) { tag = nil } }
    }

    // MARK: tags

    /// Tags on the notes in the current space, most used first.
    private var availableTags: [String] {
        var count: [String: Int] = [:]
        for n in data.visibleNotes { for t in n.tags { count[t, default: 0] += 1 } }
        return count.sorted { $0.value != $1.value ? $0.value > $1.value : $0.key < $1.key }.map(\.key)
    }

    @ViewBuilder private var tagChips: some View {
        let tags = availableTags
        if !tags.isEmpty {
            Section {
                ChipRow {
                    Chip(title: "全部", isOn: tag == nil) { tag = nil }
                        .accessibilityIdentifier("notes-tag-all")
                    ForEach(tags, id: \.self) { t in
                        Chip(title: "#\(t)", isOn: tag == t) { tag = tag == t ? nil : t }
                    }
                }
                .listRowInsets(EdgeInsets())
                .listRowBackground(Color.clear)
            }
            .listSectionSpacing(4)
        }
    }

    // MARK: grouped list

    private var filtered: [Noted_V1_Note] {
        var ns = data.visibleNotes
        if let tag { ns = ns.filter { $0.tags.contains(tag) } }
        let q = trimmed.lowercased()
        if !q.isEmpty {
            ns = ns.filter { n in
                n.title.lowercased().contains(q) || n.content.lowercased().contains(q)
                    || n.tags.contains { $0.lowercased().contains(q) }
            }
        }
        return ns.sorted { $0.updateTime.date > $1.updateTime.date }
    }

    @ViewBuilder private var groupedNotes: some View {
        let ns = filtered
        if ns.isEmpty {
            Section {
                if !data.isLoaded {
                    ProgressView().frame(maxWidth: .infinity).padding(.vertical, 22)
                } else {
                    EmptyState(text: trimmed.isEmpty ? (tag == nil ? "没有笔记" : "没有 #\(tag!) 的笔记") : "没有找到“\(trimmed)”")
                }
            }
        } else {
            let today = Date.now.startOfDay
            let weekAgo = today.adding(days: -6)
            let pinned = ns.filter(\.pinned)
            let rest = ns.filter { !$0.pinned }
            if !pinned.isEmpty {
                Section {
                    rows(pinned)
                } header: {
                    Label("置顶", systemImage: "pin").labelStyle(.titleAndIcon)
                }
            }
            let groups: [(String, [Noted_V1_Note])] = [
                ("今天", rest.filter { $0.updateTime.date >= today }),
                ("过去 7 天", rest.filter { $0.updateTime.date < today && $0.updateTime.date >= weekAgo }),
                ("更早", rest.filter { $0.updateTime.date < weekAgo }),
            ]
            ForEach(groups, id: \.0) { title, xs in
                if !xs.isEmpty {
                    Section(title) { rows(xs) }
                }
            }
        }
    }

    // MARK: by meaning

    @ViewBuilder private var meaningResults: some View {
        Section {
            if let hits = semanticHits {
                let shown = tag.map { t in hits.filter { $0.tags.contains(t) } } ?? hits
                if shown.isEmpty {
                    EmptyState(text: "没有找到“\(trimmed)”")
                } else {
                    rows(shown)
                }
            } else {
                ProgressView().frame(maxWidth: .infinity).padding(.vertical, 22)
            }
        } header: {
            Text("按含义")
        }
    }

    private func searchByMeaning() async {
        guard searchingByMeaning else { semanticHits = nil; return }
        semanticHits = nil
        try? await Task.sleep(for: .milliseconds(350))
        guard !Task.isCancelled else { return }
        let hits = await data.searchNotes(trimmed, semantic: true, space: session.mode.space)
        guard !Task.isCancelled else { return }
        // The cached copy is fresher than the hit when it was just edited here.
        semanticHits = (hits ?? []).map { data.note($0.note.id) ?? $0.note }.filter { data.inSpace($0.space) }
    }

    // MARK: rows

    private func rows(_ ns: [Noted_V1_Note]) -> some View {
        ForEach(ns, id: \.id) { n in
            Button { router.push(.note(n.id)) } label: {
                NoteListRow(note: n, projectTitle: data.projectTitle(n.projectID))
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("note-row")
            .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                Button { deleting = n } label: { Label("删除", systemImage: "trash") }
                    .tint(.danger)
            }
            .swipeActions(edge: .leading, allowsFullSwipe: true) {
                Button { Task { await togglePin(n) } } label: {
                    Label(n.pinned ? "取消置顶" : "置顶", systemImage: n.pinned ? "pin.slash" : "pin")
                }
                .tint(.warning)
            }
            .contextMenu {
                Button { Task { await togglePin(n) } } label: {
                    Label(n.pinned ? "取消置顶" : "置顶", systemImage: n.pinned ? "pin.slash" : "pin")
                }
                Button(role: .destructive) { deleting = n } label: { Label("删除", systemImage: "trash") }
            }
        }
    }

    private func togglePin(_ n: Noted_V1_Note) async {
        let on = !n.pinned
        if await data.setPinned(note: n.id, on) {
            toaster.show(on ? "已置顶" : "已取消置顶", systemImage: on ? "pin" : "pin.slash")
        }
    }

    private func delete(_ n: Noted_V1_Note) async {
        deleting = nil
        NoteDrafts.clear(n.id)
        if await data.deleteNote(n.id) {
            semanticHits?.removeAll { $0.id == n.id }
            toaster.show("已删除", systemImage: "trash")
        }
    }
}

/// 字面 / 含义 under the search field, only when the server can search by meaning.
private struct MeaningScope: ViewModifier {
    let enabled: Bool
    @Binding var scope: NotesScreen.SearchScope

    func body(content: Content) -> some View {
        if enabled {
            content.searchScopes($scope, activation: .onSearchPresentation) {
                Text("字面").tag(NotesScreen.SearchScope.literal)
                Text("含义").tag(NotesScreen.SearchScope.meaning)
            }
        } else {
            content
        }
    }
}

/// A note in the list: title, two lines of the body, then when · space · project · #tags.
struct NoteListRow: View {
    let note: Noted_V1_Note
    var projectTitle: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(NoteText.title(note))
                .font(.headline)
                .foregroundStyle(note.title.isEmpty ? AnyShapeStyle(.secondaryText) : AnyShapeStyle(.primary))
                .lineLimit(1)
            let preview = NoteText.preview(note.content)
            Text(preview.isEmpty ? "无内容" : preview)
                .font(.subheadline)
                .foregroundStyle(.secondaryText)
                .lineLimit(2)
            HStack(spacing: 6) {
                Text(Fmt.stamp(note.updateTime.date)).monospacedDigit().foregroundStyle(.secondaryText)
                SpaceDot(space: note.space == .work ? .work : .life, size: 6)
                if let projectTitle {
                    HStack(spacing: 3) {
                        Image(systemName: "folder").font(.system(size: 11))
                        Text(projectTitle).lineLimit(1)
                    }
                    .foregroundStyle(.secondaryText)
                }
                ForEach(note.tags, id: \.self) { t in
                    Text("#\(t)").foregroundStyle(.tint).lineLimit(1)
                }
            }
            .font(.caption)
            .padding(.top, 2)
        }
        .padding(.vertical, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}
