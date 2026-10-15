import SwiftUI

/// 项目: header, progress card, 待办 / 日程 / 笔记 / 目标 sections, "添加" at the bottom, ··· in the bar.
struct ProjectDetailView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let projectID: String

    @State private var askDelete = false
    @State private var pendingDelete: PendingItem?
    @State private var loaded = false

    /// An item waiting for its delete confirmation.
    struct PendingItem: Identifiable {
        let ref: ItemRef
        let kind: String
        let title: String
        var id: ItemRef { ref }
    }

    var body: some View {
        Group {
            if let p = data.project(projectID) {
                content(p)
            } else if loaded {
                List { Section { EmptyState(text: "项目已删除") } }
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle(data.project(projectID)?.title ?? "项目")
        .navigationBarTitleDisplayMode(.inline)
        .task { await reload() }
        .reloadOnReturn { await reload() }
    }

    private func reload() async {
        await data.loadProject(projectID)
        loaded = true
    }

    // MARK: content

    private func content(_ p: Noted_V1_Project) -> some View {
        let d = data.projectDetails[projectID]
        let tasks = d?.tasks ?? []
        let events = d?.events ?? []
        let notes = d?.notes ?? []
        let goals = d?.goals ?? []
        let objectives = d?.objectives ?? []
        let itemCount = tasks.count + events.count + notes.count + goals.count + objectives.count
        return List {
            Section {
                PlanDetailHeader(title: p.title, subtitle: headerLine(p)) {
                    SpaceTag(space: p.space)
                    if p.pinned { InfoTag(text: "置顶", systemImage: "pin") }
                    if p.archived { InfoTag(text: "已归档", systemImage: "archivebox") }
                }
            }
            Section { progressCard(p).planCardRow(bottom: 0) }

            Section {
                if tasks.isEmpty { EmptyState(text: "没有待办") }
                ForEach(tasks, id: \.id) { t in
                    ProjectTaskRow(task: t, menu: itemMenu(.task(t.id), kind: "待办", title: t.title))
                        .swipeActions(edge: .leading, allowsFullSwipe: true) {
                            if !t.completed {
                                Button {
                                    Task { if await data.setCompleted(t.id, true) { toaster.show("已完成") } }
                                } label: { Label("完成", systemImage: "checkmark") }
                                .tint(.success)
                            }
                        }
                        .swipeActions(edge: .trailing) { swipeButtons(.task(t.id), kind: "待办", title: t.title) }
                }
            } header: { Text("待办 · \(tasks.count)") }

            if !events.isEmpty {
                Section {
                    ForEach(events, id: \.id) { e in
                        ProjectItemRow(icon: "calendar", iconTint: e.space.tint, title: e.title, subtitle: eventLine(e),
                                       menu: itemMenu(.event(e.id), kind: "日程", title: e.title)) {
                            router.push(.event(e.id))
                        }
                        .swipeActions(edge: .trailing) { swipeButtons(.event(e.id), kind: "日程", title: e.title) }
                    }
                } header: { Text("日程 · \(events.count)") }
            }

            if !notes.isEmpty {
                Section {
                    ForEach(notes, id: \.id) { n in
                        let title = n.title.isEmpty ? "无标题" : n.title
                        ProjectItemRow(icon: "doc.text", iconTint: .warning, title: title, subtitle: PlanFmt.preview(n.content),
                                       menu: itemMenu(.note(n.id), kind: "笔记", title: title)) {
                            router.push(.note(n.id))
                        }
                        .swipeActions(edge: .trailing) { swipeButtons(.note(n.id), kind: "笔记", title: title) }
                    }
                } header: { Text("笔记 · \(notes.count)") }
            }

            if !goals.isEmpty || !objectives.isEmpty {
                Section {
                    ForEach(goals, id: \.id) { g in
                        ProjectItemRow(ring: g.fraction, ringDone: g.isAchieved, title: g.title,
                                       subtitle: "\(Fmt.number(g.done))/\(Fmt.number(g.goalTarget)) \(g.period.current)",
                                       menu: itemMenu(.goal(g.id), kind: "目标", title: g.title)) {
                            router.push(.goal(g.id))
                        }
                        .swipeActions(edge: .trailing) { swipeButtons(.goal(g.id), kind: "目标", title: g.title) }
                    }
                    ForEach(objectives, id: \.id) { o in
                        ProjectItemRow(ring: o.fraction, ringDone: o.fraction >= 1, title: o.title,
                                       subtitle: "\(o.dimsDone)/\(o.goals.count) 个维度达成",
                                       menu: itemMenu(.objective(o.id), kind: "目标", title: o.title)) {
                            router.push(.objective(o.id))
                        }
                        .swipeActions(edge: .trailing) { swipeButtons(.objective(o.id), kind: "目标", title: o.title) }
                    }
                } header: { Text("目标 · \(goals.count + objectives.count)") }
            }
        }
        .listStyle(.insetGrouped)
        .refreshable { await reload() }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            BottomBar { addMenu }
        }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { moreMenu(p) }
        }
        .confirmationDialog("删除“\(p.title)”？", isPresented: $askDelete, titleVisibility: .visible) {
            if itemCount > 0 {
                Button("只删项目，保留内容", role: .destructive) { deleteProject(items: false) }
                Button("连同内容一起删除", role: .destructive) { deleteProject(items: true) }
            } else {
                Button("删除项目", role: .destructive) { deleteProject(items: false) }
            }
            Button("取消", role: .cancel) {}
        } message: {
            if itemCount > 0 { Text("项目里有 \(itemCount) 项内容") }
        }
        .confirmationDialog("删除\(pendingDelete?.kind ?? "")？", isPresented: Binding(get: { pendingDelete != nil }, set: { if !$0 { pendingDelete = nil } }),
                            titleVisibility: .visible, presenting: pendingDelete) { item in
            Button("删除\(item.kind)", role: .destructive) { deleteItem(item.ref) }
            Button("取消", role: .cancel) {}
        } message: { item in
            Text("“\(item.title)”")
        }
    }

    // MARK: header and progress

    private func headerLine(_ p: Noted_V1_Project) -> String {
        var parts: [String] = []
        switch (p.hasStartTime, p.hasDueTime) {
        case (true, true): parts.append("\(Fmt.md(p.startTime.date)) – \(Fmt.md(p.dueTime.date))")
        case (true, false): parts.append("\(Fmt.md(p.startTime.date)) 开始")
        case (false, true): parts.append("截止 \(Fmt.md(p.dueTime.date))")
        default: break
        }
        let notes = p.notes.trimmingCharacters(in: .whitespacesAndNewlines)
        if !notes.isEmpty { parts.append(notes) }
        return parts.joined(separator: " · ")
    }

    private func progressCard(_ p: Noted_V1_Project) -> some View {
        let pr = p.progress
        return VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top) {
                HStack(alignment: .firstTextBaseline, spacing: 0) {
                    Text("\(pr.tasksDone)").font(.system(size: 44, weight: .bold, design: .monospaced))
                    Text("/\(pr.tasksTotal) 待办").font(.system(size: 22, weight: .medium)).foregroundStyle(.secondaryText)
                }
                .contentTransition(.numericText())
                Spacer(minLength: 8)
                VStack(alignment: .trailing, spacing: 2) {
                    if let n = p.daysLeft() {
                        if n >= 0 {
                            HStack(alignment: .firstTextBaseline, spacing: 4) {
                                Text("还剩")
                                Text("\(n)").font(.system(size: 22, weight: .bold)).foregroundStyle(.primary).monospacedDigit()
                                Text("天")
                            }
                        } else {
                            Text("已过截止日").foregroundStyle(.danger)
                        }
                    }
                    if p.hasDueTime { Text("截止 \(Fmt.mdw(p.dueTime.date))") }
                }
                .font(.footnote)
                .foregroundStyle(.secondaryText)
            }
            ProgressBar(value: p.fraction, tint: p.space.tint, height: ProgressBar.large)
                .padding(.top, 10)
            if !pr.nextTitle.isEmpty {
                HStack {
                    Text("下一步").foregroundStyle(.secondaryText)
                    Spacer(minLength: 12)
                    Text(pr.hasNextTime ? "\(pr.nextTitle) · \(Fmt.day(pr.nextTime.date))" : pr.nextTitle).lineLimit(1)
                }
                .font(.subheadline)
                .padding(.top, 14)
            }
        }
        .card()
        .accessibilityElement(children: .combine)
    }

    // MARK: menus

    /// A new note filed in this project: the note page starts from the seed and creates it once it has content.
    private func newNote() {
        var n = Noted_V1_Note()
        n.projectID = projectID
        if let p = data.project(projectID) { n.space = p.space }
        NoteDrafts.seed(n)
        router.push(.note(""))
    }

    private var addMenu: some View {
        Menu {
            Button { router.push(.create(CreateSpec(kind: .todo, projectID: projectID))) } label: { Label("新建待办", systemImage: "checklist") }
            Button { router.push(.create(CreateSpec(kind: .event, projectID: projectID))) } label: { Label("新建日程", systemImage: "calendar") }
            Button(action: newNote) { Label("新建笔记", systemImage: "doc.text") }
            Button { router.push(.create(CreateSpec(kind: .goal, projectID: projectID))) } label: { Label("新建目标", systemImage: "target") }
            Divider()
            Button { router.push(.linkExisting(projectID: projectID)) } label: { Label("关联已有…", systemImage: "link") }
        } label: {
            Label("添加", systemImage: "plus")
                .font(.body.weight(.semibold))
                .frame(maxWidth: .infinity)
                .frame(height: 50)
                .foregroundStyle(.white)
                .background(RoundedRectangle(cornerRadius: 14).fill(.tint))
                .contentShape(RoundedRectangle(cornerRadius: 14))
        }
        .accessibilityLabel("添加")
        .accessibilityIdentifier("project-add")
    }

    private func moreMenu(_ p: Noted_V1_Project) -> some View {
        Menu {
            Button { router.push(.projectEdit(p.id)) } label: { Label("编辑", systemImage: "pencil") }
            Button {
                Task { if await data.setPinned(project: p.id, !p.pinned) { toaster.show(p.pinned ? "已取消置顶" : "已置顶", systemImage: "pin") } }
            } label: { Label(p.pinned ? "取消置顶" : "置顶", systemImage: p.pinned ? "pin.slash" : "pin") }
            Button {
                let archiving = !p.archived
                Task {
                    if await data.setArchived(project: p.id, archiving) {
                        toaster.show(archiving ? "已归档" : "已取消归档", systemImage: "archivebox")
                        if archiving { router.pop() }
                    }
                }
            } label: { Label(p.archived ? "取消归档" : "归档", systemImage: "archivebox") }
            Divider()
            Button(role: .destructive) { askDelete = true } label: { Label("删除项目", systemImage: "trash") }
        } label: {
            Image(systemName: "ellipsis.circle")
        }
        .accessibilityLabel("更多")
        .accessibilityIdentifier("project-more")
    }

    /// The ··· on a row: 移出项目 / 删除.
    private func itemMenu(_ ref: ItemRef, kind: String, title: String) -> ProjectItemMenu {
        ProjectItemMenu(remove: { removeFromProject(ref) },
                 delete: { pendingDelete = PendingItem(ref: ref, kind: kind, title: title) },
                 kind: kind)
    }

    @ViewBuilder
    private func swipeButtons(_ ref: ItemRef, kind: String, title: String) -> some View {
        Button { pendingDelete = PendingItem(ref: ref, kind: kind, title: title) } label: { Label("删除", systemImage: "trash") }
            .tint(.danger)
        Button { removeFromProject(ref) } label: { Label("移出", systemImage: "folder.badge.minus") }
            .tint(.warning)
    }

    // MARK: actions

    private func removeFromProject(_ ref: ItemRef) {
        Task { if await data.setProject([ref], projectID: "") > 0 { toaster.show("已移出项目", systemImage: "folder") } }
    }

    private func deleteItem(_ ref: ItemRef) {
        Task {
            let ok: Bool
            switch ref {
            case .task(let id): ok = await data.deleteTask(id)
            case .event(let id): ok = await data.deleteEvent(id)
            case .note(let id): ok = await data.deleteNote(id)
            case .goal(let id): ok = await data.deleteGoal(id)
            case .objective(let id): ok = await data.deleteObjective(id)
            }
            if ok {
                toaster.show("已删除", systemImage: "trash")
                await data.loadProject(projectID)
            }
        }
    }

    private func deleteProject(items: Bool) {
        Task {
            if await data.deleteProject(projectID, deleteItems: items) {
                router.pop()
                toaster.show(items ? "已删除项目及内容" : "已删除项目", systemImage: "trash")
            }
        }
    }

    private func eventLine(_ e: Noted_V1_Event) -> String {
        var s = Fmt.mdw(e.start) + (e.allDay ? " 全天" : " \(Fmt.hm(e.start))")
        if !e.location.isEmpty { s += " · \(e.location)" }
        return s
    }
}

/// What the ··· on a project row does.
private struct ProjectItemMenu {
    let remove: () -> Void
    let delete: () -> Void
    let kind: String
}

/// The ··· button at the end of a project row.
private struct ProjectItemMenuButton: View {
    let menu: ProjectItemMenu
    var body: some View {
        Menu {
            Button(action: menu.remove) { Label("移出项目", systemImage: "folder.badge.minus") }
            Divider()
            Button(role: .destructive, action: menu.delete) { Label("删除\(menu.kind)", systemImage: "trash") }
        } label: {
            Image(systemName: "ellipsis")
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(.tertiaryText)
                .frame(width: 36, height: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.borderless)
        .accessibilityLabel("更多")
    }
}

/// A to-do in a project: check, title with priority marks, due day (red when overdue) and progress, ···.
private struct ProjectTaskRow: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let task: Noted_V1_Task
    let menu: ProjectItemMenu

    var body: some View {
        HStack(spacing: 10) {
            CheckButton(isOn: task.completed) {
                let done = !task.completed
                Task { if await data.setCompleted(task.id, done), done { toaster.show("已完成") } }
            }
            .buttonStyle(.borderless)
            .padding(.horizontal, -10)
            Button { router.push(.todo(task.id)) } label: {
                VStack(alignment: .leading, spacing: 1) {
                    (Text(task.priorityMarks.isEmpty ? "" : task.priorityMarks + " ").foregroundColor(.danger).bold()
                        + Text(task.title).foregroundColor(task.completed ? .secondaryText : .primary))
                        .strikethrough(task.completed, color: .tertiaryText)
                        .lineLimit(1)
                    if let sub = subtitle { sub.font(.footnote).foregroundStyle(.secondaryText).lineLimit(1) }
                }
                .frame(maxWidth: .infinity, minHeight: 40, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            ProjectItemMenuButton(menu: menu)
        }
        .padding(.trailing, -8)
        .accessibilityIdentifier("project-todo-\(task.id)")
    }

    private var subtitle: Text? {
        var parts: [Text] = []
        if let d = task.due {
            let label = task.hasClockTime ? Fmt.dayTime(d) : Fmt.day(d)
            parts.append(task.isOverdue() ? Text(label).foregroundColor(.danger) : Text(label))
        }
        if !task.completed && task.progress > 0 { parts.append(Text("\(task.progress)%").monospacedDigit()) }
        guard !parts.isEmpty else { return nil }
        return parts.dropFirst().reduce(parts[0]) { $0 + Text(" · ") + $1 }
    }
}

/// An event, note or goal in a project: icon (or ring), title, gray line, ···.
private struct ProjectItemRow: View {
    var icon: String?
    var iconTint: Color = .secondaryText
    var ring: Double?
    var ringDone = false
    let title: String
    let subtitle: String
    let menu: ProjectItemMenu
    let open: () -> Void

    init(icon: String, iconTint: Color, title: String, subtitle: String, menu: ProjectItemMenu, open: @escaping () -> Void) {
        self.icon = icon; self.iconTint = iconTint; self.title = title; self.subtitle = subtitle; self.menu = menu; self.open = open
    }

    init(ring: Double, ringDone: Bool, title: String, subtitle: String, menu: ProjectItemMenu, open: @escaping () -> Void) {
        self.ring = ring; self.ringDone = ringDone; self.title = title; self.subtitle = subtitle; self.menu = menu; self.open = open
    }

    var body: some View {
        HStack(spacing: 12) {
            Button(action: open) {
                HStack(spacing: 12) {
                    Group {
                        if let ring {
                            ProgressRing(value: ring, lineWidth: 3.5, tint: ringDone ? .success : nil)
                        } else if let icon {
                            Image(systemName: icon).font(.system(size: 18)).foregroundStyle(iconTint)
                        }
                    }
                    .frame(width: 24, height: 24)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(title).lineLimit(1)
                        if !subtitle.isEmpty {
                            Text(subtitle).font(.footnote).foregroundStyle(.secondaryText).lineLimit(1)
                        }
                    }
                    .frame(maxWidth: .infinity, minHeight: 40, alignment: .leading)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            ProjectItemMenuButton(menu: menu)
        }
        .padding(.trailing, -8)
    }
}
