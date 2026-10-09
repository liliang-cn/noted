import SwiftUI

/// 设置: server, intelligence, theme, and Pro.
struct SettingsView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var ai = ProcessInfo.processInfo.environment["NOTED_SHEET"] == "aisettings"
    @State private var editor = ProcessInfo.processInfo.environment["NOTED_SHEET"] == "themeeditor"
    @State private var modules = ProcessInfo.processInfo.environment["NOTED_SHEET"] == "modules"
    @State private var paywall = ProcessInfo.processInfo.environment["NOTED_SHEET"] == "paywall"
    @State private var exporting = false
    @State private var exported: ExportedFile?
    @State private var exportError: String?

    private let themes: [(id: String, name: String)] = [("daylight", "日光"), ("paper", "纸本"), ("moss", "苔绿")]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Button { dismiss() } label: { Label("概览", systemImage: "chevron.left").font(.system(size: 14, weight: .semibold)) }.accessibilityIdentifier("back")
                    Spacer()
                }
                Text("设置").font(.system(size: 30, weight: .bold)).foregroundStyle(p.ink)

                group {
                    link("服务器") {
                        Text("\(session.host):\(session.port)").font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2).lineLimit(1)
                        tag("已连接", p.ok, Color(hex: 0xE1F3EA))
                    } action: {}
                    link("智能") {
                        Text(session.aiChat ? "可用" : "未开启").font(.system(size: 13)).foregroundStyle(p.ink2)
                    } action: { ai = true }
                }

                Eyebrow(text: "主题")
                HStack(spacing: 10) {
                    ForEach(themes, id: \.id) { t in swatch(t.id, t.name) }
                }

                group {
                    HStack {
                        Text("工作 / 生活各用一种颜色").font(.system(size: 15)).foregroundStyle(p.ink)
                        Spacer()
                        Toggle("", isOn: modeColors).labelsHidden().accessibilityLabel("工作 / 生活各用一种颜色")
                    }.padding(.horizontal, 14).frame(minHeight: p.rowHeight).tapsToToggle(modeColors)
                    link("自定义主题") { pro } action: { session.pro.isPro ? (editor = true) : (paywall = true) }
                    link("页面模块") { pro } action: { session.pro.isPro ? (modules = true) : (paywall = true) }
                }

                Eyebrow(text: "提醒")
                group {
                    muteRow("工作模式下静音生活提醒", \.lifeInWork)
                    muteRow("生活模式下静音工作提醒", \.workInLife)
                }

                Eyebrow(text: "数据")
                group {
                    link("导出我的数据") {
                        if exporting { ProgressView() } else {
                            Text("笔记、日程、待办、项目、目标").font(.system(size: 12)).foregroundStyle(p.ink2).lineLimit(1)
                        }
                    } action: { Task { await export() } }
                    .disabled(exporting)
                    .accessibilityIdentifier("export")
                }
                if let exportError { Text(exportError).font(.system(size: 13)).foregroundStyle(p.bad) }

                Button { paywall = true } label: {
                    HStack {
                        Text("Noted Pro").font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink)
                        Spacer()
                        Text(session.pro.isPro ? "已订阅" : "未订阅").font(.system(size: 13)).foregroundStyle(p.ink2)
                        Image(systemName: "chevron.right").font(.system(size: 12)).foregroundStyle(p.ink3)
                    }.padding(.horizontal, 14).frame(minHeight: p.rowHeight).card(padding: 0)
                }.buttonStyle(.plain)

                Button(role: .destructive) {
                    dismiss()
                    session.disconnect()
                } label: {
                    Text("断开连接").font(.system(size: 16, weight: .semibold)).frame(maxWidth: .infinity).frame(height: 48)
                        .background(p.badBg, in: RoundedRectangle(cornerRadius: 10)).foregroundStyle(p.bad)
                }
            }
            .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 24)
        }
        .background(p.bg.ignoresSafeArea())
        .sheet(isPresented: $ai) { AISettingsView() }
        .sheet(isPresented: $editor) { ThemeEditorView() }
        .sheet(isPresented: $modules) { ModulesView() }
        .sheet(isPresented: $paywall) { PaywallView() }
        .sheet(item: $exported) { ActivitySheet(items: [$0.url]) }
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
        return HStack {
            Text(title).font(.system(size: 15)).foregroundStyle(p.ink)
            Spacer()
            Toggle("", isOn: on).labelsHidden().accessibilityLabel(title)
        }.padding(.horizontal, 14).frame(minHeight: p.rowHeight).tapsToToggle(on)
    }

    private var pro: some View {
        tag("PRO", p.warn, Color(hex: 0xF8EBCB))
    }

    private func tag(_ t: String, _ fg: Color, _ bg: Color) -> some View {
        Text(t).font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
            .background(bg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(fg)
    }

    private func group<C: View>(@ViewBuilder _ c: () -> C) -> some View {
        VStack(spacing: 0) {
            Group(subviews: c()) { subs in
                ForEach(subs.indices, id: \.self) { i in
                    if i > 0 { Divider().overlay(p.line) }
                    subs[i]
                }
            }
        }
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
    }

    private func link<T: View>(_ title: String, @ViewBuilder trailing: () -> T, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Text(title).font(.system(size: 15)).foregroundStyle(p.ink)
                Spacer()
                trailing()
            }
            .padding(.horizontal, 14).frame(minHeight: p.rowHeight)
            .contentShape(Rectangle())
        }.buttonStyle(.plain)
    }

    private func swatch(_ id: String, _ name: String) -> some View {
        let pal = Palette.builtin(id)
        let on = session.theme.base == id
        return Button {
            var t = session.theme; t.base = id
            if !session.pro.isPro { t = ThemeSpec(base: id, modeColors: t.modeColors) }
            session.saveTheme(t)
        } label: {
            VStack(spacing: 8) {
                ZStack {
                    RoundedRectangle(cornerRadius: pal.radius).fill(pal.bg)
                    VStack(spacing: 5) {
                        RoundedRectangle(cornerRadius: 3).fill(pal.surface).frame(height: 14)
                            .overlay(RoundedRectangle(cornerRadius: 3).stroke(pal.line))
                        HStack(spacing: 4) {
                            Circle().fill(pal.work).frame(width: 8, height: 8)
                            Circle().fill(pal.life).frame(width: 8, height: 8)
                            Spacer()
                        }
                        RoundedRectangle(cornerRadius: 3).fill(pal.accentDef).frame(width: 30, height: 8)
                    }.padding(10)
                }
                .frame(height: 84)
                .overlay(RoundedRectangle(cornerRadius: pal.radius).stroke(on ? session.mode.accent(p) : p.line, lineWidth: on ? 2 : 1))
                Text(name).font(.system(size: 13, weight: .semibold)).foregroundStyle(p.ink)
            }
        }
        .buttonStyle(.plain).frame(maxWidth: .infinity)
        .accessibilityLabel("主题 \(name)")
        .accessibilityValue(on ? "已选" : "")
    }
}
