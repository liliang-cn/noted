import SwiftUI

/// The signed-in app: four tabs (a TabView with the system bar hidden, so each tab keeps its stack and
/// only the visible one is on screen for VoiceOver and UI tests), and our own tab bar with the round
/// "+" in the middle. The bar belongs to each tab's root page (a bottom safe-area inset), so it slides
/// away with the root when a page is pushed and comes back with it, as on the prototype.
struct AppShell: View {
    @Environment(Session.self) private var session
    @Environment(AppData.self) private var data
    @Environment(AppNavigation.self) private var nav
    @Environment(\.scenePhase) private var phase

    var body: some View {
        TabView(selection: Binding(get: { nav.tab }, set: { nav.tab = $0 })) {
            ForEach(AppTab.allCases) { t in
                TabStack(router: nav.router(t)) { TabRoot(tab: t) }
                    .toolbar(.hidden, for: .tabBar)
                    .tag(t)
            }
        }
        .toastHost()
        .overlay(alignment: .top) {
            if let r = session.reminders.banner {
                ReminderBanner(reminder: r) { session.reminders.dismissBanner() }
            }
        }
        .animation(.snappy, value: session.reminders.banner?.id)
        .task { await start() }
        .onDisappear { for r in nav.routers.values { r.popToRoot() } }
        .onChange(of: phase) { _, new in
            guard let api = session.api else { return }
            if new == .active {
                WatchBridge.shared.start()
                session.reminders.startWatching(api: api) { session.mode.space }
                Task {
                    if data.isLoaded { await data.refreshAll() }
                    await session.syncExtras()
                }
            } else if new == .background {
                session.reminders.stopWatching()
            }
        }
    }

    private func start() async {
        if let api = session.api { session.reminders.startWatching(api: api) { session.mode.space } }
        #if DEBUG
        if ProcessInfo.processInfo.environment["NOTED_SHEET"] == "capture" { nav.create(CreateSpec(space: session.mode.space)) }
        #endif
        async let loaded: Void = data.refreshAll()
        async let prefs: Void = session.loadPreferences()
        _ = await (loaded, prefs)
        await session.refreshAI()
        await session.pro.load()
        WatchBridge.shared.perform = { action, id in await SnapshotBuilder.perform(action, id: id, session: session) }
        await session.syncExtras()
        // The system's permission prompt waits for an answer, so nothing else waits on it.
        await session.reminders.requestAccess()
        await session.syncReminders()
    }
}

/// One tab: its stack, its root page with the tab bar under it, and every route.
private struct TabStack<Root: View>: View {
    @Bindable var router: Router
    @ViewBuilder var root: Root

    var body: some View {
        NavigationStack(path: $router.path) {
            root
                .safeAreaInset(edge: .bottom, spacing: 0) { AppTabBar() }
                .navigationDestination(for: Route.self) { RouteDestination(route: $0) }
        }
        .environment(router)
    }
}

private struct TabRoot: View {
    let tab: AppTab

    var body: some View {
        switch tab {
        case .today: TodayView()
        case .calendar: CalendarScreen()
        case .plan: PlanView()
        case .notes: NotesScreen()
        }
    }
}

/// 今天 · 日历 · (+) · 计划 · 笔记
struct AppTabBar: View {
    @Environment(AppNavigation.self) private var nav
    @Environment(Session.self) private var session

    var body: some View {
        HStack(spacing: 0) {
            item(.today)
            item(.calendar)
            Button {
                nav.create(CreateSpec(space: session.mode.space))
            } label: {
                Image(systemName: "plus")
                    .font(.system(size: 22, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(width: 44, height: 44)
                    .background(Circle().fill(.tint))
                    .shadow(color: session.accent.opacity(0.35), radius: 6, y: 4)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
                    .padding(.top, 3)
                    .contentShape(Rectangle())
            }
            .buttonStyle(PressScaleStyle())
            .accessibilityLabel("新建")
            .accessibilityIdentifier("tab-new")
            item(.plan)
            item(.notes)
        }
        .padding(.horizontal, 4)
        .frame(height: 49)
        .background {
            Rectangle().fill(.bar).ignoresSafeArea(edges: .bottom)
                .overlay(alignment: .top) { Rectangle().fill(.hairline).frame(height: 0.5) }
        }
    }

    private func item(_ t: AppTab) -> some View {
        let on = nav.tab == t
        return Button { nav.select(t) } label: {
            VStack(spacing: 2) {
                Image(systemName: t.symbol)
                    .font(.system(size: 21, weight: .regular))
                    .frame(height: 26)
                Text(t.title).font(.system(size: 10, weight: .medium))
            }
            .padding(.top, 6)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .foregroundStyle(on ? AnyShapeStyle(.tint) : AnyShapeStyle(.tabInactive))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(t.title)
        .accessibilityAddTraits(on ? .isSelected : [])
        .accessibilityIdentifier("tab-\(t.rawValue)")
    }
}

/// Shrinks a little while pressed (the "+" and the check-in buttons).
struct PressScaleStyle: ButtonStyle {
    var scale: CGFloat = 0.9
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? scale : 1)
            .animation(.snappy(duration: 0.2), value: configuration.isPressed)
    }
}

/// A reminder that fired while the app is open.
struct ReminderBanner: View {
    let reminder: Noted_V1_Reminder
    let onClose: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "bell.fill").font(.system(size: 16)).foregroundStyle(reminder.space.tint)
            VStack(alignment: .leading, spacing: 2) {
                Text(reminder.title).font(.subheadline.weight(.semibold)).lineLimit(1)
                Text(reminder.kind == .task ? "待办提醒" : "\(Fmt.hm(reminder.dueTime.date)) 开始")
                    .font(.footnote).foregroundStyle(.secondaryText)
            }
            Spacer()
            Button(action: onClose) {
                Image(systemName: "xmark").font(.system(size: 12, weight: .bold)).foregroundStyle(.tertiaryText)
                    .frame(width: 32, height: 32).contentShape(Rectangle())
            }
            .accessibilityLabel("关闭提醒")
        }
        .padding(.leading, 14).padding(.trailing, 6).padding(.vertical, 10)
        .background(.white, in: RoundedRectangle(cornerRadius: 14))
        .shadow(color: .black.opacity(0.12), radius: 14, y: 6)
        .padding(.horizontal, 12).padding(.top, 4)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("reminder-banner")
        .transition(.move(edge: .top).combined(with: .opacity))
    }
}
