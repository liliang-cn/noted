import XCTest

/// 待办 (a to-do's page): logging and deleting sittings, editing its fields, deleting it.
final class TodoUITests: NotedUITestCase {
    func testLogASittingUpdatesProgressAndHistory() throws {
        try launch()
        openTodo("UIT 记录")
        exists(any("进度 0%"))
        app.buttons["todo-log-open"].tap()
        let chip = app.buttons["30 分"].firstMatch
        exists(chip)
        chip.tap()
        let slider = app.sliders["完成到"]
        exists(slider)
        slider.adjust(toNormalizedSliderPosition: 0.5)
        let pct = (slider.value as? String) ?? ""
        XCTAssertTrue(pct.hasSuffix("%"), pct)
        type("UIT 写了开头", into: app.textFields["todo-log-note"])
        app.buttons["todo-log-save"].tap()
        exists(any("进度 \(pct)"))
        exists(any("已做 1 次"))
        let entry = cell("UIT 写了开头")
        exists(entry)
        XCTAssertTrue(entry.label.contains("30 分") || entry.staticTexts["30 分"].exists, entry.label)
    }

    func testDeleteASitting() throws {
        try launch()
        openTodo("UIT 删记录")
        let entry = cell("UIT 第一次")
        reveal(entry).swipeLeft()
        let delete = app.buttons["删除"].firstMatch
        if delete.waitForExistence(timeout: 2) { delete.tap() }
        gone(cell("UIT 第一次"))
        exists(any("进度 0%"))
    }

    func testEditedFieldsAreKept() throws {
        try skipLateAtNight()
        try launch()
        openTodo("UIT 编辑")
        // 时间 on (the next hour), 提醒 15 minutes before, into 菲律宾旅行, then 工作.
        toggle("时间")
        pick("提前 15 分钟", inMenu: "提醒")
        pick("菲律宾旅行", inMenu: "项目")
        let work = app.segmentedControls.buttons["工作"].firstMatch
        reveal(work).tap()
        reveal(app.buttons.matching(NSPredicate(format: "label CONTAINS '项目 菲律宾旅行'")).firstMatch, up: false)
        let hm = String(format: "%02d:00", min(Calendar.current.component(.hour, from: .now) + 1, 23))
        // Tomorrow in the date calendar.
        let date = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '日期'")).firstMatch
        reveal(date, up: false).tap()
        pickDay(Calendar.current.date(byAdding: .day, value: 1, to: .now)!)
        exists(any("明天 \(hm)"))
        sleep(2)

        // Everything is still there after a fresh start (read back from the server).
        relaunch()
        openProject("菲律宾旅行")
        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS 'UIT 编辑'")).firstMatch
        exists(row)
        reveal(row).tap()
        exists(app.navigationBars["待办"])
        exists(any("明天 \(hm)"))
        exists(app.buttons.matching(NSPredicate(format: "label CONTAINS '提前 15 分钟'")).firstMatch)
        XCTAssertTrue(reveal(app.segmentedControls.buttons["工作"].firstMatch).isSelected)
        reveal(app.buttons.matching(NSPredicate(format: "label CONTAINS '项目 菲律宾旅行'")).firstMatch, up: false)
    }

    func testDeleteFromTheMoreMenu() throws {
        try launch()
        openTodo("UIT 页删除")
        app.buttons["todo-more"].tap()
        let delete = app.buttons["删除"].firstMatch
        exists(delete)
        delete.tap()
        let confirm = app.buttons["删除待办"].firstMatch
        exists(confirm)
        confirm.tap()
        exists(app.navigationBars["今天"])
        gone(todoRow("UIT 页删除"))
    }

    func testRepeatingTodoMakesTheNextOneWhenDone() throws {
        try launch()
        app.buttons["tab-new"].tap()
        let title = unique("UIT 每周")
        type(title, into: app.textFields["create-title"])
        pick("每周", inMenu: "重复")
        app.buttons["create-add"].tap()
        exists(app.navigationBars["今天"])
        let row = todoRow(title)
        XCTAssertTrue(reveal(row).label.contains("每周"), row.label)
        row.buttons["完成"].tap()
        exists(todoRow(title).buttons["标为未完成"])
        sawToast("已完成")
        exists(todoRow(title).buttons["标为未完成"])
        // The next one is due a week later.
        tab("calendar")
        app.segmentedControls["calendar-mode"].buttons["日"].tap()
        app.buttons["calendar-next"].tap()
        exists(app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'todo-block-' AND label CONTAINS %@", title)).firstMatch, 10)
    }
}
