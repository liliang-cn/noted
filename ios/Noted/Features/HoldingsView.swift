import SwiftUI

/// 标的: what the person invests in, what they paid, and the monthly plan.
/// Prices are typed in; noted does not fetch quotes or place orders.
struct HoldingCard: View {
    @Environment(\.palette) private var p
    let holding: Noted_V1_Holding
    let onChange: () -> Void
    @State private var open = false
    @State private var buying = false

    var body: some View {
        let pos = holding.position
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Text(holding.symbol).font(.system(size: 20, weight: .bold, design: .monospaced)).foregroundStyle(p.ink)
                if !holding.name.isEmpty { Text(holding.name).font(.system(size: 13)).foregroundStyle(p.ink2).lineLimit(1) }
                Spacer()
                if pos.dca.active { planChip(pos.dca) }
            }
            if pos.shares > 0 {
                HStack(alignment: .firstTextBaseline) {
                    Text("\(Fmt.shares(pos.shares)) 股 · 均价 \(Fmt.money(pos.avgCost))").font(.system(size: 13, design: .monospaced)).foregroundStyle(p.ink2)
                    Spacer()
                    if pos.hasPrice_p {
                        Text("\(Fmt.signedMoney(pos.unrealizedPnl)) (\(Fmt.signedMoney(pos.unrealizedPercent * 100))%)")
                            .font(.system(size: 13, weight: .semibold, design: .monospaced)).foregroundStyle(pos.unrealizedPnl >= 0 ? p.ok : p.bad)
                    }
                }
                if pos.hasPrice_p {
                    Text("市值 \(Fmt.money(pos.marketValue)) \(holding.currency)").font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2)
                } else {
                    Text("成本 \(Fmt.money(pos.costBasis)) \(holding.currency)").font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2)
                }
            } else {
                Text("还没有持仓").font(.system(size: 13)).foregroundStyle(p.ink3)
            }
            if pos.dca.active {
                HStack {
                    Text("每月 \(holding.dcaDay) 号\(holding.dcaAmount > 0 ? " · \(Fmt.money(holding.dcaAmount).replacingOccurrences(of: ".00", with: "")) \(holding.currency)" : "") · 下次 \(Fmt.md(pos.dca.nextTime.date))")
                        .font(.system(size: 12)).foregroundStyle(p.ink2)
                    Spacer()
                    if pos.dca.streakMonths > 0 { Text("连续 \(pos.dca.streakMonths) 个月").font(.system(size: 12)).foregroundStyle(p.ink2) }
                }
            }
            Button { buying = true } label: {
                Text("记一笔买入").font(.system(size: 13, weight: .semibold)).foregroundStyle(p.ink).frame(maxWidth: .infinity).frame(height: 34)
                    .background(p.surface2, in: RoundedRectangle(cornerRadius: 9))
            }.buttonStyle(.plain).accessibilityLabel("买入 \(holding.symbol)")
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card(padding: 14)
        .contentShape(Rectangle())
        .onTapGesture { open = true }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("holding-\(holding.symbol)")
        .sheet(isPresented: $open, onDismiss: onChange) { HoldingDetailView(id: holding.id) }
        .sheet(isPresented: $buying, onDismiss: onChange) { TradeSheet(holding: holding) }
    }

    private func planChip(_ d: Noted_V1_DcaStatus) -> some View {
        Text(d.doneThisMonth ? "本月已投" : "本月待投").font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
            .background(d.doneThisMonth ? Color(hex: 0xE1F3EA) : p.badBg, in: RoundedRectangle(cornerRadius: 6))
            .foregroundStyle(d.doneThisMonth ? p.ok : p.bad)
    }
}

struct HoldingDetailView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let id: String
    @State private var holding: Noted_V1_Holding?
    @State private var trades: [Noted_V1_Trade] = []
    @State private var price = ""
    @State private var day = 15
    @State private var amount = ""
    @State private var planOn = false
    @State private var buying = false
    @State private var error: String?
    @State private var confirmDelete = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Button { dismiss() } label: { Label("返回", systemImage: "chevron.left").font(.system(size: 14, weight: .semibold)) }
                    Spacer()
                    Menu {
                        Button("归档") { Task { await archive() } }
                        Button("删除标的", role: .destructive) { confirmDelete = true }
                    } label: { Image(systemName: "ellipsis").font(.system(size: 16)).frame(width: 32, height: 32) }.accessibilityLabel("更多")
                }
                ErrorLine(text: error)
                if let h = holding { content(h) }
            }
            .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 24)
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .sheet(isPresented: $buying, onDismiss: { Task { await load() } }) { if let h = holding { TradeSheet(holding: h) } }
        .confirmationDialog("删除这个标的?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("删除标的和它的全部记录", role: .destructive) { Task { await remove() } }
        } message: { Text("每月的定投提醒也会一起删除。") }
    }

    @ViewBuilder private func content(_ h: Noted_V1_Holding) -> some View {
        let pos = h.position
        VStack(alignment: .leading, spacing: 4) {
            Text(h.symbol).font(.system(size: 30, weight: .bold, design: .monospaced)).foregroundStyle(p.ink)
            if !h.name.isEmpty { Text(h.name).font(.system(size: 14)).foregroundStyle(p.ink2) }
        }
        VStack(spacing: 0) {
            stat("持有", "\(Fmt.shares(pos.shares)) 股")
            stat("均价", Fmt.money(pos.avgCost))
            stat("成本", "\(Fmt.money(pos.costBasis)) \(h.currency)")
            if pos.hasPrice_p {
                stat("市值", "\(Fmt.money(pos.marketValue)) \(h.currency)")
                stat("浮动盈亏", "\(Fmt.signedMoney(pos.unrealizedPnl)) (\(Fmt.signedMoney(pos.unrealizedPercent * 100))%)", color: pos.unrealizedPnl >= 0 ? p.ok : p.bad)
            }
            if pos.realizedPnl != 0 { stat("已实现", Fmt.signedMoney(pos.realizedPnl), color: pos.realizedPnl >= 0 ? p.ok : p.bad) }
            stat("累计投入", "\(Fmt.money(pos.totalBought)) \(h.currency)", last: true)
        }
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))

        Eyebrow(text: "现价 · 自己填")
        HStack(spacing: 8) {
            TextField(h.hasLastPrice ? Fmt.money(h.lastPrice) : "现价", text: $price).keyboardType(.decimalPad)
                .padding(.horizontal, 12).frame(height: 40).background(p.surface, in: RoundedRectangle(cornerRadius: 10)).overlay(RoundedRectangle(cornerRadius: 10).stroke(p.line))
                .accessibilityIdentifier("last-price")
            Button { Task { await savePrice() } } label: {
                Text("保存").font(.system(size: 14, weight: .semibold)).foregroundStyle(.white).padding(.horizontal, 16).frame(height: 40).background(p.accentDef, in: RoundedRectangle(cornerRadius: 10))
            }.disabled(Double(price.replacingOccurrences(of: ",", with: ".")) == nil).accessibilityIdentifier("save-price")
        }
        if h.hasLastPrice, h.hasLastPriceTime { Text("更新于 \(Fmt.md(h.lastPriceTime.date))").font(.system(size: 12)).foregroundStyle(p.ink3) }

        Eyebrow(text: "每月定投")
        VStack(spacing: 0) {
            Toggle("每月提醒我定投", isOn: $planOn).padding(.horizontal, 14).frame(minHeight: 48).tapsToToggle($planOn).accessibilityIdentifier("plan-toggle")
            if planOn {
                Divider().overlay(p.line)
                Stepper("每月 \(day) 号", value: $day, in: 1...28).padding(.horizontal, 14).frame(minHeight: 48)
                Divider().overlay(p.line)
                HStack {
                    Text("计划金额 (\(h.currency))").font(.system(size: 15)).foregroundStyle(p.ink)
                    Spacer()
                    TextField("0", text: $amount).keyboardType(.decimalPad).multilineTextAlignment(.trailing).frame(width: 90).accessibilityIdentifier("plan-amount")
                }.padding(.horizontal, 14).frame(minHeight: 48)
            }
            Divider().overlay(p.line)
            Button { Task { await savePlan() } } label: {
                Text("保存定投设置").font(.system(size: 14, weight: .semibold)).foregroundStyle(p.accentDef).frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 14).frame(minHeight: 46)
            }.accessibilityIdentifier("save-plan")
        }
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        if pos.dca.active {
            Text("本月\(pos.dca.doneThisMonth ? "已投 \(Fmt.money(pos.dca.investedThisMonth))" : "还没有买入") · 连续 \(pos.dca.streakMonths) 个月 · 下次 \(Fmt.md(pos.dca.nextTime.date))")
                .font(.system(size: 12)).foregroundStyle(p.ink2)
        }

        Button { buying = true } label: {
            Text("+ 记一笔").font(.system(size: 16, weight: .semibold)).foregroundStyle(.white).frame(maxWidth: .infinity).frame(height: 48)
                .background(p.accentDef, in: RoundedRectangle(cornerRadius: 12))
        }.accessibilityIdentifier("add-trade")

        if !trades.isEmpty {
            Eyebrow(text: "买卖记录")
            VStack(spacing: 0) {
                ForEach(Array(trades.reversed().enumerated()), id: \.element.id) { i, t in
                    if i > 0 { Divider().overlay(p.line) }
                    HStack(spacing: 10) {
                        Text(t.side == "buy" ? "买" : "卖").font(.system(size: 12, weight: .bold)).frame(width: 24, height: 24)
                            .background(t.side == "buy" ? p.workBg : p.badBg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(t.side == "buy" ? p.work : p.bad)
                        VStack(alignment: .leading, spacing: 2) {
                            Text("\(Fmt.shares(t.shares)) 股 @ \(Fmt.money(t.price))").font(.system(size: 14, design: .monospaced)).foregroundStyle(p.ink)
                            Text(Fmt.ymd(t.time.date)).font(.system(size: 12)).foregroundStyle(p.ink2)
                        }
                        Spacer()
                        Button { Task { await delete(t) } } label: { Image(systemName: "xmark").font(.system(size: 11, weight: .bold)).foregroundStyle(p.ink3).frame(width: 28, height: 28) }
                            .accessibilityLabel("删除 \(Fmt.ymd(t.time.date)) 的记录")
                    }.padding(.horizontal, 14).frame(minHeight: 52)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
    }

    private func stat(_ label: String, _ value: String, color: Color? = nil, last: Bool = false) -> some View {
        VStack(spacing: 0) {
            HStack {
                Text(label).font(.system(size: 14)).foregroundStyle(p.ink2)
                Spacer()
                Text(value).font(.system(size: 15, weight: .medium, design: .monospaced)).foregroundStyle(color ?? p.ink)
            }.padding(.horizontal, 14).frame(minHeight: 44)
            if !last { Divider().overlay(p.line) }
        }
    }

    private func load() async {
        guard let api = session.api else { return }
        do {
            var g = Noted_V1_GetHoldingRequest(); g.id = id
            let h = try await api.holdings.getHolding(g)
            holding = h
            planOn = h.dcaDay > 0
            day = h.dcaDay > 0 ? Int(h.dcaDay) : 15
            amount = h.dcaAmount > 0 ? Fmt.number(h.dcaAmount) : ""
            var r = Noted_V1_ListTradesRequest(); r.holdingID = id
            trades = try await api.holdings.listTrades(r).trades
            error = nil
        } catch { if !(error is CancellationError) { self.error = describe(error) } }
    }

    private func update(_ paths: [String], _ fill: (inout Noted_V1_Holding) -> Void) async {
        guard let api = session.api else { return }
        var r = Noted_V1_UpdateHoldingRequest()
        r.holding.id = id
        fill(&r.holding)
        r.updateMask.paths = paths
        do { holding = try await api.holdings.updateHolding(r); error = nil; await load() } catch { self.error = describe(error) }
    }

    private func savePrice() async {
        guard let v = Double(price.replacingOccurrences(of: ",", with: ".")) else { return }
        await update(["last_price"]) { $0.lastPrice = v }
        price = ""
    }

    private func savePlan() async {
        let amt = Double(amount.replacingOccurrences(of: ",", with: ".")) ?? 0
        await update(["dca_day", "dca_amount"]) { $0.dcaDay = planOn ? Int32(day) : 0; $0.dcaAmount = planOn ? amt : 0 }
    }

    private func archive() async { await update(["archived"]) { $0.archived = true }; dismiss() }

    private func delete(_ t: Noted_V1_Trade) async {
        guard let api = session.api else { return }
        var r = Noted_V1_DeleteTradeRequest(); r.id = t.id
        do { _ = try await api.holdings.deleteTrade(r); await load() } catch { self.error = describe(error) }
    }

    private func remove() async {
        guard let api = session.api else { return }
        var r = Noted_V1_DeleteHoldingRequest(); r.id = id
        do { _ = try await api.holdings.deleteHolding(r); dismiss() } catch { self.error = describe(error) }
    }
}

struct TradeSheet: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let holding: Noted_V1_Holding
    @State private var side = "buy"
    @State private var shares = ""
    @State private var price = ""
    @State private var fee = ""
    @State private var date = Date()
    @State private var error: String?

    private var num: (Double?, Double?) { (Double(shares.replacingOccurrences(of: ",", with: ".")), Double(price.replacingOccurrences(of: ",", with: "."))) }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Button("取消") { dismiss() }
                Spacer()
                Text(holding.symbol).font(.system(size: 16, weight: .semibold, design: .monospaced))
                Spacer()
                Button("保存") { Task { await save() } }.fontWeight(.semibold).disabled(num.0 == nil || num.1 == nil).accessibilityIdentifier("save-trade")
            }
            Picker("", selection: $side) { Text("买入").tag("buy"); Text("卖出").tag("sell") }.pickerStyle(.segmented)
            VStack(spacing: 0) {
                row("股数") { TextField("0", text: $shares).keyboardType(.decimalPad).accessibilityIdentifier("trade-shares") }
                Divider().overlay(p.line)
                row("成交价 (\(holding.currency))") { TextField("0.00", text: $price).keyboardType(.decimalPad).accessibilityIdentifier("trade-price") }
                Divider().overlay(p.line)
                row("手续费") { TextField("0", text: $fee).keyboardType(.decimalPad) }
                Divider().overlay(p.line)
                HStack {
                    Text("日期").font(.system(size: 15)).foregroundStyle(p.ink)
                    Spacer()
                    DatePicker("", selection: $date, in: ...Date(), displayedComponents: .date).labelsHidden()
                }.padding(.horizontal, 14).frame(minHeight: 48)
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
            if let s = num.0, let pr = num.1 {
                Text("合计 \(Fmt.money(s * pr + (Double(fee) ?? 0))) \(holding.currency)").font(.system(size: 13, design: .monospaced)).foregroundStyle(p.ink2)
            }
            ErrorLine(text: error)
            Spacer()
        }
        .padding(16).background(p.bg.ignoresSafeArea())
        .presentationDetents([.medium])
    }

    private func row<T: View>(_ label: String, @ViewBuilder field: () -> T) -> some View {
        HStack {
            Text(label).font(.system(size: 15)).foregroundStyle(p.ink)
            Spacer()
            field().multilineTextAlignment(.trailing).frame(width: 120)
        }.padding(.horizontal, 14).frame(minHeight: 48)
    }

    private func save() async {
        guard let api = session.api, let s = num.0, let pr = num.1 else { return }
        var r = Noted_V1_RecordTradeRequest()
        r.holdingID = holding.id
        r.trade.side = side; r.trade.shares = s; r.trade.price = pr
        r.trade.fee = Double(fee.replacingOccurrences(of: ",", with: ".")) ?? 0
        r.trade.time = .init(Calendar.current.isDateInToday(date) ? Date() : Calendar.current.date(bySettingHour: 12, minute: 0, second: 0, of: date) ?? date)
        do { _ = try await api.holdings.recordTrade(r); dismiss() } catch { self.error = describe(error) }
    }
}

struct NewHoldingView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var symbol = ""
    @State private var name = ""
    @State private var planOn = true
    @State private var day = 15
    @State private var amount = ""
    @State private var owned = ""
    @State private var cost = ""
    @State private var error: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Button("取消") { dismiss() }
                    Spacer()
                    Text("新建标的").font(.system(size: 16, weight: .semibold))
                    Spacer()
                    Button("保存") { Task { await save() } }.fontWeight(.semibold).disabled(symbol.trimmingCharacters(in: .whitespaces).isEmpty).accessibilityIdentifier("save-holding")
                }
                TextField("代码,如 QQQM", text: $symbol).textInputAutocapitalization(.characters).autocorrectionDisabled()
                    .font(.system(size: 24, weight: .bold, design: .monospaced)).accessibilityIdentifier("holding-symbol")
                TextField("名称(可不填)", text: $name).font(.system(size: 15))

                Eyebrow(text: "已经持有")
                VStack(spacing: 0) {
                    field("股数", $owned, id: "owned-shares")
                    Divider().overlay(p.line)
                    field("每股成本", $cost, id: "owned-cost")
                }
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))

                Eyebrow(text: "每月定投")
                VStack(spacing: 0) {
                    Toggle("每月提醒我定投", isOn: $planOn).padding(.horizontal, 14).frame(minHeight: 48).tapsToToggle($planOn)
                    if planOn {
                        Divider().overlay(p.line)
                        Stepper("每月 \(day) 号", value: $day, in: 1...28).padding(.horizontal, 14).frame(minHeight: 48)
                        Divider().overlay(p.line)
                        field("计划金额 (USD)", $amount, id: "plan-amount")
                    }
                }
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
                ErrorLine(text: error)
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
    }

    private func field(_ label: String, _ text: Binding<String>, id: String) -> some View {
        HStack {
            Text(label).font(.system(size: 15)).foregroundStyle(p.ink)
            Spacer()
            TextField("0", text: text).keyboardType(.decimalPad).multilineTextAlignment(.trailing).frame(width: 110).accessibilityIdentifier(id)
        }.padding(.horizontal, 14).frame(minHeight: 48)
    }

    private func save() async {
        guard let api = session.api else { return }
        var r = Noted_V1_CreateHoldingRequest()
        r.holding.symbol = symbol; r.holding.name = name
        r.holding.dcaDay = planOn ? Int32(day) : 0
        r.holding.dcaAmount = planOn ? (Double(amount.replacingOccurrences(of: ",", with: ".")) ?? 0) : 0
        r.holding.timeZone = TimeZone.current.identifier
        do {
            let h = try await api.holdings.createHolding(r)
            if let s = Double(owned.replacingOccurrences(of: ",", with: ".")), s > 0, let c = Double(cost.replacingOccurrences(of: ",", with: ".")) {
                var t = Noted_V1_RecordTradeRequest()
                t.holdingID = h.id; t.trade.side = "buy"; t.trade.shares = s; t.trade.price = c; t.trade.note = "已有持仓"
                t.trade.time = .init(Calendar.current.date(byAdding: .month, value: -1, to: .now) ?? .now)
                _ = try await api.holdings.recordTrade(t)
            }
            dismiss()
        } catch { self.error = describe(error) }
    }
}
