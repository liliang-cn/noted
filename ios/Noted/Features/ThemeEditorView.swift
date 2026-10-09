import SwiftUI

/// Pro: change the theme's colors, corner radius, and density, with a live preview.
struct ThemeEditorView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var spec = ThemeSpec()

    private let accents: [UInt32] = [0x1F5BFF, 0x276B49, 0xB03A24, 0x6B4E16, 0x0E7C86, 0x1A1A1A]
    private let works: [UInt32] = [0x1F5BFF, 0x2A5F8F, 0x0E7C86]
    private let lives: [UInt32] = [0xB94A14, 0x96560F, 0xB03A24]
    private let bgs: [UInt32] = [0xF4F6F8, 0xF4EEE1, 0xF0F3EC, 0xFFFFFF]

    var body: some View {
        let p = spec.palette
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Button("取消") { dismiss() }
                    Spacer()
                    HStack(spacing: 6) {
                        Text("自定义主题").font(.system(size: 16, weight: .semibold))
                        Text("PRO").font(.system(size: 11, weight: .semibold)).padding(.horizontal, 6).frame(height: 20)
                            .background(Color(hex: 0xF8EBCB), in: RoundedRectangle(cornerRadius: 5)).foregroundStyle(p.warn)
                    }
                    Spacer()
                    Button("保存") { session.saveTheme(spec); dismiss() }.fontWeight(.semibold)
                }
                preview(p)
                VStack(spacing: 0) {
                    row("主色", p) { chips(accents, spec.accent ?? colorHex(p.accentDef)) { spec.accent = $0 } }
                    Divider().overlay(p.line)
                    row("工作色", p) { chips(works, spec.work ?? colorHex(p.work)) { spec.work = $0 } }
                    Divider().overlay(p.line)
                    row("生活色", p) { chips(lives, spec.life ?? colorHex(p.life)) { spec.life = $0 } }
                    Divider().overlay(p.line)
                    row("底色", p) { chips(bgs, spec.bg ?? colorHex(p.bg)) { spec.bg = $0 } }
                    Divider().overlay(p.line)
                    row("圆角", p) {
                        Slider(value: Binding(get: { spec.radius ?? Double(p.radius) }, set: { spec.radius = $0 }), in: 2...24, step: 1)
                        Text("\(Int(spec.radius ?? Double(p.radius)))").font(.system(size: 13, weight: .semibold, design: .monospaced)).foregroundStyle(p.ink)
                    }
                    Divider().overlay(p.line)
                    row("密度", p) {
                        Picker("", selection: Binding(get: { spec.density ?? "regular" }, set: { spec.density = $0 })) {
                            Text("紧凑").tag("compact"); Text("标准").tag("regular"); Text("宽松").tag("roomy")
                        }.pickerStyle(.segmented)
                    }
                }
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
                Button("还原为内置主题") { spec = ThemeSpec(base: spec.base, modeColors: spec.modeColors) }
                    .font(.system(size: 14, weight: .semibold)).foregroundStyle(p.ink2)
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
        .environment(\.palette, p)
        .onAppear { spec = session.theme }
    }

    private func preview(_ p: Palette) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack { Eyebrow(text: "预览").environment(\.palette, p); Spacer() }
            HStack(alignment: .top, spacing: 10) {
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        HStack(spacing: 5) { Circle().fill(p.life).frame(width: 8, height: 8); Text("生活").font(.system(size: 11, weight: .semibold)) }
                            .padding(.horizontal, 8).frame(height: 22).background(p.lifeBg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(p.life)
                        Spacer()
                        Text("43 天").font(.system(size: 13, weight: .semibold, design: .monospaced)).foregroundStyle(p.ink)
                    }
                    Text("菲律宾旅行").font(.system(size: 17, weight: .semibold)).foregroundStyle(p.ink)
                    ProgressBar(value: 0.33, tint: p.life).environment(\.palette, p)
                }
                .padding(14).frame(maxWidth: .infinity, alignment: .leading)
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
                VStack(spacing: 8) {
                    Text("主按钮").font(.system(size: 13, weight: .semibold)).foregroundStyle(.white).padding(.horizontal, 14).frame(height: 34)
                        .background(p.accentDef, in: RoundedRectangle(cornerRadius: max(p.radius - 4, 4)))
                    Text("次按钮").font(.system(size: 13, weight: .semibold)).foregroundStyle(p.ink).padding(.horizontal, 14).frame(height: 34)
                        .background(p.surface2, in: RoundedRectangle(cornerRadius: max(p.radius - 4, 4)))
                }
            }
            VStack(spacing: 0) {
                prow("14:00", "牙医复诊", p.life, p, strong: true)
                Divider().overlay(p.line)
                prow("16:00", "季度报告评审", p.work, p, strong: false)
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
        .padding(14).background(p.bg, in: RoundedRectangle(cornerRadius: p.radius + 4))
        .overlay(RoundedRectangle(cornerRadius: p.radius + 4).stroke(p.line))
    }

    private func prow(_ time: String, _ title: String, _ dot: Color, _ p: Palette, strong: Bool) -> some View {
        HStack(spacing: 12) {
            Text(time).font(.system(size: 12, weight: .medium, design: .monospaced)).foregroundStyle(strong ? p.accentDef : p.ink2).frame(width: 46, alignment: .leading)
            Circle().fill(dot).frame(width: 8, height: 8)
            Text(title).font(.system(size: 14, weight: strong ? .semibold : .medium)).foregroundStyle(p.ink)
            Spacer()
        }.padding(.horizontal, 14).frame(height: p.rowHeight)
    }

    private func row<C: View>(_ title: String, _ p: Palette, @ViewBuilder _ c: () -> C) -> some View {
        HStack(spacing: 12) {
            Text(title).font(.system(size: 14)).foregroundStyle(p.ink2).frame(width: 46, alignment: .leading)
            c()
        }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
    }

    private func chips(_ colors: [UInt32], _ current: UInt32, _ set: @escaping (UInt32) -> Void) -> some View {
        HStack(spacing: 10) {
            ForEach(colors, id: \.self) { c in
                Button { set(c) } label: {
                    Circle().fill(Color(hex: c)).frame(width: 28, height: 28)
                        .overlay(Circle().stroke(Color.black.opacity(0.12)))
                        .overlay(Circle().stroke(Color(hex: 0x0E1A22), lineWidth: current == c ? 2 : 0).padding(-3))
                }
            }
            Spacer(minLength: 0)
        }
    }

    private func colorHex(_ c: Color) -> UInt32 {
        let ui = UIColor(c)
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        ui.getRed(&r, green: &g, blue: &b, alpha: &a)
        return (UInt32((r * 255).rounded()) << 16) | (UInt32((g * 255).rounded()) << 8) | UInt32((b * 255).rounded())
    }
}
