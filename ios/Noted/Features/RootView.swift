import SwiftUI

enum Tab: Hashable { case focus, calendar, projects, notes }

struct RootView: View {
    @Environment(Session.self) private var session
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
    @State private var capturing = false
    @State private var refresh = 0
    @Environment(\.scenePhase) private var phase

    var body: some View {
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
            TabBar(tab: $tab) { capturing = true }
        }
        .background(p.bg.ignoresSafeArea())
        .task {
            await session.refreshAI()
            await session.loadPreferences()
            await session.pro.load()
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
        .overlay(alignment: .top) {
            if let r = session.reminders.banner { ReminderBanner(reminder: r) { session.reminders.dismissBanner() } }
        }
        .sheet(isPresented: $capturing, onDismiss: { refresh += 1; Task { await session.syncExtras() } }) { CaptureView() }
    }
}

private struct TabBar: View {
    @Environment(\.palette) private var p
    @Environment(\.mode) private var mode
    @Binding var tab: Tab
    let onAdd: () -> Void

    var body: some View {
        HStack(alignment: .top) {
            item(.focus, "概览", "viewfinder")
            item(.calendar, "日历", "calendar")
            Button(action: onAdd) {
                Image(systemName: "plus").font(.system(size: 20, weight: .bold)).foregroundStyle(.white)
                    .frame(width: 52, height: 40)
                    .background(mode.accent(p), in: RoundedRectangle(cornerRadius: 10))
            }.accessibilityLabel("记一笔")
            item(.projects, "项目", "flag")
            item(.notes, "笔记", "doc.text")
        }
        .padding(.horizontal, 8).padding(.top, 8)
        .frame(maxWidth: .infinity)
        .background(p.surface.ignoresSafeArea(edges: .bottom))
        .overlay(alignment: .top) { Rectangle().fill(p.line).frame(height: 1) }
    }

    private func item(_ t: Tab, _ label: String, _ icon: String) -> some View {
        Button { tab = t } label: {
            VStack(spacing: 3) {
                Image(systemName: icon).font(.system(size: 20))
                Text(label).font(.system(size: 10, weight: .semibold))
            }
            .foregroundStyle(tab == t ? mode.accent(p) : p.ink3)
            .frame(width: 56, height: 46)
        }
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
                Text(reminder.title).font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink).lineLimit(1)
                Text(reminder.kind == .task ? "待办提醒" : "\(Fmt.hm(reminder.dueTime.date)) 开始")
                    .font(.system(size: 12)).foregroundStyle(p.ink2)
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
