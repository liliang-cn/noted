import SwiftUI
import UIKit

/// A note on its own page (`.note(id)`; an empty id is a new note, created on the first save).
/// It opens as a rendered preview; a tap switches to the Markdown text with a formatting bar
/// above the keyboard. It saves itself while you type and keeps unsaved text on the device.
struct NoteScreen: View {
    let id: String

    @Environment(AppData.self) private var data
    @Environment(Session.self) private var session
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    @Environment(\.scenePhase) private var phase

    @State private var note = Noted_V1_Note()
    @State private var saved: Noted_V1_Note?
    @State private var loaded = false
    @State private var missing = false
    @State private var deleted = false
    @State private var editing = false
    @State private var selection: TextSelection?
    @FocusState private var focus: Field?
    @State private var status = SaveStatus.idle
    @State private var pending: Task<Void, Never>?
    @State private var inFlight = false
    @State private var again = false
    @State private var mention: MarkdownEdit.Mention?
    @State private var linkTime = Date.distantPast
    @State private var naming = false
    @State private var newTag = ""
    @State private var confirmDelete = false
    @State private var extract = ExtractState.idle

    enum Field: Hashable { case title, body }
    enum SaveStatus: Equatable { case idle, saving, saved(Date), local }
    enum ExtractState { case idle, loading, empty, ready(Noted_V1_Proposal) }

    var body: some View {
        Group {
            if missing {
                List { Section { EmptyState(text: "笔记已删除", systemImage: "note.text") } }
            } else if !loaded {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                page
            }
        }
        .background(Color.white)
        .navigationTitle(NoteText.title(note))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                Text(statusText).font(.footnote).foregroundStyle(status == .local ? AnyShapeStyle(.warning) : AnyShapeStyle(.secondaryText))
                    .monospacedDigit()
                    .accessibilityIdentifier("note-status")
            }
            if loaded && !missing {
                ToolbarItem(placement: .topBarTrailing) {
                    if editing {
                        Button("完成") { finishEditing() }
                            .fontWeight(.semibold)
                            .accessibilityIdentifier("note-done")
                    } else {
                        moreMenu
                    }
                }
            }
        }
        .task { await load() }
        .onDisappear { flush() }
        .onChange(of: phase) { _, p in if p != .active { flush() } }
    }

    // MARK: page

    private var page: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                metaRow
                extractCard
                TextField("标题", text: $note.title, axis: .vertical)
                    .font(.system(size: 28, weight: .bold))
                    .focused($focus, equals: .title)
                    .submitLabel(.next)
                    .onSubmit { startEditing(cursorAtEnd: false) }
                    .accessibilityIdentifier("note-title")
                if editing { editor } else { preview }
            }
            .padding(.horizontal, 16)
            .padding(.top, 4)
            .padding(.bottom, 40)
        }
        .scrollDismissesKeyboard(.interactively)
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if editing {
                VStack(spacing: 0) {
                    if let m = mention, focus == .body {
                        NoteMentionPanel(query: m.query, selfID: note.id) { pick($0) }
                    }
                    keyboardBar
                }
            }
        }
        .onChange(of: note) { _, n in noteChanged(n) }
        .onChange(of: note.title) { _, t in
            // The title is one line: return moves on to the body.
            if t.contains("\n") {
                note.title = t.replacingOccurrences(of: "\n", with: "")
                startEditing(cursorAtEnd: false)
            }
        }
        .onChange(of: note.content) { old, new in bodyChanged(old, new) }
        .onChange(of: focus) { _, f in
            if f != nil, !editing { editing = true }
            if f != .body { mention = nil }
        }
        .alert("新标签", isPresented: $naming) {
            TextField("标签名", text: $newTag).textInputAutocapitalization(.never)
            Button("取消", role: .cancel) {}
            Button("添加") { addTag(newTag) }
        }
        .confirmationDialog("删除笔记？", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("删除", role: .destructive) { Task { await remove() } }
            Button("取消", role: .cancel) {}
        } message: {
            Text("“\(NoteText.title(note))”")
        }
    }

    private var statusText: String {
        switch status {
        case .saving: return "正在保存…"
        case .saved(let d): return "已保存 \(Fmt.hm(d))"
        case .local: return "未同步 · 已存在本机"
        case .idle:
            guard !note.id.isEmpty, note.hasUpdateTime else { return "" }
            return "已保存 \(Fmt.stamp(note.updateTime.date))"
        }
    }

    // MARK: meta: space, project, tags

    private var metaRow: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) {
                spaceMenu
                projectMenu
                ForEach(note.tags, id: \.self) { t in
                    Menu {
                        Button(role: .destructive) { note.tags.removeAll { $0 == t } } label: {
                            Label("移除 #\(t)", systemImage: "xmark")
                        }
                    } label: {
                        Text("#\(t)").notePill(accent: true)
                    }
                    .accessibilityLabel("标签 \(t)")
                }
                tagMenu
            }
            .padding(.vertical, 2)
        }
        .scrollIndicators(.hidden)
        .scrollBounceBehavior(.basedOnSize)
    }

    private var noteSpace: Noted_V1_Space { note.space == .work ? .work : .life }

    private var spaceMenu: some View {
        Menu {
            Picker("空间", selection: Binding(get: { noteSpace }, set: { setSpace($0) })) {
                Label { Text("个人") } icon: { Image(uiImage: .dot(Noted_V1_Space.life.tint)) }.tag(Noted_V1_Space.life)
                Label { Text("工作") } icon: { Image(uiImage: .dot(Noted_V1_Space.work.tint)) }.tag(Noted_V1_Space.work)
            }
            .pickerStyle(.inline)
        } label: {
            HStack(spacing: 6) {
                SpaceDot(space: noteSpace)
                Text(noteSpace.label)
            }
            .notePill()
        }
        .accessibilityLabel("空间 \(noteSpace.label)")
        .accessibilityIdentifier("note-space")
    }

    private var projectMenu: some View {
        let current = data.project(note.projectID)
        let open = data.projects.filter { !$0.archived || $0.id == note.projectID }
        return Menu {
            Button { note.projectID = "" } label: {
                if note.projectID.isEmpty { Label("无项目", systemImage: "checkmark") } else { Text("无项目") }
            }
            ForEach([Noted_V1_Space.life, .work], id: \.self) { sp in
                let ps = open.filter { ($0.space == .work ? Noted_V1_Space.work : .life) == sp }
                if !ps.isEmpty {
                    Section(sp.label) {
                        ForEach(ps, id: \.id) { p in
                            Button { setProject(p) } label: {
                                if p.id == note.projectID { Label(p.title, systemImage: "checkmark") } else { Text(p.title) }
                            }
                        }
                    }
                }
            }
        } label: {
            HStack(spacing: 5) {
                Image(systemName: "folder").font(.system(size: 13))
                Text(current?.title ?? "无项目")
            }
            .notePill()
        }
        .accessibilityLabel("项目 \(current?.title ?? "无")")
        .accessibilityIdentifier("note-project")
    }

    private var tagMenu: some View {
        let all = Array(Set(data.noteTags + note.tags)).sorted()
        return Menu {
            ForEach(all, id: \.self) { t in
                Button {
                    if note.tags.contains(t) { note.tags.removeAll { $0 == t } } else { note.tags.append(t) }
                } label: {
                    if note.tags.contains(t) { Label("#\(t)", systemImage: "checkmark") } else { Text("#\(t)") }
                }
            }
            if !all.isEmpty { Divider() }
            Button { newTag = ""; naming = true } label: { Label("新标签…", systemImage: "plus") }
        } label: {
            HStack(spacing: 5) {
                Image(systemName: "tag").font(.system(size: 13))
                if note.tags.isEmpty { Text("标签") }
            }
            .notePill()
        }
        .accessibilityLabel("标签")
        .accessibilityIdentifier("note-tags")
    }

    private func setSpace(_ s: Noted_V1_Space) {
        note.space = s
        if let p = data.project(note.projectID), (p.space == .work ? Noted_V1_Space.work : .life) != s { note.projectID = "" }
    }

    private func setProject(_ p: Noted_V1_Project) {
        note.projectID = p.id
        note.space = p.space == .work ? .work : .life
    }

    private func addTag(_ raw: String) {
        let t = raw.trimmingCharacters(in: .whitespacesAndNewlines).trimmingCharacters(in: CharacterSet(charactersIn: "#＃"))
        guard !t.isEmpty, !note.tags.contains(t) else { return }
        note.tags.append(t)
    }

    // MARK: ··· menu

    private var moreMenu: some View {
        Menu {
            Button { togglePin() } label: {
                Label(note.pinned ? "取消置顶" : "置顶", systemImage: note.pinned ? "pin.slash" : "pin")
            }
            Button { startEditing(cursorAtEnd: true) } label: { Label("编辑", systemImage: "pencil") }
            Button { copyText() } label: { Label("复制文字", systemImage: "doc.on.doc") }
            ShareLink(item: NoteImage.text(note), subject: Text(NoteText.title(note))) {
                Label("分享", systemImage: "square.and.arrow.up")
            }
            if session.aiNoteTools && !note.content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                Button { Task { await runExtract() } } label: { Label("提取待办", systemImage: "checklist") }
            }
            Divider()
            Button(role: .destructive) { confirmDelete = true } label: { Label("删除笔记", systemImage: "trash") }
        } label: {
            Image(systemName: "ellipsis.circle")
        }
        .accessibilityLabel("更多")
        .accessibilityIdentifier("note-more")
    }

    private func togglePin() {
        note.pinned.toggle()
        toaster.show(note.pinned ? "已置顶" : "已取消置顶", systemImage: note.pinned ? "pin" : "pin.slash")
        flush()
    }

    private func copyText() {
        UIPasteboard.general.string = NoteImage.text(note)
        toaster.show("已复制", systemImage: "doc.on.doc")
    }

    private func remove() async {
        pending?.cancel()
        deleted = true
        NoteDrafts.clear(note.id)
        if note.id.isEmpty { router.pop(); return }
        if await data.deleteNote(note.id) {
            router.pop()
            toaster.show("已删除", systemImage: "trash")
        } else {
            deleted = false
        }
    }

    // MARK: body: editor and preview

    private var editor: some View {
        TextEditor(text: $note.content, selection: $selection)
            .font(.body)
            .lineSpacing(4)
            .scrollContentBackground(.hidden)
            .scrollDisabled(true)
            .focused($focus, equals: .body)
            .frame(maxWidth: .infinity, minHeight: 320, alignment: .topLeading)
            .padding(.horizontal, -5)
            .overlay(alignment: .topLeading) {
                if note.content.isEmpty {
                    Text("开始书写…").foregroundStyle(.tertiaryText)
                        .padding(.top, 8).allowsHitTesting(false)
                }
            }
            .accessibilityLabel("正文")
            .accessibilityIdentifier("note-body")
    }

    private var preview: some View {
        Group {
            if note.content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                Text("轻点开始书写").foregroundStyle(.tertiaryText)
            } else {
                NoteMarkdownView(text: note.content) { line in
                    linkTime = .now
                    note.content = MarkdownEdit.toggleTodo(note.content, line: line)
                }
            }
        }
        .frame(maxWidth: .infinity, minHeight: 320, alignment: .topLeading)
        .contentShape(Rectangle())
        .onTapGesture {
            // A tap on a reference or a check opens or ticks it; it should not also start editing.
            Task { @MainActor in
                if Date.now.timeIntervalSince(linkTime) < 0.5 { return }
                startEditing(cursorAtEnd: true)
            }
        }
        .environment(\.openURL, OpenURLAction { url in
            guard url.scheme == "noted" else { return .systemAction }
            MainActor.assumeIsolated {
                linkTime = .now
                if let ref = MarkdownEdit.reference(url) { open(ref) }
            }
            return .handled
        })
        .accessibilityIdentifier("note-preview")
    }

    /// Goes to what a reference points at, on this tab's stack.
    private func open(_ ref: MarkdownEdit.Reference) {
        if ref.kind == "note", ref.id == note.id { return }
        guard let r = ref.route else { return }
        flush()
        router.push(r)
    }

    private func startEditing(cursorAtEnd: Bool) {
        editing = true
        if cursorAtEnd {
            let s = note.content
            selection = TextSelection(insertionPoint: s.endIndex)
        }
        // The editor replaces the preview in this update; focus it once it is on screen.
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(60))
            focus = .body
        }
    }

    private func finishEditing() {
        focus = nil
        mention = nil
        editing = false
        flush()
    }

    // MARK: keyboard bar

    private var keyboardBar: some View {
        HStack(spacing: 0) {
            Menu {
                Button("标题 1") { linePrefix("# ") }
                Button("标题 2") { linePrefix("## ") }
                Button("标题 3") { linePrefix("### ") }
            } label: {
                key { Text("H").font(.system(size: 18, weight: .bold)) }
            }
            .accessibilityLabel("标题")
            .accessibilityIdentifier("kb-heading")
            tool("粗体", "kb-bold") { Text("B").font(.system(size: 18, weight: .bold)) } action: { wrap("**") }
            tool("斜体", "kb-italic") { Text("I").font(.system(size: 19, weight: .regular, design: .serif)).italic() } action: { wrap("*") }
            tool("列表", "kb-list") { Image(systemName: "list.bullet") } action: { linePrefix("- ") }
            tool("待办", "kb-todo") { Image(systemName: "checklist") } action: { linePrefix("- [ ] ") }
            tool("引用", "kb-quote") { Image(systemName: "text.quote") } action: { linePrefix("> ") }
            tool("代码", "kb-code") { Image(systemName: "chevron.left.forwardslash.chevron.right") } action: { wrap("`") }
            tool("链接", "kb-link") { Image(systemName: "link") } action: { insertLink() }
            tool("提及", "kb-mention", on: mention != nil) { Image(systemName: "at") } action: { toggleMention() }
            tool(focus == nil ? "预览" : "收起键盘", "kb-done") {
                Image(systemName: focus == nil ? "eye" : "keyboard.chevron.compact.down")
            } action: { finishEditing() }
        }
        .font(.system(size: 17, weight: .medium))
        .foregroundStyle(.primary)
        .frame(height: 46)
        .padding(.horizontal, 4)
        .background {
            Rectangle().fill(.bar).ignoresSafeArea(edges: .bottom)
                .overlay(alignment: .top) { Rectangle().fill(.hairline).frame(height: 0.5) }
        }
    }

    private func key<L: View>(on: Bool = false, @ViewBuilder _ label: () -> L) -> some View {
        label()
            .frame(maxWidth: .infinity, minHeight: 40)
            .background(on ? AnyShapeStyle(.fillTrack) : AnyShapeStyle(.clear), in: RoundedRectangle(cornerRadius: 8))
            .contentShape(Rectangle())
    }

    private func tool<L: View>(_ name: String, _ identifier: String, on: Bool = false, @ViewBuilder _ label: () -> L, action: @escaping () -> Void) -> some View {
        Button(action: action) { key(on: on, label) }
            .buttonStyle(.plain)
            .accessibilityLabel(name)
            .accessibilityIdentifier(identifier)
            .accessibilityAddTraits(on ? .isSelected : [])
    }

    /// The selection as character offsets into the body; the end of the text when there is none.
    private func range() -> Range<Int> {
        let s = note.content
        guard let sel = selection, case .selection(let r) = sel.indices,
              r.lowerBound >= s.startIndex, r.upperBound <= s.endIndex else { return s.count..<s.count }
        return s.distance(from: s.startIndex, to: r.lowerBound)..<s.distance(from: s.startIndex, to: r.upperBound)
    }

    private func apply(_ r: MarkdownEdit.Result) {
        note.content = r.text
        let s = note.content
        let lo = s.index(s.startIndex, offsetBy: min(r.selection.lowerBound, s.count))
        let hi = s.index(s.startIndex, offsetBy: min(r.selection.upperBound, s.count))
        selection = TextSelection(range: lo..<hi)
        if focus != .body { focus = .body }
    }

    private func linePrefix(_ prefix: String) { apply(MarkdownEdit.toggleLinePrefix(note.content, range(), prefix)) }
    private func wrap(_ marker: String) { apply(MarkdownEdit.wrap(note.content, range(), marker)) }

    private func insertLink() {
        let r = range()
        let chars = Array(note.content)
        let picked = r.isEmpty ? "" : String(chars[r]).replacingOccurrences(of: "\n", with: " ")
        let snippet = "[\(picked)](https://)"
        apply(MarkdownEdit.insert(note.content, r, snippet, cursorAt: picked.isEmpty ? 1 : snippet.count - 1))
    }

    private func toggleMention() {
        if mention != nil { mention = nil; return }
        let r = range()
        let chars = Array(note.content)
        let needsSpace = r.lowerBound > 0 && !(chars[r.lowerBound - 1].isWhitespace)
        let snippet = needsSpace ? " @" : "@"
        apply(MarkdownEdit.insert(note.content, r, snippet))
        mention = MarkdownEdit.Mention(at: r.lowerBound + snippet.count - 1, query: "")
    }

    // MARK: typing

    private func bodyChanged(_ old: String, _ new: String) {
        guard editing, focus == .body else { return }
        let cursor = MarkdownEdit.changeEnd(old: old, new: new)
        // Return on a list line starts the next item; on an empty item it ends the list.
        if new.count == old.count + 1, let r = MarkdownEdit.continueList(old: old, new: new, cursor: cursor) {
            apply(r)
            return
        }
        trackMention(old, new, cursor: cursor)
    }

    /// An `@` typed opens the reference panel; what follows filters it; moving off closes it.
    private func trackMention(_ old: String, _ new: String, cursor: Int) {
        let chars = Array(new)
        if new.count == old.count + 1, cursor > 0, cursor <= chars.count, MarkdownEdit.isMentionMark(chars[cursor - 1]) {
            mention = MarkdownEdit.Mention(at: cursor - 1, query: "")
            return
        }
        guard let m = mention else { return }
        guard let now = MarkdownEdit.mention(in: new, cursor: cursor), now.at == m.at,
              now.query.first?.isWhitespace != true, now.query.count <= 20 else {
            mention = nil
            return
        }
        mention = now
    }

    private func pick(_ item: NoteMentionPanel.Item) {
        guard let m = mention else { return }
        let link = MarkdownEdit.referenceLink(title: item.title, kind: item.kind, id: item.id)
        mention = nil
        var r = MarkdownEdit.replaceMention(note.content, atOffset: m.at, queryLength: m.query.count, link: link)
        // A space after the link, so typing goes on as plain text.
        let chars = Array(r.text)
        let c = r.selection.lowerBound
        if c >= chars.count || !chars[c].isWhitespace {
            r = MarkdownEdit.insert(r.text, c..<c, " ")
        }
        apply(r)
    }

    // MARK: loading and saving

    private func load() async {
        guard !loaded else { return }
        if id.isEmpty {
            // A new note left unsaved last time becomes a note of its own rather than being lost.
            if let left = NoteDrafts.load(""), !NoteText.isBlank(left) {
                NoteDrafts.clear("")
                await data.createNote(left)
            }
            var n = NoteDrafts.takeSeed() ?? Noted_V1_Note()
            if n.space == .unspecified { n.space = session.mode == .work ? .work : .life }
            note = n
            loaded = true
            editing = true
            try? await Task.sleep(for: .milliseconds(350))
            focus = .title
            return
        }
        var base = data.note(id)
        if base == nil { base = await data.loadNote(id) }
        let draft = NoteDrafts.load(id)
        guard let start = draft ?? base else { missing = true; return }
        note = start
        saved = base
        loaded = true
        if NoteText.isBlank(start) { editing = true }
        if let draft {
            if base.map({ !NoteText.sameContent($0, draft) }) ?? true { scheduleSave() } else { NoteDrafts.clear(id) }
        }
    }

    private func noteChanged(_ n: Noted_V1_Note) {
        guard loaded, !deleted else { return }
        if let s = saved, NoteText.sameContent(s, n) { return }
        if n.id.isEmpty && NoteText.isBlank(n) { NoteDrafts.clear(""); return }
        NoteDrafts.store(n)
        scheduleSave()
    }

    private func scheduleSave() {
        pending?.cancel()
        pending = Task {
            try? await Task.sleep(for: .milliseconds(800))
            if Task.isCancelled { return }
            await persist()
        }
    }

    /// Saves now (leaving the page, going to the background, 完成).
    private func flush() {
        pending?.cancel()
        Task { await persist() }
    }

    private func persist() async {
        guard loaded, !deleted, !missing else { return }
        if inFlight { again = true; return }
        let current = note
        if let s = saved, NoteText.sameContent(s, current) { NoteDrafts.clear(current.id); return }
        if current.id.isEmpty && NoteText.isBlank(current) { return }
        inFlight = true
        status = .saving
        if current.id.isEmpty {
            if let made = await data.createNote(current) {
                var s = current
                s.id = made.id; s.createTime = made.createTime; s.updateTime = made.updateTime
                saved = s
                NoteDrafts.clear("")
                note.id = made.id
                note.createTime = made.createTime
                note.updateTime = made.updateTime
                status = .saved(.now)
            } else {
                status = .local
            }
        } else {
            if let n = await data.updateNote(current, fields: ["title", "content", "tags", "pinned", "project_id", "space"]) {
                var s = current
                s.updateTime = n.updateTime
                saved = s
                note.updateTime = n.updateTime
                status = .saved(.now)
            } else {
                status = .local
            }
        }
        inFlight = false
        if let s = saved, NoteText.sameContent(s, note) {
            NoteDrafts.clear(note.id)
        } else if again {
            again = false
            scheduleSave()
        }
        again = false
    }

    // MARK: 提取待办

    @ViewBuilder private var extractCard: some View {
        switch extract {
        case .idle:
            EmptyView()
        case .loading:
            HStack(spacing: 10) {
                ProgressView()
                Text("正在找笔记里的待办…").font(.subheadline).foregroundStyle(.secondaryText)
                Spacer()
            }
            .padding(14)
            .background(.fillQuiet, in: RoundedRectangle(cornerRadius: 12))
        case .empty:
            HStack {
                Text("没有找到待办").font(.subheadline).foregroundStyle(.secondaryText)
                Spacer()
                Button("关闭") { extract = .idle }.font(.subheadline.weight(.semibold))
            }
            .padding(14)
            .background(.fillQuiet, in: RoundedRectangle(cornerRadius: 12))
        case .ready(let p):
            ExtractProposalCard(proposal: p) { added in
                extract = .idle
                if added > 0 { toaster.show("已加入 \(added) 项待办") }
            }
        }
    }

    private func runExtract() async {
        pending?.cancel()
        await persist()
        guard !note.id.isEmpty else { return }
        extract = .loading
        guard let p = await data.extractTasks(noteID: note.id) else { extract = .idle; return }
        extract = p.operations.isEmpty ? .empty : .ready(p)
    }
}

/// The to-dos the assistant found in the note, shown in place: pick which to add, fill what it
/// could not know, then add them. Nothing is created before 加入.
private struct ExtractProposalCard: View {
    @Environment(AppData.self) private var data
    let proposal: Noted_V1_Proposal
    var onDone: (Int) -> Void

    @State private var picked: Set<Int>
    @State private var dates: [String: Date] = [:]
    @State private var texts: [String: String] = [:]
    @State private var busy = false

    init(proposal: Noted_V1_Proposal, onDone: @escaping (Int) -> Void) {
        self.proposal = proposal
        self.onDone = onDone
        _picked = State(initialValue: Set(proposal.operations.indices))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Label("笔记里有 \(proposal.operations.count) 件事", systemImage: "sparkles")
                    .font(.subheadline.weight(.semibold))
                Spacer()
            }
            .padding(.horizontal, 14).padding(.vertical, 12)
            ForEach(Array(proposal.operations.enumerated()), id: \.offset) { i, op in
                Divider().padding(.leading, 14)
                Button {
                    if picked.contains(i) { picked.remove(i) } else { picked.insert(i) }
                } label: {
                    HStack(spacing: 12) {
                        CheckMark(isOn: picked.contains(i), size: 22)
                        Text(op.label).font(.subheadline).multilineTextAlignment(.leading)
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 14).frame(minHeight: 44)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(picked.contains(i) ? .isSelected : [])
            }
            ForEach(proposal.inputs, id: \.name) { input in
                Divider().padding(.leading, 14)
                HStack {
                    Text(input.label).font(.subheadline)
                    Spacer()
                    if input.type == "date" {
                        DatePicker(input.label, selection: dateBinding(input.name), displayedComponents: .date)
                            .labelsHidden()
                            .environment(\.locale, Fmt.zh)
                    } else {
                        TextField(input.required ? "必填" : "选填", text: Binding(get: { texts[input.name] ?? "" }, set: { texts[input.name] = $0 }))
                            .multilineTextAlignment(.trailing)
                            .font(.subheadline)
                    }
                }
                .padding(.horizontal, 14).frame(minHeight: 44)
            }
            Divider()
            HStack(spacing: 10) {
                Button("忽略") {
                    Task { await data.dismissProposal(proposal.id) }
                    onDone(0)
                }
                .buttonStyle(.wide(.gray, small: true))
                .accessibilityIdentifier("extract-dismiss")
                Button(busy ? "正在加入…" : "加入 \(picked.count) 项待办") { Task { await accept() } }
                    .buttonStyle(.wide(.filled, small: true))
                    .disabled(picked.isEmpty || busy || missingRequired)
                    .accessibilityIdentifier("extract-accept")
            }
            .padding(12)
        }
        .background(.fillQuiet, in: RoundedRectangle(cornerRadius: 12))
    }

    private var missingRequired: Bool {
        proposal.inputs.contains { $0.required && $0.type != "date" && (texts[$0.name] ?? "").trimmingCharacters(in: .whitespaces).isEmpty }
    }

    private func dateBinding(_ name: String) -> Binding<Date> {
        Binding(get: { dates[name] ?? Date.now.startOfDay }, set: { dates[name] = $0 })
    }

    private func accept() async {
        busy = true
        defer { busy = false }
        var inputs: [String: String] = [:]
        for input in proposal.inputs {
            if input.type == "date" { inputs[input.name] = Fmt.ymd(dates[input.name] ?? Date.now.startOfDay) }
            else if let t = texts[input.name], !t.isEmpty { inputs[input.name] = t }
        }
        let n = picked.count
        if await data.acceptProposal(proposal.id, indexes: picked.sorted(), inputs: inputs) != nil { onDone(n) }
    }
}

private extension View {
    /// The meta row's 30 pt pill (`.pill`): gray, or accent-tinted for tags.
    func notePill(accent: Bool = false) -> some View {
        self.font(.subheadline)
            .lineLimit(1)
            .padding(.horizontal, 12)
            .frame(height: 30)
            .foregroundStyle(accent ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary))
            .background(RoundedRectangle(cornerRadius: 8).fill(accent ? AnyShapeStyle(.tint.opacity(0.12)) : AnyShapeStyle(.fillTrack)))
            .contentShape(Rectangle())
    }
}
