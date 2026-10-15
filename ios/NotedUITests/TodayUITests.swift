import XCTest

/// 今天: the seeded day in time order, the swipes, opening a to-do, checking in a goal.
final class TodayUITests: NotedUITestCase {
    func testSeededItemsShowInTimeOrder() throws {
        try launch()
        exists(app.navigationBars["今天"])
        let rows = [eventRow("团队站会"), todoRow("交房租"), eventRow("牙医复诊"), eventRow("季度报告评审"), todoRow("写周报")]
        exists(rows[0], 10)
        // Walking down the day: each one is below the one before (or that one has scrolled away).
        for (a, b) in zip(rows, rows.dropFirst()) {
            reveal(b)
            if a.exists && a.frame.height > 0 {
                XCTAssertLessThan(a.frame.minY, b.frame.minY, "\(a.label) should come before \(b.label)")
            }
        }
        // 写周报 has two sittings: it is also 进行中 at 60%.
        reveal(app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'inprogress-' AND label CONTAINS '写周报'")).firstMatch)
        reveal(any("今天打卡"))
    }

    func testSwipeRightCompletesATodo() throws {
        try launch()
        let row = todoRow("UIT 滑动完成")
        swipeRight(reveal(row), action: "swipe-complete")
        sawToast("已完成")
        // Done to-dos stay on the day, ticked and moved after the open ones.
        exists(reveal(todoRow("UIT 滑动完成")).buttons["标为未完成"])
        let open = todoRow("UIT 打开")
        if open.exists && open.frame.height > 0 {
            XCTAssertGreaterThan(todoRow("UIT 滑动完成").frame.minY, open.frame.minY)
        }
    }

    func testSwipeLeftDeleteAsksThenDeletes() throws {
        try launch()
        let row = todoRow("UIT 滑动删除")
        reveal(row).swipeLeft()
        let delete = app.buttons["删除"].firstMatch
        exists(delete)
        delete.tap()
        let confirm = app.buttons["删除待办"].firstMatch
        exists(confirm)
        confirm.tap()
        gone(todoRow("UIT 滑动删除"))
    }

    func testPostponeMovesItToTomorrow() throws {
        try launch()
        let row = todoRow("UIT 推迟")
        reveal(row).swipeLeft()
        let later = app.buttons["推迟"].firstMatch
        exists(later)
        later.tap()
        sawToast("已推迟到明天")
        gone(todoRow("UIT 推迟"))
        // It is on tomorrow in the calendar.
        tab("calendar")
        app.segmentedControls["calendar-mode"].buttons["日"].tap()
        let tomorrow = Calendar.current.date(byAdding: .day, value: 1, to: .now)!
        let f = DateFormatter(); f.dateFormat = "yyyy-MM-dd"
        let day = app.buttons["cal-day-\(f.string(from: tomorrow))"]
        if !day.exists { app.buttons["calendar-next"].tap() }
        exists(day)
        day.tap()
        exists(app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'todo-block-' AND label CONTAINS 'UIT 推迟'")).firstMatch)
    }

    func testTappingATodoOpensItsPage() throws {
        try launch()
        openTodo("UIT 打开")
        XCTAssertEqual(byID("todo-title").value as? String, "UIT 打开")
        exists(app.buttons["todo-log-open"])
    }

    func testCheckingInAGoalCountsIt() throws {
        try launch()
        let row = cell("健身")
        reveal(row)
        let before = count(in: row.buttons.firstMatch.label)
        row.buttons["打卡"].tap()
        sawToast("健身 +1")
        eventually(8, "健身 should count one more") { count(in: cell("健身").buttons.firstMatch.label) == before + 1 }
    }

    /// "2/3 本周" → 2
    private func count(in label: String) -> Int {
        guard let r = label.range(of: #"(\d+)/3"#, options: .regularExpression) else { return -1 }
        return Int(label[r].split(separator: "/")[0]) ?? -1
    }
}
