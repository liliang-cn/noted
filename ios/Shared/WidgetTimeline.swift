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
        s.plans = [
            .init(id: "p1", title: "减肥", metricName: "体重", unit: "kg", current: 73.2, start: 75, target: 68, percent: 0.26, daysLeft: 60, offTrack: false, dims: [
                .init(id: "g3", title: "游泳", done: 1, target: 2, unit: "次"),
                .init(id: "g4", title: "跑步", done: 3, target: 3, unit: "次"),
                .init(id: "g5", title: "控糖", done: 4, target: 7, unit: "天", behind: true),
            ]),
        ]
        let next = Calendar.current.date(byAdding: .day, value: 6, to: .now)
        s.holdings = [
            .init(id: "h1", symbol: "QQQM", planDay: 15, amount: 500, doneThisMonth: false, next: next, shares: 15, gainPercent: 0.042, streak: 3),
            .init(id: "h2", symbol: "VOO", planDay: 15, amount: 500, doneThisMonth: true, next: next, shares: 14, gainPercent: -0.011, streak: 5),
        ]
        return s
    }
}
