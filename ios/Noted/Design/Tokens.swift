import SwiftUI
import UIKit

// The prototype's :root tokens. Light only. Use them as `.foregroundStyle(.personal)`,
// `Color.success`, `.background(.groupedBackground)`. The accent is the environment tint
// (`.tint`, `Color.accentColor`), set at the root from Session.accent, so the 主题 page can change it.

extension ShapeStyle where Self == Color {
    /// Default accent, #1F5BFF. Read the live one with `.tint` / `Color.accentColor`.
    static var brand: Color { Color(hex: 0x1F5BFF) }
    /// 个人 space.
    static var personal: Color { Color(hex: 0xC2410C) }
    /// 工作 space.
    static var work: Color { Color(hex: 0x1F5BFF) }
    static var success: Color { Color(hex: 0x1B8A55) }
    static var danger: Color { Color(hex: 0xE5484D) }
    static var warning: Color { Color(hex: 0xE8890C) }
    /// The iOS switch green.
    static var switchOn: Color { Color(hex: 0x34C759) }

    /// Page background behind inset-grouped lists, #F2F2F7.
    static var groupedBackground: Color { Color(hex: 0xF2F2F7) }
    /// List cells and cards.
    static var cell: Color { .white }
    /// rgba(60,60,67,.6): subtitles, values, captions.
    static var secondaryText: Color { Color(red: 60 / 255, green: 60 / 255, blue: 67 / 255).opacity(0.6) }
    /// rgba(60,60,67,.3): placeholders, chevrons.
    static var tertiaryText: Color { Color(red: 60 / 255, green: 60 / 255, blue: 67 / 255).opacity(0.3) }
    /// rgba(60,60,67,.18): hairlines.
    static var hairline: Color { Color(red: 60 / 255, green: 60 / 255, blue: 67 / 255).opacity(0.18) }
    /// rgba(118,118,128,.12): tracks, chips, pills, steppers.
    static var fillTrack: Color { Color(red: 118 / 255, green: 118 / 255, blue: 128 / 255).opacity(0.12) }
    /// rgba(118,118,128,.08): quieter fills (inline inputs, code).
    static var fillQuiet: Color { Color(red: 118 / 255, green: 118 / 255, blue: 128 / 255).opacity(0.08) }
    /// Tab-bar unselected gray, #8E8E93.
    static var tabInactive: Color { Color(hex: 0x8E8E93) }
}

extension Color {
    /// `color` mixed into white, like the prototype's color-mix(in srgb, X n%, #fff): tinted backgrounds.
    func wash(_ amount: Double = 0.12) -> Color {
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        UIColor(self).getRed(&r, green: &g, blue: &b, alpha: &a)
        let k = CGFloat(amount)
        return Color(red: Double(1 - k + r * k), green: Double(1 - k + g * k), blue: Double(1 - k + b * k))
    }
}

/// The colored square behind a form row's icon (`--t-*`).
enum TileColor: String, CaseIterable, Sendable {
    case red, blue, orange, gray, teal, graphite, green, rust

    var color: Color {
        switch self {
        case .red: Color(hex: 0xE5484D)
        case .blue: Color(hex: 0x1F5BFF)
        case .orange: Color(hex: 0xE8890C)
        case .gray: Color(hex: 0x8E8E93)
        case .teal: Color(hex: 0x0F8B8D)
        case .graphite: Color(hex: 0x636366)
        case .green: Color(hex: 0x1B8A55)
        case .rust: Color(hex: 0xC2410C)
        }
    }
}

/// The accents the 主题 page offers, stored as ThemeSpec.accent.
enum AccentChoice: UInt32, CaseIterable, Identifiable, Sendable {
    case blue = 0x1F5BFF, rust = 0xC2410C, pine = 0x1B8A55, teal = 0x0F8B8D, graphite = 0x3A3A3C
    var id: UInt32 { rawValue }
    var color: Color { Color(hex: rawValue) }
    var label: String {
        switch self {
        case .blue: "蓝色"
        case .rust: "陶土"
        case .pine: "松绿"
        case .teal: "青色"
        case .graphite: "石墨"
        }
    }
}

extension Noted_V1_Space {
    /// The space's color: 工作 blue, everything else 个人 rust.
    var tint: Color { self == .work ? .work : .personal }
}
