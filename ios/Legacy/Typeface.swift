import CoreText
import SwiftUI
import UIKit

/// The design's type: Geist for text, Geist Mono for figures and times, and PingFang SC of the
/// same weight for Chinese, which Geist does not cover.
enum Typeface {
    /// Registers the bundled faces. Call once at launch, before any view draws.
    static func register() {
        let urls = Bundle.main.urls(forResourcesWithExtension: "ttf", subdirectory: nil) ?? []
        CTFontManagerRegisterFontURLs(urls as CFArray, .process, true, nil)
    }

    nonisolated(unsafe) private static var cache: [String: Font] = [:]
    private static let lock = NSLock()

    static func font(_ size: CGFloat, _ weight: Font.Weight, mono: Bool) -> Font {
        let face: String, cjk: String
        switch weight {
        case .bold, .heavy, .black: face = "Bold"; cjk = "PingFangSC-Semibold"
        case .semibold: face = "SemiBold"; cjk = "PingFangSC-Semibold"
        case .regular, .light, .thin, .ultraLight: face = "Regular"; cjk = "PingFangSC-Regular"
        default: face = "Medium"; cjk = "PingFangSC-Medium"
        }
        let name = (mono ? "GeistMono-" : "Geist-") + face
        let key = "\(name)|\(size)"
        lock.lock(); defer { lock.unlock() }
        if let f = cache[key] { return f }
        let desc = UIFontDescriptor(name: name, size: size)
            .addingAttributes([.cascadeList: [UIFontDescriptor(name: cjk, size: size)]])
        var ui = UIFont(descriptor: desc, size: size)
        if ui.fontName != name { ui = .systemFont(ofSize: size) }   // faces not registered: stay readable
        let f = Font(ui)
        cache[key] = f
        return f
    }
}

extension Font {
    /// Text in the design's face. Body text is 500, as in the design.
    static func ui(_ size: CGFloat, _ weight: Font.Weight = .medium) -> Font { Typeface.font(size, weight, mono: false) }
    /// Figures, dates and times: tabular Geist Mono.
    static func mono(_ size: CGFloat, _ weight: Font.Weight = .medium) -> Font { Typeface.font(size, weight, mono: true) }
}
