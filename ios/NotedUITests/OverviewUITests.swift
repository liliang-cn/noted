import XCTest

final class OverviewUITests: NotedUITestCase {
    func testModeSwitchFiltersWhatIsShown() throws {
        try launch()
        exists(text("牙医复诊"))
        exists(text("团队站会"))
        button("生活").tap()
        exists(text("牙医复诊"))
        gone(text("团队站会"))
        button("工作").tap()
        exists(text("团队站会"))
        gone(text("牙医复诊"))
        button("全部").tap()
        exists(text("牙医复诊"))
        exists(text("团队站会"))
    }

    func testHorizonTabsChangeTheWindow() throws {
        try launch()
        button("接下来").tap()
        exists(text("办理签证"))
        gone(text("团队站会"))
        button("今天").tap()
        exists(text("团队站会"))
    }

    func testCompletingATaskRemovesItFromToday() throws {
        try skipLateAtNight()
        try launch()
        let title = unique("UIT done")
        openCapture()
        type(title, into: app.textFields["要做什么"])
        app.switches["设置截止时间"].tap()
        button("保存").tap()
        let box = app.buttons["完成 \(title)"]
        exists(box, 10)
        for _ in 0..<6 where !box.isHittable { app.swipeUp() }   // the list can be longer than the screen
        box.tap()
        gone(box)
    }

    func testPinnedProjectOpensItsDetail() throws {
        try launch()
        let card = text("菲律宾旅行")
        exists(card)
        card.tap()
        exists(text("办理签证"))
        exists(text("航班"))
        exists(text("行程想法"))
        exists(button("添加到这个项目"))
    }

    func testSuggestionsCanBeAcceptedAndUndone() throws {
        try launch()
        let row = button("条建议")
        exists(row, 12)
        row.tap()
        exists(text("你不点,就不会执行"))
        let accept = button("采纳")
        exists(accept)
        let first = app.staticTexts.matching(identifier: "proposal-title").element(boundBy: 0)
        exists(first)
        let title = first.label
        accept.tap()
        exists(button("撤销"))
        // accepting can bring up new suggestions (a new slot may clash with something), but not the same one
        // the undo bar repeats the title, so look only at the suggestion cards
        gone(app.staticTexts.matching(NSPredicate(format: "identifier == 'proposal-title' AND label == %@", title)).firstMatch, 10)
        button("撤销").tap()
        gone(button("撤销"))
    }

    func testDismissingASuggestionRemovesIt() throws {
        try launch()
        let row = button("条建议")
        exists(row, 12)
        row.tap()
        let ignore = app.buttons.matching(NSPredicate(format: "label == %@", "忽略")).firstMatch
        exists(ignore)
        let before = app.buttons.matching(NSPredicate(format: "label == %@", "忽略")).count
        ignore.tap()
        let deadline = Date().addingTimeInterval(8)
        while Date() < deadline, app.buttons.matching(NSPredicate(format: "label == %@", "忽略")).count >= before { usleep(300_000) }
        XCTAssertLessThan(app.buttons.matching(NSPredicate(format: "label == %@", "忽略")).count, before)
    }

    func testWeeklyReviewShowsTheWeek() throws {
        try launch()
        let r = button("回顾")
        exists(r, 12)
        r.tap()
        exists(text("本周回顾"))
        exists(text("没做完的"))
    }
}
