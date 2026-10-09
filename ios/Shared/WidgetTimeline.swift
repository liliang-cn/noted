import SwiftUI
import WidgetKit

struct Entry: TimelineEntry {
    let date: Date
    let snapshot: Snapshot
}

struct Provider: TimelineProvider {
    /// The moments at which what the widget shows can change: now, each upcoming
    /// item, and at least every 30 minutes.
    static func entries(for snap: Snapshot, now: Date) -> [Entry] {
        var dates = [now]
        dates += snap.items.compactMap(\.time).filter { $0 > now }.sorted().prefix(8)
        dates.append(now.addingTimeInterval(30 * 60))
        return Set(dates).sorted().map { Entry(date: $0, snapshot: snap) }
    }

    func placeholder(in context: Context) -> Entry { Entry(date: .now, snapshot: Sample.today) }

    func getSnapshot(in context: Context, completion: @escaping (Entry) -> Void) {
        completion(Entry(date: .now, snapshot: context.isPreview ? Sample.today : (SnapshotStore.load() ?? Snapshot())))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<Entry>) -> Void) {
        let now = Date()
        let entries = Self.entries(for: SnapshotStore.load() ?? Snapshot(), now: now)
        completion(Timeline(entries: entries, policy: .after(now.addingTimeInterval(30 * 60))))
    }
}

enum Sample {
    static var today: Snapshot {
        var s = Snapshot()
        s.pro = true
        let cal = Calendar.current
        func at(_ h: Int, _ m: Int) -> Date { cal.date(bySettingHour: h, minute: m, second: 0, of: .now) ?? .now }
        s.items = [
            .init(id: "1", title: "团队站会", time: at(9, 0), work: true),
            .init(id: "2", title: "牙医复诊", time: at(14, 0)),
            .init(id: "3", title: "交房租", time: at(18, 0), isTask: true),
        ]
        s.goals = [.init(id: "g1", title: "运动", done: 2, target: 3, unit: "次"), .init(id: "g2", title: "德语", done: 95, target: 140, unit: "分钟", behind: true)]
        return s
    }
}
