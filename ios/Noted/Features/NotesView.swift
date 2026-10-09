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
    @State private var editing: Noted_V1_Note?

    private var tags: [String] {
        var seen: [String: Int] = [:]
        for n in notes { for t in n.tags { seen[t, default: 0] += 1 } }
        return seen.sorted { $0.value == $1.value ? $0.key < $1.key : $0.value > $1.value }.map(\.key)
    }
    private var shown: [Noted_V1_Note] { tag.map { t in notes.filter { $0.tags.contains(t) } } ?? notes }

    var body: some View {
        Screen(eyebrow: "\(shown.count) 条", title: "笔记", trailing: AnyView(header)) {
            HStack(spacing: 8) {
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").foregroundStyle(p.ink3)
                    TextField("搜索", text: $query).textInputAutocapitalization(.never).submitLabel(.search)
                }
                .padding(.horizontal, 12).frame(height: 40)
                .background(p.surface, in: RoundedRectangle(cornerRadius: 10)).overlay(RoundedRectangle(cornerRadius: 10).stroke(p.line))
                HStack(spacing: 2) {
                    ForEach([false, true], id: \.self) { sem in
                        Button { semantic = sem } label: {
                            Text(sem ? "含义" : "字面").font(.system(size: 13, weight: .semibold)).padding(.horizontal, 10).frame(height: 32)
                                .foregroundStyle(semantic == sem ? p.bg : p.ink2)
                                .background(semantic == sem ? p.ink : .clear, in: RoundedRectangle(cornerRadius: 8))
                        }
                    }
                }
                .padding(3).background(p.surface, in: RoundedRectangle(cornerRadius: 10)).overlay(RoundedRectangle(cornerRadius: 10).stroke(p.line))
            }
            if !tags.isEmpty { tagTabs }
            if !query.trimmingCharacters(in: .whitespaces).isEmpty, !matchedBy.isEmpty {
                Text(matchedBy == "semantic" ? "按含义匹配 · 相关度从高到低" : (semantic ? "没有向量索引,已按字面匹配" : "按字面匹配"))
                    .font(.system(size: 12)).foregroundStyle(p.ink2)
            }
            ErrorLine(text: error)
            if shown.isEmpty {
                Text(query.isEmpty ? "还没有笔记" : "没有匹配的笔记").font(.system(size: 14)).foregroundStyle(p.ink3)
                    .frame(maxWidth: .infinity, minHeight: 80).card()
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(shown.enumerated()), id: \.element.id) { i, n in
                        if i > 0 { Divider().overlay(p.line) }
                        Button { editing = n } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                HStack(spacing: 6) {
                                    if n.pinned { Image(systemName: "pin.fill").font(.system(size: 11)).foregroundStyle(p.ink3) }
                                    Text(n.title.isEmpty ? "无标题" : n.title).font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink).lineLimit(1)
                                    Spacer()
                                    Text(Fmt.relative(n.updateTime.date)).font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2)
                                }
                                if !n.content.isEmpty {
                                    Text(n.content).font(.system(size: 13)).foregroundStyle(p.ink2).lineLimit(2).multilineTextAlignment(.leading)
                                }
                                HStack(spacing: 8) {
                                    SpaceTag(space: n.space)
                                    ForEach(n.tags.prefix(3), id: \.self) { Text($0).font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2) }
                                }
                            }
                            .frame(maxWidth: .infinity, alignment: .leading).padding(14)
                            .contentShape(Rectangle())
                        }.buttonStyle(.plain)
                    }
                }
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
            }
        }
        .task(id: "\(session.mode.rawValue)|\(query)|\(semantic)") {
            if !query.isEmpty { try? await Task.sleep(for: .milliseconds(250)) }
            await load()
        }
        .refreshable { await load() }
        .sheet(item: $editing, onDismiss: { Task { await load() } }) { NoteEditor(note: $0) }
    }

    private var header: some View {
        HStack(spacing: 8) {
            ModeSwitch(mini: true)
            Button {
                var n = Noted_V1_Note()
                n.space = session.mode == .work ? .work : .life
                editing = n
            } label: {
                Image(systemName: "plus").font(.system(size: 15, weight: .bold)).foregroundStyle(p.bg)
                    .frame(width: 34, height: 34).background(p.ink, in: RoundedRectangle(cornerRadius: 10))
            }.accessibilityLabel("新笔记")
        }
    }

    private var tagTabs: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 14) {
                tab("全部 \(notes.count)", on: tag == nil) { tag = nil }
                ForEach(tags, id: \.self) { t in tab(t, on: tag == t) { tag = t } }
            }
        }
        .scrollIndicators(.hidden)
        .overlay(alignment: .bottom) { Rectangle().fill(p.line).frame(height: 1) }
    }

    private func tab(_ label: String, on: Bool, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label).font(.system(size: 14, weight: on ? .semibold : .medium)).foregroundStyle(on ? p.ink : p.ink3)
                .padding(.vertical, 6)
                .overlay(alignment: .bottom) { if on { Rectangle().fill(session.mode.accent(p)).frame(height: 2) } }
        }
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
            if ProcessInfo.processInfo.environment["NOTED_SHEET"] == "extract", editing == nil {
                editing = notes.first { $0.title == "旅行清单" }
            }
            #endif
        } catch { if !(error is CancellationError) { self.error = describe(error) } }
    }
}

extension Noted_V1_Note: Identifiable {}

struct NoteEditor: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State var note: Noted_V1_Note
    @State private var error: String?
    @State private var shareImage: ShareImage?
    @State private var saved: String?
    private let photos = PhotoSaver()
    @State private var extracting = ProcessInfo.processInfo.environment["NOTED_SHEET"] == "extract"

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Button("关闭") { dismiss() }
                Spacer()
                Button { note.pinned.toggle() } label: { Image(systemName: note.pinned ? "pin.fill" : "pin") }
                Menu {
                    Button { copyText() } label: { Label("复制文字", systemImage: "doc.on.doc") }
                    ShareLink(item: NoteImage.text(note), subject: Text(note.title)) { Label("分享文字", systemImage: "text.alignleft") }
                    Button { shareAsImage() } label: { Label("分享为图片", systemImage: "photo") }
                    Button { saveToPhotos() } label: { Label("保存到相册", systemImage: "square.and.arrow.down") }
                } label: {
                    Image(systemName: "square.and.arrow.up")
                }
                .accessibilityLabel("分享")
                if session.aiNoteTools && !note.content.isEmpty && !note.id.isEmpty {
                    Button("提取待办") { extracting = true }
                }
                if !note.id.isEmpty { Button("删除", role: .destructive) { Task { await remove() } } }
                Button("保存") { Task { await save() } }.fontWeight(.semibold)
            }
            TextField("标题", text: $note.title).font(.system(size: 24, weight: .bold))
            Divider().overlay(p.line)
            TextEditor(text: $note.content).scrollContentBackground(.hidden).font(.system(size: 16))
            ErrorLine(text: error)
            if let saved { Text(saved).font(.system(size: 13)).foregroundStyle(p.ok) }
        }
        .padding(16)
        .background(p.bg.ignoresSafeArea())
        .sheet(item: $shareImage) { ActivitySheet(items: [$0.image]) }
        .sheet(isPresented: $extracting) { ExtractView(noteID: note.id) }
    }

    private func copyText() {
        UIPasteboard.general.string = NoteImage.text(note)
        saved = "已复制"
    }

    private func rendered() -> UIImage? {
        guard let img = NoteImage.render(note, palette: session.palette) else { error = "生成图片失败"; return nil }
        return img
    }

    private func shareAsImage() {
        if let img = rendered() { shareImage = ShareImage(image: img) }
    }

    private func saveToPhotos() {
        guard let img = rendered() else { return }
        photos.save(img) { err in
            Task { @MainActor in
                if let err { self.error = "没有保存:\(err.localizedDescription)" } else { self.saved = "已保存到相册"; self.error = nil }
            }
        }
    }

    private func save() async {
        guard let api = session.api else { return }
        do {
            if note.id.isEmpty {
                var c = Noted_V1_CreateNoteRequest()
                c.note = note
                _ = try await api.notes.createNote(c)
            } else {
                var r = Noted_V1_UpdateNoteRequest()
                r.note = note
                r.updateMask = fieldMask("title", "content", "pinned")
                _ = try await api.notes.updateNote(r)
            }
            dismiss()
        } catch { self.error = describe(error) }
    }

    private func remove() async {
        guard let api = session.api else { return }
        do {
            var r = Noted_V1_DeleteNoteRequest(); r.id = note.id
            _ = try await api.notes.deleteNote(r)
            dismiss()
        } catch { self.error = describe(error) }
    }
}

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
