import SwiftUI

enum Tab: Hashable { case focus, calendar, projects, notes }

struct RootView: View {
    @Environment(Session.self) private var session
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @State private var tab: Tab = {
        #if DEBUG
        switch ProcessInfo.processInfo.environment["NOTED_TAB"] {
        case "calendar": return .calendar
        case "projects": return .projects
        case "notes": return .notes
        default: break
        }
        #endif
        return .focus
    }()
    @State private var refresh = 0
    @Environment(\.scenePhase) private var phase

    var body: some View {
        @Bindable var router = router
        NavigationStack(path: $router.path) {
            tabs
                .routeChrome()
                .navigationDestination(for: Route.self) { RouteDestination(route: $0) }
        }
        // Above the stack, so a reminder shows on whatever page is open, not only on the tabs.
        .overlay(alignment: .top) {
            if let r = session.reminders.banner { ReminderBanner(reminder: r) { session.reminders.dismissBanner() } }
        }
    }

    private var tabs: some View {
        VStack(spacing: 0) {
            Group {
                switch tab {
                case .focus: FocusView()
                case .calendar: CalendarView()
                case .projects: ProjectsView()
                case .notes: NotesView()
                }
            }
            .id(refresh)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            TabBar(tab: $tab) { router.push(.create(CreateSpec(space: session.mode.space))) }
        }
        .background(p.bg.ignoresSafeArea())
        .task {
            await session.refreshAI()
            await session.loadPreferences()
            await session.pro.load()
            await session.refreshAccount()
            WatchBridge.shared.perform = { action, id in await SnapshotBuilder.perform(action, id: id, session: session) }
            await session.syncExtras()
            // The system's permission prompt waits for the person to answer, so nothing else waits on it.
            await session.reminders.requestAccess()
            await session.syncReminders()
        }
        .onChange(of: session.pro.isPro) { Task { await session.syncExtras() } }
        .onChange(of: phase) { _, new in
            guard let api = session.api else { return }
            if new == .active {
                WatchBridge.shared.start()
                session.reminders.startWatching(api: api) { session.mode.space }
                Task { await session.syncExtras() }
            } else {
                session.reminders.stopWatching()
            }
        }
        .onAppear {
            if let api = session.api { session.reminders.startWatching(api: api) { session.mode.space } }
        }
        .onChange(of: router.path) { old, new in
            // Back from creating something: refresh what the tabs show.
            if new.count < old.count, case .create = old.last { refresh += 1; Task { await session.syncExtras() } }
            if new.count < old.count, case .note = old.last { refresh += 1 }
        }
        .onAppear {
            #if DEBUG
            if ProcessInfo.processInfo.environment["NOTED_SHEET"] == "capture", router.path.isEmpty { router.push(.create(CreateSpec())) }
            #endif
        }
    }
}

private struct TabBar: View {
    @Environment(\.palette) private var p
    @Environment(\.mode) private var mode
    @Binding var tab: Tab
    let onAdd: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            item(.focus, "概览", .focus)
            item(.calendar, "日历", .calendar)
            Button(action: onAdd) {
                Image(systemName: "plus").font(.system(size: 20, weight: .bold)).foregroundStyle(.white)
                    .frame(width: 52, height: 40)
                    .background(mode.accent(p), in: RoundedRectangle(cornerRadius: 10))
                    .frame(maxWidth: .infinity)
                    .contentShape(Rectangle())
            }.accessibilityLabel("记一笔").frame(maxWidth: .infinity)
            item(.projects, "项目", .projects)
            item(.notes, "笔记", .notes)
        }
        .padding(.top, 8)
        .frame(maxWidth: .infinity)
        .background(p.surface.ignoresSafeArea(edges: .bottom))
        .overlay(alignment: .top) { Rectangle().fill(p.line).frame(height: 1) }
    }

    private func item(_ t: Tab, _ label: String, _ icon: Tab) -> some View {
        Button { tab = t } label: {
            VStack(spacing: 3) {
                TabGlyph(tab: icon).stroke(style: StrokeStyle(lineWidth: 1.7, lineCap: .round, lineJoin: .round)).frame(width: 24, height: 24)
                Text(label).font(.ui(10, .semibold))
            }
            .foregroundStyle(tab == t ? mode.accent(p) : p.ink3)
            .frame(maxWidth: .infinity).frame(height: 46)
            .contentShape(Rectangle())
        }
    }
}

/// The tab icons from the design, drawn on its 24-unit grid with a 1.7 stroke.
private struct TabGlyph: Shape {
    let tab: Tab

    func path(in rect: CGRect) -> Path {
        let k = rect.width / 24
        func P(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: rect.minX + x * k, y: rect.minY + y * k) }
        var d = Path()
        switch tab {
        case .focus:
            d.move(to: P(4, 9)); d.addLine(to: P(4, 6)); d.addQuadCurve(to: P(6, 4), control: P(4, 4)); d.addLine(to: P(9, 4))
            d.move(to: P(15, 4)); d.addLine(to: P(18, 4)); d.addQuadCurve(to: P(20, 6), control: P(20, 4)); d.addLine(to: P(20, 9))
            d.move(to: P(20, 15)); d.addLine(to: P(20, 18)); d.addQuadCurve(to: P(18, 20), control: P(20, 20)); d.addLine(to: P(15, 20))
            d.move(to: P(9, 20)); d.addLine(to: P(6, 20)); d.addQuadCurve(to: P(4, 18), control: P(4, 20)); d.addLine(to: P(4, 15))
            d.addEllipse(in: CGRect(x: rect.minX + 9.5 * k, y: rect.minY + 9.5 * k, width: 5 * k, height: 5 * k))
        case .calendar:
            d.addRoundedRect(in: CGRect(x: rect.minX + 3.5 * k, y: rect.minY + 5 * k, width: 17 * k, height: 15.5 * k),
                             cornerSize: CGSize(width: 2.5 * k, height: 2.5 * k))
            d.move(to: P(8, 3)); d.addLine(to: P(8, 7))
            d.move(to: P(16, 3)); d.addLine(to: P(16, 7))
            d.move(to: P(3.5, 10)); d.addLine(to: P(20.5, 10))
        case .projects:
            d.move(to: P(5, 21)); d.addLine(to: P(5, 4))
            d.move(to: P(5, 5)); d.addLine(to: P(18, 5)); d.addLine(to: P(15.5, 9)); d.addLine(to: P(18, 13)); d.addLine(to: P(5, 13))
        case .notes:
            d.move(to: P(6, 3)); d.addLine(to: P(15, 3)); d.addLine(to: P(19, 7)); d.addLine(to: P(19, 21)); d.addLine(to: P(6, 21)); d.closeSubpath()
            d.move(to: P(14, 3)); d.addLine(to: P(14, 8)); d.addLine(to: P(19, 8))
            d.move(to: P(9, 13)); d.addLine(to: P(16, 13))
            d.move(to: P(9, 17)); d.addLine(to: P(14, 17))
        }
        return d
    }
}

struct ReminderBanner: View {
    @Environment(\.palette) private var p
    let reminder: Noted_V1_Reminder
    let onClose: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "bell.fill").font(.system(size: 16)).foregroundStyle(reminder.space.color(p))
            VStack(alignment: .leading, spacing: 2) {
                Text(reminder.title).font(.ui(15, .semibold)).foregroundStyle(p.ink).lineLimit(1)
                Text(reminder.kind == .task ? "待办提醒" : "\(Fmt.hm(reminder.dueTime.date)) 开始")
                    .font(.ui(12)).foregroundStyle(p.ink2)
            }
            Spacer()
            Button(action: onClose) { Image(systemName: "xmark").font(.system(size: 12, weight: .bold)).foregroundStyle(p.ink3) }
                .accessibilityLabel("关闭提醒")
        }
        .padding(14)
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
        .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        .shadow(color: .black.opacity(0.08), radius: 12, y: 4)
        .padding(.horizontal, 16).padding(.top, 8)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("reminder-banner")
        .transition(.move(edge: .top).combined(with: .opacity))
    }
}
