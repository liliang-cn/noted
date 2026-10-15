import SwiftUI

/// 标的: price (tap 更新价格), the position grid, 定投 (switch, day, amount, next), the trades
/// (swipe to delete), and 记一笔买入 / 卖出 at the bottom.
struct HoldingDetailView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let holdingID: String

    @State private var editPrice = false
    @State private var editAmount = false
    @State private var trade: TradeSide?
    @State private var askDelete = false
    @State private var loaded = false

    enum TradeSide: String, Identifiable {
        case buy, sell
        var id: String { rawValue }
        var label: String { self == .buy ? "买入" : "卖出" }
    }

    static let dcaDays = [1, 5, 10, 15, 20, 25]

    var body: some View {
        Group {
            if let h = data.holding(holdingID) {
                content(h)
            } else if loaded {
                List { Section { EmptyState(text: "标的已删除") } }
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle(data.holding(holdingID)?.symbol ?? "标的")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            await data.loadTrades(holdingID)
            loaded = true
        }
    }

    private func content(_ h: Noted_V1_Holding) -> some View {
        let trades = data.trades[holdingID] ?? []
        return List {
            Section { header(h) }
            Section { positionGrid(h).planCardRow(bottom: 0) }
            Section { dcaRows(h) } header: { Text("定投") }
            Section {
                if trades.isEmpty { EmptyState(text: "没有交易") }
                ForEach(trades, id: \.id) { t in
                    TradeRow(trade: t, currency: h.currencyCode)
                        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                            Button(role: .destructive) {
                                Task { if await data.deleteTrade(t.id, holdingID: holdingID) { toaster.show("已删除", systemImage: "trash") } }
                            } label: { Label("删除", systemImage: "trash") }
                        }
                }
            } header: { Text("交易记录") }
        }
        .listStyle(.insetGrouped)
        .refreshable {
            await data.refreshHoldings()
            await data.loadTrades(holdingID)
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            BottomBar {
                Button { trade = .buy } label: { Label("记一笔买入", systemImage: "plus") }
                    .buttonStyle(.wide(.success))
                    .accessibilityIdentifier("holding-buy")
                Button("卖出") { trade = .sell }
                    .buttonStyle(.wide(.danger))
                    .disabled(h.position.shares <= 0)
                    .accessibilityIdentifier("holding-sell")
            }
        }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Button(role: .destructive) { askDelete = true } label: { Label("删除标的", systemImage: "trash") }
                } label: { Image(systemName: "ellipsis.circle") }
                .accessibilityLabel("更多")
                .accessibilityIdentifier("holding-more")
            }
        }
        .confirmationDialog("删除 \(h.symbol)？", isPresented: $askDelete, titleVisibility: .visible) {
            Button("删除标的", role: .destructive) {
                Task {
                    if await data.deleteHolding(holdingID) {
                        router.pop()
                        toaster.show("已删除", systemImage: "trash")
                    }
                }
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text("交易记录会一起删除")
        }
        .sheet(isPresented: $editPrice) {
            PlanNumberSheet(title: "\(h.symbol) 最新价", caption: h.currencyCode, doneTitle: "更新", initial: h.price) { v in
                Task {
                    var x = h
                    x.lastPrice = v
                    if await data.updateHolding(x, fields: ["last_price"]) != nil { toaster.show("已更新价格") }
                }
            }
        }
        .sheet(isPresented: $editAmount) {
            PlanNumberSheet(title: "定投金额", caption: "\(h.currencyCode) · 每月 \(h.dcaDay) 号", initial: h.dcaAmount > 0 ? h.dcaAmount : nil) { v in
                Task {
                    var x = h
                    x.dcaAmount = v
                    if await data.updateHolding(x, fields: ["dca_amount"]) != nil { toaster.show("已更新定投金额") }
                }
            }
        }
        .sheet(item: $trade) { side in
            TradeSheet(holding: h, side: side) { side, shares, price, fee, date in
                Task {
                    if await data.recordTrade(holdingID, side: side.rawValue, shares: shares, price: price, fee: fee, at: date) != nil {
                        toaster.show(side == .buy ? "已记录买入" : "已记录卖出")
                    }
                }
            }
        }
    }

    // MARK: header and grid

    private func header(_ h: Noted_V1_Holding) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            if !h.name.isEmpty && h.name != h.symbol {
                Text(h.name).font(.subheadline).foregroundStyle(.secondaryText)
            }
            Text(h.symbol).font(.largeTitle.weight(.bold))
            HStack(spacing: 8) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(h.price.map(PlanFmt.price) ?? "—").font(.system(size: 34, weight: .semibold, design: .monospaced))
                    Text(h.currencyCode).foregroundStyle(.secondaryText)
                }
                .accessibilityElement(children: .combine)
                Spacer()
                Button { editPrice = true } label: {
                    Label(h.price == nil ? "设置价格" : "更新价格", systemImage: "pencil")
                }
                .buttonStyle(PillButtonStyle(accent: true))
                .accessibilityIdentifier("holding-update-price")
            }
            if h.hasLastPriceTime {
                Text("更新于 \(Fmt.stamp(h.lastPriceTime.date))").font(.caption).foregroundStyle(.secondaryText)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .listRowBackground(Color.clear)
        .listRowInsets(EdgeInsets(top: 0, leading: 4, bottom: 4, trailing: 4))
    }

    private func positionGrid(_ h: Noted_V1_Holding) -> some View {
        let p = h.position
        let cur = h.currencyCode
        let tint: Color? = p.hasPrice_p ? (h.pnl >= 0 ? .success : .danger) : nil
        return Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 14) {
            GridRow {
                cell("持仓", "\(Fmt.shares(p.shares)) 股")
                cell("均价", PlanFmt.price(p.avgCost))
            }
            GridRow {
                cell("市值", p.hasPrice_p ? Fmt.money(p.marketValue, currency: cur) : "—")
                cell("成本", Fmt.money(p.costBasis, currency: cur))
            }
            GridRow {
                cell("浮动盈亏", p.hasPrice_p ? Fmt.money(p.unrealizedPnl, currency: cur, signed: true) : "—", tint: tint)
                cell("收益率", p.hasPrice_p ? Fmt.percent(h.pnlPercent, signed: true) : "—", tint: tint)
            }
            if p.realizedPnl != 0 {
                GridRow {
                    cell("已实现盈亏", Fmt.money(p.realizedPnl, currency: cur, signed: true), tint: p.realizedPnl >= 0 ? .success : .danger)
                    cell("交易", "\(p.trades) 笔")
                }
            }
        }
        .card()
    }

    private func cell(_ title: String, _ value: String, tint: Color? = nil) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title).font(.footnote).foregroundStyle(.secondaryText)
            Text(value).font(.system(size: 18, weight: .semibold, design: .monospaced))
                .foregroundStyle(tint.map { AnyShapeStyle($0) } ?? AnyShapeStyle(.primary))
                .lineLimit(1).minimumScaleFactor(0.7)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    // MARK: 定投

    @ViewBuilder
    private func dcaRows(_ h: Noted_V1_Holding) -> some View {
        Toggle(isOn: Binding(get: { h.hasPlan }, set: { on in setPlan(h, on: on) })) {
            FieldLabel("每月定投", icon: "repeat", tile: .blue)
        }
        .tint(.switchOn)
        .accessibilityIdentifier("holding-dca-toggle")
        if h.hasPlan {
            Picker(selection: Binding(get: { Int(h.dcaDay) }, set: { d in
                var x = h
                x.dcaDay = Int32(d)
                Task { _ = await data.updateHolding(x, fields: ["dca_day"]) }
            })) {
                ForEach(Self.dcaDays, id: \.self) { Text("每月 \($0) 号").tag($0) }
                if !Self.dcaDays.contains(Int(h.dcaDay)) { Text("每月 \(h.dcaDay) 号").tag(Int(h.dcaDay)) }
            } label: {
                FieldLabel("扣款日", icon: "calendar", tile: .red)
            }
            .pickerStyle(.menu)
            FieldRow("金额", icon: "dollarsign", tile: .green, value: Fmt.money(h.dcaAmount, currency: h.currencyCode)) {
                editAmount = true
            }
            .accessibilityIdentifier("holding-dca-amount")
            if let next = h.nextDCA() {
                HStack {
                    FieldLabel("下次", icon: "clock", tile: .orange, value: Fmt.mdw(next))
                    if h.position.dca.doneThisMonth {
                        Image(systemName: "checkmark.circle.fill").foregroundStyle(.success).accessibilityLabel("本月已投")
                    }
                }
            }
        }
    }

    private func setPlan(_ h: Noted_V1_Holding, on: Bool) {
        var x = h
        var fields = ["dca_day"]
        if on {
            x.dcaDay = 15
            if x.dcaAmount <= 0 { x.dcaAmount = 500; fields.append("dca_amount") }
        } else {
            x.dcaDay = 0
        }
        Task {
            if await data.updateHolding(x, fields: fields) != nil {
                toaster.show(on ? "已开启定投" : "已暂停定投", systemImage: "repeat")
            }
        }
    }
}

/// One trade: 买入 / 卖出 tag, "10 股 × 27.00", the day, and the total.
private struct TradeRow: View {
    let trade: Noted_V1_Trade
    let currency: String

    var body: some View {
        let buy = trade.side != "sell"
        HStack(spacing: 12) {
            Text(buy ? "买入" : "卖出")
                .font(.footnote.weight(.semibold))
                .padding(.horizontal, 9).frame(height: 24)
                .foregroundStyle(buy ? Color.success : Color.danger)
                .background((buy ? Color.success.wash(0.13) : Color.danger.wash(0.12)), in: Capsule())
            VStack(alignment: .leading, spacing: 1) {
                Text("\(Fmt.shares(trade.shares)) 股 × \(PlanFmt.price(trade.price))").monospacedDigit()
                Text(Fmt.mdw(trade.time.date) + (trade.fee > 0 ? " · 手续费 \(PlanFmt.price(trade.fee))" : ""))
                    .font(.footnote).foregroundStyle(.secondaryText)
            }
            Spacer(minLength: 8)
            Text(Fmt.money(trade.shares * trade.price, currency: currency)).font(.system(size: 15, design: .monospaced))
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }
}

/// 记一笔: 买入 / 卖出, 股数, 成交价, 手续费, 日期.
private struct TradeSheet: View {
    @Environment(\.dismiss) private var dismiss
    let holding: Noted_V1_Holding
    let onSave: (HoldingDetailView.TradeSide, Double, Double, Double, Date) -> Void

    @State private var side: HoldingDetailView.TradeSide
    @State private var shares = ""
    @State private var price = ""
    @State private var fee = ""
    @State private var date = Date.now
    @FocusState private var focused: Bool

    init(holding: Noted_V1_Holding, side: HoldingDetailView.TradeSide,
         onSave: @escaping (HoldingDetailView.TradeSide, Double, Double, Double, Date) -> Void) {
        self.holding = holding
        self.onSave = onSave
        _side = State(initialValue: side)
        _price = State(initialValue: holding.price.map { $0 == $0.rounded() ? Fmt.number($0) : String(format: "%g", $0) } ?? "")
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Picker("方向", selection: $side) {
                        Text("买入").tag(HoldingDetailView.TradeSide.buy)
                        Text("卖出").tag(HoldingDetailView.TradeSide.sell)
                    }
                    .pickerStyle(.segmented)
                    .planCardRow(bottom: 0)
                }
                Section {
                    row("股数", "0", $shares).focused($focused)
                    row("成交价", "0.00", $price)
                    row("手续费", "0", $fee)
                    DatePicker("日期", selection: $date, in: ...Date.now, displayedComponents: .date)
                        .environment(\.locale, Fmt.zh)
                } footer: {
                    if let error { Text(error).foregroundStyle(.danger) }
                }
            }
            .navigationTitle("记一笔 · \(holding.symbol)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存") {
                        guard let q = PlanFmt.parse(shares), let p = PlanFmt.parse(price), error == nil else { return }
                        let at = date.isSameDay(as: .now) ? Date.now : date.at(hour: 12)
                        onSave(side, q, p, PlanFmt.parse(fee) ?? 0, at)
                        dismiss()
                    }
                    .fontWeight(.semibold)
                    .disabled(!valid)
                    .accessibilityIdentifier("trade-save")
                }
            }
        }
        .presentationDetents([.medium, .large])
        .onAppear { focused = true }
    }

    private var valid: Bool {
        guard let q = PlanFmt.parse(shares), q > 0, let p = PlanFmt.parse(price), p > 0 else { return false }
        return error == nil
    }

    private var error: String? {
        if side == .sell, let q = PlanFmt.parse(shares), q > holding.position.shares + 1e-9 {
            return "最多卖出 \(Fmt.shares(holding.position.shares)) 股"
        }
        if let f = PlanFmt.parse(fee), f < 0 { return "手续费不能为负" }
        return nil
    }

    private func row(_ label: String, _ placeholder: String, _ text: Binding<String>) -> some View {
        HStack {
            Text(label)
            TextField(placeholder, text: text)
                .multilineTextAlignment(.trailing)
                .keyboardType(.decimalPad)
                .font(.body.monospacedDigit())
        }
    }
}
