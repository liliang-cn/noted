import SwiftUI
import XCTest
import GRPCCore
@testable import Noted

final class ModelTests: XCTestCase {
    // MARK: theme

    func testThemeRoundTrip() throws {
        var t = ThemeSpec(base: "paper")
        t.accent = 0x123456; t.radius = 9; t.density = "compact"; t.modeColors = false
        let back = try JSONDecoder().decode(ThemeSpec.self, from: JSONEncoder().encode(t))
        XCTAssertEqual(t, back)
    }

    /// A theme stored by an older or newer app may lack fields; it must still load.
    func testThemeDecodesWhenFieldsAreMissing() throws {
        let t = try JSONDecoder().decode(ThemeSpec.self, from: Data(#"{"base":"moss"}"#.utf8))
        XCTAssertEqual(t.base, "moss")
        XCTAssertTrue(t.modeColors)
        XCTAssertNil(t.accent)
        let empty = try JSONDecoder().decode(ThemeSpec.self, from: Data("{}".utf8))
        XCTAssertEqual(empty.base, "daylight")
    }

    func testThemeOverridesApply() {
        var t = ThemeSpec(base: "daylight")
        XCTAssertFalse(t.isCustom)
        t.radius = 4; t.density = "roomy"; t.accent = 0x00FF00
        XCTAssertTrue(t.isCustom)
        XCTAssertEqual(t.palette.radius, 4)
        XCTAssertEqual(t.palette.rowHeight, 60)
        t.density = "compact"
        XCTAssertEqual(t.palette.rowHeight, 44)
    }

    func testUnknownThemeFallsBackToDaylight() {
        XCTAssertEqual(Palette.builtin("nope").radius, Palette.daylight.radius)
        XCTAssertEqual(Palette.builtin("paper").radius, 6)
        XCTAssertEqual(Palette.builtin("moss").radius, 20)
    }

    func testModeAccentFollowsModeOnlyWhenEnabled() {
        var p = Palette.daylight
        XCTAssertEqual(Mode.life.accent(p), p.life)
        XCTAssertEqual(Mode.work.accent(p), p.work)
        XCTAssertEqual(Mode.all.accent(p), p.accentDef)
        p.modeColors = false
        XCTAssertEqual(Mode.life.accent(p), p.accentDef)
        XCTAssertEqual(Mode.work.accent(p), p.accentDef)
    }

    func testModeMapsToServerSpace() {
        XCTAssertEqual(Mode.all.space, .unspecified)
        XCTAssertEqual(Mode.life.space, .life)
        XCTAssertEqual(Mode.work.space, .work)
    }

    // MARK: layout

    func testLayoutDefaults() {
        let l = Layout()
        XCTAssertEqual(l.modules(for: .all).filter(\.on).map(\.id), ["pinned", "items", "goals"])
        XCTAssertEqual(Layout.standard.count, Layout.catalog.count)
    }

    func testLayoutSharedUntilPerMode() {
        var l = Layout()
        var m = l.modules(for: .work)
        m[0].on = false
        l.set(m, for: .work)
        XCTAssertFalse(l.modules(for: .life)[0].on, "one shared layout: the change shows in every mode")
        l.perMode = true
        l.life = Layout.standard
        var w = l.modules(for: .work)
        w.reverse()
        l.set(w, for: .work)
        XCTAssertEqual(l.modules(for: .life).first?.id, "pinned")
        XCTAssertEqual(l.modules(for: .work).first?.id, "heat")
    }

    func testLayoutDecodesWhenFieldsAreMissing() throws {
        let l = try JSONDecoder().decode(Layout.self, from: Data(#"{"perMode":true}"#.utf8))
        XCTAssertTrue(l.perMode)
        XCTAssertEqual(l.all, Layout.standard)
    }

    // MARK: formatting

    func testTimeKeepsTheMinutesPadded() {
        var c = DateComponents(); c.year = 2026; c.month = 10; c.day = 8; c.hour = 9; c.minute = 0
        let d = Calendar.current.date(from: c)!
        XCTAssertEqual(Fmt.hm(d), "09:00")
        XCTAssertEqual(Fmt.ymd(d), "2026-10-08")
        c.hour = 23; c.minute = 5
        XCTAssertEqual(Fmt.hm(Calendar.current.date(from: c)!), "23:05")
    }

    func testNumbers() {
        XCTAssertEqual(Fmt.number(3), "3")
        XCTAssertEqual(Fmt.number(2.5), "2.5")
        XCTAssertEqual(Fmt.number(140), "140")
    }

    // MARK: errors

    func testErrorsAreShownInChinese() {
        XCTAssertEqual(describe(RPCError(code: .unauthenticated, message: "invalid token")), "令牌无效或已被撤销,请重新连接")
        XCTAssertEqual(describe(RPCError(code: .unavailable, message: "connection refused")), "连不上服务器")
        XCTAssertEqual(describe(RPCError(code: .failedPrecondition, message: "AI is not enabled")), "这台服务器没有开启智能功能")
        XCTAssertEqual(describe(RPCError(code: .internalError, message: "AI request failed (ask); check the server log")), "智能功能这次没有答上来,稍后再试")
        XCTAssertEqual(describe(RPCError(code: .notFound, message: "note not found")), "note not found")
    }
}

final class LayoutReconcileTests: XCTestCase {
    func testModulesAddedLaterAppearSwitchedOff() throws {
        let l = try JSONDecoder().decode(Layout.self, from: Data(#"{"all":[{"id":"items","on":true},{"id":"pinned","on":true}]}"#.utf8))
        XCTAssertEqual(l.all.prefix(2).map(\.id), ["items", "pinned"], "the saved order is kept")
        XCTAssertEqual(l.all.count, Layout.catalog.count)
        XCTAssertFalse(l.all.first { $0.id == "heat" }!.on)
    }

    func testUnknownModulesAreDropped() throws {
        let l = try JSONDecoder().decode(Layout.self, from: Data(#"{"all":[{"id":"gone","on":true},{"id":"goals","on":true}]}"#.utf8))
        XCTAssertFalse(l.all.contains { $0.id == "gone" })
        XCTAssertTrue(l.all.contains { $0.id == "goals" && $0.on })
    }
}
