import SwiftUI

/// Pick items that belong to no project yet and put them into this one.
struct LinkExistingView: View {
    enum Kind: String, CaseIterable, Identifiable {
        case task = "待办", event = "日程", note = "笔记", goal = "目标", objective = "多维目标"
        var id: String { rawValue }
        var icon: String {
            switch self {
            case .task: "checkmark.circle"
            case .event: "calendar"
            case .note: "doc.text"
            case .goal: "target"
            case .objective: "square.stack.3d.up"
            }
        }
    }

    struct Candidate: Identifiable {
        let id: String
        let title: String
        let detail: String
    }

    let projectID: String
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var kind: Kind = .task
    @State private var items: [Candidate] = []
    @State private var picked: Set<String> = []
    @State private var loading = false
    @State private var saving = false
    @State private var error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Eyebrow(text: "关联已有")
                Spacer()
                Button("取消") { dismiss() }
                    .font(.ui(13, .semibold)).foregroundStyle(session.mode.accent(p))
            }
            .padding(.top, 6)

            Menu {
                ForEach(Kind.allCases) { k in
                    Button { kind = k } label: { Label(k.rawValue, systemImage: k.icon) }
                }
            } label: {
                HStack(spacing: 10) {
                    Text("类型").font(.ui(13, .medium)).foregroundStyle(p.ink2).frame(width: 44, alignment: .leading)
                    Image(systemName: kind.icon).font(.system(size: 15)).foregroundStyle(session.mode.accent(p))
                    Text(kind.rawValue).font(.ui(16, .semibold)).foregroundStyle(p.ink)
                    Spacer()
                    Image(systemName: "chevron.up.chevron.down").font(.system(size: 12, weight: .semibold)).foregroundStyle(p.ink3)
                }
                .padding(.horizontal, 14).frame(height: p.rowHeight)
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))
            }
            .accessibilityIdentifier("link-kind-menu")

            ErrorLine(text: error)

            ScrollView {
                if items.isEmpty {
                    Text(loading ? "读取中…" : "没有可以关联的\(kind.rawValue)")
                        .font(.ui(14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 90).card()
                } else {
                    VStack(spacing: 0) {
                        ForEach(Array(items.enumerated()), id: \.element.id) { i, it in
                            if i > 0 { Divider().overlay(p.line) }
                            let on = picked.contains(it.id)
                            Button { if on { picked.remove(it.id) } else { picked.insert(it.id) } } label: {
                                HStack(spacing: 12) {
                                    Image(systemName: on ? "checkmark.circle.fill" : "circle")
                                        .font(.ui(20)).foregroundStyle(on ? session.mode.accent(p) : p.ink3)
                                    Text(it.title.isEmpty ? "无标题" : it.title).font(.ui(15, .medium)).foregroundStyle(p.ink).lineLimit(1)
                                    Spacer(minLength: 8)
                                    Text(it.detail).font(.ui(12)).foregroundStyle(p.ink2).lineLimit(1)
                                }
                                .padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .accessibilityLabel(it.title)
                            .accessibilityValue(on ? "已选" : "未选")
                        }
                    }
                    .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                    .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))
                }
            }
            .scrollIndicators(.hidden)

            Button { Task { await link() } } label: {
                Text(saving ? "关联中…" : picked.isEmpty ? "选择要关联的内容" : "关联 \(picked.count) 项")
                    .font(.ui(16, .semibold)).frame(maxWidth: .infinity).frame(height: 50)
                    .background(session.mode.accent(p).opacity(picked.isEmpty || saving ? 0.4 : 1), in: RoundedRectangle(cornerRadius: p.radiusSm))
                    .foregroundStyle(.white)
            }
            .disabled(picked.isEmpty || saving)
            .accessibilityIdentifier("link-save")
            .padding(.bottom, 8)
        }
        .padding(.horizontal, 16)
        .background(p.bg.ignoresSafeArea())
        .presentationDragIndicator(.visible)
        .presentationCornerRadius(20)
        .task(id: kind) { picked = []; await load() }
    }

    // MARK: loading what could be linked

    private func load() async {
        guard let api = session.api else { return }
        loading = true
        defer { loading = false }
        do {
            switch kind {
            case .task:
                var r = Noted_V1_ListTasksRequest(); r.filter = .open; r.pageSize = 200
                items = try await api.calendar.listTasks(r).tasks.filter { $0.projectID.isEmpty }.map {
                    Candidate(id: $0.id, title: $0.title, detail: $0.hasDueTime ? Fmt.md($0.dueTime.date) : "")
                }
            case .event:
                var r = Noted_V1_ListEventsRequest()
                r.from = .init(Date().addingTimeInterval(-30 * 86400)); r.to = .init(Date().addingTimeInterval(400 * 86400))
                var seen = Set<String>()
                items = try await api.calendar.listEvents(r).occurrences.compactMap { o in
                    let e = o.event
                    guard e.projectID.isEmpty, seen.insert(e.id).inserted else { return nil }
                    return Candidate(id: e.id, title: e.title, detail: Fmt.md(e.startTime.date))
                }
            case .note:
                var r = Noted_V1_ListNotesRequest(); r.pageSize = 100
                items = try await api.notes.listNotes(r).notes.filter { $0.projectID.isEmpty }.map {
                    Candidate(id: $0.id, title: $0.title, detail: $0.content.prefix(20).replacingOccurrences(of: "\n", with: " "))
                }
            case .goal:
                items = try await api.goals.listGoals(Noted_V1_ListGoalsRequest()).goals
                    .filter { $0.projectID.isEmpty && $0.objectiveID.isEmpty }
                    .map { Candidate(id: $0.id, title: $0.title, detail: "\(Fmt.number($0.progress.done))/\(Fmt.number($0.progress.target)) \($0.unit)") }
            case .objective:
                items = try await api.objectives.listObjectives(Noted_V1_ListObjectivesRequest()).objectives
                    .filter { $0.projectID.isEmpty }
                    .map { Candidate(id: $0.id, title: $0.title, detail: "\($0.goals.count) 个维度") }
            }
            error = nil
        } catch { if !(error is CancellationError) { self.error = describe(error) } }
    }

    // MARK: linking

    private func link() async {
        guard let api = session.api else { return }
        saving = true
        defer { saving = false }
        do {
            for id in picked {
                switch kind {
                case .task:
                    var u = Noted_V1_UpdateTaskRequest(); u.task.id = id; u.task.projectID = projectID; u.updateMask = fieldMask("project_id")
                    _ = try await api.calendar.updateTask(u)
                case .event:
                    var u = Noted_V1_UpdateEventRequest(); u.event.id = id; u.event.projectID = projectID; u.updateMask = fieldMask("project_id")
                    _ = try await api.calendar.updateEvent(u)
                case .note:
                    var u = Noted_V1_UpdateNoteRequest(); u.note.id = id; u.note.projectID = projectID; u.updateMask = fieldMask("project_id")
                    _ = try await api.notes.updateNote(u)
                case .goal:
                    var u = Noted_V1_UpdateGoalRequest(); u.goal.id = id; u.goal.projectID = projectID; u.updateMask = fieldMask("project_id")
                    _ = try await api.goals.updateGoal(u)
                case .objective:
                    var u = Noted_V1_UpdateObjectiveRequest(); u.objective.id = id; u.objective.projectID = projectID; u.updateMask = fieldMask("project_id")
                    _ = try await api.objectives.updateObjective(u)
                }
            }
            dismiss()
        } catch { self.error = describe(error) }
    }
}
