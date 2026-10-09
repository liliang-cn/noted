import SwiftUI

struct CaptureView: View {
    enum Kind: String, CaseIterable, Identifiable {
        case note = "笔记", task = "待办", event = "日程"
        var id: String { rawValue }
    }

    var projectID: String = ""
    var initialSpace: Noted_V1_Space? = nil
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var kind: Kind = .task
    @State private var title = ""
    @State private var content = ""
    @State private var when = CaptureView.defaultTime()
    @State private var hasWhen = false
    @State private var remindMinutes = -1   // events: -1 none, 0 at start, else minutes before
    @State private var remindTask = false
    @State private var space: Noted_V1_Space = .life
    @State private var error: String?
    @State private var saving = false

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Button("取消") { dismiss() }
                Spacer()
                Button(saving ? "保存中…" : "保存") { Task { await save() } }
                    .fontWeight(.semibold).disabled(title.trimmingCharacters(in: .whitespaces).isEmpty || saving)
            }
            Picker("", selection: $kind) { ForEach(Kind.allCases) { Text($0.rawValue).tag($0) } }.pickerStyle(.segmented)
            TextField(kind == .note ? "标题" : "要做什么", text: $title).font(.system(size: 22, weight: .bold))
            if kind == .note {
                TextEditor(text: $content).scrollContentBackground(.hidden).frame(minHeight: 140)
                    .padding(8).background(p.surface, in: RoundedRectangle(cornerRadius: 10)).overlay(RoundedRectangle(cornerRadius: 10).stroke(p.line))
            } else {
                if kind == .task { toggleRow("设置截止时间", $hasWhen) }
                if kind == .event || hasWhen { DatePicker("时间", selection: $when).environment(\.locale, Fmt.zh) }
                if kind == .event {
                    Picker("提醒", selection: $remindMinutes) {
                        Text("不提醒").tag(-1)
                        Text("开始时").tag(0)
                        Text("提前 5 分钟").tag(5)
                        Text("提前 10 分钟").tag(10)
                        Text("提前 30 分钟").tag(30)
                        Text("提前 1 小时").tag(60)
                    }.accessibilityIdentifier("remind-picker")
                }
                if kind == .task && hasWhen { toggleRow("到期时提醒", $remindTask) }
            }
            Picker("", selection: $space) {
                Text("生活").tag(Noted_V1_Space.life)
                Text("工作").tag(Noted_V1_Space.work)
            }.pickerStyle(.segmented)
            ErrorLine(text: error)
            Spacer()
        }
        .padding(16)
        .background(p.bg.ignoresSafeArea())
        .onAppear { if let s = initialSpace, s != .unspecified { space = s } else if session.mode == .work { space = .work } }
    }

    /// An hour from now. UI tests ask for less, so they do not depend on the time of day.
    static func defaultTime() -> Date {
        var minutes = 60.0
        #if DEBUG
        if let m = ProcessInfo.processInfo.environment["NOTED_CAPTURE_MINUTES"], let v = Double(m) { minutes = v }
        #endif
        return Date().addingTimeInterval(minutes * 60)
    }

    private func toggleRow(_ title: String, _ on: Binding<Bool>) -> some View {
        HStack {
            Text(title).font(.system(size: 15)).foregroundStyle(p.ink)
            Spacer()
            Toggle("", isOn: on).labelsHidden().accessibilityLabel(title)
        }.frame(minHeight: 40).tapsToToggle(on)
    }

    private func save() async {
        guard let api = session.api else { return }
        saving = true
        defer { saving = false }
        let t = title.trimmingCharacters(in: .whitespaces)
        do {
            switch kind {
            case .note:
                var r = Noted_V1_CreateNoteRequest()
                r.note.title = t; r.note.content = content; r.note.space = space; r.note.projectID = projectID
                _ = try await api.notes.createNote(r)
            case .task:
                var r = Noted_V1_CreateTaskRequest()
                r.task.title = t; r.task.space = space; r.task.projectID = projectID
                if hasWhen {
                    r.task.dueTime = .init(when)
                    if remindTask { r.task.remindTime = .init(when) }
                }
                _ = try await api.calendar.createTask(r)
            case .event:
                var r = Noted_V1_CreateEventRequest()
                r.event.title = t; r.event.space = space; r.event.projectID = projectID
                r.event.startTime = .init(when)
                r.event.endTime = .init(when.addingTimeInterval(3600))
                r.event.timeZone = TimeZone.current.identifier
                if remindMinutes >= 0 { r.event.remindBeforeMinutes = Int32(remindMinutes) }
                _ = try await api.calendar.createEvent(r)
            }
            dismiss()
        } catch { self.error = describe(error) }
    }
}
