import SwiftUI
import StoreKit

struct PaywallView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var chosen: Product?
    @State private var manage = false

    private let features: [(String, Bool)] = [
        ("内置主题(日光、纸本、苔绿)", true),
        ("自定义主题与配色", false),
        ("页面模块与顺序", false),
        ("工作 / 生活各用一套布局", false),
        ("桌面、锁屏小组件与 Apple Watch 表盘", false),
    ]

    var body: some View {
        let pro = session.pro
        ZStack(alignment: .bottom) {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Button { dismiss() } label: { Label("设置", systemImage: "chevron.left").font(.system(size: 14, weight: .semibold)) }
                    Spacer()
                    Button("恢复购买") { Task { await pro.restore() } }.font(.system(size: 14, weight: .semibold))
                }
                Text("Noted Pro").font(.system(size: 30, weight: .bold)).foregroundStyle(p.ink)
                VStack(spacing: 0) {
                    HStack {
                        Eyebrow(text: "功能"); Spacer()
                        Eyebrow(text: "免费").frame(width: 44)
                        Text("PRO").font(.system(size: 11, weight: .medium, design: .monospaced)).tracking(0.8).foregroundStyle(session.mode.accent(p)).frame(width: 44)
                    }.padding(.horizontal, 14).frame(height: 40)
                    ForEach(features, id: \.0) { f in
                        Divider().overlay(p.line)
                        HStack {
                            Text(f.0).font(.system(size: 15)).foregroundStyle(p.ink)
                            Spacer()
                            Text(f.1 ? "✓" : "—").font(.system(size: 14, design: .monospaced)).foregroundStyle(p.ink3).frame(width: 44)
                            Text("✓").font(.system(size: 14, weight: .bold, design: .monospaced)).foregroundStyle(session.mode.accent(p)).frame(width: 44)
                        }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                    }
                }
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))

                if pro.isPro {
                    Text("已订阅 Pro").font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ok).card()
                        .accessibilityIdentifier("pro-active")
                    Button("管理订阅") { manage = true }.font(.system(size: 14, weight: .semibold))
                } else if pro.products.isEmpty {
                    Text("暂时无法获取价格,稍后再试。").font(.system(size: 14)).foregroundStyle(p.ink3).card()
                } else {
                    HStack(spacing: 10) {
                        ForEach(pro.products, id: \.id) { prod in
                            let on = (chosen ?? pro.products.first)?.id == prod.id
                            Button { chosen = prod } label: {
                                VStack(alignment: .leading, spacing: 6) {
                                    HStack {
                                        Text(prod.id == Pro.yearlyID ? "年付" : "月付").font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink)
                                        if prod.id == Pro.yearlyID, let off = pro.yearlySaving {
                                            Spacer(minLength: 4)
                                            Text("省 \(off)%").font(.system(size: 11, weight: .semibold)).padding(.horizontal, 6).frame(height: 20)
                                                .background(Color(hex: 0xE1F3EA), in: RoundedRectangle(cornerRadius: 5)).foregroundStyle(p.ok)
                                        }
                                    }
                                    Text(prod.displayPrice).font(.system(size: 22, weight: .semibold, design: .monospaced)).foregroundStyle(p.ink)
                                    Text(prod.id == Pro.yearlyID ? "每年" : "每月").font(.system(size: 13)).foregroundStyle(p.ink2)
                                }
                                .frame(maxWidth: .infinity, alignment: .leading).padding(14)
                                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                                .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(on ? session.mode.accent(p) : p.line, lineWidth: on ? 1.5 : 1))
                            }.buttonStyle(.plain)
                        }
                    }
                }
                if let m = pro.message { Text(m).font(.system(size: 13)).foregroundStyle(p.bad) }
                Text("订阅由 App Store 管理,可随时取消。取消后已有的主题和布局继续生效,只是不能再修改。")
                    .font(.system(size: 12)).foregroundStyle(p.ink3)
            }
            .padding(16).padding(.bottom, pro.isPro ? 16 : 80)
        }
            if !pro.isPro {
                    Button {
                        if let prod = chosen ?? pro.products.first { Task { await pro.buy(prod) } }
                    } label: {
                        Text(pro.busy ? "处理中…" : "订阅 Pro").font(.system(size: 16, weight: .semibold)).foregroundStyle(.white)
                            .frame(maxWidth: .infinity).frame(height: 50)
                            .background(session.mode.accent(p), in: RoundedRectangle(cornerRadius: 12))
                    }
                    .disabled(pro.products.isEmpty || pro.busy)
                    .accessibilityIdentifier("subscribe")
                    .padding(.horizontal, 16).padding(.bottom, 12)
                }
        }
        .background(p.bg.ignoresSafeArea())
        .task { await pro.load() }
        .manageSubscriptionsSheet(isPresented: $manage)
        .onChange(of: pro.isPro) { Task { await session.syncExtras() } }
    }
}
