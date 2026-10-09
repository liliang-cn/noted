import XCTest

final class CalendarUITests: NotedUITestCase {
    func testNewEventShowsOnTodayInTheCalendar() throws {
        try skipLateAtNight()
        try launch()
        let title = unique("UIT event")
        openCapture()
        app.segmentedControls.buttons["日程"].tap()
        type(title, into: app.textFields["要做什么"])
        button("保存").tap()
        tab("日历")
        exists(text(title), 10)
    }

    func testCalendarShowsSeededEventsAndTasks() throws {
        try launch()
        tab("日历")
        exists(text("牙医复诊"))
        exists(text("团队站会"))
        button("工作").tap()
        gone(text("牙医复诊"))
        exists(text("团队站会"))
    }
}
