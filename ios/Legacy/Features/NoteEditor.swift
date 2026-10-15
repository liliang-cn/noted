import SwiftUI

/// Unsaved edits kept on the device, so nothing typed is lost to a crash, a dropped connection
/// or a swipe back before the server answered.
enum NoteDrafts {
    static func key(_ id: String) -> String { id.isEmpty ? "draft.note.new" : "draft.note.\(id)" }

    static func load(_ id: String) -> Noted_V1_Note? {
        guard let s = UserDefaults.standard.string(forKey: key(id)) else { return nil }
        return try? Noted_V1_Note(jsonString: s)
    }

    static func store(_ n: Noted_V1_Note) {
        if let s = try? n.jsonString() { UserDefaults.standard.set(s, forKey: key(n.id)) }
    }

    static func clear(_ id: String) { UserDefaults.standard.removeObject(forKey: key(id)) }
}

/// A note on its own page: title, where it belongs, and the Markdown body. It saves itself
/// while you type; there is no save button to forget.
struct NoteEditor: View {
    @Environment(Session.self) private var session
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.mode) private var mode
    @Environment(\.dismiss) private var dismiss

    @State private var note: Noted_V1_Note
    @State private var saved: Noted_V1_Note?
    @State private var editing: Bool
    @State private var restored: Bool
    @State private var selection: TextSelection?
    @FocusState private var bodyFocused: Bool
    @FocusState private var titleFocused: Bool
    @State private var status = Status.idle
    @State private var pending: Task<Void, Never>?
    @State private var projects: [Noted_V1_Project] = []
    @State private var naming = false
    @State private var newTag = ""
    @State private var confirmDelete = false
    @State private var shareImage: ShareImage?
    @State private var error: String?
    @State private var mention: MarkdownEdit.Mention?
    @State private var mentionProject: Noted_V1_Project?
    @State private var mentionDetail: Noted_V1_ProjectDetail?
    @State private var mentionBase = 0          // query length when the project was picked
    @State private var linkTime = Date.distantPast
    private let photos = PhotoSaver()

    enum Status: Equatable { case idle, saving, saved(Date), local }

    /// Opened from the create page: keeps the create header and its kinds.
    private let fromCreate: Bool

    init(note: Noted_V1_Note) {
        fromCreate = note.id.isEmpty
        let draft = NoteDrafts.load(note.id)
        let start = draft ?? note
        _note = State(initialValue: start)
        _saved = State(initialValue: note.id.isEmpty ? nil : note)
        _restored = State(initialValue: draft != nil && draft != note)
        _editing = State(initialValue: start.id.isEmpty || start.content.isEmpty)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                if fromCreate {
                    CreateHeader(selected: .note) { switchKind($0) }
                    HStack(spacing: 14) { Spacer(); headerActions }
                } else {
                    header
                }
                if restored { restoredBanner }
                TextField("标题", text: $note.title, axis: .vertical)
                    .font(.ui(30, .bold)).foregroundStyle(p.ink)
                    .focused($titleFocused)
                    .submitLabel(.next)
                    .onSubmit { editing = true; bodyFocused = true }
                    .accessibilityIdentifier("note-title")
                meta
                ErrorLine(text: error)
                if editing { editor } else { preview }
            }
            .padding(.horizontal, 20).padding(.top, 12).padding(.bottom, 40)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(p.bg.ignoresSafeArea())
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(spacing: 0) {
                if mention != nil { mentionPanel }
                toolbar
            }
        }
        .onChange(of: note) { _, _ in scheduleSave() }
        .onChange(of: note.content) { old, new in continueList(old, new); trackMention(old, new) }
        .onChange(of: editing) { _, on in if !on { closeMention() } }
        .onChange(of: bodyFocused) { _, on in if !on { closeMention() } }
        .onAppear {
            if note.id.isEmpty && note.title.isEmpty { titleFocused = true }
        }
        .onDisappear { flush() }
        .task { await loadProjects() }
        .alert("添加标签", isPresented: $naming) {
            TextField("标签", text: $newTag).textInputAutocapitalization(.never)
            Button("取消", role: .cancel) {}
            Button("添加") { addTag() }
        }
        .confirmationDialog(note.id.isEmpty ? "丢弃这条笔记?" : "删除这条笔记?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button(note.id.isEmpty ? "丢弃" : "删除", role: .destructive) { Task { await remove() } }
            Button("取消", role: .cancel) {}
        }
        .sheet(item: $shareImage) { ActivitySheet(items: [$0.image]) }
    }

    // MARK: header and meta

    private var header: some View {
        HStack(spacing: 14) {
            BackButton(title: "笔记")
            Spacer()
            headerActions
        }
    }

    @ViewBuilder private var headerActions: some View {
            Button { note.pinned.toggle() } label: {
                Image(systemName: note.pinned ? "pin.fill" : "pin").font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(note.pinned ? mode.accent(p) : p.ink3)
            }
            .accessibilityLabel(note.pinned ? "取消置顶" : "置顶")
            Menu {
                Button { copyText() } label: { Label("复制文字", systemImage: "doc.on.doc") }
                ShareLink(item: NoteImage.text(note), subject: Text(note.title)) { Label("分享文字", systemImage: "text.alignleft") }
                Button { shareAsImage() } label: { Label("分享为图片", systemImage: "photo") }
                Button { saveToPhotos() } label: { Label("保存到相册", systemImage: "square.and.arrow.down") }
                if session.aiNoteTools && !note.content.isEmpty && !note.id.isEmpty {
                    Button { flush(); router.push(.extract(note.id)) } label: { Label("提取待办", systemImage: "checklist") }
                }
                Divider()
                Button(role: .destructive) { confirmDelete = true } label: {
                    Label(note.id.isEmpty ? "丢弃" : "删除", systemImage: "trash")
                }
            } label: {
                Image(systemName: "ellipsis").font(.system(size: 17, weight: .semibold)).foregroundStyle(p.ink2)
                    .frame(width: 28, height: 32)
            }
            .accessibilityLabel("更多")
            Button { pending?.cancel(); Task { await persist(); dismiss() } } label: {
                Text("完成").font(.ui(15, .semibold)).foregroundStyle(mode.accent(p))
            }
            .accessibilityIdentifier("note-done")
    }

    /// From the create page's kinds: the title moves to the other kind and this note is dropped.
    private func switchKind(_ k: CaptureView.Kind) {
        guard k != .note else { return }
        pending?.cancel()
        NoteDrafts.clear(note.id)
        if !note.id.isEmpty, let api = session.api {
            var r = Noted_V1_DeleteNoteRequest(); r.id = note.id
            Task { _ = try? await api.notes.deleteNote(r) }
        }
        let spec = CreateSpec(kind: k.rawValue, projectID: note.projectID, space: note.space, title: note.title)
        note = Noted_V1_Note()
        var t = Transaction(); t.disablesAnimations = true
        withTransaction(t) { router.replaceTop(.create(spec)) }
    }

    private var restoredBanner: some View {
        HStack(spacing: 10) {
            Image(systemName: "arrow.uturn.backward.circle").foregroundStyle(p.warn)
            Text("恢复了上次没保存完的内容").font(.ui(13)).foregroundStyle(p.ink)
            Spacer()
            Button("放弃") {
                if let s = saved { note = s } else { note = Noted_V1_Note(); note.space = session.mode == .work ? .work : .life }
                NoteDrafts.clear(note.id)
                restored = false
            }
            .font(.ui(13, .semibold)).foregroundStyle(p.ink2)
        }
        .padding(.horizontal, 14).padding(.vertical, 10)
        .background(Color(hex: 0xFFF1CC), in: RoundedRectangle(cornerRadius: p.radiusSm))
    }

    private var meta: some View {
        FlowLayout(spacing: 6) {
            Menu {
                Button { note.space = .life } label: { Label("个人", systemImage: note.space == .life ? "checkmark" : "") }
                Button { note.space = .work } label: { Label("工作", systemImage: note.space == .work ? "checkmark" : "") }
            } label: { SpaceTag(space: note.space == .unspecified ? .life : note.space) }
            .accessibilityLabel("空间")

            Menu {
                Button { note.projectID = "" } label: { Label("不属于项目", systemImage: note.projectID.isEmpty ? "checkmark" : "") }
                ForEach(projects, id: \.id) { pr in
                    Button { note.projectID = pr.id } label: { Label(pr.title, systemImage: note.projectID == pr.id ? "checkmark" : "") }
                }
            } label: {
                tag(projects.first { $0.id == note.projectID }?.title ?? "项目", faint: note.projectID.isEmpty)
            }
            .accessibilityLabel("项目")

            ForEach(note.tags, id: \.self) { t in
                Menu {
                    Button(role: .destructive) { note.tags.removeAll { $0 == t } } label: { Label("移除 #\(t)", systemImage: "xmark") }
                } label: { tag("#\(t)") }
            }
            Button { newTag = ""; naming = true } label: { tag("+ 标签", dashed: true) }
                .accessibilityLabel("添加标签")
            Text(statusText).font(.mono(12)).foregroundStyle(status == .local ? p.warn : p.ink3)
                .padding(.leading, 4).frame(height: 22)
                .accessibilityIdentifier("save-status")
        }
    }

    private func tag(_ s: String, faint: Bool = false, dashed: Bool = false) -> some View {
        Text(s).font(.ui(11, .semibold))
            .padding(.horizontal, 8).frame(height: 22)
            .background(dashed ? Color.clear : p.surface2, in: RoundedRectangle(cornerRadius: 6))
            .overlay {
                if dashed { RoundedRectangle(cornerRadius: 6).stroke(p.ink3, style: StrokeStyle(lineWidth: 1, dash: [3, 2])) }
            }
            .foregroundStyle(faint || dashed ? p.ink3 : p.ink2)
    }

    private var statusText: String {
        switch status {
        case .idle: return note.id.isEmpty ? "未保存" : Fmt.relative(note.updateTime.date) + " " + Fmt.hm(note.updateTime.date)
        case .saving: return "保存中…"
        case .saved(let d): return "已保存 " + Fmt.hm(d)
        case .local: return "已存在本机,联网后同步"
        }
    }

    // MARK: body

    private var editor: some View {
        TextEditor(text: $note.content, selection: $selection)
            .font(.ui(15)).foregroundStyle(p.ink)
            .lineSpacing(6)
            .scrollContentBackground(.hidden)
            .scrollDisabled(true)
            .frame(minHeight: 320, alignment: .topLeading)
            .focused($bodyFocused)
            .padding(.horizontal, -5)
            .overlay(alignment: .topLeading) {
                if note.content.isEmpty {
                    Text("写点什么。# 标题,- 列表,- [ ] 待办").font(.ui(15)).foregroundStyle(p.ink3)
                        .padding(.top, 8).allowsHitTesting(false)
                }
            }
            .accessibilityIdentifier("note-body")
    }

    private var preview: some View {
        MarkdownView(text: note.content) { line in note.content = MarkdownEdit.toggleTodo(note.content, line: line) }
            .frame(maxWidth: .infinity, minHeight: 320, alignment: .topLeading)
            .contentShape(Rectangle())
            .onTapGesture {
                // A tap on a reference opens it; it should not also switch to editing.
                Task { @MainActor in
                    if Date.now.timeIntervalSince(linkTime) < 0.5 { return }
                    editing = true; bodyFocused = true
                }
            }
            .environment(\.openURL, OpenURLAction { url in
                guard url.scheme == "noted" else { return .systemAction }
                MainActor.assumeIsolated {
                    linkTime = .now
                    if let ref = MarkdownEdit.reference(url) { Task { await open(ref) } }
                }
                return .handled
            })
            .accessibilityIdentifier("note-preview")
    }

    /// Opens what a reference points at. A to-do, event or goal opens the project it belongs to.
    private func open(_ ref: MarkdownEdit.Reference) async {
        guard let api = session.api else { return }
        switch ref.kind {
        case "project": router.push(.project(ref.id))
        case "objective": router.push(.objective(ref.id))
        case "note":
            guard ref.id != note.id else { return }
            var r = Noted_V1_GetNoteRequest(); r.id = ref.id
            if let n = try? await api.notes.getNote(r) { router.push(.note(n)) }
        case "task":
            var r = Noted_V1_GetTaskRequest(); r.id = ref.id
            if let t = try? await api.calendar.getTask(r), !t.projectID.isEmpty { router.push(.project(t.projectID)) }
        case "event":
            var r = Noted_V1_GetEventRequest(); r.id = ref.id
            if let e = try? await api.calendar.getEvent(r), !e.projectID.isEmpty { router.push(.project(e.projectID)) }
        case "goal":
            var r = Noted_V1_GetGoalRequest(); r.id = ref.id
            guard let g = try? await api.goals.getGoal(r) else { return }
            if !g.projectID.isEmpty { router.push(.project(g.projectID)) }
            else if !g.objectiveID.isEmpty { router.push(.objective(g.objectiveID)) }
        default: break
        }
    }

    // MARK: @ references

    /// A row of the @ panel. `title` goes into the link; `label` is what the row shows.
    private struct MentionItem {
        var kind: String
        var id: String
        var title: String
        var label: String
        var detail = ""
        var key: String { "\(kind)/\(id)/\(label)" }
    }

    private static let kindLabels = ["project": "项目", "task": "待办", "event": "日程", "note": "笔记", "goal": "目标", "objective": "多维目标"]

    private var mentionItems: [MentionItem] {
        let q = String((mention?.query ?? "").dropFirst(mentionBase)).trimmingCharacters(in: .whitespaces).lowercased()
        let match: (MentionItem) -> Bool = { q.isEmpty || $0.title.lowercased().contains(q) }
        guard let pr = mentionProject else {
            return projects.map { MentionItem(kind: "project", id: $0.id, title: $0.title, label: $0.title, detail: projectDate($0)) }.filter(match)
        }
        guard let d = mentionDetail else { return [] }
        var out: [MentionItem] = []
        out += d.tasks.map { MentionItem(kind: "task", id: $0.id, title: $0.title, label: $0.title, detail: $0.hasDueTime ? Fmt.md($0.dueTime.date) : "") }
        out += d.events.map {
            MentionItem(kind: "event", id: $0.id, title: $0.title, label: $0.title,
                        detail: Fmt.md($0.startTime.date) + ($0.allDay ? "" : " " + Fmt.hm($0.startTime.date)))
        }
        out += d.notes.filter { $0.id != note.id }.map {
            let first = $0.content.split(separator: "\n").first.map(String.init) ?? ""
            return MentionItem(kind: "note", id: $0.id, title: $0.title, label: $0.title.isEmpty ? "无标题" : $0.title, detail: first)
        }
        out += d.goals.map { MentionItem(kind: "goal", id: $0.id, title: $0.title, label: $0.title) }
        out += d.objectives.map { MentionItem(kind: "objective", id: $0.id, title: $0.title, label: $0.title) }
        let own = MentionItem(kind: "project", id: pr.id, title: pr.title, label: "引用项目本身", detail: pr.title)
        return ([own] + out).filter(match)
    }

    private func projectDate(_ pr: Noted_V1_Project) -> String {
        if pr.hasStartTime { return "\(Fmt.md(pr.startTime.date))出发" }
        if pr.hasDueTime { return "\(Fmt.md(pr.dueTime.date))截止" }
        return ""
    }

    private var mentionPanel: some View {
        let items = mentionItems
        return VStack(spacing: 0) {
            HStack(spacing: 8) {
                if let pr = mentionProject {
                    Button { backToProjects() } label: {
                        HStack(spacing: 3) {
                            Image(systemName: "chevron.left").font(.system(size: 13, weight: .semibold))
                            Text(pr.title).font(.ui(13, .semibold)).lineLimit(1)
                        }
                        .foregroundStyle(mode.accent(p)).frame(minHeight: 36).contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("mention-back")
                } else {
                    Eyebrow(text: "引用")
                }
                Spacer()
                Button { closeMention() } label: {
                    Text("取消").font(.ui(13, .semibold)).foregroundStyle(p.ink2).frame(minHeight: 36).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("mention-cancel")
            }
            .padding(.horizontal, 14)
            Rectangle().fill(p.line).frame(height: 1)
            ViewThatFits(in: .vertical) {
                mentionRows(items)
                ScrollView { mentionRows(items) }.scrollIndicators(.hidden)
            }
            .frame(maxHeight: 232)
        }
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
        .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))
        .padding(.horizontal, 12).padding(.bottom, 8)
        .accessibilityIdentifier("mention-panel")
    }

    @ViewBuilder private func mentionRows(_ items: [MentionItem]) -> some View {
        VStack(spacing: 0) {
            ForEach(Array(items.enumerated()), id: \.element.key) { i, it in
                if i > 0 { Rectangle().fill(p.line).frame(height: 1).padding(.leading, 14) }
                mentionRow(it)
            }
            if mentionProject != nil && mentionDetail == nil {
                ProgressView().frame(maxWidth: .infinity, minHeight: 44)
            } else if items.isEmpty {
                Text("没有匹配的内容").font(.ui(13)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 44)
            }
        }
    }

    private func mentionRow(_ it: MentionItem) -> some View {
        Button { choose(it) } label: {
            HStack(spacing: 10) {
                Text(Self.kindLabels[it.kind] ?? "").font(.ui(11, .semibold))
                    .padding(.horizontal, 6).frame(height: 20)
                    .background(p.surface2, in: RoundedRectangle(cornerRadius: 5))
                    .foregroundStyle(p.ink2)
                VStack(alignment: .leading, spacing: 2) {
                    Text(it.label).font(.ui(15)).foregroundStyle(p.ink).lineLimit(1)
                    if !it.detail.isEmpty { Text(it.detail).font(.ui(12)).foregroundStyle(p.ink2).lineLimit(1) }
                }
                Spacer(minLength: 0)
                if it.kind == "project" && mentionProject == nil {
                    Image(systemName: "chevron.right").font(.system(size: 12, weight: .semibold)).foregroundStyle(p.ink3)
                }
            }
            .padding(.horizontal, 14).padding(.vertical, 6)
            .frame(minHeight: 44).contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("mention-\(it.kind)")
    }

    /// The person typed into the body: an `@` opens the panel; while it is open, what follows the
    /// `@` filters it, and moving off the `@` closes it.
    private func trackMention(_ old: String, _ new: String) {
        guard editing else { return }
        let cursor = MarkdownEdit.changeEnd(old: old, new: new)
        let chars = Array(new)
        if new.count == old.count + 1, cursor > 0, cursor <= chars.count, MarkdownEdit.isMentionMark(chars[cursor - 1]) {
            openMention(at: cursor - 1)
            return
        }
        guard let m = mention else { return }
        guard let now = MarkdownEdit.mention(in: new, cursor: cursor), now.at == m.at else { closeMention(); return }
        if now.query.count < mentionBase { backToProjects() }
        mention = now
        if now.query.first?.isWhitespace == true || (now.query.last?.isWhitespace == true && mentionItems.isEmpty) { closeMention() }
    }

    private func openMention(at offset: Int) {
        mention = MarkdownEdit.Mention(at: offset, query: "")
        mentionProject = nil
        mentionDetail = nil
        mentionBase = 0
        if projects.isEmpty { Task { await loadProjects() } }
    }

    private func closeMention() {
        mention = nil
        mentionProject = nil
        mentionDetail = nil
        mentionBase = 0
    }

    private func backToProjects() {
        mentionProject = nil
        mentionDetail = nil
        mentionBase = 0
    }

    private func choose(_ it: MentionItem) {
        if it.kind == "project", mentionProject == nil, let pr = projects.first(where: { $0.id == it.id }) {
            mentionProject = pr
            mentionDetail = nil
            mentionBase = mention?.query.count ?? 0
            Task {
                var r = Noted_V1_GetProjectRequest(); r.id = pr.id
                let d = try? await session.api?.projects.getProject(r)
                if mentionProject?.id == pr.id { mentionDetail = d ?? Noted_V1_ProjectDetail() }
            }
            return
        }
        guard let m = mention else { return }
        let link = MarkdownEdit.referenceLink(title: it.title, kind: it.kind, id: it.id)
        closeMention()
        beginEditing()
        apply(MarkdownEdit.replaceMention(note.content, atOffset: m.at, queryLength: m.query.count, link: link))
    }

    // MARK: Markdown toolbar

    private var toolbar: some View {
        HStack(spacing: 0) {
            Menu {
                Button("标题 1") { linePrefix("# ") }
                Button("标题 2") { linePrefix("## ") }
                Button("标题 3") { linePrefix("### ") }
            } label: { toolLabel { Text("H").font(.mono(17, .bold)) } }
            .accessibilityLabel("标题")
            tool("粗体") { Text("B").font(.ui(17, .bold)) } action: { wrap("**") }
            tool("斜体") { Text("I").font(.ui(17, .semibold)).italic() } action: { wrap("*") }
            tool("列表") { Image(systemName: "list.bullet") } action: { linePrefix("- ") }
            tool("待办") { Image(systemName: "checklist") } action: { linePrefix("- [ ] ") }
            tool("引用") { Image(systemName: "text.quote") } action: { linePrefix("> ") }
            tool("代码") { Image(systemName: "chevron.left.forwardslash.chevron.right") } action: { wrap("`") }
            tool("链接") { Image(systemName: "link") } action: { insert("[](https://)", cursorAt: 1) }
            tool("提及") { Text("@").font(.ui(18, .semibold)) } action: {
                let at = range().lowerBound
                insert("@", cursorAt: 1)
                openMention(at: at)
            }
            Rectangle().fill(p.line).frame(width: 1, height: 24)
            if bodyFocused || titleFocused {
                tool("收起键盘") { Image(systemName: "keyboard.chevron.compact.down") } action: { bodyFocused = false; titleFocused = false }
            } else {
                tool(editing ? "预览" : "编辑") { Image(systemName: editing ? "eye" : "pencil") } action: {
                    editing.toggle()
                    if editing { bodyFocused = true }
                }
            }
        }
        .font(.system(size: 18, weight: .medium))
        .foregroundStyle(p.ink2)
        .frame(height: 52)
        .frame(maxWidth: .infinity)
        .background(p.surface.ignoresSafeArea(edges: .bottom))
        .overlay(alignment: .top) { Rectangle().fill(p.line).frame(height: 1) }
    }

    private func toolLabel<L: View>(@ViewBuilder _ label: () -> L) -> some View {
        label().frame(maxWidth: .infinity, minHeight: 44).contentShape(Rectangle())
    }

    private func tool<L: View>(_ name: String, @ViewBuilder _ label: () -> L, action: @escaping () -> Void) -> some View {
        Button(action: action) { toolLabel(label) }
            .buttonStyle(.plain)
            .accessibilityLabel(name)
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
    }

    private func beginEditing() {
        if !editing { editing = true }
        bodyFocused = true
    }

    private func linePrefix(_ prefix: String) { beginEditing(); apply(MarkdownEdit.toggleLinePrefix(note.content, range(), prefix)) }
    private func wrap(_ marker: String) { beginEditing(); apply(MarkdownEdit.wrap(note.content, range(), marker)) }
    private func insert(_ s: String, cursorAt: Int) { beginEditing(); apply(MarkdownEdit.insert(note.content, range(), s, cursorAt: cursorAt)) }

    /// Return on a list line starts the next item; return on an empty item ends the list.
    private func continueList(_ old: String, _ new: String) {
        guard editing, new.count == old.count + 1 else { return }
        let a = Array(old), b = Array(new)
        var i = 0
        while i < a.count, a[i] == b[i] { i += 1 }
        if let r = MarkdownEdit.continueList(old: old, new: new, cursor: i + 1) { apply(r) }
    }

    // MARK: saving

    private var isBlank: Bool {
        note.title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && note.content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func scheduleSave() {
        guard !isBlank else { return }
        NoteDrafts.store(note)
        pending?.cancel()
        pending = Task {
            try? await Task.sleep(for: .milliseconds(800))
            if Task.isCancelled { return }
            await persist()
        }
    }

    /// Saves now. Leaving the page calls it, so the last keystrokes are not lost.
    private func flush() {
        pending?.cancel()
        guard !isBlank else { return }
        Task { await persist() }
    }

    private func persist() async {
        guard let api = session.api, !isBlank else { return }
        var current = note
        if let s = saved, sameContent(s, current) { return }
        status = .saving
        do {
            if current.id.isEmpty {
                var c = Noted_V1_CreateNoteRequest()
                c.note = current
                let made = try await api.notes.createNote(c)
                NoteDrafts.clear("")
                current.id = made.id
                current.createTime = made.createTime
                current.updateTime = made.updateTime
                note.id = made.id
                note.createTime = made.createTime
            } else {
                var r = Noted_V1_UpdateNoteRequest()
                r.note = current
                r.updateMask = fieldMask("title", "content", "pinned", "tags", "space", "project_id")
                _ = try await api.notes.updateNote(r)
            }
            saved = current
            NoteDrafts.clear(current.id)
            if sameContent(current, note) { status = .saved(.now) }
            restored = false
        } catch {
            NoteDrafts.store(note)
            status = .local
        }
    }

    private func sameContent(_ a: Noted_V1_Note, _ b: Noted_V1_Note) -> Bool {
        a.id == b.id && a.title == b.title && a.content == b.content && a.pinned == b.pinned
            && a.tags == b.tags && a.space == b.space && a.projectID == b.projectID
    }

    // MARK: other actions

    private func addTag() {
        let t = newTag.trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "#"))
        guard !t.isEmpty, !note.tags.contains(t) else { return }
        note.tags.append(t)
    }

    private func loadProjects() async {
        projects = (try? await session.api?.projects.listProjects(Noted_V1_ListProjectsRequest()).projects) ?? []
    }

    private func copyText() {
        UIPasteboard.general.string = NoteImage.text(note)
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
            Task { @MainActor in self.error = err.map { "没有保存:\($0.localizedDescription)" } }
        }
    }

    private func remove() async {
        pending?.cancel()
        NoteDrafts.clear(note.id)
        guard !note.id.isEmpty, let api = session.api else {
            note = Noted_V1_Note()
            dismiss()
            return
        }
        do {
            var r = Noted_V1_DeleteNoteRequest(); r.id = note.id
            _ = try await api.notes.deleteNote(r)
            saved = nil
            note = Noted_V1_Note()
            dismiss()
        } catch { self.error = describe(error) }
    }
}

/// Lays chips out in rows, wrapping when a row is full.
struct FlowLayout: SwiftUI.Layout {
    var spacing: CGFloat = 6

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, row: CGFloat = 0, widest: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if x > 0, x + s.width > width { y += row + spacing; x = 0; row = 0 }
            x += s.width + spacing
            row = max(row, s.height)
            widest = max(widest, x - spacing)
        }
        return CGSize(width: min(widest, width), height: y + row)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, row: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if x > bounds.minX, x + s.width > bounds.maxX { y += row + spacing; x = bounds.minX; row = 0 }
            v.place(at: CGPoint(x: x, y: y + (row > 0 ? 0 : 0)), proposal: ProposedViewSize(s))
            x += s.width + spacing
            row = max(row, s.height)
        }
    }
}
