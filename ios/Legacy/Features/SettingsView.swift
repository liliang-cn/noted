import SwiftUI

/// 设置: server, intelligence, theme, and Pro.
struct SettingsView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.mode) private var mode
    @Environment(\.dismiss) private var dismiss
    @Environment(Router.self) private var router
    @State private var paywall = ProcessInfo.processInfo.environment["NOTED_SHEET"] == "paywall"
    @State private var exporting = false
    @State private var exported: ExportedFile?
    @State private var exportError: String?
    @State private var account: Noted_V1_Account?
    @State private var accountError: String?

    private let themes: [(id: String, name: String)] = [("daylight", "日光"), ("paper", "纸本"), ("moss", "苔绿")]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    BackButton(title: "概览")
                    Spacer()
                }
                Text("设置").font(.ui(30, .bold)).tracking(-0.6).foregroundStyle(p.ink)

                group {
                    link("服务器") {
                        Text("\(session.host):\(session.port)").font(.mono(13)).foregroundStyle(p.ink2).lineLimit(1)
                        tag("已连接", p.ok, Color(hex: 0xE1F3EA))
                    } action: {}
                    link("智能", chevron: true) {
                        Text(session.aiChat ? "可用" : "未开启").font(.ui(13)).foregroundStyle(p.ink2)
                    } action: { router.push(.aiSettings) }
                }

                EyebrowSection(title: "主题") {
                    HStack(alignment: .top, spacing: 10) {
                        ForEach(themes, id: \.id) { t in swatch(t.id, t.name) }
                    }
                }

                group {
                    HStack(spacing: 12) {
                        Text("工作 / 个人各用一种颜色").font(.ui(15)).foregroundStyle(p.ink)
                        Spacer()
                        Toggle("", isOn: modeColors).labelsHidden().tint(mode.accent(p)).accessibilityLabel("工作 / 个人各用一种颜色")
                    }.padding(.horizontal, 14).frame(minHeight: p.rowHeight).tapsToToggle(modeColors)
                    link("自定义主题", chevron: true) { ProTag() } action: { session.pro.isPro ? router.push(.themeEditor) : (paywall = true) }
                    link("页面模块", chevron: true) { ProTag() } action: { session.pro.isPro ? router.push(.modules) : (paywall = true) }
                }

                EyebrowSection(title: "提醒") {
                    group {
                        muteRow("工作模式下静音个人提醒", \.lifeInWork)
                        muteRow("个人模式下静音工作提醒", \.workInLife)
                    }
                }

                if let account, account.appleEnabled {
                    EyebrowSection(title: "账号") {
                        group {
                            HStack(spacing: 12) {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Apple ID").font(.ui(15)).foregroundStyle(p.ink)
                                    Text(account.appleLinked ? "已绑定,新设备可用它登录" : "绑定后新设备不用输入令牌")
                                        .font(.ui(13)).foregroundStyle(p.ink2)
                                }
                                Spacer()
                                if account.appleLinked {
                                    Button("解除") { Task { await unlink() } }
                                        .font(.ui(13, .bold)).foregroundStyle(p.bad)
                                        .accessibilityIdentifier("apple-unlink")
                                } else {
                                    AppleButton(type: .continue) { r in
                                        switch r {
                                        case .success(let proof): Task { await link(proof) }
                                        case .failure(let e): if !e.isAppleCancel { accountError = e.localizedDescription }
                                        }
                                    }
                                    .frame(width: 150, height: 34)
                                    .accessibilityIdentifier("apple-link")
                                }
                            }.padding(.horizontal, 14).padding(.vertical, 8).frame(minHeight: p.rowHeight)
                        }
                    }
                    if let accountError { Text(accountError).font(.ui(13)).foregroundStyle(p.bad) }
                }

                EyebrowSection(title: "数据") {
                    group {
                        link("导出我的数据", chevron: !exporting) {
                            if exporting { ProgressView() } else {
                                Text("笔记、日程、待办、项目、目标").font(.ui(13)).foregroundStyle(p.ink2).lineLimit(1)
                            }
                        } action: { Task { await export() } }
                        .disabled(exporting)
                        .accessibilityIdentifier("export")
                    }
                }
                if let exportError { Text(exportError).font(.ui(13)).foregroundStyle(p.bad) }

                Button { paywall = true } label: {
                    HStack(spacing: 12) {
                        Text("Noted Pro").font(.ui(15, .bold)).foregroundStyle(p.ink)
                        Spacer()
                        Text(session.pro.isPro ? "已订阅" : "未订阅").font(.ui(13)).foregroundStyle(p.ink2)
                        chevronMark
                    }
                    .padding(.horizontal, 14).frame(minHeight: p.rowHeight).contentShape(Rectangle()).card(padding: 0)
                }.buttonStyle(.plain)

                Button(role: .destructive) {
                    dismiss()
                    session.disconnect()
                } label: {
                    Text("断开连接").font(.ui(15, .bold)).frame(maxWidth: .infinity).frame(height: 46)
                        .background(p.badBg, in: RoundedRectangle(cornerRadius: p.radiusSm)).foregroundStyle(p.bad)
                }
            }
            .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 24)
        }
        .scrollIndicators(.hidden)
        .background(p.bg.ignoresSafeArea())
        .sheet(isPresented: $paywall) { PaywallView() }
        .sheet(item: $exported) { ActivitySheet(items: [$0.url]) }
        .task { account = try? await session.api?.account.getAccount(Noted_V1_GetAccountRequest()) }
        .onAppear(perform: openFromLaunch)
    }

    private func openFromLaunch() {
        #if DEBUG
        guard router.path.count == 1 else { return }
        switch ProcessInfo.processInfo.environment["NOTED_SHEET"] ?? "" {
        case "aisettings": router.push(.aiSettings)
        case "themeeditor": router.push(.themeEditor)
        case "modules": router.push(.modules)
        default: break
        }
        #endif
    }

    private func link(_ proof: AppleProof) async {
        guard let api = session.api else { return }
        accountError = nil
        var req = Noted_V1_LinkAppleRequest()
        req.identityToken = proof.identityToken; req.nonce = proof.nonce
        do { account = try await api.account.linkApple(req) } catch { accountError = describe(error) }
    }

    private func unlink() async {
        guard let api = session.api else { return }
        accountError = nil
        do { account = try await api.account.unlinkApple(Noted_V1_UnlinkAppleRequest()) } catch { accountError = describe(error) }
    }

    private func export() async {
        guard let api = session.api else { return }
        exporting = true; exportError = nil
        defer { exporting = false }
        do {
            let url = try await Exporter.run(api: api)
            #if DEBUG
            if let copy = ProcessInfo.processInfo.environment["NOTED_EXPORT_COPY"] {
                try? FileManager.default.removeItem(atPath: copy)
                try? FileManager.default.copyItem(at: url, to: URL(fileURLWithPath: copy))
            }
            #endif
            exported = ExportedFile(url: url)
        }
        catch { exportError = "导出失败:\(describe(error))" }
    }

    private var modeColors: Binding<Bool> {
        Binding(get: { session.theme.modeColors }, set: { v in
            var t = session.theme; t.modeColors = v; session.saveTheme(t)
        })
    }

    private func muteRow(_ title: String, _ key: WritableKeyPath<MuteRules, Bool>) -> some View {
        let on = Binding(get: { session.mute[keyPath: key] }, set: { v in
            var m = session.mute; m[keyPath: key] = v; session.saveMute(m)
        })
        return HStack(spacing: 12) {
            Text(title).font(.ui(15)).foregroundStyle(p.ink)
            Spacer()
            Toggle("", isOn: on).labelsHidden().tint(mode.accent(p)).accessibilityLabel(title)
        }.padding(.horizontal, 14).frame(minHeight: p.rowHeight).tapsToToggle(on)
    }

    private var chevronMark: some View {
        Image(systemName: "chevron.right").font(.system(size: 12, weight: .semibold)).foregroundStyle(p.ink3)
    }

    private func tag(_ t: String, _ fg: Color, _ bg: Color) -> some View {
        Text(t).font(.ui(11, .semibold)).padding(.horizontal, 8).frame(height: 22)
            .background(bg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(fg)
    }

    private func group<C: View>(@ViewBuilder _ c: () -> C) -> some View {
        VStack(spacing: 0) {
            Group(subviews: c()) { subs in
                ForEach(subs.indices, id: \.self) { i in
                    if i > 0 { Rectangle().fill(p.line).frame(height: 1) }
                    subs[i]
                }
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: p.radius))
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
        .overlay(RoundedRectangle(cornerRadius: p.radius).strokeBorder(p.line))
    }

    private func link<T: View>(_ title: String, chevron: Bool = false, @ViewBuilder trailing: () -> T, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Text(title).font(.ui(15)).foregroundStyle(p.ink).lineLimit(1)
                Spacer(minLength: 0)
                trailing()
                if chevron { chevronMark }
            }
            .padding(.horizontal, 14).frame(minHeight: p.rowHeight)
            .contentShape(Rectangle())
        }.buttonStyle(.plain)
    }

    /// A small picture of the theme: its title bar over a card, in the theme's own colors and corners.
    private func swatch(_ id: String, _ name: String) -> some View {
        let pal = Palette.builtin(id)
        let on = session.theme.base == id
        let r = max(pal.radius - 2, 6)
        return Button {
            var t = session.theme; t.base = id
            if !session.pro.isPro { t = ThemeSpec(base: id, modeColors: t.modeColors) }
            session.saveTheme(t)
        } label: {
            VStack(alignment: .leading, spacing: 8) {
                VStack(alignment: .leading, spacing: 10) {
                    RoundedRectangle(cornerRadius: pal.radius / 5).fill(pal.ink).frame(width: 46, height: 8)
                    VStack(alignment: .leading, spacing: 0) {
                        RoundedRectangle(cornerRadius: pal.radius / 8).fill(pal.accentDef).frame(width: 40, height: 5)
                        RoundedRectangle(cornerRadius: pal.radius / 10).fill(pal.line).frame(width: 56, height: 4).padding(.top, 9)
                        RoundedRectangle(cornerRadius: pal.radius / 10).fill(pal.line).frame(width: 30, height: 4).padding(.top, 6)
                    }
                    .padding(.leading, 8).padding(.top, 10)
                    .frame(maxWidth: .infinity, minHeight: 50, maxHeight: 50, alignment: .topLeading)
                    .background(pal.surface, in: RoundedRectangle(cornerRadius: pal.radius * 0.6))
                    .overlay(RoundedRectangle(cornerRadius: pal.radius * 0.6).strokeBorder(pal.line))
                }
                .padding(.horizontal, 10).padding(.top, 12)
                .frame(maxWidth: .infinity, minHeight: 104, maxHeight: 104, alignment: .topLeading)
                .background(pal.bg, in: RoundedRectangle(cornerRadius: r))
                .overlay(RoundedRectangle(cornerRadius: r).strokeBorder(on ? p.ink : p.line, lineWidth: on ? 2 : 1))
                HStack {
                    Text(name).font(.ui(13, .bold)).foregroundStyle(p.ink)
                    Spacer()
                    if on { Image(systemName: "checkmark").font(.system(size: 12, weight: .bold)).foregroundStyle(p.ink) }
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain).frame(maxWidth: .infinity)
        .accessibilityLabel("主题 \(name)")
        .accessibilityValue(on ? "已选" : "")
    }
}

/// The design's `.tag.pro`: outlined, in the mono face.
private struct ProTag: View {
    @Environment(\.palette) private var p
    var body: some View {
        Text("PRO").font(.mono(11, .semibold)).tracking(0.66).foregroundStyle(p.ink2)
            .padding(.horizontal, 8).frame(height: 20)
            .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(p.ink3, lineWidth: 1))
    }
}
