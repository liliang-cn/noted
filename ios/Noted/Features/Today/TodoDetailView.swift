import SwiftUI
import SwiftProtobuf

/// 待办 (route .todo): title and check, space + date line, the 进度 card with the inline 记一次 form,
/// the sittings, and the to-do's fields, each saved as soon as it changes.
struct TodoDetailView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let id: String

    @State private var loading = true
    @State private var confirmDelete = false
    @State private var title = ""
    @State private var notes = ""
    @FocusState private var focus: Field?

    private enum Field { case title, notes }

    var body: some View {
        Group {
            if let task = data.task(id) {
                content(task)
            } else if loading {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity).background(.groupedBackground)
            } else {
                List { Section { EmptyState(text: "这个待办已删除") } }
            }
        }
        .navigationTitle("待办")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let task = data.task(id) {
                ToolbarItem(placement: .topBarTrailing) { moreMenu(task) }
            }
        }
        .confirmationDialog("删除待办？", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("删除待办", role: .destructive) { delete() }
                .accessibilityIdentifier("todo-delete-confirm")
        } message: {
            Text("“\(data.task(id)?.title ?? "")”")
        }
        .task(id: id) {
            if let t = data.task(id) { title = t.title; notes = t.notes }
            async let t: Noted_V1_Task? = data.loadTask(id)
            async let w: Void = data.loadTaskWork(id)
            if let t = await t, focus == nil { title = t.title; notes = t.notes }
            _ = await w
            loading = false
        }
        .onChange(of: focus) { old, _ in
            if old == .title { saveTitle() }
            if old == .notes { saveNotes() }
        }
        .onDisappear { saveTitle(); saveNotes() }
    }

    // MARK: page

    private func content(_ task: Noted_V1_Task) -> some View {
        List {
            header(task)
            ProgressSection(task: task)
            history(task)
            fields(task)
        }
        .listStyle(.insetGrouped)
        .listSectionSpacing(20)
        .scrollDismissesKeyboard(.interactively)
    }

    private func header(_ task: Noted_V1_Task) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 10) {
                HStack(alignment: .top, spacing: 8) {
                    CheckButton(isOn: task.completed, size: 28) { toggleDone(task) }
                        .padding(.leading, -8).padding(.vertical, -6)
                        .accessibilityIdentifier("todo-complete")
                    TextField("标题", text: $title, axis: .vertical)
                        .font(.system(size: 28, weight: .bold))
                        .foregroundStyle(task.completed ? .secondaryText : .primary)
                        .strikethrough(task.completed, color: .tertiaryText)
                        .focused($focus, equals: .title)
                        .submitLabel(.done)
                        .onChange(of: title) { _, new in
                            // Return ends editing (the field is multi-line only to wrap long titles).
                            if new.contains("\n") { title = new.replacingOccurrences(of: "\n", with: ""); focus = nil }
                        }
                        .accessibilityLabel("标题")
                        .accessibilityIdentifier("todo-title")
                }
                HStack(spacing: 8) {
                    SpaceTag(space: task.space)
                    if let p = data.project(task.projectID) {
                        Button { router.push(.project(p.id)) } label: {
                            HStack(spacing: 4) {
                                Image(systemName: "folder").font(.system(size: 12))
                                Text(p.title).lineLimit(1)
                                Image(systemName: "chevron.right").font(.system(size: 10, weight: .semibold))
                            }
                        }
                        .buttonStyle(PillButtonStyle(height: 24))
                        .accessibilityLabel("项目 \(p.title)")
                    }
                    if let d = task.due {
                        Text(Fmt.dayTime(d, hasTime: task.hasClockTime))
                            .font(.subheadline)
                            .foregroundStyle(task.isOverdue() ? AnyShapeStyle(Color.danger) : AnyShapeStyle(.secondaryText))
                            .lineLimit(1)
                    }
                }
            }
            .listRowBackground(Color.clear)
            .listRowInsets(EdgeInsets(top: 0, leading: 4, bottom: 0, trailing: 4))
        }
    }

    // MARK: 记录

    @ViewBuilder
    private func history(_ task: Noted_V1_Task) -> some View {
        let entries = data.taskWork[task.id] ?? []
        if !entries.isEmpty {
            Section {
                ForEach(entries, id: \.id) { w in
                    HStack(alignment: .top, spacing: 12) {
                        VStack(alignment: .leading, spacing: 1) {
                            Text(Fmt.mdw(w.time.date))
                            if !w.note.isEmpty { Text(w.note).font(.footnote).foregroundStyle(.secondaryText) }
                        }
                        Spacer(minLength: 8)
                        VStack(alignment: .trailing, spacing: 1) {
                            Text("\(w.minutes) 分").font(.system(.body, design: .monospaced))
                            if w.progress >= 0 {
                                Text("→ \(w.progress)%").font(.system(.caption, design: .monospaced)).foregroundStyle(.secondaryText)
                            }
                        }
                    }
                    .padding(.vertical, 2)
                    .accessibilityElement(children: .combine)
                    .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                        Button {
                            Task { if await data.deleteTaskWork(w.id, taskID: task.id) { toaster.show("已删除", systemImage: "trash") } }
                        } label: { Label("删除", systemImage: "trash") }
                        .tint(.danger)
                    }
                }
            } header: {
                Text("记录")
            }
        }
    }

    // MARK: fields

    @ViewBuilder
    private func fields(_ task: Noted_V1_Task) -> some View {
        Section {
            DateToggleRows(date: Binding(get: { task.due }, set: { setDue($0) }))
            if task.due != nil {
                TimeToggleRows(date: Binding(get: { task.due ?? Date.now.startOfDay }, set: { setDue($0) }))
                ReminderPicker(lead: Binding(get: { task.reminderLead }, set: { lead in
                    save(["remind_time"]) { $0.setReminder(lead) }
                }))
            }
            // Repeating needs a date to count from.
            RepeatPicker(rule: Binding(get: { task.repeatRule }, set: { rule in
                save(["rrule"]) { $0.rrule = EventDetailView.merge(rrule: $0.rrule, with: rule) }
            }), defaultWeekday: Fmt.calendar.component(.weekday, from: task.due ?? .now))
            .disabled(task.due == nil)
        }
        Section {
            ProjectPicker(projectID: Binding(get: { task.projectID }, set: { setProject($0) }))
            SpaceSegmentRow(space: Binding(get: { task.space }, set: { s in
                save(["space"]) { $0.space = s }
            }))
            PriorityPicker(priority: Binding(get: { task.priority }, set: { p in
                save(["priority"]) { $0.priority = p }
            }))
        }
        Section {
            NotesField(text: $notes)
                .focused($focus, equals: .notes)
                .accessibilityIdentifier("todo-notes")
        }
    }

    private func moreMenu(_ task: Noted_V1_Task) -> some View {
        Menu {
            Button { toggleDone(task) } label: {
                Label(task.completed ? "标为未完成" : "完成", systemImage: task.completed ? "arrow.uturn.backward" : "checkmark")
            }
            Button {
                Task { if let d = await data.postpone(task.id) { toaster.show("已推迟到\(Fmt.day(d))", systemImage: "clock") } }
            } label: {
                Label("推迟到明天", systemImage: "clock")
            }
            Divider()
            Button(role: .destructive) { confirmDelete = true } label: {
                Label("删除", systemImage: "trash")
            }
            .accessibilityIdentifier("todo-delete")
        } label: {
            Image(systemName: "ellipsis.circle")
        }
        .accessibilityLabel("更多")
        .accessibilityIdentifier("todo-more")
    }

    // MARK: saving

    /// Applies `change` to the live copy and saves the listed fields.
    private func save(_ fields: [String], _ change: (inout Noted_V1_Task) -> Void) {
        guard var t = data.task(id) else { return }
        change(&t)
        Task { await data.updateTask(t, fields: fields) }
    }

    /// A new due date (nil: none). The reminder keeps its lead before the new time; without a date it goes.
    private func setDue(_ date: Date?) {
        guard let cur = data.task(id) else { return }
        if cur.due == date { return }
        let lead = cur.reminderLead
        // Without a date it cannot repeat either.
        save(date == nil && cur.repeats ? ["due_time", "remind_time", "rrule"] : ["due_time", "remind_time"]) { t in
            if let date {
                t.dueTime = Google_Protobuf_Timestamp(date: date)
                t.setReminder(lead)
            } else {
                t.clearDueTime()
                t.clearRemindTime()
                t.rrule = ""
            }
        }
    }

    /// Moving into a project also moves the to-do into the project's space.
    private func setProject(_ projectID: String) {
        let space = data.project(projectID)?.space
        save(space != nil ? ["project_id", "space"] : ["project_id"]) { t in
            t.projectID = projectID
            if let space { t.space = space }
        }
    }

    private func saveTitle() {
        let v = title.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let t = data.task(id) else { return }
        if v.isEmpty { title = t.title; return }
        guard v != t.title else { return }
        save(["title"]) { $0.title = v }
    }

    private func saveNotes() {
        guard let t = data.task(id), notes != t.notes else { return }
        let v = notes
        save(["notes"]) { $0.notes = v }
    }

    private func toggleDone(_ task: Noted_V1_Task) {
        let done = !task.completed
        Task { if await data.setCompleted(task.id, done), done { toaster.show("已完成") } }
    }

    private func delete() {
        Task {
            if await data.deleteTask(id) {
                router.pop()
                toaster.show("已删除", systemImage: "trash")
            }
        }
    }
}

// MARK: - 进度 card

/// The big percentage, the bar, "已做 N 次 · 共 X", and 记一次 (opens the form inline).
private struct ProgressSection: View {
    @Environment(AppData.self) private var data
    @Environment(Toaster.self) private var toaster
    let task: Noted_V1_Task

    @State private var logging = false
    @State private var minutes = 30
    @State private var custom = false
    @State private var customText = ""
    @State private var percent = 0.0
    @State private var keepProgress = false
    @State private var note = ""
    @State private var saving = false
    @FocusState private var customFocused: Bool

    private static let presets = [15, 30, 60, 120]

    var body: some View {
        Section {
            VStack(alignment: .leading, spacing: 0) {
                top
                ProgressBar(value: Double(task.progress) / 100, tint: task.progress >= 100 ? .success : nil, height: ProgressBar.large)
                    .padding(.top, 12)
                if logging {
                    form.transition(.opacity.combined(with: .move(edge: .top)))
                } else {
                    Button {
                        open()
                    } label: {
                        Label("记一次", systemImage: "plus")
                    }
                    .buttonStyle(.wide(.tinted, small: true))
                    .padding(.top, 14)
                    .accessibilityIdentifier("todo-log-open")
                }
            }
            .padding(.vertical, 8)
        }
    }

    private var top: some View {
        HStack(alignment: .top) {
            HStack(alignment: .firstTextBaseline, spacing: 2) {
                Text("\(task.progress)")
                    .font(.system(size: 48, weight: .bold, design: .monospaced))
                    .contentTransition(.numericText(value: Double(task.progress)))
                    .animation(.snappy, value: task.progress)
                Text("%").font(.system(size: 17, weight: .semibold, design: .monospaced)).foregroundStyle(.secondaryText)
            }
            .accessibilityElement(children: .combine)
            .accessibilityLabel("进度 \(task.progress)%")
            Spacer()
            Group {
                if task.workSessions > 0 {
                    Text("已做 \(task.workSessions) 次\n共 \(Fmt.duration(minutes: Int(task.workedMinutes)))")
                } else {
                    Text("进度")
                }
            }
            .font(.footnote).foregroundStyle(.secondaryText)
            .multilineTextAlignment(.trailing)
            .padding(.top, 8)
        }
    }

    private var form: some View {
        VStack(alignment: .leading, spacing: 0) {
            Divider().padding(.top, 14)
            Text("这次用时").font(.footnote).foregroundStyle(.secondaryText).padding(.top, 12).padding(.bottom, 8)
            HStack(spacing: 6) {
                ForEach(Self.presets, id: \.self) { m in
                    minuteChip(m < 60 ? "\(m) 分" : "\(m / 60) 小时", on: !custom && minutes == m) {
                        custom = false; customFocused = false; minutes = m
                    }
                }
                minuteChip("自定义", on: custom) {
                    custom = true
                    if customText.isEmpty { customText = String(minutes) }
                    customFocused = true
                }
            }
            if custom {
                HStack {
                    TextField("分钟", text: $customText)
                        .keyboardType(.numberPad)
                        .focused($customFocused)
                        .monospacedDigit()
                        .accessibilityLabel("自定义分钟")
                    Text("分钟").foregroundStyle(.secondaryText)
                }
                .padding(.horizontal, 12).frame(height: 40)
                .background(.fillQuiet, in: RoundedRectangle(cornerRadius: 10))
                .padding(.top, 8)
            }
            HStack {
                Text("完成到").font(.footnote).foregroundStyle(.secondaryText)
                Spacer()
                Text(keepProgress ? "—" : "\(Int(percent))%")
                    .font(.system(.subheadline, design: .monospaced).weight(.bold))
                    .contentTransition(.numericText())
            }
            .padding(.top, 14)
            Slider(value: $percent, in: 0...100, step: 5)
                .disabled(keepProgress)
                .opacity(keepProgress ? 0.4 : 1)
                .accessibilityLabel("完成到")
                .accessibilityValue("\(Int(percent))%")
            Toggle("不更新进度", isOn: $keepProgress)
                .font(.subheadline)
                .tint(.switchOn)
                .padding(.top, 2)
            TextField("做了什么", text: $note)
                .padding(.horizontal, 12).frame(height: 44)
                .background(.fillQuiet, in: RoundedRectangle(cornerRadius: 10))
                .padding(.top, 10)
                .accessibilityIdentifier("todo-log-note")
            HStack(spacing: 10) {
                Button("取消") { withAnimation(.snappy) { logging = false } }
                    .buttonStyle(.wide(.gray, small: true))
                Button("记录") { save() }
                    .buttonStyle(.wide(.filled, small: true))
                    .disabled(saving || chosenMinutes == nil)
                    .accessibilityIdentifier("todo-log-save")
            }
            .padding(.top, 12)
        }
    }

    private func minuteChip(_ title: String, on: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .font(.subheadline)
                .lineLimit(1).minimumScaleFactor(0.8)
                .frame(maxWidth: .infinity).frame(height: 34)
                .foregroundStyle(on ? AnyShapeStyle(.white) : AnyShapeStyle(.primary))
                .background(Capsule().fill(on ? AnyShapeStyle(.tint) : AnyShapeStyle(.fillTrack)))
                .contentShape(Capsule())
        }
        .buttonStyle(.borderless)
        .accessibilityAddTraits(on ? .isSelected : [])
    }

    private var chosenMinutes: Int? {
        guard custom else { return minutes }
        guard let m = Int(customText.trimmingCharacters(in: .whitespaces)), m > 0, m <= 1440 else { return nil }
        return m
    }

    private func open() {
        minutes = 30; custom = false; customText = ""
        percent = Double(min(100, Int(task.progress) + 20))
        keepProgress = false
        note = ""
        withAnimation(.snappy) { logging = true }
    }

    private func save() {
        guard let m = chosenMinutes else { return }
        saving = true
        let pct = keepProgress ? nil : Int(percent)
        let text = note.trimmingCharacters(in: .whitespacesAndNewlines)
        Task {
            defer { saving = false }
            guard let t = await data.logTaskWork(task.id, minutes: m, progress: pct, note: text) else { return }
            withAnimation(.snappy) { logging = false }
            customFocused = false
            toaster.show(t.completed && !task.completed ? "已完成 · 记录 \(Fmt.duration(minutes: m))" : "已记录 \(Fmt.duration(minutes: m))")
        }
    }
}
