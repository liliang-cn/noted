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

// MARK: plans (大目标)

func snapMetric(_ p: Snapshot.Plan) -> String {
    guard !p.unit.isEmpty else { return "" }
    return "\(p.current.map(SnapFmt.number) ?? "—") \(p.unit)"
}

func snapPercent(_ v: Double) -> String { "\(Int((min(max(v, 0), 1) * 100).rounded()))%" }

struct PlanSmallView: View {
    let s: Snapshot
    var body: some View {
        let c = s.colors
        if let p = s.featuredPlan {
            VStack(alignment: .leading, spacing: 5) {
                HStack {
                    Text(p.title).font(.system(size: 13, weight: .semibold)).foregroundStyle(Color(rgb: c.ink2)).lineLimit(1)
                    Spacer(minLength: 2)
                    if p.offTrack { Circle().fill(Color(rgb: c.bad)).frame(width: 7, height: 7) }
                }
                if p.unit.isEmpty {
                    Text(snapPercent(p.percent)).font(.system(size: 30, weight: .bold)).foregroundStyle(Color(rgb: c.ink))
                } else {
                    Text(p.current.map(SnapFmt.number) ?? "—").font(.system(size: 30, weight: .bold)).foregroundStyle(Color(rgb: c.ink))
                    Text("\(p.unit) → \(SnapFmt.number(p.target))").font(.system(size: 11, design: .monospaced)).foregroundStyle(Color(rgb: c.ink2))
                }
                SnapProgress(value: p.percent, tint: Color(rgb: p.work ? c.work : c.life), track: Color(rgb: c.line))
                Spacer(minLength: 0)
                if !p.dims.isEmpty {
                    Text("本期 \(p.achieved)/\(p.dims.count) 项达标").font(.system(size: 11)).foregroundStyle(Color(rgb: c.ink2))
                }
                if let d = p.daysLeft { Text(d >= 0 ? "还有 \(d) 天" : "已过期").font(.system(size: 11)).foregroundStyle(Color(rgb: c.ink2)) }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        } else {
            Text("还没有目标计划").font(.system(size: 13)).foregroundStyle(Color(rgb: c.ink2)).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        }
    }
}

struct PlanMediumView: View {
    let s: Snapshot
    var body: some View {
        let c = s.colors
        if let p = s.featuredPlan {
            HStack(alignment: .top, spacing: 14) {
                VStack(alignment: .leading, spacing: 5) {
                    Text(p.title).font(.system(size: 13, weight: .semibold)).foregroundStyle(Color(rgb: c.ink2)).lineLimit(1)
                    Text(p.unit.isEmpty ? snapPercent(p.percent) : (p.current.map(SnapFmt.number) ?? "—"))
                        .font(.system(size: 32, weight: .bold)).foregroundStyle(Color(rgb: c.ink))
                    if !p.unit.isEmpty {
                        Text("\(p.unit) → \(SnapFmt.number(p.target))").font(.system(size: 11, design: .monospaced)).foregroundStyle(Color(rgb: c.ink2))
                    }
                    SnapProgress(value: p.percent, tint: Color(rgb: p.work ? c.work : c.life), track: Color(rgb: c.line))
                    Spacer(minLength: 0)
                    HStack(spacing: 6) {
                        if let d = p.daysLeft { Text(d >= 0 ? "还有 \(d) 天" : "已过期").font(.system(size: 11)).foregroundStyle(Color(rgb: c.ink2)) }
                        if p.offTrack { Text("落后进度").font(.system(size: 11, weight: .semibold)).foregroundStyle(Color(rgb: c.bad)) }
                    }
                }
                .frame(width: 120, alignment: .leading)
                VStack(alignment: .leading, spacing: 8) {
                    Text("本期 \(p.achieved)/\(p.dims.count)").font(.system(size: 11, weight: .medium, design: .monospaced)).foregroundStyle(Color(rgb: c.ink2))
                    ForEach(p.dims.prefix(4)) { g in
                        VStack(alignment: .leading, spacing: 3) {
                            HStack {
                                Text(g.title).font(.system(size: 12, weight: .semibold)).foregroundStyle(Color(rgb: c.ink)).lineLimit(1)
                                Spacer(minLength: 2)
                                Text("\(SnapFmt.number(g.done))/\(SnapFmt.number(g.target))")
                                    .font(.system(size: 10, design: .monospaced)).foregroundStyle(g.behind ? Color(rgb: c.bad) : Color(rgb: c.ink2))
                            }
                            SnapProgress(value: g.percent, tint: Color(rgb: p.work ? c.work : c.life), track: Color(rgb: c.line))
                        }
                    }
                    Spacer(minLength: 0)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        } else {
            Text("还没有目标计划").font(.system(size: 13)).foregroundStyle(Color(rgb: c.ink2)).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        }
    }
}

struct PlanRectangularView: View {
    let s: Snapshot
    var body: some View {
        if let p = s.featuredPlan {
            VStack(alignment: .leading, spacing: 2) {
                Text("\(p.title) · \(snapPercent(p.percent))").font(.system(size: 11, weight: .medium, design: .monospaced))
                Text(p.unit.isEmpty ? "本期 \(p.achieved)/\(p.dims.count) 项达标" : "\(snapMetric(p)) → \(SnapFmt.number(p.target))")
                    .font(.system(size: 14, weight: .semibold)).lineLimit(1)
                ProgressView(value: min(max(p.percent, 0), 1))
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            Text("还没有目标计划").font(.system(size: 14, weight: .semibold)).frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

struct PlanCircularView: View {
    let s: Snapshot
    var body: some View {
        if let p = s.featuredPlan {
            Gauge(value: min(max(p.percent, 0), 1)) {
                Text(p.title.prefix(2))
            } currentValueLabel: {
                Text(snapPercent(p.percent).replacingOccurrences(of: "%", with: "")).font(.system(size: 14, weight: .bold))
            }
            .gaugeStyle(.accessoryCircularCapacity)
        } else {
            Image(systemName: "target")
        }
    }
}

// MARK: investing (标的)

/// "还有 6 天", "今天" or "明天" for the next plan day.
func snapUntil(_ d: Date?, now: Date) -> String {
    guard let d else { return "" }
    let cal = Calendar.current
    let n = cal.dateComponents([.day], from: cal.startOfDay(for: now), to: cal.startOfDay(for: d)).day ?? 0
    switch n {
    case ...0: return "今天"
    case 1: return "明天"
    default: return "\(n) 天后"
    }
}

struct HoldingChip: View {
    let h: Snapshot.Holding
    let c: Snapshot.Colors
    var body: some View {
        Text(h.doneThisMonth ? "已投" : "待投").font(.system(size: 10, weight: .semibold)).padding(.horizontal, 6).frame(height: 18)
            .background(h.doneThisMonth ? Color(rgb: c.ink2).opacity(0.15) : Color(rgb: c.bad).opacity(0.15), in: RoundedRectangle(cornerRadius: 5))
            .foregroundStyle(h.doneThisMonth ? Color(rgb: c.ink2) : Color(rgb: c.bad))
    }
}

struct InvestSmallView: View {
    let s: Snapshot
    var now: Date = .now
    var body: some View {
        let c = s.colors
        let todo = s.planned.filter(\.todo)
        VStack(alignment: .leading, spacing: 6) {
            Text("定投").font(.system(size: 11, weight: .medium, design: .monospaced)).foregroundStyle(Color(rgb: c.ink2))
            if s.planned.isEmpty {
                Text("没有定投计划").font(.system(size: 13)).foregroundStyle(Color(rgb: c.ink2))
            } else {
                Text(todo.isEmpty ? "本月已完成" : "\(todo.count) 个待投").font(.system(size: 22, weight: .bold)).foregroundStyle(Color(rgb: c.ink))
                Spacer(minLength: 0)
                ForEach(s.planned.prefix(3)) { h in
                    HStack {
                        Text(h.symbol).font(.system(size: 12, weight: .semibold, design: .monospaced)).foregroundStyle(Color(rgb: c.ink))
                        Spacer(minLength: 2)
                        HoldingChip(h: h, c: c)
                    }
                }
                if let next = s.planned.first?.next { Text("\(snapUntil(next, now: now)) · \(Calendar.current.component(.day, from: next)) 号").font(.system(size: 11)).foregroundStyle(Color(rgb: c.ink2)) }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
    }
}

struct InvestMediumView: View {
    let s: Snapshot
    var now: Date = .now
    var body: some View {
        let c = s.colors
        VStack(alignment: .leading, spacing: 7) {
            Text("定投 · 每月").font(.system(size: 11, weight: .medium, design: .monospaced)).foregroundStyle(Color(rgb: c.ink2))
            if s.planned.isEmpty { Text("没有定投计划").font(.system(size: 13)).foregroundStyle(Color(rgb: c.ink2)) }
            ForEach(s.planned.prefix(4)) { h in
                HStack(spacing: 8) {
                    Text(h.symbol).font(.system(size: 14, weight: .bold, design: .monospaced)).foregroundStyle(Color(rgb: c.ink)).frame(width: 64, alignment: .leading)
                    Text("\(h.planDay) 号\(h.amount > 0 ? " · \(SnapFmt.number(h.amount)) \(h.currency)" : "")")
                        .font(.system(size: 11, design: .monospaced)).foregroundStyle(Color(rgb: c.ink2)).lineLimit(1)
                    Spacer(minLength: 2)
                    if h.streak > 0 { Text("连续 \(h.streak) 月").font(.system(size: 10)).foregroundStyle(Color(rgb: c.ink2)) }
                    HoldingChip(h: h, c: c)
                }
            }
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct InvestRectangularView: View {
    let s: Snapshot
    var now: Date = .now
    var body: some View {
        let todo = s.planned.filter(\.todo)
        VStack(alignment: .leading, spacing: 2) {
            if s.planned.isEmpty {
                Text("没有定投计划").font(.system(size: 14, weight: .semibold))
            } else if let h = todo.first {
                Text("定投 · 本月待投 \(todo.count)").font(.system(size: 11, weight: .medium, design: .monospaced))
                Text(todo.prefix(3).map(\.symbol).joined(separator: " ")).font(.system(size: 14, weight: .semibold)).lineLimit(1)
                Text("\(snapUntil(h.next, now: now)) \(h.planDay) 号").font(.system(size: 11))
            } else {
                Text("定投 · 本月已完成").font(.system(size: 11, weight: .medium, design: .monospaced))
                Text(s.planned.prefix(3).map(\.symbol).joined(separator: " ")).font(.system(size: 14, weight: .semibold)).lineLimit(1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct InvestCircularView: View {
    let s: Snapshot
    var body: some View {
        let todo = s.planned.filter(\.todo).count
        VStack(spacing: 0) {
            Text(todo == 0 ? "✓" : "\(todo)").font(.system(size: 20, weight: .bold))
            Text("定投").font(.system(size: 9))
        }
    }
}
