import SwiftUI

struct NotesView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @State private var notes: [Noted_V1_Note] = []
    @State private var query = ""
    @State private var semantic = false
    @State private var matchedBy = ""
    @State private var tag: String?
    @State private var error: String?
    @State private var pendingDelete: PendingDelete?
    @Environment(Router.self) private var router

    private var tags: [String] {
        var seen: [String: Int] = [:]
        for n in notes { for t in n.tags { seen[t, default: 0] += 1 } }
        return seen.sorted { $0.value == $1.value ? $0.key < $1.key : $0.value > $1.value }.map(\.key)
    }
    private var shown: [Noted_V1_Note] { tag.map { t in notes.filter { $0.tags.contains(t) } } ?? notes }

    var body: some View {
        TabScreen(title: "笔记") {
            header
        } content: {
            HStack(spacing: 8) {
                HStack(spacing: 10) {
                    Image(systemName: "magnifyingglass").font(.system(size: 16, weight: .medium)).foregroundStyle(p.ink2)
                    TextField("搜索", text: $query).font(.ui(15)).foregroundStyle(p.ink)
                        .textInputAutocapitalization(.never).submitLabel(.search)
                }
                .padding(.horizontal, 12).frame(height: 42)
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radiusSm))
                .overlay(RoundedRectangle(cornerRadius: p.radiusSm).strokeBorder(p.line))
                HStack(spacing: 0) {
                    ForEach([false, true], id: \.self) { sem in
                        Button { semantic = sem } label: {
                            Text(sem ? "含义" : "字面").font(.ui(14, .semibold)).frame(maxWidth: .infinity).frame(height: 34)
                                .foregroundStyle(semantic == sem ? p.bg : p.ink2)
                                .background(semantic == sem ? p.ink : .clear, in: RoundedRectangle(cornerRadius: max(p.radiusSm - 2, 2)))
                                .contentShape(Rectangle())
                        }
                    }
                }
                .padding(2).frame(width: 124, height: 42)
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radiusSm))
                .overlay(RoundedRectangle(cornerRadius: p.radiusSm).strokeBorder(p.line))
            }
            if !tags.isEmpty { tagTabs }
            ErrorLine(text: error)
            let searching = !query.trimmingCharacters(in: .whitespaces).isEmpty && !matchedBy.isEmpty
            if shown.isEmpty {
                Text(query.isEmpty ? "还没有笔记" : "没有匹配的笔记").font(.ui(14)).foregroundStyle(p.ink3)
                    .frame(maxWidth: .infinity, minHeight: 80).card()
            } else if searching {
                EyebrowSection(title: matchedBy == "semantic" ? "按含义匹配 · 相关度从高到低" : (semantic ? "没有向量索引,已按字面匹配" : "按字面匹配")) {
                    list(best: matchedBy == "semantic")
                }
            } else {
                list(best: false)
            }
        }
        .task(id: "\(session.mode.rawValue)|\(query)|\(semantic)") {
            if !query.isEmpty { try? await Task.sleep(for: .milliseconds(250)) }
            await load()
        }
        .refreshable { await load() }
        .reloadOnReturn { await load() }
        .confirmDelete($pendingDelete) { err in error = err; await load() }
    }

    /// The notes in one card. With `best`, the first one is the closest match and says so.
    private func list(best: Bool) -> some View {
        VStack(spacing: 0) {
            ForEach(Array(shown.enumerated()), id: \.element.id) { i, n in
                if i > 0 { Rectangle().fill(p.line).frame(height: 1) }
                Button { router.push(.note(n)) } label: {
                    row(n, best: best && i == 0)
                }
                .buttonStyle(.plain)
                .deleteMenu { pendingDelete = .note(n) }
            }
        }
        .background(p.surface)
        .clipShape(RoundedRectangle(cornerRadius: p.radius))
        .overlay(RoundedRectangle(cornerRadius: p.radius).strokeBorder(p.line))
    }

    private func row(_ n: Noted_V1_Note, best: Bool) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                if n.pinned { Image(systemName: "pin.fill").font(.system(size: 11)).foregroundStyle(p.ink3) }
                Text(n.title.isEmpty ? "无标题" : n.title).font(.ui(15, .bold)).foregroundStyle(p.ink).lineLimit(1)
                Spacer(minLength: 8)
                Text(Fmt.relative(n.updateTime.date)).font(.mono(13)).foregroundStyle(p.ink2)
            }
            if !n.content.isEmpty {
                Text(n.content).font(.ui(13)).foregroundStyle(p.ink2).lineLimit(2).multilineTextAlignment(.leading)
            }
            HStack(spacing: 6) {
                Circle().fill(n.space.color(p)).frame(width: 8, height: 8)
                Text(n.tags.isEmpty ? n.space.label : n.tags.prefix(3).joined(separator: " · "))
                    .font(.mono(13)).foregroundStyle(p.ink2).lineLimit(1)
                if best {
                    Text("最相关").font(.ui(11, .semibold)).foregroundStyle(p.ink2)
                        .padding(.horizontal, 8).frame(height: 22)
                        .background(p.surface2, in: RoundedRectangle(cornerRadius: 6))
                        .padding(.leading, 6)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 14).padding(.vertical, 12)
        .background(best ? p.surface2 : p.surface)
        .contentShape(Rectangle())
    }

    private var header: some View {
        HStack(spacing: 8) {
            ModeSwitch(mini: true)
            Button {
                var n = Noted_V1_Note()
                n.space = session.mode == .work ? .work : .life
                router.push(.note(n))
            } label: {
                Image(systemName: "plus").font(.system(size: 16, weight: .semibold)).foregroundStyle(p.bg)
                    .frame(width: 34, height: 34).background(p.ink, in: RoundedRectangle(cornerRadius: p.radiusSm))
            }.accessibilityLabel("新笔记")
        }
    }

    private var tagTabs: some View {
        let labels = ["全部 \(notes.count)"] + tags
        let sel = tag.flatMap { tags.firstIndex(of: $0) }.map { $0 + 1 } ?? 0
        return UnderlineTabs(items: labels, selected: sel, gap: 18) { tag = $0 == 0 ? nil : tags[$0 - 1] }
    }

    private func load() async {
        guard let api = session.api, !Task.isCancelled else { return }
        do {
            if query.trimmingCharacters(in: .whitespaces).isEmpty {
                var r = Noted_V1_ListNotesRequest(); r.space = session.mode.space; r.pageSize = 100
                notes = try await api.notes.listNotes(r).notes
                matchedBy = ""
            } else {
                var r = Noted_V1_SearchNotesRequest(); r.query = query; r.space = session.mode.space; r.semantic = semantic
                let res = try await api.notes.searchNotes(r)
                notes = res.hits.map(\.note)
                matchedBy = res.mode
            }
            if let t = tag, !notes.contains(where: { $0.tags.contains(t) }) { tag = nil }
            error = nil
            #if DEBUG
            if ProcessInfo.processInfo.environment["NOTED_SHEET"] == "extract", router.path.isEmpty,
               let n = notes.first(where: { $0.title == "旅行清单" }) {
                router.push(.note(n)); router.push(.extract(n.id))
            }
            #endif
        } catch { if !(error is CancellationError) { self.error = describe(error) } }
    }
}

extension Noted_V1_Note: Identifiable {}

struct ShareImage: Identifiable {
    let id = UUID()
    let image: UIImage
}

/// The system share sheet for a picture.
struct ActivitySheet: UIViewControllerRepresentable {
    let items: [Any]
    func makeUIViewController(context: Context) -> UIActivityViewController { UIActivityViewController(activityItems: items, applicationActivities: nil) }
    func updateUIViewController(_ vc: UIActivityViewController, context: Context) {}
}
