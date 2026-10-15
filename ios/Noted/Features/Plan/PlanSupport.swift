import SwiftUI

// Pieces shared by the 计划 pages (root, project, goal, objective, invest, holding).

// MARK: - formatting

enum PlanFmt {
    private static let ymdParser: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.calendar = Calendar(identifier: .gregorian)
        f.timeZone = .current
        f.dateFormat = "yyyy-MM-dd"
        return f
    }()

    /// "2026-10-08" → that day at midnight.
    static func day(_ s: String) -> Date? { ymdParser.date(from: s) }

    /// A price with two decimals: "27.50".
    static func price(_ v: Double) -> String { String(format: "%.2f", v) }

    /// A number typed by the person ("78,6", "1 200.5").
    static func parse(_ s: String) -> Double? {
        Double(s.replacingOccurrences(of: ",", with: ".").replacingOccurrences(of: " ", with: "").trimmingCharacters(in: .whitespaces))
    }

    /// Up to one decimal, no trailing ".0": 78.6, 80.
    static func value(_ v: Double) -> String {
        let r = (v * 10).rounded() / 10
        return Fmt.number(r)
    }

    /// Markdown reduced to a one-line preview.
    static func preview(_ md: String) -> String {
        var s = md.replacingOccurrences(of: "\n", with: " ")
        for token in ["# ", "## ", "### ", "- [ ] ", "- [x] ", "**", "__", "`", "> ", "* "] {
            s = s.replacingOccurrences(of: token, with: "")
        }
        s = s.replacingOccurrences(of: "\\[([^\\]]*)\\]\\([^)]*\\)", with: "$1", options: .regularExpression)
        return s.trimmingCharacters(in: .whitespaces)
    }
}

extension Noted_V1_GoalPeriod {
    /// 每天 / 每周 / 每月 choices for pickers.
    static let planChoices: [Noted_V1_GoalPeriod] = [.day, .week, .month]
    /// .unspecified counts as weekly.
    var planNormalized: Noted_V1_GoalPeriod { self == .unspecified ? .week : self }
}

extension Noted_V1_Goal {
    /// "每周 3 次", "每天 30 分钟".
    var planRule: String { "\(period.every) \(Fmt.number(target)) \(isMinutes ? "分钟" : unitLabel)" }
    /// The running total's name ("课时"), or 累计.
    var planCounterName: String { counterUnit.isEmpty ? "累计" : counterUnit }
}

// MARK: - holdings

extension Noted_V1_Holding {
    var currencyCode: String { currency.isEmpty ? "USD" : currency }
    var marketValue: Double { position.hasPrice_p ? position.marketValue : position.costBasis }
    var pnl: Double { position.hasPrice_p ? position.unrealizedPnl : 0 }
    /// In percent units (7.8 = 7.8%).
    var pnlPercent: Double { position.hasPrice_p ? position.unrealizedPercent * 100 : 0 }

    /// The next planned 定投 day, nil without a plan.
    func nextDCA(now: Date = .now) -> Date? {
        guard hasPlan else { return nil }
        if position.dca.hasNextTime { return position.dca.nextTime.date }
        let cal = Fmt.calendar
        let day = Int(dcaDay)
        var c = cal.dateComponents([.year, .month], from: now)
        c.day = day
        guard let thisMonth = cal.date(from: c) else { return nil }
        if thisMonth >= now.startOfDay { return thisMonth }
        return cal.date(byAdding: .month, value: 1, to: thisMonth)
    }
}

/// 总市值 / 盈亏 over every holding.
struct InvestTotals {
    var value = 0.0
    var cost = 0.0
    var pnl = 0.0
    var percent: Double { cost > 0 ? pnl / cost * 100 : 0 }
    /// The next 定投 day and how much is planned on it.
    var nextDCA: (date: Date, amount: Double)?

    init(_ holdings: [Noted_V1_Holding], now: Date = .now) {
        for h in holdings {
            value += h.marketValue
            if h.position.hasPrice_p { cost += h.position.costBasis; pnl += h.pnl }
        }
        let planned = holdings.compactMap { h in h.nextDCA(now: now).map { (h, $0) } }
        if let first = planned.map(\.1).min() {
            let amount = planned.filter { $0.1.isSameDay(as: first) }.reduce(0) { $0 + $1.0.dcaAmount }
            nextDCA = (first, amount)
        }
    }
}

/// The white summary card: 总市值, P/L and 下次定投. Tappable on 计划 (opens 投资).
struct InvestSummaryCard: View {
    let holdings: [Noted_V1_Holding]
    var onTap: (() -> Void)?

    var body: some View {
        if let onTap {
            Button(action: onTap) { content }
                .buttonStyle(CardPressStyle())
                .accessibilityIdentifier("invest-summary")
        } else {
            content
        }
    }

    private var content: some View {
        let t = InvestTotals(holdings)
        let tint: Color = t.pnl >= 0 ? .success : .danger
        return VStack(alignment: .leading, spacing: 0) {
            Text("总市值 · USD").font(.footnote).foregroundStyle(.secondaryText)
            Text(Fmt.money(t.value))
                .font(.system(size: 34, weight: .bold, design: .monospaced))
                .minimumScaleFactor(0.6).lineLimit(1)
                .padding(.top, 2)
            Text("\(Fmt.money(t.pnl, signed: true))　\(Fmt.percent(t.percent, signed: true))")
                .font(.system(size: 15, design: .monospaced))
                .foregroundStyle(tint)
                .padding(.top, 4)
            Divider().padding(.top, 12)
            HStack {
                Text("下次定投").foregroundStyle(.secondaryText)
                Spacer()
                if let n = t.nextDCA {
                    Text("\(Fmt.mdw(n.date)) · \(Fmt.money(n.amount, cents: n.amount != n.amount.rounded()))").monospacedDigit()
                } else {
                    Text("已暂停").foregroundStyle(.secondaryText)
                }
            }
            .font(.subheadline)
            .padding(.top, 11)
        }
        .foregroundStyle(.primary)
        .card()
        .contentShape(RoundedRectangle(cornerRadius: 14))
    }
}

/// A holding: symbol, "12 股 · 均价 205.00", price and P/L, and the colored percent pill.
struct HoldingRow: View {
    @Environment(Router.self) private var router
    let holding: Noted_V1_Holding

    var body: some View {
        let h = holding
        let up = h.pnl >= 0
        Button { router.push(.holding(h.id)) } label: {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 1) {
                    Text(h.symbol).font(.body.weight(.semibold))
                    Text("\(Fmt.shares(h.position.shares)) 股 · 均价 \(PlanFmt.price(h.position.avgCost))")
                        .font(.footnote).foregroundStyle(.secondaryText).monospacedDigit()
                }
                Spacer(minLength: 8)
                VStack(alignment: .trailing, spacing: 1) {
                    Text(h.price.map(PlanFmt.price) ?? "—").font(.system(size: 16, design: .monospaced))
                    if h.position.hasPrice_p {
                        Text(Fmt.money(h.pnl, currency: h.currencyCode, signed: true))
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundStyle(up ? Color.success : Color.danger)
                    }
                }
                Text(h.position.hasPrice_p ? Fmt.percent(h.pnlPercent, signed: true) : "—")
                    .font(.system(size: 13, weight: .bold, design: .monospaced))
                    .foregroundStyle(.white)
                    .lineLimit(1)
                    .padding(.horizontal, 8)
                    .frame(minWidth: 76, minHeight: 28)
                    .background(RoundedRectangle(cornerRadius: 7).fill(h.position.hasPrice_p ? (up ? Color.success : Color.danger) : Color.tabInactive))
            }
            .padding(.vertical, 4)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("holding-\(h.id)")
    }
}

// MARK: - detail page pieces

/// The top of a detail page: tags, a large title and a gray line, on the page background.
struct PlanDetailHeader<Tags: View>: View {
    let title: String
    var subtitle: String = ""
    @ViewBuilder var tags: Tags

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) { tags }
            Text(title).font(.largeTitle.weight(.bold)).fixedSize(horizontal: false, vertical: true)
            if !subtitle.isEmpty {
                Text(subtitle).font(.subheadline).foregroundStyle(.secondaryText).fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .listRowBackground(Color.clear)
        .listRowInsets(EdgeInsets(top: 0, leading: 4, bottom: 4, trailing: 4))
    }
}

/// A small project chip that opens the project.
struct PlanProjectChip: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    let projectID: String

    var body: some View {
        if let p = data.project(projectID) {
            Button { router.push(.project(p.id)) } label: {
                HStack(spacing: 4) {
                    Image(systemName: "folder").font(.system(size: 11, weight: .semibold))
                    Text(p.title).font(.footnote.weight(.semibold)).lineLimit(1)
                }
                .padding(.horizontal, 9).frame(height: 24)
                .foregroundStyle(.primary)
                .background(.fillTrack, in: Capsule())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("项目 \(p.title)")
        }
    }
}

/// A white tile with a gray caption and a big value (连续达成, 本月).
struct PlanStatTile<Value: View>: View {
    let title: String
    @ViewBuilder var value: Value
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.footnote).foregroundStyle(.secondaryText)
            value
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14)
        .background(.white, in: RoundedRectangle(cornerRadius: 14))
        .accessibilityElement(children: .combine)
    }
}

/// One day of the last-7-days strip.
struct PlanDayDot: Identifiable {
    let date: Date
    let on: Bool
    var id: Date { date }
}

/// 日 一 二 … 今: a check in a green dot for days with a check-in.
struct PlanSevenDayDots: View {
    let days: [PlanDayDot]
    var now: Date = .now

    var body: some View {
        HStack(spacing: 0) {
            ForEach(days) { d in
                let today = d.date.isSameDay(as: now)
                VStack(spacing: 6) {
                    ZStack {
                        Circle().fill(d.on ? Color.success : Color.fillTrack)
                        if d.on { Image(systemName: "checkmark").font(.system(size: 10, weight: .bold)).foregroundStyle(.white) }
                    }
                    .frame(width: 22, height: 22)
                    .overlay {
                        if today { Circle().strokeBorder(.tint, lineWidth: 1.6).frame(width: 28, height: 28) }
                    }
                    .frame(width: 28, height: 28)
                    Text(today ? "今" : Fmt.weekdayShort(d.date)).font(.caption).foregroundStyle(.secondaryText)
                }
                .frame(maxWidth: .infinity)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("\(Fmt.mdw(d.date)) \(d.on ? "已打卡" : "未打卡")")
            }
        }
    }

    /// The last 7 days ending today, from the progress's recent totals.
    static func days(from recent: [[Noted_V1_DayTotal]], now: Date = .now) -> [PlanDayDot] {
        var amounts: [String: Double] = [:]
        for list in recent { for d in list { amounts[d.date, default: 0] += d.amount } }
        return (0..<7).reversed().map { i in
            let day = now.startOfDay.adding(days: -i)
            return PlanDayDot(date: day, on: (amounts[Fmt.ymd(day)] ?? 0) > 0)
        }
    }
}

/// The big ring card: ring with "c/t" and the period, the main button, the 7-day strip.
struct PlanRingCard: View {
    let fraction: Double
    let done: Bool
    let value: String
    let total: String
    let caption: String
    let buttonTitle: String
    let buttonIcon: String
    let days: [PlanDayDot]
    let action: () -> Void
    @State private var bump = 0

    var body: some View {
        VStack(spacing: 0) {
            ProgressRing(value: fraction, lineWidth: 14, tint: done ? .success : nil, animateOnAppear: true) {
                VStack(spacing: 4) {
                    HStack(alignment: .firstTextBaseline, spacing: 0) {
                        Text(value).font(.system(size: 30, weight: .bold, design: .monospaced))
                            .contentTransition(.numericText())
                        Text("/\(total)").font(.system(size: 20, weight: .medium, design: .monospaced)).foregroundStyle(.secondaryText)
                    }
                    Text(caption).font(.footnote).foregroundStyle(.secondaryText)
                }
            }
            .frame(width: 168, height: 168)
            .padding(.top, 6)
            Button {
                bump += 1
                action()
            } label: {
                Label(buttonTitle, systemImage: buttonIcon)
            }
            .buttonStyle(.wide(done ? .success : .filled))
            .sensoryFeedback(.increase, trigger: bump)
            .padding(.top, 18)
            .accessibilityIdentifier("goal-checkin")
            PlanSevenDayDots(days: days).padding(.top, 16)
        }
        .frame(maxWidth: .infinity)
        .padding(16)
        .background(.white, in: RoundedRectangle(cornerRadius: 14))
        .animation(.snappy, value: value)
    }
}

/// A one-field number sheet (最新价, 定投金额, 记一次体重): big centered field, a caption, 取消 / done.
struct PlanNumberSheet: View {
    @Environment(\.dismiss) private var dismiss
    let title: String
    var caption: String = ""
    var doneTitle = "保存"
    var initial: Double?
    var allowsZero = false
    let onSave: (Double) -> Void
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextField("0", text: $text)
                        .keyboardType(.decimalPad)
                        .font(.system(size: 28, weight: .semibold, design: .monospaced))
                        .multilineTextAlignment(.center)
                        .focused($focused)
                        .accessibilityLabel(title)
                } footer: {
                    if !caption.isEmpty {
                        Text(caption).frame(maxWidth: .infinity).multilineTextAlignment(.center)
                    }
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(doneTitle) {
                        guard let v = value else { return }
                        onSave(v)
                        dismiss()
                    }
                    .fontWeight(.semibold)
                    .disabled(value == nil)
                    .accessibilityIdentifier("number-sheet-save")
                }
            }
        }
        .presentationDetents([.height(230)])
        .onAppear {
            if let initial, text.isEmpty { text = initial == initial.rounded() ? Fmt.number(initial) : String(format: "%g", initial) }
            focused = true
        }
    }

    private var value: Double? {
        guard let v = PlanFmt.parse(text), v.isFinite else { return nil }
        return allowsZero ? (v >= 0 ? v : nil) : (v > 0 ? v : nil)
    }
}

/// "+ 添加维度"-style centered accent row.
struct PlanAddRow: View {
    let title: String
    var systemImage = "plus"
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            Label(title, systemImage: systemImage)
                .frame(maxWidth: .infinity)
                .foregroundStyle(.tint)
                .contentShape(Rectangle())
        }
        .buttonStyle(.borderless)
    }
}
