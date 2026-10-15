import SwiftUI

/// 投资: the summary, 配置 (allocation bar), 持仓 and 定投计划. "+" adds a holding.
struct InvestView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster

    @State private var adding = false
    @State private var deleting: Noted_V1_Holding?

    /// Allocation colors, in holding order.
    static let palette: [Color] = [.accentColor, Color(hex: 0x0F8B8D), Color(hex: 0xC2410C), Color(hex: 0x1B8A55), Color(hex: 0x8E8E93)]

    var body: some View {
        let holdings = data.holdings
        let total = holdings.reduce(0.0) { $0 + $1.marketValue }
        List {
            Section { InvestSummaryCard(holdings: holdings).planCardRow(bottom: 0) }

            if total > 0 {
                Section {
                    allocation(holdings, total: total)
                } header: { Text("配置") }
            }

            Section {
                if holdings.isEmpty { EmptyState(text: "没有持仓") }
                ForEach(holdings, id: \.id) { h in
                    HoldingRow(holding: h)
                        .swipeActions(edge: .trailing) {
                            Button { deleting = h } label: { Label("删除", systemImage: "trash") }.tint(.danger)
                        }
                }
            } header: { Text("持仓") }

            if !holdings.isEmpty {
                Section {
                    ForEach(holdings, id: \.id) { h in
                        Button { router.push(.holding(h.id)) } label: {
                            HStack {
                                Text(h.symbol).foregroundStyle(.primary)
                                Spacer(minLength: 8)
                                Text(h.hasPlan ? "每月 \(h.dcaDay) 号 · \(Fmt.money(h.dcaAmount, currency: h.currencyCode, cents: h.dcaAmount != h.dcaAmount.rounded()))" : "已暂停")
                                    .foregroundStyle(.secondaryText).monospacedDigit()
                                Image(systemName: "chevron.right").font(.system(size: 13, weight: .semibold)).foregroundStyle(.tertiaryText)
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                    }
                } header: { Text("定投计划") }
            }
        }
        .listStyle(.insetGrouped)
        .refreshable { await data.refreshHoldings() }
        .navigationTitle("投资")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { adding = true } label: { Image(systemName: "plus") }
                    .accessibilityLabel("添加标的")
                    .accessibilityIdentifier("invest-add")
            }
        }
        .sheet(isPresented: $adding) {
            AddHoldingSheet { symbol, name, shares, avg, last in
                Task { await add(symbol: symbol, name: name, shares: shares, avg: avg, last: last) }
            }
        }
        .confirmationDialog("删除 \(deleting?.symbol ?? "")？", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { h in
            Button("删除标的", role: .destructive) {
                Task { if await data.deleteHolding(h.id) { toaster.show("已删除", systemImage: "trash") } }
            }
            Button("取消", role: .cancel) {}
        } message: { _ in
            Text("交易记录会一起删除")
        }
    }

    private func allocation(_ holdings: [Noted_V1_Holding], total: Double) -> some View {
        let shown = holdings.filter { $0.marketValue > 0 }
        return VStack(alignment: .leading, spacing: 10) {
            GeometryReader { g in
                let gaps = CGFloat(max(shown.count - 1, 0)) * 2
                HStack(spacing: 2) {
                    ForEach(Array(shown.enumerated()), id: \.element.id) { i, h in
                        Rectangle().fill(color(of: h, in: holdings))
                            .frame(width: max(0, (g.size.width - gaps) * h.marketValue / total))
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: 5))
            }
            .frame(height: 10)
            FlowLegend(items: shown.map { h in
                (h.symbol, color(of: h, in: holdings), Fmt.percent(h.marketValue / total * 100, digits: 1))
            })
        }
        .padding(.vertical, 6)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(shown.map { "\($0.symbol) \(Fmt.percent($0.marketValue / total * 100, digits: 1))" }.joined(separator: "，"))
    }

    private func color(of h: Noted_V1_Holding, in all: [Noted_V1_Holding]) -> Color {
        let i = all.firstIndex { $0.id == h.id } ?? 0
        return Self.palette[i % Self.palette.count]
    }

    private func add(symbol: String, name: String, shares: Double, avg: Double, last: Double?) async {
        var h = Noted_V1_Holding()
        h.symbol = symbol
        h.name = name.isEmpty ? symbol : name
        h.currency = "USD"
        if let last { h.lastPrice = last } else if avg > 0 { h.lastPrice = avg }
        guard let made = await data.createHolding(h) else { return }
        if shares > 0 && avg > 0 {
            _ = await data.recordTrade(made.id, side: "buy", shares: shares, price: avg)
        }
        toaster.show("已添加 \(symbol)")
    }
}

/// "● QQQM 29.9%" items that wrap onto more lines.
private struct FlowLegend: View {
    let items: [(String, Color, String)]
    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 16) { ForEach(items, id: \.0) { item($0) } }
            VStack(alignment: .leading, spacing: 6) {
                ForEach(Array(stride(from: 0, to: items.count, by: 3)), id: \.self) { start in
                    HStack(spacing: 16) { ForEach(items[start..<min(start + 3, items.count)], id: \.0) { item($0) } }
                }
            }
        }
        .font(.footnote)
    }

    private func item(_ i: (String, Color, String)) -> some View {
        HStack(spacing: 6) {
            Circle().fill(i.1).frame(width: 8, height: 8)
            Text(i.0)
            Text(i.2).foregroundStyle(.secondaryText).monospacedDigit()
        }
    }
}

/// 添加标的: 代码, 名称, 股数, 均价, 最新价.
private struct AddHoldingSheet: View {
    @Environment(\.dismiss) private var dismiss
    let onAdd: (String, String, Double, Double, Double?) -> Void
    @State private var symbol = ""
    @State private var name = ""
    @State private var shares = ""
    @State private var avg = ""
    @State private var last = ""
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            List {
                Section {
                    HStack {
                        Text("代码")
                        TextField("如 SCHD", text: $symbol)
                            .multilineTextAlignment(.trailing)
                            .textInputAutocapitalization(.characters)
                            .autocorrectionDisabled()
                            .focused($focused)
                    }
                    HStack {
                        Text("名称")
                        TextField("可选", text: $name).multilineTextAlignment(.trailing)
                    }
                }
                Section {
                    numberRow("股数", "0", $shares)
                    numberRow("均价", "0.00", $avg)
                    numberRow("最新价", "0.00", $last)
                }
            }
            .navigationTitle("添加标的")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("添加") {
                        onAdd(code, name.trimmingCharacters(in: .whitespaces),
                              PlanFmt.parse(shares) ?? 0, PlanFmt.parse(avg) ?? 0, PlanFmt.parse(last).flatMap { $0 > 0 ? $0 : nil })
                        dismiss()
                    }
                    .fontWeight(.semibold)
                    .disabled(code.isEmpty)
                    .accessibilityIdentifier("holding-add-save")
                }
            }
        }
        .presentationDetents([.medium, .large])
        .onAppear { focused = true }
    }

    private var code: String { symbol.trimmingCharacters(in: .whitespaces).uppercased() }

    private func numberRow(_ label: String, _ placeholder: String, _ text: Binding<String>) -> some View {
        HStack {
            Text(label)
            TextField(placeholder, text: text)
                .multilineTextAlignment(.trailing)
                .keyboardType(.decimalPad)
                .font(.body.monospacedDigit())
        }
    }
}
