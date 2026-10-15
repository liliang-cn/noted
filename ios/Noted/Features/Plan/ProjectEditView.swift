import SwiftUI

/// 编辑项目: title, 开始 / 截止 (optional), 空间, 置顶, 备注. 完成 saves and goes back.
struct ProjectEditView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let projectID: String

    @State private var title = ""
    @State private var notes = ""
    @State private var start: Date?
    @State private var due: Date?
    @State private var space: Noted_V1_Space = .life
    @State private var pinned = false
    @State private var filled = false
    @State private var saving = false

    var body: some View {
        List {
            Section {
                TextField("项目名称", text: $title)
                    .font(.title3.weight(.semibold))
                    .accessibilityIdentifier("project-edit-title")
            }
            Section {
                DateToggleRows(date: $start, title: "开始", icon: "calendar", tile: .red)
                DateToggleRows(date: $due, title: "截止", icon: "flag", tile: .rust)
                if let start, let due, due < start {
                    Text("截止早于开始").font(.footnote).foregroundStyle(.danger)
                }
            }
            Section {
                SpaceSegmentRow(space: $space)
                Toggle(isOn: $pinned) { FieldLabel("置顶", icon: "pin", tile: .orange) }
                    .tint(.switchOn)
            }
            Section {
                NotesField(text: $notes)
            }
        }
        .listStyle(.insetGrouped)
        .navigationTitle("编辑项目")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("完成") { save() }
                    .fontWeight(.semibold)
                    .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty || saving)
                    .accessibilityIdentifier("project-edit-done")
            }
        }
        .onAppear(perform: fill)
    }

    private func fill() {
        guard !filled, let p = data.project(projectID) else { return }
        filled = true
        title = p.title
        notes = p.notes
        start = p.hasStartTime ? p.startTime.date.startOfDay : nil
        due = p.hasDueTime ? p.dueTime.date.startOfDay : nil
        space = p.space == .work ? .work : .life
        pinned = p.pinned
    }

    private func save() {
        guard var p = data.project(projectID) else { router.pop(); return }
        var fields: [String] = []
        let t = title.trimmingCharacters(in: .whitespacesAndNewlines)
        if t != p.title { p.title = t; fields.append("title") }
        if notes != p.notes { p.notes = notes; fields.append("notes") }
        let oldStart = p.hasStartTime ? p.startTime.date.startOfDay : nil
        if start != oldStart {
            if let start { p.startTime = .init(date: start) } else { p.clearStartTime() }
            fields.append("start_time")
        }
        let oldDue = p.hasDueTime ? p.dueTime.date.startOfDay : nil
        if due != oldDue {
            if let due { p.dueTime = .init(date: due) } else { p.clearDueTime() }
            fields.append("due_time")
        }
        if space != p.space { p.space = space; fields.append("space") }
        if pinned != p.pinned { p.pinned = pinned; fields.append("pinned") }
        guard !fields.isEmpty else { router.pop(); return }
        saving = true
        Task {
            defer { saving = false }
            if await data.updateProject(p, fields: fields) != nil {
                if fields.contains("space") { await data.loadProject(projectID) }
                router.pop()
                toaster.show("已保存")
            }
        }
    }
}
