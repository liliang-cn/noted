import XCTest

/// 日历: 日 / 周 / 月, a new event from an empty hour, opening, renaming and deleting an event.
final class CalendarUITests: NotedUITestCase {
    private func mode(_ m: String) {
        let b = app.segmentedControls["calendar-mode"].buttons[m]
        exists(b)
        b.tap()
    }

    private var todayKey: String {
        let f = DateFormatter(); f.dateFormat = "yyyy-MM-dd"
        return f.string(from: .now)
    }

    private func block(_ title: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'event-block-' AND label CONTAINS %@", title)).firstMatch
    }

    func testDayWeekMonth() throws {
        try launch(tab: "calendar")
        exists(app.navigationBars["日历"])
        mode("周")
        exists(app.buttons["cal-week-day-\(todayKey)"])
        mode("月")
        exists(app.buttons["cal-month-day-\(todayKey)"])
        mode("日")
        exists(app.buttons["cal-day-\(todayKey)"])
        exists(app.buttons["cal-slot-7"])
        exists(block("团队站会"))
    }

    func testTappingAnEmptyHourOpensANewEventThere() throws {
        try launch(tab: "calendar")
        mode("日")
        app.buttons["calendar-today"].tap()
        let slot = app.buttons["cal-slot-7"]
        exists(slot)
        slot.tap()
        exists(app.navigationBars["新建"])
        XCTAssertTrue(app.segmentedControls["create-kind"].buttons["日程"].isSelected)
        exists(any("07:00"))
        let title = unique("UIT 七点")
        type(title, into: app.textFields["create-title"])
        app.buttons["create-add"].tap()
        exists(app.navigationBars["日历"])
        exists(block(title), 10)
        XCTAssertTrue(block(title).label.contains("07:00"), block(title).label)
    }

    func testOpenRenameAndDeleteEvents() throws {
        try launch(tab: "calendar")
        mode("日")
        app.buttons["calendar-today"].tap()
        let e = block("UIT 日程编辑")
        reveal(e).tap()
        exists(app.navigationBars["日程"])
        let title = byID("event-title")
        exists(title)
        replace("UIT 日程改名", in: title)
        sleep(2)   // autosave waits for typing to pause
        back()
        exists(block("UIT 日程改名"), 10)
        gone(block("UIT 日程编辑"))

        let d = block("UIT 日程删除")
        reveal(d).tap()
        exists(app.navigationBars["日程"])
        reveal(app.buttons["event-delete"]).tap()
        let confirm = app.buttons["删除日程"].firstMatch
        exists(confirm)
        confirm.tap()
        exists(app.navigationBars["日历"])
        gone(block("UIT 日程删除"))
    }
}
