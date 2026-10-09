import Foundation
import WatchConnectivity

/// Phone side of the link to the watch. The watch asks for things to be done
/// (finish a task, check in); the phone does them against the server and sends back
/// the new snapshot.
final class WatchBridge: NSObject, WCSessionDelegate, @unchecked Sendable {
    static let shared = WatchBridge()
    private var wc: WCSession? { WCSession.isSupported() ? WCSession.default : nil }
    /// Set by the app once it is connected.
    var perform: (@Sendable (_ action: String, _ id: String) async -> Snapshot?)?

    func start() {
        guard let wc else { return }
        wc.delegate = self
        if wc.activationState != .activated { wc.activate() }
    }

    private let lock = NSLock()
    private var latest: Snapshot?

    /// Remembers the newest snapshot and sends it as soon as the link is ready. Activation takes a
    /// moment after launch, and the first snapshot is usually built before that.
    func push(_ snap: Snapshot) {
        lock.lock(); latest = snap; lock.unlock()
        flush()
    }

    private func flush() {
        guard let wc else { return }
        lock.lock(); let snap = latest; lock.unlock()
        guard wc.activationState == .activated, wc.isPaired, wc.isWatchAppInstalled,
              let snap, let data = SnapshotStore.encode(snap) else { return }
        do { try wc.updateApplicationContext(["snapshot": data]) } catch {
        }
    }

    func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        flush()
    }

    func sessionWatchStateDidChange(_ session: WCSession) { flush() }
    func sessionReachabilityDidChange(_ session: WCSession) { flush() }
    func sessionDidBecomeInactive(_ session: WCSession) {}
    func sessionDidDeactivate(_ session: WCSession) { session.activate() }

    func session(_ session: WCSession, didReceiveMessage message: [String: Any], replyHandler: @escaping ([String: Any]) -> Void) {
        guard let action = message["action"] as? String, let id = message["id"] as? String, let perform else {
            replyHandler(["error": "手机上还没有连接服务器"])
            return
        }
        // WatchConnectivity hands over a reply closure that is safe to call from any thread.
        nonisolated(unsafe) let reply = replyHandler
        Task {
            if let snap = await perform(action, id), let data = SnapshotStore.encode(snap) {
                reply(["snapshot": data])
            } else {
                reply(["error": "没有完成,稍后再试"])
            }
        }
    }
}
