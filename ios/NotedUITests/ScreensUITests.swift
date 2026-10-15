import XCTest

/// Walks the main screens and saves a picture of each to /tmp/noted-ui-shots (to compare with
/// design/prototype/shots). It only checks that each screen opens.
final class ScreensUITests: NotedUITestCase {
    private let dir = "/tmp/noted-ui-shots"

    private func shot(_ name: String) {
        sleep(1)
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/\(name).png"))
    }

    func testMainScreens() throws {
        try launch()
        exists(eventRow("团队站会"), 10)
        shot("01-today")
        openTodo("写周报")
        shot("20-todo")
        back()

        tab("calendar")
        let mode = app.segmentedControls["calendar-mode"]
        mode.buttons["日"].tap(); shot("03-cal-day")
        mode.buttons["周"].tap(); shot("04-cal-week")
        mode.buttons["月"].tap(); shot("05-cal-month")
        mode.buttons["日"].tap()
        let e = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'event-block-' AND label CONTAINS '牙医复诊'")).firstMatch
        exists(e)
        e.tap()
        exists(app.navigationBars["日程"])
        shot("60-event")
        back()

        tab("plan")
        exists(app.navigationBars["计划"])
        shot("06-plan")
        openProject("菲律宾旅行")
        shot("30-project")
        back()
        let o = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '减肥'")).firstMatch
        reveal(o).tap()
        exists(app.navigationBars["减肥"])
        shot("50-objective")
        back()
        let h = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'QQQM'")).firstMatch
        reveal(h).tap()
        exists(app.navigationBars["QQQM"])
        shot("57-holding")
        back()
        reveal(app.buttons["plan-invest-all"]).tap()
        shot("86-invest")
        back()

        tab("notes")
        exists(app.navigationBars["笔记"])
        shot("08-notes")
        let n = app.buttons.matching(NSPredicate(format: "identifier == 'note-row' AND label BEGINSWITH '行程想法'")).firstMatch
        reveal(n).tap()
        exists(byID("note-preview"))
        shot("40-note")
        back()

        app.buttons["tab-new"].tap()
        exists(app.textFields["create-title"])
        shot("10-new")
        back()

        tab("today")
        openSettings()
        shot("64-settings")
        app.buttons["settings-ai-connect"].tap()
        shot("65-ai-connect")
    }
}
