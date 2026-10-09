import SwiftUI
import WatchConnectivity
import WidgetKit

@main
struct NotedWatchApp: App {
    @State private var store = WatchStore()

    var body: some Scene {
        WindowGroup {
            WatchRoot().environment(store)
        }
    }
}

/// The watch's copy of today, kept in step with the phone.
@MainActor @Observable
final class WatchStore: NSObject, WCSessionDelegate {
    var snapshot: Snapshot = {
        #if DEBUG
        if ProcessInfo.processInfo.environment["NOTED_SAMPLE"] == "1" { return Sample.today }
        #endif
        return SnapshotStore.load() ?? Snapshot()
    }()
    var busy = false
    var message: String?
    /// Tests run the watch on sample data with no phone, even when a paired phone is nearby.
    private let isolated: Bool

    static var sampleMode: Bool {
        #if DEBUG
        ProcessInfo.processInfo.environment["NOTED_SAMPLE"] == "1"
        #else
        false
        #endif
    }

    init(isolated: Bool = WatchStore.sampleMode) {
        self.isolated = isolated
        super.init()
        guard WCSession.isSupported() else { return }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    func perform(_ action: String, id: String) {
        guard WCSession.default.isReachable, !isolated else {
            message = "手机不在身边"
            return
        }
        busy = true
        // WatchConnectivity calls these on its own queue, so they must not be main-actor closures.
        let onReply: @Sendable ([String: Any]) -> Void = { [weak self] reply in
            let data = reply["snapshot"] as? Data
            let error = reply["error"] as? String
            Task { @MainActor in self?.finished(data: data, error: error) }
        }
        let onError: @Sendable (Error) -> Void = { [weak self] _ in
            Task { @MainActor in self?.finished(data: nil, error: "手机不在身边") }
        }
        WCSession.default.sendMessage(["action": action, "id": id], replyHandler: onReply, errorHandler: onError)
    }

    private func finished(data: Data?, error: String?) {
        busy = false
        if let data, let s = SnapshotStore.decode(data) { apply(s); message = nil } else { message = error ?? "没有完成" }
    }

    func apply(_ s: Snapshot) {
        if isolated { return }
        snapshot = s
        SnapshotStore.save(s)
        WidgetCenter.shared.reloadAllTimelines()
    }

    nonisolated func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        if let d = session.receivedApplicationContext["snapshot"] as? Data, let s = SnapshotStore.decode(d) {
            Task { @MainActor in self.apply(s) }
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveApplicationContext applicationContext: [String: Any]) {
        if let d = applicationContext["snapshot"] as? Data, let s = SnapshotStore.decode(d) {
            Task { @MainActor in self.apply(s) }
        }
    }
}

struct WatchRoot: View {
    @Environment(WatchStore.self) private var store

    var body: some View {
        NavigationStack {
            // One list, so the crown reaches everything: today first, then the goals.
            List {
                TodaySection()
                GoalSection()
            }
        }
    }
}

struct TodaySection: View {
    @Environment(WatchStore.self) private var store

    var body: some View {
        let s = store.snapshot
        let c = s.colors
        Section {
            if s.items.isEmpty {
                Text("今天没有安排").foregroundStyle(Color.secondary)
            }
            ForEach(s.items) { i in
                HStack(spacing: 8) {
                    if i.isTask {
                        Button { store.perform("complete", id: String(i.id.dropFirst())) } label: {
                            Image(systemName: "circle").foregroundStyle(i.overdue ? Color(rgb: c.bad) : Color.secondary)
                        }
                        .buttonStyle(.plain).accessibilityLabel("完成 \(i.title)")
                    } else {
                        Circle().fill(Color(rgb: i.work ? c.work : c.life)).frame(width: 7, height: 7)
                    }
                    VStack(alignment: .leading, spacing: 1) {
                        Text(i.title).font(.system(size: 15, weight: .semibold)).lineLimit(2)
                        if !snapTime(i).isEmpty {
                            Text(snapTime(i)).font(.system(size: 11, design: .monospaced)).foregroundStyle(Color.secondary)
                        }
                    }
                }
            }
            if let m = store.message { Text(m).font(.system(size: 12)).foregroundStyle(Color(rgb: c.bad)) }
        } header: {
            Text("今天 · \(s.items.count)")
        }
    }
}

struct GoalSection: View {
    @Environment(WatchStore.self) private var store

    var body: some View {
        let s = store.snapshot
        let c = s.colors
        Section {
            if s.goals.isEmpty { Text("还没有目标").foregroundStyle(Color.secondary) }
            ForEach(s.goals) { g in
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text(g.title).font(.system(size: 15, weight: .semibold))
                        Spacer()
                        Text("\(SnapFmt.number(g.done))/\(SnapFmt.number(g.target))")
                            .font(.system(size: 12, design: .monospaced)).foregroundStyle(g.behind ? Color(rgb: c.bad) : Color.secondary)
                    }
                    SnapProgress(value: g.percent, tint: Color(rgb: g.work ? c.work : c.life), track: Color.white.opacity(0.2))
                    Button("打卡 +1") { store.perform("checkin", id: g.id) }
                        .font(.system(size: 13, weight: .semibold)).disabled(store.busy)
                }
            }
        } header: {
            Text("目标")
        }
    }
}
