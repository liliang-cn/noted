import SwiftUI

extension Color {
    init(hex: UInt32) {
        self.init(
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255)
    }
}

/// The values every screen reads. A theme is a set of these and nothing else.
struct Palette: Sendable {
    var bg, surface, surface2, ink, ink2, ink3, line: Color
    var accentDef, work, life, ok, bad, warn: Color
    var workBg, lifeBg, badBg: Color
    var radius: CGFloat
    var rowHeight: CGFloat
    /// Whether the accent follows the work / life mode.
    var modeColors = true

    static let daylight = Palette(
        bg: Color(hex: 0xF4F6F8), surface: Color(hex: 0xFFFFFF), surface2: Color(hex: 0xEBEEF1),
        ink: Color(hex: 0x0E1A22), ink2: Color(hex: 0x55626C), ink3: Color(hex: 0x7C8790), line: Color(hex: 0xDDE2E7),
        accentDef: Color(hex: 0x1F5BFF), work: Color(hex: 0x1F5BFF), life: Color(hex: 0xB94A14), ok: Color(hex: 0x1B7F51),
        bad: Color(hex: 0xC62F26), warn: Color(hex: 0x8A5A00),
        workBg: Color(hex: 0xE6EDFF), lifeBg: Color(hex: 0xFBE9DD), badBg: Color(hex: 0xFBE4E2),
        radius: 14, rowHeight: 52)

    static let paper = Palette(
        bg: Color(hex: 0xF4EEE1), surface: Color(hex: 0xFBF7EE), surface2: Color(hex: 0xECE4D2),
        ink: Color(hex: 0x2A241C), ink2: Color(hex: 0x5F5546), ink3: Color(hex: 0x8A7F6B), line: Color(hex: 0xDCD2BC),
        accentDef: Color(hex: 0xB03A24), work: Color(hex: 0x2B5A88), life: Color(hex: 0x96560F), ok: Color(hex: 0x386F43),
        bad: Color(hex: 0xB03A24), warn: Color(hex: 0x7A5200),
        workBg: Color(hex: 0xE1EAF2), lifeBg: Color(hex: 0xF3E4CC), badBg: Color(hex: 0xF4DDD6),
        radius: 6, rowHeight: 50)

    static let moss = Palette(
        bg: Color(hex: 0xF0F3EC), surface: Color(hex: 0xFFFFFF), surface2: Color(hex: 0xE3E9DD),
        ink: Color(hex: 0x17251C), ink2: Color(hex: 0x4F6054), ink3: Color(hex: 0x7B8C80), line: Color(hex: 0xD5DDCF),
        accentDef: Color(hex: 0x276B49), work: Color(hex: 0x2A5F8F), life: Color(hex: 0xA2561A), ok: Color(hex: 0x276B49),
        bad: Color(hex: 0xBE3A2E), warn: Color(hex: 0x835400),
        workBg: Color(hex: 0xE1EBF4), lifeBg: Color(hex: 0xF7E6D6), badBg: Color(hex: 0xF9E1DE),
        radius: 20, rowHeight: 54)

    static func builtin(_ id: String) -> Palette {
        switch id {
        case "paper": .paper
        case "moss": .moss
        default: .daylight
        }
    }

    init(bg: Color, surface: Color, surface2: Color, ink: Color, ink2: Color, ink3: Color, line: Color,
         accentDef: Color, work: Color, life: Color, ok: Color, bad: Color, warn: Color,
         workBg: Color, lifeBg: Color, badBg: Color, radius: CGFloat, rowHeight: CGFloat) {
        self.bg = bg; self.surface = surface; self.surface2 = surface2
        self.ink = ink; self.ink2 = ink2; self.ink3 = ink3; self.line = line
        self.accentDef = accentDef; self.work = work; self.life = life
        self.ok = ok; self.bad = bad; self.warn = warn
        self.workBg = workBg; self.lifeBg = lifeBg; self.badBg = badBg
        self.radius = radius; self.rowHeight = rowHeight
    }
}

/// What the person chose, as stored in the "ui.theme" preference. Only the
/// differences from the base theme are kept.
struct ThemeSpec: Codable, Equatable, Sendable {
    var base = "daylight"
    var accent: UInt32?
    var work: UInt32?
    var life: UInt32?
    var bg: UInt32?
    var radius: Double?
    var density: String?   // compact | regular | roomy
    var modeColors = true

    init(base: String = "daylight", modeColors: Bool = true) {
        self.base = base
        self.modeColors = modeColors
    }

    /// Any field may be missing: a theme saved by another version of the app still loads.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        base = try c.decodeIfPresent(String.self, forKey: .base) ?? "daylight"
        accent = try c.decodeIfPresent(UInt32.self, forKey: .accent)
        work = try c.decodeIfPresent(UInt32.self, forKey: .work)
        life = try c.decodeIfPresent(UInt32.self, forKey: .life)
        bg = try c.decodeIfPresent(UInt32.self, forKey: .bg)
        radius = try c.decodeIfPresent(Double.self, forKey: .radius)
        density = try c.decodeIfPresent(String.self, forKey: .density)
        modeColors = try c.decodeIfPresent(Bool.self, forKey: .modeColors) ?? true
    }

    var isCustom: Bool { accent != nil || work != nil || life != nil || bg != nil || radius != nil || density != nil }

    var palette: Palette {
        var p = Palette.builtin(base)
        if let a = accent { p.accentDef = Color(hex: a) }
        if let w = work { p.work = Color(hex: w); p.workBg = Color(hex: w).opacity(0.12) }
        if let l = life { p.life = Color(hex: l); p.lifeBg = Color(hex: l).opacity(0.12) }
        if let b = bg { p.bg = Color(hex: b) }
        if let r = radius { p.radius = CGFloat(r) }
        switch density {
        case "compact": p.rowHeight = 44
        case "roomy": p.rowHeight = 60
        default: break
        }
        p.modeColors = modeColors
        return p
    }
}

extension EnvironmentValues {
    @Entry var palette: Palette = .daylight
    @Entry var mode: Mode = .all
}

/// 全部 / 生活 / 工作. The accent follows it.
enum Mode: String, CaseIterable, Identifiable, Sendable {
    case all, life, work
    var id: String { rawValue }
    var label: String {
        switch self {
        case .all: "全部"
        case .life: "生活"
        case .work: "工作"
        }
    }
    var space: Noted_V1_Space {
        switch self {
        case .all: .unspecified
        case .life: .life
        case .work: .work
        }
    }
    func accent(_ p: Palette) -> Color {
        guard p.modeColors else { return p.accentDef }
        switch self {
        case .all: return p.accentDef
        case .life: return p.life
        case .work: return p.work
        }
    }
}

extension Noted_V1_Space {
    func color(_ p: Palette) -> Color { self == .work ? p.work : p.life }
    var label: String { self == .work ? "工作" : "生活" }
}

struct Card<Content: View>: ViewModifier {
    @Environment(\.palette) private var p
    var padding: CGFloat = 14
    func body(content: Self.Content) -> some View {
        content
            .padding(padding)
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
            .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))
    }
}

extension View {
    func card(padding: CGFloat = 14) -> some View { modifier(Card<Self>(padding: padding)) }
}

struct Eyebrow: View {
    @Environment(\.palette) private var p
    let text: String
    var body: some View {
        Text(text).font(.system(size: 11, weight: .medium, design: .monospaced)).tracking(0.8).foregroundStyle(p.ink3)
    }
}

struct ProgressBar: View {
    @Environment(\.palette) private var p
    let value: Double
    let tint: Color
    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(p.surface2)
                Capsule().fill(tint).frame(width: g.size.width * min(max(value, 0), 1))
            }
        }
        .frame(height: 6)
    }
}

extension View {
    /// A row with a switch: tapping anywhere on the row, not only the small switch, flips it.
    func tapsToToggle(_ value: Binding<Bool>) -> some View {
        contentShape(Rectangle()).onTapGesture { value.wrappedValue.toggle() }
    }
}
