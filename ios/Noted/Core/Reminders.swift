import Foundation
import Observation
import UserNotifications

/// Debug builds append to the file named by NOTED_DEBUG_LOG, so tests can see what the app did.
func debugLog(_ line: String) {
    #if DEBUG
    guard let path = ProcessInfo.processInfo.environment["NOTED_DEBUG_LOG"] else { return }
    let text = "\(Date().formatted(.iso8601)) \(line)\n"
    if let h = FileHandle(forWritingAtPath: path) { h.seekToEndOfFile(); h.write(Data(text.utf8)); try? h.close() }
    else { try? text.write(toFile: path, atomically: false, encoding: .utf8) }
    #endif
}

struct PlannedReminder: Equatable, Sendable {
    var id: String
    var title: String
    var body: String
    var fire: Date
}

/// Which reminders to mute while the app is in the other mode. Stored in "ui.mute".
struct MuteRules: Codable, Equatable, Sendable {
    var lifeInWork = false
    var workInLife = false

    init() {}
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        lifeInWork = try c.decodeIfPresent(Bool.self, forKey: .lifeInWork) ?? false
        workInLife = try c.decodeIfPresent(Bool.self, forKey: .workInLife) ?? false
    }

    func mutes(_ space: Noted_V1_Space, in mode: Mode) -> Bool {
        (mode == .work && space == .life && lifeInWork) || (mode == .life && space == .work && workInLife)
    }
}

/// Decides which local notifications to schedule. Pure: nothing here touches the system.
enum ReminderPlanner {
    /// iOS keeps at most 64 pending local notifications.
    static let limit = 60

    static func plan(occurrences: [Noted_V1_Occurrence], tasks: [Noted_V1_Task], now: Date, mode: Mode, mute: MuteRules) -> [PlannedReminder] {
        var out: [PlannedReminder] = []
        for o in occurrences where o.event.hasRemindBeforeMinutes {
            guard !mute.mutes(o.event.space, in: mode) else { continue }
            let start = o.startTime.date
            let fire = start.addingTimeInterval(-Double(o.event.remindBeforeMinutes) * 60)
            guard fire > now else { continue }
            let lead = o.event.remindBeforeMinutes
            out.append(PlannedReminder(
                id: "noted.e.\(o.event.id).\(Int(start.timeIntervalSince1970))",
                title: o.event.title,
                body: lead == 0 ? "现在开始" : "\(Fmt.hm(start)) 开始 · \(lead) 分钟后",
                fire: fire))
        }
        for t in tasks where !t.completed && t.hasRemindTime {
            guard !mute.mutes(t.space, in: mode) else { continue }
            let fire = t.remindTime.date
            guard fire > now else { continue }
            out.append(PlannedReminder(
                id: "noted.t.\(t.id)",
                title: t.title,
                body: t.hasDueTime ? "截止 \(Fmt.md(t.dueTime.date)) \(Fmt.hm(t.dueTime.date))" : "待办提醒",
                fire: fire))
        }
        return Array(out.sorted { $0.fire < $1.fire }.prefix(limit))
    }
}

/// Schedules the local notifications and listens for reminders the server fires
/// while the app is open.
@MainActor @Observable
final class ReminderCenter: NSObject, UNUserNotificationCenterDelegate {
    var banner: Noted_V1_Reminder?
    var authorized = false
    private(set) var streaming = false
    private var watcher: Task<Void, Never>?
    private var dismissal: Task<Void, Never>?

    override init() {
        super.init()
        UNUserNotificationCenter.current().delegate = self
    }

    func requestAccess() async {
        var options: UNAuthorizationOptions = [.alert, .sound, .badge]
        #if DEBUG
        // Tests cannot answer the system's permission alert; provisional access needs none.
        if ProcessInfo.processInfo.environment["NOTED_PROVISIONAL_NOTIFICATIONS"] == "1" { options.insert(.provisional) }
        #endif
        authorized = (try? await UNUserNotificationCenter.current().requestAuthorization(options: options)) ?? false
        #if DEBUG
        let st = await UNUserNotificationCenter.current().notificationSettings().authorizationStatus
        debugLog("auth granted=\(authorized) status=\(st.rawValue)")
        #endif
    }

    /// Replaces every pending Noted notification with what the server says is coming up.
    func sync(api: API, mode: Mode, mute: MuteRules, now: Date = .now) async {
        var ev = Noted_V1_ListEventsRequest()
        ev.from = .init(now); ev.to = .init(now.addingTimeInterval(14 * 86400))
        var tk = Noted_V1_ListTasksRequest()
        tk.filter = .open; tk.pageSize = 200
        let occ: [Noted_V1_Occurrence]
        let tasks: [Noted_V1_Task]
        do {
            occ = try await api.calendar.listEvents(ev).occurrences
            tasks = try await api.calendar.listTasks(tk).tasks
        } catch {
            #if DEBUG
            debugLog("sync could not read the calendar: \(error)")
            #endif
            return
        }
        let plan = ReminderPlanner.plan(occurrences: occ, tasks: tasks, now: now, mode: mode, mute: mute)
        #if DEBUG
        debugLog("sync occurrences=\(occ.count) tasks=\(tasks.count) planned=\(plan.count) mode=\(mode.rawValue)")
        #endif
        await schedule(plan)
    }

    func schedule(_ planned: [PlannedReminder]) async {
        let center = UNUserNotificationCenter.current()
        let pending = await center.pendingNotificationRequests().map(\.identifier).filter { $0.hasPrefix("noted.") }
        center.removePendingNotificationRequests(withIdentifiers: pending)
        for r in planned {
            let content = UNMutableNotificationContent()
            content.title = r.title
            content.body = r.body
            content.sound = .default
            let comps = Calendar.current.dateComponents([.year, .month, .day, .hour, .minute, .second], from: r.fire)
            let trigger = UNCalendarNotificationTrigger(dateMatching: comps, repeats: false)
            do { try await center.add(UNNotificationRequest(identifier: r.id, content: content, trigger: trigger)) } catch {
                #if DEBUG
                debugLog("add failed \(r.id): \(error)")
                #endif
            }
        }
        #if DEBUG
        debugLog("scheduled planned=\(planned.count)")
        #endif
        #if DEBUG
        // UI tests cannot see the system's pending list, so they read it from here.
        if let path = ProcessInfo.processInfo.environment["NOTED_DUMP_PENDING"] {
            let ids = await center.pendingNotificationRequests().map(\.identifier).filter { $0.hasPrefix("noted.") }.sorted()
            try? JSONEncoder().encode(ids).write(to: URL(fileURLWithPath: path))
        }
        #endif
    }

    // MARK: live stream

    func startWatching(api: API, space: @escaping @MainActor @Sendable () -> Noted_V1_Space) {
        stopWatching()
        let onReminder: @Sendable (Noted_V1_Reminder) async -> Void = { [weak self] r in await self?.show(r) }
        let setStreaming: @Sendable (Bool) async -> Void = { [weak self] v in await self?.setStreaming(v) }
        watcher = Task {
            var delay: UInt64 = 2
            while !Task.isCancelled {
                let started = Date()
                var req = Noted_V1_WatchRemindersRequest()
                req.space = await space()
                do {
                    try await api.calendar.watchReminders(req) { response in
                        await setStreaming(true)
                        for try await r in response.messages { await onReminder(r) }
                    }
                } catch { /* reconnect below */ }
                await setStreaming(false)
                if Task.isCancelled { break }
                if Date().timeIntervalSince(started) > 10 { delay = 2 }
                try? await Task.sleep(nanoseconds: delay * 1_000_000_000)
                delay = min(delay * 2, 30)
            }
        }
    }

    private func setStreaming(_ v: Bool) { streaming = v }

    func stopWatching() {
        watcher?.cancel()
        watcher = nil
        streaming = false
    }

    func show(_ r: Noted_V1_Reminder) {
        banner = r
        dismissal?.cancel()
        dismissal = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 8_000_000_000)
            if !Task.isCancelled { self?.banner = nil }
        }
    }

    func dismissBanner() { banner = nil }

    // While the app is open the in-app banner covers it, unless the stream is down.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        await MainActor.run { self.streaming } ? [] : [.banner, .sound]
    }
}
