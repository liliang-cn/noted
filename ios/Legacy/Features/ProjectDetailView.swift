import SwiftUI

/// One project on its own screen: its progress and every item that belongs to it,
/// with ways to add new items, link existing ones, take items out, and edit the project.
struct ProjectDetailView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let id: String
    @State private var detail: Noted_V1_ProjectDetail?
    @State private var error: String?
    @State private var linking = false
    @State private var editing = false
    @State private var confirmDelete = false
    @State private var pendingDelete: PendingDelete?
    @Environment(Router.self) private var router

    var body: some View {
        ZStack(alignment: .bottom) {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    topBar
                    ErrorLine(text: error)
                    if let d = detail { content(d) }
                }
                .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 96)
            }
            .confirmDelete($pendingDelete) { err in error = err; await load() }
            addButton
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .reloadOnReturn { await load() }
        .sheet(isPresented: $linking, onDismiss: { Task { await load() } }) { LinkExistingView(projectID: id) }
        .sheet(isPresented: $editing, onDismiss: { Task { await load() } }) {
            if let d = detail { EditProjectView(project: d.project) }
        }
        .confirmationDialog("删除这个项目", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("只删除项目,保留里面的内容") { Task { await delete(items: false) } }
            Button("连同待办、日程、笔记一起删除", role: .destructive) { Task { await delete(items: true) } }
            Button("取消", role: .cancel) {}
        }
    }

    private var accent: Color { detail?.project.space.color(p) ?? p.life }

    // MARK: header

    private var topBar: some View {
        HStack(spacing: 10) {
            BackButton(title: "返回")
            .accessibilityIdentifier("back")
            Spacer()
            if let d = detail {
                Button { Task { await togglePin(d.project) } } label: {
                    Text(d.project.pinned ? "已置顶" : "置顶").font(.ui(12, .semibold))
                        .padding(.horizontal, 10).frame(height: 28)
                        .background(d.project.space == .work ? p.workBg : p.lifeBg, in: RoundedRectangle(cornerRadius: 6))
                        .foregroundStyle(d.project.space.color(p))
                }
                Menu {
                    Button { editing = true } label: { Label("编辑项目", systemImage: "pencil") }
                    Button { Task { await setArchived(d.project, !d.project.archived) } } label: {
                        Label(d.project.archived ? "取消归档" : "归档", systemImage: "archivebox")
                    }
                    Divider()
                    Button(role: .destructive) { confirmDelete = true } label: { Label("删除项目", systemImage: "trash") }
                } label: {
                    Image(systemName: "ellipsis").font(.system(size: 16, weight: .semibold)).foregroundStyle(p.ink2)
                        .frame(width: 32, height: 28)
                }
                .accessibilityLabel("项目操作")
                .accessibilityIdentifier("project-menu")
            }
        }
    }

    private var addButton: some View {
        Menu {
            Button { router.push(.create(CreateSpec(projectID: id, space: detail?.project.space ?? .unspecified))) } label: { Label("新建…", systemImage: "plus") }
            Button { linking = true } label: { Label("关联已有…", systemImage: "link") }
        } label: {
            Text("+ 添加到这个项目").font(.ui(16, .semibold)).foregroundStyle(.white)
                .frame(maxWidth: .infinity).frame(height: 50)
                .background(accent, in: RoundedRectangle(cornerRadius: p.radiusSm))
        }
        .accessibilityIdentifier("add-to-project")
        .padding(.horizontal, 16).padding(.bottom, 12)
    }

    // MARK: content

    @ViewBuilder private func content(_ d: Noted_V1_ProjectDetail) -> some View {
        let pr = d.project.progress
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                SpaceTag(space: d.project.space)
                if d.project.archived { badge("已归档", p.ink2, p.surface2) }
            }
            Text(d.project.title).font(.ui(30, .bold)).foregroundStyle(p.ink)
            if !d.project.notes.isEmpty {
                Text(d.project.notes).font(.ui(14)).foregroundStyle(p.ink2)
            }
        }
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 6) {
                Eyebrow(text: "进度")
                (Text("\(pr.tasksDone)").foregroundStyle(p.ink) + Text("/\(pr.tasksTotal)").foregroundStyle(p.ink2))
                    .font(.mono(28, .semibold))
                ProgressBar(value: pr.tasksTotal > 0 ? Double(pr.tasksDone) / Double(pr.tasksTotal) : 0, tint: accent)
            }
            Spacer(minLength: 24)
            VStack(alignment: .trailing, spacing: 6) {
                Eyebrow(text: dateLabel(d.project))
                if pr.hasDaysLeft {
                    (Text("\(pr.daysLeft)").foregroundStyle(accent) + Text(" 天").foregroundStyle(p.ink2))
                        .font(.mono(28, .semibold))
                }
                if pr.tasksOverdue > 0 {
                    badge("逾期 \(pr.tasksOverdue)", p.bad, p.badBg)
                } else {
                    badge("没有逾期", p.ok, Color(hex: 0xE1F3EA))
                }
            }
        }
        .card()

        if !d.tasks.isEmpty {
            section("待办", d.tasks.count) { ForEach(Array(d.tasks.enumerated()), id: \.element.id) { i, t in taskRow(t, first: i == 0) } }
        }
        if !d.events.isEmpty {
            section("日程", d.events.count) { ForEach(Array(d.events.enumerated()), id: \.element.id) { i, e in eventRow(e, first: i == 0) } }
        }
        if !d.notes.isEmpty {
            section("笔记", d.notes.count) { ForEach(Array(d.notes.enumerated()), id: \.element.id) { i, n in noteRow(n, first: i == 0) } }
        }
        if !d.goals.isEmpty {
            header("目标", d.goals.count)
            ForEach(d.goals, id: \.id) { g in
                GoalRow(goal: g) { Task { await load() } }
                    .contextMenu { Button { Task { await unlink { r in
                        var u = Noted_V1_UpdateGoalRequest(); u.goal.id = g.id; u.updateMask = fieldMask("project_id")
                        _ = try await r.goals.updateGoal(u) } } } label: { Label("移出项目", systemImage: "minus.circle") }
                        Divider()
                        Button(role: .destructive) { pendingDelete = .goal(g) } label: { Label("删除", systemImage: "trash") }
                    }
            }
        }
        if !d.objectives.isEmpty {
            header("多维目标", d.objectives.count)
            ForEach(d.objectives, id: \.id) { o in
                ObjectiveCard(objective: o) { Task { await load() } }
                    .contextMenu { Button { Task { await unlink { r in
                        var u = Noted_V1_UpdateObjectiveRequest(); u.objective.id = o.id; u.updateMask = fieldMask("project_id")
                        _ = try await r.objectives.updateObjective(u) } } } label: { Label("移出项目", systemImage: "minus.circle") }
                        Divider()
                        Button(role: .destructive) { pendingDelete = .objective(o) } label: { Label("删除", systemImage: "trash") }
                    }
            }
        }
        if d.tasks.isEmpty && d.events.isEmpty && d.notes.isEmpty && d.goals.isEmpty && d.objectives.isEmpty {
            VStack(spacing: 6) {
                Text("项目里还没有内容").font(.ui(14, .medium)).foregroundStyle(p.ink2)
                Text("新建一个,或把已有的待办、日程、笔记、目标关联进来").font(.ui(13)).foregroundStyle(p.ink3)
            }
            .frame(maxWidth: .infinity, minHeight: 90).card()
        }
    }

    private func header(_ title: String, _ count: Int) -> some View {
        HStack {
            Eyebrow(text: title)
            Spacer()
            Text("\(count)").font(.mono(12)).foregroundStyle(p.ink3)
        }
    }

    private func section<C: View>(_ title: String, _ count: Int, @ViewBuilder _ rows: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            header(title, count)
            VStack(spacing: 0, content: rows)
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
    }

    // MARK: rows

    private func rowMenu(delete item: PendingDelete, _ unlinkAction: @escaping () async -> Void) -> some View {
        Menu {
            Button(role: .destructive) { Task { await unlinkAction() } } label: { Label("移出项目", systemImage: "minus.circle") }
            Divider()
            Button(role: .destructive) { pendingDelete = item } label: { Label("删除", systemImage: "trash") }
        } label: {
            Image(systemName: "ellipsis").font(.system(size: 14, weight: .semibold)).foregroundStyle(p.ink3).frame(width: 32, height: 32)
        }
        .accessibilityLabel("更多")
    }

    @ViewBuilder private func taskRow(_ t: Noted_V1_Task, first: Bool) -> some View {
        if !first { Divider().overlay(p.line) }
        HStack(spacing: 12) {
            Button { Task { await toggle(t) } } label: {
                Image(systemName: t.completed ? "checkmark.square.fill" : "square")
                    .font(.ui(20)).foregroundStyle(t.completed ? p.ok : p.ink3)
            }
            .accessibilityLabel(t.completed ? "取消完成 \(t.title)" : "完成 \(t.title)")
            Button { router.push(.task(t)) } label: {
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 5) {
                        Text(t.title).font(.ui(15, t.completed ? .regular : .medium))
                            .strikethrough(t.completed).foregroundStyle(t.completed ? p.ink2 : p.ink)
                        if t.workSessions > 0 && !t.completed {
                            HStack(spacing: 6) {
                                ProgressBar(value: Double(t.progress) / 100, tint: accent).frame(width: 64)
                                Text("\(t.progress)%").font(.mono(11)).foregroundStyle(p.ink3)
                            }
                        }
                    }
                    Spacer(minLength: 0)
                    if t.hasDueTime {
                        Text(Fmt.md(t.dueTime.date)).font(.mono(12)).foregroundStyle(t.completed ? p.ink3 : accent)
                    } else if !t.completed {
                        Text("没有日期").font(.ui(12)).foregroundStyle(p.ink3)
                    }
                }
                .padding(.vertical, 8)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("open-task")
            rowMenu(delete: .task(t)) { await unlink { r in
                var u = Noted_V1_UpdateTaskRequest(); u.task.id = t.id; u.updateMask = fieldMask("project_id")
                _ = try await r.calendar.updateTask(u) } }
        }
        .padding(.leading, 14).padding(.trailing, 4).frame(minHeight: p.rowHeight).opacity(t.completed ? 0.6 : 1)
    }

    @ViewBuilder private func eventRow(_ e: Noted_V1_Event, first: Bool) -> some View {
        if !first { Divider().overlay(p.line) }
        HStack(spacing: 12) {
            Button { router.push(.event(e)) } label: {
                HStack(spacing: 12) {
                    Text(Fmt.md(e.startTime.date)).font(.mono(12, .medium))
                        .foregroundStyle(p.ink2).fixedSize().frame(width: 46, alignment: .leading)
                    Text(e.title).font(.ui(15, .medium)).foregroundStyle(p.ink)
                    Spacer()
                    if !e.allDay { Text(Fmt.hm(e.startTime.date)).font(.mono(12)).foregroundStyle(p.ink2) }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            rowMenu(delete: .event(e)) { await unlink { r in
                var u = Noted_V1_UpdateEventRequest(); u.event.id = e.id; u.updateMask = fieldMask("project_id")
                _ = try await r.calendar.updateEvent(u) } }
        }
        .padding(.leading, 14).padding(.trailing, 4).frame(minHeight: p.rowHeight)
    }

    @ViewBuilder private func noteRow(_ n: Noted_V1_Note, first: Bool) -> some View {
        if !first { Divider().overlay(p.line) }
        HStack(spacing: 10) {
            Button { router.push(.note(n)) } label: {
                HStack(spacing: 10) {
                    Text(n.title.isEmpty ? "无标题" : n.title).font(.ui(15, .medium)).foregroundStyle(p.ink)
                    Text(n.content).font(.ui(13)).foregroundStyle(p.ink2).lineLimit(1)
                    Spacer(minLength: 0)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            rowMenu(delete: .note(n)) { await unlink { r in
                var u = Noted_V1_UpdateNoteRequest(); u.note.id = n.id; u.updateMask = fieldMask("project_id")
                _ = try await r.notes.updateNote(u) } }
        }
        .padding(.leading, 14).padding(.trailing, 4).frame(minHeight: p.rowHeight)
    }

    // MARK: actions

    private func dateLabel(_ pr: Noted_V1_Project) -> String {
        if pr.hasStartTime { return "\(Fmt.md(pr.startTime.date))出发" }
        if pr.hasDueTime { return "\(Fmt.md(pr.dueTime.date))截止" }
        return "没有日期"
    }

    private func badge(_ text: String, _ fg: Color, _ bg: Color) -> some View {
        Text(text).font(.ui(11, .semibold)).padding(.horizontal, 8).frame(height: 22)
            .background(bg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(fg)
    }

    private func load() async {
        guard let api = session.api else { return }
        do {
            var r = Noted_V1_GetProjectRequest(); r.id = id
            detail = try await api.projects.getProject(r)
            error = nil
        } catch { self.error = describe(error) }
    }

    /// Takes one item out of the project; its own data stays.
    private func unlink(_ call: (API) async throws -> Void) async {
        guard let api = session.api else { return }
        do { try await call(api); await load() } catch { self.error = describe(error) }
    }

    private func toggle(_ t: Noted_V1_Task) async {
        var r = Noted_V1_UpdateTaskRequest()
        r.task = t; r.task.completed = !t.completed; r.updateMask = fieldMask("completed")
        _ = try? await session.api?.calendar.updateTask(r)
        await load()
    }

    private func togglePin(_ pr: Noted_V1_Project) async {
        var r = Noted_V1_UpdateProjectRequest()
        r.project = pr; r.project.pinned.toggle(); r.updateMask = fieldMask("pinned")
        _ = try? await session.api?.projects.updateProject(r)
        await load()
    }

    private func setArchived(_ pr: Noted_V1_Project, _ on: Bool) async {
        var r = Noted_V1_UpdateProjectRequest()
        r.project = pr; r.project.archived = on; r.updateMask = fieldMask("archived")
        _ = try? await session.api?.projects.updateProject(r)
        await load()
    }

    private func delete(items: Bool) async {
        var r = Noted_V1_DeleteProjectRequest()
        r.id = id; r.deleteItems = items
        do {
            _ = try await session.api?.projects.deleteProject(r)
            dismiss()
        } catch { self.error = describe(error) }
    }
}

// MARK: - edit

struct EditProjectView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let project: Noted_V1_Project
    @State private var title = ""
    @State private var notes = ""
    @State private var hasStart = false
    @State private var start = Date()
    @State private var hasDue = false
    @State private var due = Date()
    @State private var space: Noted_V1_Space = .life
    @State private var pinned = false
    @State private var error: String?
    @State private var loaded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Eyebrow(text: "编辑项目")
                Spacer()
                Button("取消") { dismiss() }
                    .font(.ui(13, .semibold)).foregroundStyle(session.mode.accent(p))
            }
            .padding(.top, 6)
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    VStack(spacing: 0) {
                        row("标题") { TextField("项目名称", text: $title).font(.ui(16, .semibold)).accessibilityIdentifier("project-title") }
                        Divider().overlay(p.line)
                        row("开始") {
                            if hasStart { DatePicker("", selection: $start, displayedComponents: .date).labelsHidden().environment(\.locale, Fmt.zh) }
                            else { Text("无").foregroundStyle(p.ink3) }
                            Spacer()
                            Toggle("", isOn: $hasStart).labelsHidden().accessibilityLabel("设置开始日期")
                        }
                        Divider().overlay(p.line)
                        row("截止") {
                            if hasDue { DatePicker("", selection: $due, displayedComponents: .date).labelsHidden().environment(\.locale, Fmt.zh) }
                            else { Text("无").foregroundStyle(p.ink3) }
                            Spacer()
                            Toggle("", isOn: $hasDue).labelsHidden().accessibilityLabel("设置截止日期")
                        }
                        Divider().overlay(p.line)
                        row("空间") {
                            SpacePicker(selection: $space)
                            Spacer()
                        }
                        Divider().overlay(p.line)
                        row("置顶") { Spacer(); Toggle("", isOn: $pinned).labelsHidden().accessibilityLabel("置顶") }
                    }
                    .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                    .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))

                    VStack(alignment: .leading, spacing: 6) {
                        Eyebrow(text: "备注")
                        TextEditor(text: $notes).scrollContentBackground(.hidden).frame(minHeight: 90)
                            .font(.ui(15)).padding(8)
                            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                            .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))
                    }
                    ErrorLine(text: error)
                }
            }
            .scrollIndicators(.hidden)
            Button { Task { await save() } } label: {
                Text("保存").font(.ui(16, .semibold)).frame(maxWidth: .infinity).frame(height: 50)
                    .background(session.mode.accent(p).opacity(title.trimmingCharacters(in: .whitespaces).isEmpty ? 0.4 : 1), in: RoundedRectangle(cornerRadius: p.radiusSm))
                    .foregroundStyle(.white)
            }
            .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty)
            .accessibilityIdentifier("project-save")
            .padding(.bottom, 8)
        }
        .padding(.horizontal, 16)
        .background(p.bg.ignoresSafeArea())
        .presentationDragIndicator(.visible)
        .presentationCornerRadius(20)
        .onAppear {
            guard !loaded else { return }
            loaded = true
            title = project.title; notes = project.notes; space = project.space == .work ? .work : .life; pinned = project.pinned
            if project.hasStartTime { hasStart = true; start = project.startTime.date }
            if project.hasDueTime { hasDue = true; due = project.dueTime.date }
        }
    }

    private func row<C: View>(_ label: String, @ViewBuilder _ content: () -> C) -> some View {
        HStack(spacing: 10) {
            Text(label).font(.ui(13, .medium)).foregroundStyle(p.ink2).frame(width: 44, alignment: .leading)
            content()
        }
        .padding(.horizontal, 14).frame(minHeight: p.rowHeight)
    }


    private func save() async {
        var r = Noted_V1_UpdateProjectRequest()
        r.project.id = project.id
        r.project.title = title.trimmingCharacters(in: .whitespaces)
        r.project.notes = notes
        r.project.space = space
        r.project.pinned = pinned
        if hasStart { r.project.startTime = .init(start) }
        if hasDue { r.project.dueTime = .init(due) }
        r.updateMask = fieldMask("title", "notes", "space", "pinned", "start_time", "due_time")
        do {
            _ = try await session.api?.projects.updateProject(r)
            dismiss()
        } catch { self.error = describe(error) }
    }
}

// MARK: - task

/// One to-do on its own page, for work done over several sittings: how far along it is,
/// a form to log today's sitting, and every sitting so far.
struct TaskDetailView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var task: Noted_V1_Task
    @State private var entries: [Noted_V1_TaskWork] = []
    @State private var projectName = ""
    @State private var error: String?
    @State private var confirmDelete = false
    @State private var minutes = 0
    @State private var progress = 0.0
    @State private var progressSet = false
    @State private var note = ""
    @State private var saving = false
    @FocusState private var noteFocused: Bool

    private static let presets = [15, 30, 60, 120]

    init(task: Noted_V1_Task) {
        _task = State(initialValue: task)
        _progress = State(initialValue: Double(task.progress))
    }

    private var accent: Color { task.space.color(p) }
    private var shownPercent: Int { task.completed ? max(Int(task.progress), 100) : Int(task.progress) }
    private var canLog: Bool {
        minutes > 0 || progressSet || !note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                topBar
                ErrorLine(text: error)
                heading
                progressCard
                logCard
                if !entries.isEmpty { history }
            }
            .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 32)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .reloadOnReturn { await load() }
        .confirmationDialog("删除这个待办", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("删除", role: .destructive) { Task { await deleteTask() } }
            Button("取消", role: .cancel) {}
        }
    }

    // MARK: header

    private var topBar: some View {
        HStack(spacing: 10) {
            BackButton(title: "返回")
            Spacer()
            Menu {
                Button { Task { await toggleDone() } } label: {
                    Label(task.completed ? "取消完成" : "完成", systemImage: task.completed ? "arrow.uturn.backward" : "checkmark")
                }
                Divider()
                Button(role: .destructive) { confirmDelete = true } label: { Label("删除", systemImage: "trash") }
            } label: {
                Image(systemName: "ellipsis").font(.system(size: 16, weight: .semibold)).foregroundStyle(p.ink2)
                    .frame(width: 32, height: 28)
            }
            .accessibilityLabel("待办操作")
            .accessibilityIdentifier("task-menu")
        }
    }

    private var heading: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(task.title).font(.ui(28, .bold)).foregroundStyle(task.completed ? p.ink2 : p.ink)
                .strikethrough(task.completed)
            HStack(spacing: 8) {
                SpaceTag(space: task.space)
                if !projectName.isEmpty {
                    Text(projectName).font(.ui(12, .medium)).foregroundStyle(p.ink2).lineLimit(1)
                }
                if task.hasDueTime {
                    Text("\(Fmt.md(task.dueTime.date))截止").font(.mono(12)).foregroundStyle(task.completed ? p.ink3 : accent)
                }
                if task.completed { badge("已完成", p.ok, Color(hex: 0xE1F3EA)) }
            }
            if !task.notes.isEmpty {
                Text(task.notes).font(.ui(14)).foregroundStyle(p.ink2)
            }
        }
    }

    private var progressCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 6) {
                    Eyebrow(text: "进度")
                    (Text("\(shownPercent)").foregroundStyle(p.ink) + Text("%").foregroundStyle(p.ink2))
                        .font(.mono(28, .semibold))
                        .accessibilityIdentifier("task-progress")
                }
                Spacer(minLength: 24)
                VStack(alignment: .trailing, spacing: 6) {
                    Eyebrow(text: "累计")
                    Text(Self.duration(Int(task.workedMinutes))).font(.mono(18, .semibold)).foregroundStyle(accent)
                }
            }
            ProgressBar(value: Double(shownPercent) / 100, tint: task.completed ? p.ok : accent)
            HStack {
                Text(task.workSessions > 0 ? "已做 \(task.workSessions) 次 · 共 \(Self.duration(Int(task.workedMinutes)))" : "还没有记录")
                    .font(.ui(13)).foregroundStyle(p.ink2)
                Spacer()
                if task.hasLastWorkedTime {
                    let d = task.lastWorkedTime.date
                    Text("上次 \(Fmt.relative(d)) \(Fmt.hm(d))").font(.ui(12)).foregroundStyle(p.ink3)
                }
            }
        }
        .card()
    }

    // MARK: log a sitting

    private var logCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            Eyebrow(text: "记一次进度")
            HStack(spacing: 8) {
                ForEach(Self.presets, id: \.self) { m in minutesChip(m) }
            }
            HStack(spacing: 10) {
                Text("用时").font(.ui(13, .medium)).foregroundStyle(p.ink2)
                Text(minutes > 0 ? Self.duration(minutes) : "不计").font(.mono(15, .semibold))
                    .foregroundStyle(minutes > 0 ? p.ink : p.ink3)
                Spacer()
                Stepper("", value: $minutes, in: 0...1440, step: 5).labelsHidden()
                    .accessibilityLabel("用时")
                    .accessibilityIdentifier("task-minutes-stepper")
            }
            Divider().overlay(p.line)
            HStack(spacing: 10) {
                Text("进度").font(.ui(13, .medium)).foregroundStyle(p.ink2)
                if progressSet {
                    Text("\(Int(progress))%").font(.mono(15, .semibold)).foregroundStyle(accent)
                    Spacer()
                    Button { progressSet = false; progress = Double(task.progress) } label: {
                        Text("不更新").font(.ui(12, .semibold)).foregroundStyle(p.ink2)
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("task-progress-reset")
                } else {
                    Text("不更新进度").font(.ui(13)).foregroundStyle(p.ink3)
                    Spacer()
                }
            }
            Slider(value: Binding(get: { progress }, set: { progress = $0; progressSet = true }), in: 0...100, step: 5)
                .tint(accent)
                .accessibilityLabel("进度")
                .accessibilityIdentifier("task-progress-slider")
            TextField("这次做了什么", text: $note, axis: .vertical)
                .lineLimit(1...4).font(.ui(15)).focused($noteFocused)
                .padding(.horizontal, 12).padding(.vertical, 10)
                .background(p.surface2, in: RoundedRectangle(cornerRadius: 10))
                .accessibilityIdentifier("task-log-note")
            Button { Task { await logWork() } } label: {
                Text("记下这次").font(.ui(16, .semibold)).foregroundStyle(.white)
                    .frame(maxWidth: .infinity).frame(height: 46)
                    .background(accent.opacity(canLog && !saving ? 1 : 0.4), in: RoundedRectangle(cornerRadius: p.radiusSm))
            }
            .disabled(!canLog || saving)
            .accessibilityIdentifier("task-log-save")
        }
        .card()
    }

    private func minutesChip(_ m: Int) -> some View {
        let on = minutes == m
        return Button { minutes = on ? 0 : m } label: {
            Text(Self.chipLabel(m)).font(.ui(13, .semibold))
                .foregroundStyle(on ? .white : p.ink)
                .frame(maxWidth: .infinity).frame(height: 32)
                .background(on ? accent : p.surface2, in: RoundedRectangle(cornerRadius: 9))
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? .isSelected : [])
        .accessibilityIdentifier("task-minutes-\(m)")
    }

    // MARK: history

    private var history: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Eyebrow(text: "记录")
                Spacer()
                Text("\(entries.count)").font(.mono(12)).foregroundStyle(p.ink3)
            }
            VStack(spacing: 0) {
                ForEach(Array(entries.enumerated()), id: \.element.id) { i, w in
                    if i > 0 { Divider().overlay(p.line) }
                    workRow(w)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
            .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
    }

    private func workRow(_ w: Noted_V1_TaskWork) -> some View {
        HStack(alignment: .top, spacing: 10) {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    Text("\(Fmt.md(w.time.date)) \(Fmt.hm(w.time.date))").font(.mono(12, .medium)).foregroundStyle(p.ink2)
                    if w.minutes > 0 {
                        Text(Self.duration(Int(w.minutes))).font(.mono(13, .semibold)).foregroundStyle(p.ink)
                    }
                    if w.progress >= 0 {
                        Text("\(w.progress)%").font(.mono(11, .semibold)).padding(.horizontal, 7).frame(height: 20)
                            .background(accent.opacity(0.12), in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(accent)
                    }
                }
                if !w.note.isEmpty {
                    Text(w.note).font(.ui(14)).foregroundStyle(p.ink).fixedSize(horizontal: false, vertical: true)
                }
            }
            .padding(.vertical, 10)
            Spacer(minLength: 0)
            Menu {
                Button(role: .destructive) { Task { await deleteWork(w) } } label: { Label("删除", systemImage: "trash") }
            } label: {
                Image(systemName: "ellipsis").font(.system(size: 14, weight: .semibold)).foregroundStyle(p.ink3).frame(width: 32, height: 32)
            }
            .accessibilityLabel("更多")
            .accessibilityIdentifier("task-work-menu")
            .padding(.top, 4)
        }
        .padding(.leading, 14).padding(.trailing, 4).frame(minHeight: p.rowHeight)
    }

    // MARK: helpers

    private func badge(_ text: String, _ fg: Color, _ bg: Color) -> some View {
        Text(text).font(.ui(11, .semibold)).padding(.horizontal, 8).frame(height: 22)
            .background(bg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(fg)
    }

    static func chipLabel(_ m: Int) -> String { m < 60 ? "\(m)分" : "\(m / 60)小时" }

    static func duration(_ m: Int) -> String {
        if m < 60 { return "\(m) 分" }
        return m % 60 == 0 ? "\(m / 60) 小时" : "\(m / 60) 小时 \(m % 60) 分"
    }

    // MARK: actions

    private func load() async {
        guard let api = session.api else { return }
        do {
            var g = Noted_V1_GetTaskRequest(); g.id = task.id
            let t = try await api.calendar.getTask(g)
            apply(t)
            var l = Noted_V1_ListTaskWorkRequest(); l.taskID = t.id
            entries = try await api.calendar.listTaskWork(l).entries
            if t.projectID.isEmpty {
                projectName = ""
            } else {
                var r = Noted_V1_GetProjectRequest(); r.id = t.projectID
                projectName = (try? await api.projects.getProject(r).project.title) ?? ""
            }
            error = nil
        } catch { if !(error is CancellationError) { self.error = describe(error) } }
    }

    /// Takes the server's copy of the task; the slider follows it unless the user has moved it.
    private func apply(_ t: Noted_V1_Task) {
        task = t
        if !progressSet { progress = Double(t.progress) }
    }

    private func logWork() async {
        guard let api = session.api, canLog else { return }
        saving = true; defer { saving = false }
        var r = Noted_V1_LogTaskWorkRequest()
        r.taskID = task.id
        r.minutes = Int32(minutes)
        if progressSet { r.progress = Int32(progress) }
        r.note = note.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            let t = try await api.calendar.logTaskWork(r)
            minutes = 0; note = ""; progressSet = false; noteFocused = false
            apply(t)
            var l = Noted_V1_ListTaskWorkRequest(); l.taskID = t.id
            entries = try await api.calendar.listTaskWork(l).entries
            error = nil
        } catch { self.error = describe(error) }
    }

    private func deleteWork(_ w: Noted_V1_TaskWork) async {
        guard let api = session.api else { return }
        var r = Noted_V1_DeleteTaskWorkRequest(); r.id = w.id
        do {
            apply(try await api.calendar.deleteTaskWork(r))
            entries.removeAll { $0.id == w.id }
            error = nil
        } catch { self.error = describe(error) }
    }

    private func toggleDone() async {
        guard let api = session.api else { return }
        var r = Noted_V1_UpdateTaskRequest()
        r.task = task; r.task.completed = !task.completed; r.updateMask = fieldMask("completed")
        do { apply(try await api.calendar.updateTask(r)); error = nil } catch { self.error = describe(error) }
    }

    private func deleteTask() async {
        guard let api = session.api else { return }
        var r = Noted_V1_DeleteTaskRequest(); r.id = task.id
        do {
            _ = try await api.calendar.deleteTask(r)
            dismiss()
        } catch { self.error = describe(error) }
    }
}
