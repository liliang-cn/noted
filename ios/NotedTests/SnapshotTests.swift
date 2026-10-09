import SwiftUI
import XCTest
@testable import Noted

@MainActor
final class SnapshotTests: XCTestCase {
    private func sample() -> Snapshot {
        var s = Snapshot()
        s.pro = true
        s.items = [
            .init(id: "e1", title: "团队站会", time: Date(timeIntervalSince1970: 1_800_000_000), work: true),
            .init(id: "t1", title: "交房租", time: nil, isTask: true, overdue: true),
        ]
        s.goals = [.init(id: "g1", title: "运动", done: 2, target: 3, unit: "次")]
        return s
    }

    func testEncodeDecodeKeepsEverything() {
        let s = sample()
        let back = SnapshotStore.decode(SnapshotStore.encode(s)!)
        XCTAssertEqual(back?.items, s.items)
        XCTAssertEqual(back?.goals, s.goals)
        XCTAssertEqual(back?.pro, true)
    }

    func testStorePersistsToFile() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("snap-\(UUID().uuidString).json")
        SnapshotStore.overrideURL = url
        defer { SnapshotStore.overrideURL = nil; try? FileManager.default.removeItem(at: url) }
        XCTAssertNil(SnapshotStore.load())
        SnapshotStore.save(sample())
        XCTAssertEqual(SnapshotStore.load()?.items.count, 2)
    }

    func testDecodesWhenFieldsAreMissing() {
        let s = SnapshotStore.decode(Data("{}".utf8))
        XCTAssertNotNil(s)
        XCTAssertFalse(s!.pro)
        XCTAssertTrue(s!.items.isEmpty)
    }

    func testNextPrefersTheUpcomingTimedItem() {
        var s = Snapshot()
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        s.items = [
            .init(id: "1", title: "已过去", time: now.addingTimeInterval(-3600)),
            .init(id: "2", title: "下一个", time: now.addingTimeInterval(600)),
            .init(id: "3", title: "更晚", time: now.addingTimeInterval(7200)),
        ]
        XCTAssertEqual(s.next(now: now)?.title, "下一个")
        s.items = [.init(id: "1", title: "已过去", time: now.addingTimeInterval(-3600)), .init(id: "t", title: "某待办", isTask: true)]
        XCTAssertEqual(s.next(now: now)?.title, "某待办", "with nothing ahead, fall back to an open task")
        s.items = []
        XCTAssertNil(s.next(now: now))
    }

    func testGoalPercentIsCappedAndSafe() {
        XCTAssertEqual(Snapshot.Goal(id: "", title: "", done: 5, target: 3, unit: "").percent, 1)
        XCTAssertEqual(Snapshot.Goal(id: "", title: "", done: 1, target: 0, unit: "").percent, 0)
    }

    func testBuilderFromServerResponse() {
        var focus = Noted_V1_GetFocusResponse()
        var ev = Noted_V1_FocusItem()
        var o = Noted_V1_Occurrence()
        o.event.id = "e9"; o.event.title = "牙医复诊"; o.event.space = .life
        o.startTime = .init(Date(timeIntervalSince1970: 1_800_000_000))
        ev.event = o
        var tk = Noted_V1_FocusItem()
        var t = Noted_V1_Task(); t.id = "t9"; t.title = "交房租"; t.space = .work
        tk.task = t; tk.overdue = true
        focus.items = [ev, tk]
        var g = Noted_V1_Goal(); g.id = "g9"; g.title = "德语"; g.unit = "分钟"; g.space = .life
        g.progress.done = 95; g.progress.target = 140; g.progress.behind = true

        let s = SnapshotBuilder.make(focus: focus, goals: [g], palette: .daylight, pro: true)
        XCTAssertEqual(s.items.map(\.title), ["牙医复诊", "交房租"])
        XCTAssertFalse(s.items[0].isTask)
        XCTAssertTrue(s.items[1].isTask && s.items[1].overdue && s.items[1].work)
        XCTAssertEqual(s.items[1].id, "tt9")
        XCTAssertEqual(s.goals.first?.done, 95)
        XCTAssertTrue(s.goals.first?.behind == true)
        XCTAssertTrue(s.pro)
        XCTAssertEqual(s.colors.bg, 0xF4F6F8, "the widget uses the app's theme colors")
    }

    func testColorsFollowTheTheme() {
        XCTAssertEqual(SnapshotBuilder.colors(.paper).bg, 0xF4EEE1)
        XCTAssertEqual(SnapshotBuilder.colors(.moss).accent, 0x276B49)
    }

    // MARK: the views render

    private func render<V: View>(_ v: V, width: CGFloat = 170, height: CGFloat = 170) -> UIImage? {
        let r = ImageRenderer(content: v.frame(width: width, height: height).background(Color.white))
        r.scale = 2
        return r.uiImage
    }

    func testWidgetViewsRender() {
        let s = sample()
        XCTAssertNotNil(render(SmallSnapshotView(s: s)))
        XCTAssertNotNil(render(MediumSnapshotView(s: s), width: 360))
        XCTAssertNotNil(render(RectangularSnapshotView(s: s), width: 160, height: 60))
        XCTAssertNotNil(render(CircularSnapshotView(s: s), width: 60, height: 60))
        XCTAssertNotNil(render(LockedView(c: s.colors)))
    }

    func testEmptySnapshotRendersWithoutCrashing() {
        let s = Snapshot()
        XCTAssertNotNil(render(SmallSnapshotView(s: s)))
        XCTAssertNotNil(render(MediumSnapshotView(s: s), width: 360))
        XCTAssertNotNil(render(InlineSnapshotView(s: s), width: 200, height: 20))
    }

    func testRowTimeText() {
        XCTAssertEqual(snapTime(.init(id: "", title: "", allDay: true)), "全天")
        XCTAssertEqual(snapTime(.init(id: "", title: "", time: nil)), "")
        var c = DateComponents(); c.year = 2026; c.month = 10; c.day = 8; c.hour = 9; c.minute = 5
        XCTAssertEqual(snapTime(.init(id: "", title: "", time: Calendar.current.date(from: c))), "09:05")
    }

    /// Writes each widget size to a folder, for looking at. A no-op unless NOTED_WIDGET_PNG_DIR is set.
    func testWriteWidgetPictures() throws {
        guard let dir = ProcessInfo.processInfo.environment["NOTED_WIDGET_PNG_DIR"] else { return }
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        var s = Sample.today
        s.pro = true
        func write<V: View>(_ name: String, _ v: V, _ w: CGFloat, _ h: CGFloat, bg: Color) throws {
            let r = ImageRenderer(content: v.padding(14).frame(width: w, height: h).background(bg))
            r.scale = 3
            try XCTUnwrap(r.uiImage?.pngData()).write(to: URL(fileURLWithPath: "\(dir)/\(name).png"))
        }
        let bg = Color(rgb: s.colors.bg)
        try write("small", SmallSnapshotView(s: s), 170, 170, bg: bg)
        try write("medium", MediumSnapshotView(s: s), 364, 170, bg: bg)
        try write("rect", RectangularSnapshotView(s: s), 172, 76, bg: .black.opacity(0.0))
        try write("circular", CircularSnapshotView(s: s), 76, 76, bg: .clear)
        try write("locked", LockedView(c: s.colors), 170, 170, bg: bg)
        var paper = s; paper.colors = SnapshotBuilder.colors(.paper)
        try write("small-paper", SmallSnapshotView(s: paper), 170, 170, bg: Color(rgb: paper.colors.bg))
    }
}
