import SwiftUI

struct ModeSwitch: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    var mini = false

    var body: some View {
        HStack(spacing: mini ? 2 : 4) {
            ForEach(Mode.allCases) { m in
                let on = session.mode == m
                Button { session.setMode(m) } label: {
                    HStack(spacing: 6) {
                        if m != .all && !mini {
                            Circle().fill(on ? Color.white : m.accent(p)).frame(width: 7, height: 7)
                        }
                        Text(m.label).font(.ui(mini ? 12 : 14, .semibold))
                    }
                    .frame(maxWidth: mini ? nil : .infinity).padding(.horizontal, mini ? 12 : 0).frame(height: mini ? 28 : 38)
                    .foregroundStyle(on ? (m == .all ? p.bg : .white) : p.ink2)
                    .background(on ? (m == .all ? p.ink : m.accent(p)) : .clear, in: RoundedRectangle(cornerRadius: mini ? 7 : 9))
                }
            }
        }
        .padding(mini ? 3 : 4)
        .background(p.surface, in: RoundedRectangle(cornerRadius: mini ? 10 : 12))
        .overlay(RoundedRectangle(cornerRadius: mini ? 10 : 12).stroke(p.line, lineWidth: 1))
    }
}

struct Screen<Content: View>: View {
    @Environment(\.palette) private var p
    let eyebrow: String
    let title: String
    var trailing: AnyView = AnyView(EmptyView())
    @ViewBuilder var content: Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .bottom) {
                    VStack(alignment: .leading, spacing: 6) {
                        Eyebrow(text: eyebrow)
                        Text(title).font(.ui(30, .bold)).foregroundStyle(p.ink)
                    }
                    Spacer()
                    trailing
                }
                content
            }
            .padding(.horizontal, 16).padding(.top, 12).padding(.bottom, 24)
        }
        .scrollIndicators(.hidden)
        .background(p.bg)
    }
}

struct SpaceTag: View {
    @Environment(\.palette) private var p
    let space: Noted_V1_Space
    var body: some View {
        if space == .work || space == .life {
            HStack(spacing: 5) {
                Circle().fill(space.color(p)).frame(width: 8, height: 8)
                Text(space.label).font(.ui(11, .semibold))
            }
            .padding(.horizontal, 8).frame(height: 22)
            .background(space == .work ? p.workBg : p.lifeBg, in: RoundedRectangle(cornerRadius: 6))
            .foregroundStyle(space.color(p))
        }
    }
}

struct ErrorLine: View {
    @Environment(\.palette) private var p
    let text: String?
    var body: some View {
        if let text { Text(text).font(.ui(13)).foregroundStyle(p.bad) }
    }
}

enum Fmt {
    static let zh = Locale(identifier: "zh_CN")
    private static let hmFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm"
        return f
    }()
    private static let ymdFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        return f
    }()
    static func ymd(_ d: Date) -> String { ymdFormatter.string(from: d) }
    static func hm(_ d: Date) -> String { hmFormatter.string(from: d) }
    static func md(_ d: Date) -> String { d.formatted(.dateTime.month().day().locale(zh)) }
    static func eyebrow(_ d: Date) -> String { "\(md(d)) · \(d.formatted(.dateTime.weekday(.abbreviated).locale(zh)))" }
    static func money(_ v: Double) -> String { String(format: "%.2f", v) }
    static func signedMoney(_ v: Double) -> String { (v >= 0 ? "+" : "-") + String(format: "%.2f", abs(v)) }
    static func shares(_ v: Double) -> String {
        let s = String(format: "%.4f", v)
        return s.contains(".") ? s.replacingOccurrences(of: "0+$", with: "", options: .regularExpression).replacingOccurrences(of: "\\.$", with: "", options: .regularExpression) : s
    }
    static func relative(_ d: Date) -> String {
        let cal = Calendar.current
        if cal.isDateInToday(d) { return "今天" }
        if cal.isDateInYesterday(d) { return "昨天" }
        let days = cal.dateComponents([.day], from: cal.startOfDay(for: d), to: cal.startOfDay(for: .now)).day ?? 99
        if days > 0 && days < 7 { return d.formatted(.dateTime.weekday(.abbreviated).locale(zh)) }
        return md(d)
    }
    static func duration(from a: Date, to b: Date) -> String? {
        let m = Int(b.timeIntervalSince(a) / 60)
        guard m > 0 else { return nil }
        if m < 60 { return "\(m)分" }
        return m % 60 == 0 ? "\(m / 60)h" : "\(m / 60)h\(m % 60)分"
    }
    static func number(_ v: Double) -> String { v == v.rounded() ? String(Int(v)) : String(format: "%.1f", v) }
}

/// The design's tab strip: labels on a hairline that runs the full width, the chosen one underlined in the accent.
struct UnderlineTabs: View {
    @Environment(\.palette) private var p
    @Environment(\.mode) private var mode
    let items: [String]
    let selected: Int
    var gap: CGFloat = 22
    let onSelect: (Int) -> Void

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            Rectangle().fill(p.line).frame(height: 1)
            ScrollView(.horizontal) {
                HStack(spacing: gap) {
                    ForEach(Array(items.enumerated()), id: \.offset) { i, label in
                        Button { onSelect(i) } label: {
                            Text(label).font(.ui(14, .semibold))
                                .foregroundStyle(selected == i ? p.ink : p.ink3)
                                .padding(.top, 8).padding(.bottom, 10)
                                .overlay(alignment: .bottom) {
                                    if selected == i { Rectangle().fill(mode.accent(p)).frame(height: 2).offset(y: 1) }
                                }
                        }
                    }
                }
            }
            .scrollIndicators(.hidden)
            .scrollBounceBehavior(.basedOnSize)
        }
        .frame(maxWidth: .infinity)
    }
}

/// The back control every pushed screen uses: a chevron and where it goes back to.
struct BackButton: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.palette) private var p
    @Environment(\.mode) private var mode
    var title = "返回"

    var body: some View {
        Button { dismiss() } label: {
            HStack(spacing: 3) {
                Image(systemName: "chevron.left").font(.system(size: 15, weight: .semibold))
                Text(title).font(.ui(15, .semibold))
            }
            .foregroundStyle(mode.accent(p))
            .frame(minHeight: 32)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("back")
    }
}

/// 个人 / 工作 as one control: only the chosen side is filled, in its own color.
struct SpacePicker: View {
    @Environment(\.palette) private var p
    @Binding var selection: Noted_V1_Space

    var body: some View {
        HStack(spacing: 2) {
            option("个人", .life)
            option("工作", .work)
        }
        .padding(2)
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radiusSm))
        .overlay(RoundedRectangle(cornerRadius: p.radiusSm).stroke(p.line, lineWidth: 1))
    }

    private func option(_ label: String, _ s: Noted_V1_Space) -> some View {
        let on = selection == s
        let color = s == .work ? p.work : p.life
        return Button { selection = s } label: {
            HStack(spacing: 6) {
                Circle().fill(color).frame(width: 7, height: 7).opacity(on ? 1 : 0.45)
                Text(label).font(.ui(13, .semibold))
            }
            .padding(.horizontal, 12).frame(height: 30)
            .background(on ? (s == .work ? p.workBg : p.lifeBg) : Color.clear, in: RoundedRectangle(cornerRadius: max(p.radiusSm - 2, 2)))
            .foregroundStyle(on ? color : p.ink3)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

/// A tab's page as the design lays it out: an optional eyebrow over the title, the controls
/// level with it on the right.
struct TabScreen<Trailing: View, Content: View>: View {
    @Environment(\.palette) private var p
    var eyebrow: String? = nil
    let title: String
    @ViewBuilder var trailing: Trailing
    @ViewBuilder var content: Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .center) {
                    VStack(alignment: .leading, spacing: 6) {
                        if let eyebrow { Eyebrow(text: eyebrow) }
                        Text(title).font(.ui(30, .bold)).tracking(-0.6).foregroundStyle(p.ink)
                    }
                    Spacer()
                    trailing
                }
                content
            }
            .padding(.horizontal, 16).padding(.top, 12).padding(.bottom, 24)
        }
        .scrollIndicators(.hidden)
        .background(p.bg)
    }
}

/// The design's `.bar`: a 4pt track with the done part filled.
struct LineBar: View {
    @Environment(\.palette) private var p
    let value: Double
    let tint: Color

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Rectangle().fill(p.surface2)
                Rectangle().fill(tint).frame(width: g.size.width * min(max(value, 0), 1))
            }
        }
        .frame(height: 4)
        .clipShape(RoundedRectangle(cornerRadius: 2))
    }
}

/// The design's `.check`: a rounded square, red when the to-do is due, filled once done.
struct CheckBox: View {
    @Environment(\.palette) private var p
    var hot = false
    var done = false

    var body: some View {
        RoundedRectangle(cornerRadius: 6)
            .strokeBorder(done ? p.ok : (hot ? p.bad : p.ink3), lineWidth: 1.75)
            .background(done ? p.ok : .clear, in: RoundedRectangle(cornerRadius: 6))
            .overlay { if done { Image(systemName: "checkmark").font(.system(size: 10, weight: .heavy)).foregroundStyle(.white) } }
            .frame(width: 20, height: 20)
    }
}

/// A section as the design spaces it: the eyebrow sits 8pt above what it names.
struct EyebrowSection<Trailing: View, Content: View>: View {
    let title: String
    @ViewBuilder var trailing: Trailing
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Eyebrow(text: title)
                Spacer()
                trailing
            }
            content
        }
    }
}

extension EyebrowSection where Trailing == EmptyView {
    init(title: String, @ViewBuilder content: () -> Content) {
        self.init(title: title, trailing: { EmptyView() }, content: content)
    }
}

/// Something the person asked to delete from a list, waiting for them to confirm.
enum PendingDelete {
    case event(Noted_V1_Event)
    case task(Noted_V1_Task)
    case note(Noted_V1_Note)
    case project(Noted_V1_Project)
    case goal(Noted_V1_Goal)
    case objective(Noted_V1_Objective)
    case holding(Noted_V1_Holding)

    /// A repeating event is one row on the server: deleting it removes every repeat.
    var question: String {
        switch self {
        case .event(let e): e.rrule.isEmpty ? "删除这个日程?" : "删除这个重复日程(所有重复)?"
        case .task: "删除这个待办?"
        case .note: "删除这条笔记?"
        case .project: "删除这个项目?"
        case .goal, .objective: "删除这个目标?"
        case .holding: "删除这个标的?"
        }
    }

    var message: String? {
        switch self {
        case .objective: "目标和它的记录会删除,里面的维度变成单独的目标。"
        case .holding: "每月的定投提醒也会一起删除。"
        default: nil
        }
    }

    func perform(_ api: API, items: Bool = false) async throws {
        switch self {
        case .event(let e):
            var r = Noted_V1_DeleteEventRequest(); r.id = e.id
            _ = try await api.calendar.deleteEvent(r)
        case .task(let t):
            var r = Noted_V1_DeleteTaskRequest(); r.id = t.id
            _ = try await api.calendar.deleteTask(r)
        case .note(let n):
            var r = Noted_V1_DeleteNoteRequest(); r.id = n.id
            _ = try await api.notes.deleteNote(r)
            NoteDrafts.clear(n.id)
        case .project(let pr):
            var r = Noted_V1_DeleteProjectRequest(); r.id = pr.id; r.deleteItems = items
            _ = try await api.projects.deleteProject(r)
        case .goal(let g):
            var r = Noted_V1_DeleteGoalRequest(); r.id = g.id
            _ = try await api.goals.deleteGoal(r)
        case .objective(let o):
            var r = Noted_V1_DeleteObjectiveRequest(); r.id = o.id
            _ = try await api.objectives.deleteObjective(r)
        case .holding(let h):
            var r = Noted_V1_DeleteHoldingRequest(); r.id = h.id
            _ = try await api.holdings.deleteHolding(r)
        }
    }
}

extension View {
    /// The long-press menu of a row or card that can be deleted.
    func deleteMenu(_ action: @escaping () -> Void) -> some View {
        contextMenu {
            Button(role: .destructive, action: action) { Label("删除", systemImage: "trash") }
        }
    }

    /// Asks before deleting `pending`, deletes it, then calls `done` with what went wrong, if anything.
    func confirmDelete(_ pending: Binding<PendingDelete?>, done: @escaping @MainActor (String?) async -> Void) -> some View {
        modifier(DeleteConfirmation(pending: pending, done: done))
    }
}

private struct DeleteConfirmation: ViewModifier {
    @Environment(Session.self) private var session
    @Binding var pending: PendingDelete?
    let done: @MainActor (String?) async -> Void

    func body(content: Content) -> some View {
        content.confirmationDialog(pending?.question ?? "", isPresented: Binding(get: { pending != nil }, set: { if !$0 { pending = nil } }),
                                   titleVisibility: .visible, presenting: pending) { item in
            switch item {
            case .project:
                Button("只删除项目,保留里面的内容") { run(item, items: false) }
                Button("连同待办、日程、笔记一起删除", role: .destructive) { run(item, items: true) }
            case .objective:
                Button("删除,保留其中的维度", role: .destructive) { run(item) }
            case .holding:
                Button("删除标的和它的全部记录", role: .destructive) { run(item) }
            default:
                Button("删除", role: .destructive) { run(item) }
            }
            Button("取消", role: .cancel) {}
        } message: { item in
            if let m = item.message { Text(m) }
        }
    }

    private func run(_ item: PendingDelete, items: Bool = false) {
        Task {
            guard let api = session.api else { return }
            do {
                try await item.perform(api, items: items)
                await done(nil)
                // Reminders, widgets and the watch drop what was deleted.
                Task { await session.syncExtras() }
            } catch { await done(describe(error)) }
        }
    }
}
