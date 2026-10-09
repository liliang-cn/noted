import SwiftUI

struct ModeSwitch: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    var mini = false

    var body: some View {
        HStack(spacing: mini ? 2 : 4) {
            ForEach(Mode.allCases) { m in
                let on = session.mode == m
                Button { session.setMode(m) } label: {
                    HStack(spacing: 6) {
                        if m != .all && !mini {
                            Circle().fill(on ? Color.white : m.accent(p)).frame(width: 7, height: 7)
                        }
                        Text(m.label).font(.system(size: mini ? 12 : 14, weight: .semibold))
                    }
                    .frame(maxWidth: mini ? nil : .infinity).padding(.horizontal, mini ? 12 : 0).frame(height: mini ? 28 : 38)
                    .foregroundStyle(on ? (m == .all ? p.bg : .white) : p.ink2)
                    .background(on ? (m == .all ? p.ink : m.accent(p)) : .clear, in: RoundedRectangle(cornerRadius: mini ? 7 : 9))
                }
            }
        }
        .padding(mini ? 3 : 4)
        .background(p.surface, in: RoundedRectangle(cornerRadius: mini ? 10 : 12))
        .overlay(RoundedRectangle(cornerRadius: mini ? 10 : 12).stroke(p.line, lineWidth: 1))
    }
}

struct Screen<Content: View>: View {
    @Environment(\.palette) private var p
    let eyebrow: String
    let title: String
    var trailing: AnyView = AnyView(EmptyView())
    @ViewBuilder var content: Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .bottom) {
                    VStack(alignment: .leading, spacing: 6) {
                        Eyebrow(text: eyebrow)
                        Text(title).font(.system(size: 30, weight: .bold)).foregroundStyle(p.ink)
                    }
                    Spacer()
                    trailing
                }
                content
            }
            .padding(.horizontal, 16).padding(.top, 12).padding(.bottom, 24)
        }
        .scrollIndicators(.hidden)
        .background(p.bg)
    }
}

struct SpaceTag: View {
    @Environment(\.palette) private var p
    let space: Noted_V1_Space
    var body: some View {
        if space == .work || space == .life {
            HStack(spacing: 5) {
                Circle().fill(space.color(p)).frame(width: 8, height: 8)
                Text(space.label).font(.system(size: 11, weight: .semibold))
            }
            .padding(.horizontal, 8).frame(height: 22)
            .background(space == .work ? p.workBg : p.lifeBg, in: RoundedRectangle(cornerRadius: 6))
            .foregroundStyle(space.color(p))
        }
    }
}

struct ErrorLine: View {
    @Environment(\.palette) private var p
    let text: String?
    var body: some View {
        if let text { Text(text).font(.system(size: 13)).foregroundStyle(p.bad) }
    }
}

enum Fmt {
    static let zh = Locale(identifier: "zh_CN")
    private static let hmFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm"
        return f
    }()
    private static let ymdFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        return f
    }()
    static func ymd(_ d: Date) -> String { ymdFormatter.string(from: d) }
    static func hm(_ d: Date) -> String { hmFormatter.string(from: d) }
    static func md(_ d: Date) -> String { d.formatted(.dateTime.month().day().locale(zh)) }
    static func eyebrow(_ d: Date) -> String { "\(md(d)) · \(d.formatted(.dateTime.weekday(.abbreviated).locale(zh)))" }
    static func money(_ v: Double) -> String { String(format: "%.2f", v) }
    static func signedMoney(_ v: Double) -> String { (v >= 0 ? "+" : "-") + String(format: "%.2f", abs(v)) }
    static func shares(_ v: Double) -> String {
        let s = String(format: "%.4f", v)
        return s.contains(".") ? s.replacingOccurrences(of: "0+$", with: "", options: .regularExpression).replacingOccurrences(of: "\\.$", with: "", options: .regularExpression) : s
    }
    static func relative(_ d: Date) -> String {
        let cal = Calendar.current
        if cal.isDateInToday(d) { return "今天" }
        if cal.isDateInYesterday(d) { return "昨天" }
        let days = cal.dateComponents([.day], from: cal.startOfDay(for: d), to: cal.startOfDay(for: .now)).day ?? 99
        if days > 0 && days < 7 { return d.formatted(.dateTime.weekday(.abbreviated).locale(zh)) }
        return md(d)
    }
    static func duration(from a: Date, to b: Date) -> String? {
        let m = Int(b.timeIntervalSince(a) / 60)
        guard m > 0 else { return nil }
        if m < 60 { return "\(m)分" }
        return m % 60 == 0 ? "\(m / 60)h" : "\(m / 60)h\(m % 60)分"
    }
    static func number(_ v: Double) -> String { v == v.rounded() ? String(Int(v)) : String(format: "%.1f", v) }
}
