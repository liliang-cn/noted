import SwiftUI

extension Color {
    init(rgb: UInt32) {
        self.init(red: Double((rgb >> 16) & 0xFF) / 255, green: Double((rgb >> 8) & 0xFF) / 255, blue: Double(rgb & 0xFF) / 255)
    }
}

enum SnapFmt {
    static func hm(_ d: Date) -> String {
        let f = DateFormatter(); f.dateFormat = "HH:mm"; return f.string(from: d)
    }
    static func number(_ v: Double) -> String { v == v.rounded() ? String(Int(v)) : String(format: "%.1f", v) }
}

/// The text a row shows in the time column: a clock time, or nothing for an undated task.
func snapTime(_ i: Snapshot.Item) -> String {
    if i.allDay { return "全天" }
    guard let t = i.time else { return "" }
    return SnapFmt.hm(t)
}

struct SnapMark: View {
    let item: Snapshot.Item
    let c: Snapshot.Colors
    var body: some View {
        if item.isTask {
            RoundedRectangle(cornerRadius: 3)
                .strokeBorder(item.overdue ? Color(rgb: c.bad) : Color(rgb: c.ink2), lineWidth: 1.5).frame(width: 11, height: 11)
        } else {
            Circle().fill(Color(rgb: item.work ? c.work : c.life)).frame(width: 7, height: 7)
        }
    }
}

struct SnapProgress: View {
    let value: Double
    let tint: Color
    let track: Color
    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(track)
                Capsule().fill(tint).frame(width: g.size.width * min(max(value, 0), 1))
            }
        }.frame(height: 5)
    }
}

// MARK: home screen

struct SmallSnapshotView: View {
    let s: Snapshot
    var now: Date = .now
    var body: some View {
        let c = s.colors
        VStack(alignment: .leading, spacing: 6) {
            Text("今天").font(.system(size: 11, weight: .medium, design: .monospaced)).foregroundStyle(Color(rgb: c.ink2))
            Text("\(s.items.count)").font(.system(size: 34, weight: .bold)).foregroundStyle(Color(rgb: c.ink))
            Spacer(minLength: 0)
            if let n = s.next(now: now) {
                Text(snapTime(n)).font(.system(size: 11, weight: .medium, design: .monospaced)).foregroundStyle(Color(rgb: c.accent))
                Text(n.title).font(.system(size: 14, weight: .semibold)).foregroundStyle(Color(rgb: c.ink)).lineLimit(2)
            } else {
                Text("没有安排").font(.system(size: 13)).foregroundStyle(Color(rgb: c.ink2))
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
    }
}

struct MediumSnapshotView: View {
    let s: Snapshot
    var body: some View {
        let c = s.colors
        HStack(alignment: .top, spacing: 14) {
            VStack(alignment: .leading, spacing: 7) {
                Text("今天 · \(s.items.count)").font(.system(size: 11, weight: .medium, design: .monospaced)).foregroundStyle(Color(rgb: c.ink2))
                if s.items.isEmpty {
                    Text("没有安排").font(.system(size: 13)).foregroundStyle(Color(rgb: c.ink2))
                }
                ForEach(s.items.prefix(4)) { i in
                    HStack(spacing: 8) {
                        Text(snapTime(i)).font(.system(size: 11, weight: .medium, design: .monospaced))
                            .foregroundStyle(Color(rgb: c.ink2)).frame(width: 38, alignment: .leading)
                        SnapMark(item: i, c: c)
                        Text(i.title).font(.system(size: 13, weight: .medium)).foregroundStyle(Color(rgb: c.ink)).lineLimit(1)
                    }
                }
                Spacer(minLength: 0)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if !s.goals.isEmpty {
                VStack(alignment: .leading, spacing: 9) {
                    Text("目标").font(.system(size: 11, weight: .medium, design: .monospaced)).foregroundStyle(Color(rgb: c.ink2))
                    ForEach(s.goals.prefix(3)) { g in
                        VStack(alignment: .leading, spacing: 3) {
                            HStack {
                                Text(g.title).font(.system(size: 12, weight: .semibold)).foregroundStyle(Color(rgb: c.ink)).lineLimit(1)
                                Spacer(minLength: 2)
                                Text("\(SnapFmt.number(g.done))/\(SnapFmt.number(g.target))")
                                    .font(.system(size: 10, design: .monospaced)).foregroundStyle(g.behind ? Color(rgb: c.bad) : Color(rgb: c.ink2))
                            }
                            SnapProgress(value: g.percent, tint: Color(rgb: g.work ? c.work : c.life), track: Color(rgb: c.line))
                        }
                    }
                    Spacer(minLength: 0)
                }
                .frame(width: 130)
            }
        }
    }
}

// MARK: lock screen and watch face

struct RectangularSnapshotView: View {
    let s: Snapshot
    var now: Date = .now
    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            if let n = s.next(now: now) {
                Text("\(snapTime(n)) · 今天 \(s.items.count)").font(.system(size: 11, weight: .medium, design: .monospaced))
                Text(n.title).font(.system(size: 14, weight: .semibold)).lineLimit(2)
            } else {
                Text("今天没有安排").font(.system(size: 14, weight: .semibold))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct CircularSnapshotView: View {
    let s: Snapshot
    var body: some View {
        if let g = s.goals.first {
            Gauge(value: g.percent) {
                Text(g.title.prefix(2))
            } currentValueLabel: {
                Text(SnapFmt.number(g.done)).font(.system(size: 14, weight: .bold))
            }
            .gaugeStyle(.accessoryCircularCapacity)
        } else {
            VStack(spacing: 0) {
                Text("\(s.items.count)").font(.system(size: 20, weight: .bold))
                Text("今天").font(.system(size: 9))
            }
        }
    }
}

struct InlineSnapshotView: View {
    let s: Snapshot
    var now: Date = .now
    var body: some View {
        if let n = s.next(now: now) { Text("\(snapTime(n)) \(n.title)") } else { Text("今天没有安排") }
    }
}

struct LockedView: View {
    let c: Snapshot.Colors
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Image(systemName: "lock.fill").font(.system(size: 14))
            Text("Noted Pro").font(.system(size: 13, weight: .semibold))
            Text("订阅后显示小组件").font(.system(size: 11))
        }
        .foregroundStyle(Color(rgb: c.ink2))
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
    }
}
