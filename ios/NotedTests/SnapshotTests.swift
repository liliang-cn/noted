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
        XCTAssertEqual(s.colors.bg, 0xF2F2F7, "the widget uses the app's theme colors")
    }

    func testBuilderCarriesPlansAndHoldings() {
        var swim = Noted_V1_Goal(); swim.id = "g1"; swim.title = "游泳"; swim.unit = "次"; swim.objectiveID = "o1"
        swim.progress.done = 1; swim.progress.target = 2
        var german = Noted_V1_Goal(); german.id = "g2"; german.title = "德语"; german.progress.target = 140
        var o = Noted_V1_Objective()
        o.id = "o1"; o.title = "减肥"; o.space = .life; o.metricName = "体重"; o.metricUnit = "kg"; o.metricStart = 75; o.metricTarget = 68
        o.progress.hasMetric_p = true; o.progress.hasReading_p = true; o.progress.metricCurrent = 73.2; o.progress.percent = 0.257
        o.progress.onTrack = false; o.progress.daysLeft = 60; o.goals = [swim]
        var h = Noted_V1_Holding()
        h.id = "h1"; h.symbol = "QQQM"; h.dcaDay = 15; h.dcaAmount = 500; h.currency = "USD"
        h.position.shares = 12; h.position.costBasis = 1000; h.position.hasPrice_p = true; h.position.unrealizedPercent = 0.05
        h.position.dca.active = true; h.position.dca.doneThisMonth = false; h.position.dca.streakMonths = 3
        h.position.dca.nextTime = .init(Date(timeIntervalSince1970: 1_800_000_000))
        var idle = Noted_V1_Holding(); idle.id = "h2"; idle.symbol = "OKLO"

        let s = SnapshotBuilder.make(focus: Noted_V1_GetFocusResponse(), goals: [swim, german], objectives: [o], holdings: [h, idle], palette: .daylight, pro: true)
        XCTAssertEqual(s.goals.map(\.title), ["德语"], "a dimension is shown inside its plan, not twice")
        let p = try! XCTUnwrap(s.plans.first)
        XCTAssertEqual(p.title, "减肥"); XCTAssertEqual(p.current, 73.2); XCTAssertEqual(p.target, 68); XCTAssertEqual(p.unit, "kg")
        XCTAssertEqual(p.percent, 0.257, accuracy: 1e-9); XCTAssertEqual(p.daysLeft, 60); XCTAssertTrue(p.offTrack)
        XCTAssertEqual(p.dims.map(\.id), ["g1"], "the watch checks in by goal id")
        XCTAssertEqual(s.holdings.count, 2)
        let q = s.holdings[0]
        XCTAssertEqual(q.symbol, "QQQM"); XCTAssertEqual(q.planDay, 15); XCTAssertEqual(q.gainPercent ?? 0, 0.05, accuracy: 1e-9)
        XCTAssertTrue(q.todo); XCTAssertEqual(q.streak, 3); XCTAssertNotNil(q.next)
        XCTAssertNil(s.holdings[1].next); XCTAssertFalse(s.holdings[1].todo)
        XCTAssertEqual(s.planned.map(\.symbol), ["QQQM"], "only holdings with a plan are listed for investing")
        let back = SnapshotStore.decode(SnapshotStore.encode(s)!)
        XCTAssertEqual(back?.plans, s.plans); XCTAssertEqual(back?.holdings, s.holdings)
    }

    func testAnOldSnapshotWithoutPlansStillLoads() {
        let s = SnapshotStore.decode(Data(#"{"pro":true,"items":[],"goals":[]}"#.utf8))
        XCTAssertEqual(s?.plans, []); XCTAssertEqual(s?.holdings, []); XCTAssertEqual(s?.pro, true)
    }

    func testPlanAndHoldingOrdering() {
        var s = Snapshot()
        s.plans = [.init(id: "a", title: "近完成", percent: 0.9), .init(id: "b", title: "刚开始", percent: 0.1), .init(id: "c", title: "落后", percent: 0.5, offTrack: true)]
        XCTAssertEqual(s.featuredPlan?.id, "c", "the plan that is off track is shown first")
        s.plans[2].offTrack = false
        XCTAssertEqual(s.featuredPlan?.id, "b", "then the one furthest from done")
        let soon = Date(timeIntervalSince1970: 1_800_000_000), later = soon.addingTimeInterval(86_400)
        s.holdings = [
            .init(id: "1", symbol: "AAA", planDay: 20, doneThisMonth: true, next: soon),
            .init(id: "2", symbol: "BBB", planDay: 25, doneThisMonth: false, next: later),
            .init(id: "3", symbol: "CCC", planDay: 10, doneThisMonth: false, next: soon),
            .init(id: "4", symbol: "NOPLAN"),
        ]
        XCTAssertEqual(s.planned.map(\.symbol), ["CCC", "BBB", "AAA"], "still to buy this month first, then by date; no-plan holdings are left out")
    }

    func testUntilText() {
        let cal = Calendar.current
        let now = cal.date(from: DateComponents(year: 2026, month: 10, day: 9, hour: 15))!
        XCTAssertEqual(snapUntil(cal.date(byAdding: .hour, value: 2, to: now), now: now), "今天")
        XCTAssertEqual(snapUntil(cal.date(byAdding: .day, value: 1, to: now), now: now), "明天")
        XCTAssertEqual(snapUntil(cal.date(byAdding: .day, value: 6, to: now), now: now), "6 天后")
        XCTAssertEqual(snapUntil(nil, now: now), "")
        XCTAssertEqual(snapPercent(0.257), "26%"); XCTAssertEqual(snapPercent(1.4), "100%"); XCTAssertEqual(snapPercent(-1), "0%")
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

    func testPlanAndInvestViewsRender() {
        let s = Sample.today
        XCTAssertNotNil(render(PlanSmallView(s: s)))
        XCTAssertNotNil(render(PlanMediumView(s: s), width: 360))
        XCTAssertNotNil(render(PlanRectangularView(s: s), width: 160, height: 60))
        XCTAssertNotNil(render(PlanCircularView(s: s), width: 60, height: 60))
        XCTAssertNotNil(render(InvestSmallView(s: s)))
        XCTAssertNotNil(render(InvestMediumView(s: s), width: 360))
        XCTAssertNotNil(render(InvestRectangularView(s: s), width: 160, height: 60))
        XCTAssertNotNil(render(InvestCircularView(s: s), width: 60, height: 60))
        let empty = Snapshot()
        XCTAssertNotNil(render(PlanSmallView(s: empty)))
        XCTAssertNotNil(render(PlanMediumView(s: empty), width: 360))
        XCTAssertNotNil(render(InvestSmallView(s: empty)))
        XCTAssertNotNil(render(InvestMediumView(s: empty), width: 360))
        XCTAssertNotNil(render(InvestRectangularView(s: empty), width: 160, height: 60))
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
        try write("plan-small", PlanSmallView(s: s), 170, 170, bg: bg)
        try write("plan-medium", PlanMediumView(s: s), 364, 170, bg: bg)
        try write("plan-rect", PlanRectangularView(s: s), 172, 76, bg: .black.opacity(0.0))
        try write("invest-small", InvestSmallView(s: s), 170, 170, bg: bg)
        try write("invest-medium", InvestMediumView(s: s), 364, 170, bg: bg)
        try write("invest-rect", InvestRectangularView(s: s), 172, 76, bg: .black.opacity(0.0))
        try write("locked", LockedView(c: s.colors), 170, 170, bg: bg)
        var paper = s; paper.colors = SnapshotBuilder.colors(.paper)
        try write("small-paper", SmallSnapshotView(s: paper), 170, 170, bg: Color(rgb: paper.colors.bg))
    }
}
