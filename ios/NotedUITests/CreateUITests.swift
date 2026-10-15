import XCTest

/// 新建: every kind lands on the right screen; drafts survive; 添加 needs a title; from a project.
final class CreateUITests: NotedUITestCase {
    private func openNew() {
        let plus = app.buttons["tab-new"]
        exists(plus)
        plus.tap()
        exists(app.textFields["create-title"])
    }

    private func kind(_ name: String) {
        let b = app.segmentedControls["create-kind"].buttons[name]
        exists(b)
        b.tap()
    }

    func testTodoWithDateTimeAndReminder() throws {
        try skipLateAtNight()
        try launch()
        openNew()
        let title = unique("UIT 新待办")
        type(title, into: app.textFields["create-title"])
        XCTAssertTrue(app.collectionViews.buttons["今天"].firstMatch.isSelected, "a new to-do starts on today")
        toggle("时间")
        pick("提前 15 分钟", inMenu: "提醒")
        app.buttons["create-add"].tap()
        exists(app.navigationBars["今天"])
        let row = reveal(todoRow(title))
        let hour = Calendar.current.component(.hour, from: .now) + 1
        XCTAssertTrue(row.label.contains(String(format: "%02d:00", hour)), row.label)
    }

    func testEventWithTime() throws {
        try skipLateAtNight()
        try launch()
        openNew()
        kind("日程")
        let title = unique("UIT 新日程")
        type(title, into: app.textFields["create-title"])
        app.buttons["create-add"].tap()
        exists(app.navigationBars["今天"])
        let row = reveal(eventRow(title))
        let hour = Calendar.current.component(.hour, from: .now) + 1
        XCTAssertTrue(row.label.contains(String(format: "%02d:00", hour)), row.label)
    }

    func testProjectOpensItsPage() throws {
        try launch()
        openNew()
        kind("项目")
        let title = unique("UIT 新项目")
        type(title, into: app.textFields["create-title"])
        app.buttons["create-add"].tap()
        exists(app.navigationBars[title], 10)
        exists(app.buttons["project-add"])
    }

    func testPlainGoal() throws {
        try launch()
        openNew()
        kind("目标")
        let title = unique("UIT 新目标")
        type(title, into: app.textFields["create-title"])
        app.buttons["create-add"].tap()
        exists(app.navigationBars["今天"])
        tab("plan")
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(reveal(row).label.contains("/3 本周"), row.label)
    }

    func testGoalWithDimensionAndMetric() throws {
        try launch()
        openNew()
        kind("目标")
        let title = unique("UIT 大目标")
        type(title, into: app.textFields["create-title"])
        reveal(app.buttons["create-add-dimension"]).tap()
        let name = app.textFields["如 游泳"]
        exists(name)
        name.typeText("UIT 游泳")
        app.buttons["dimension-add"].tap()
        exists(text("UIT 游泳"))
        toggle("追踪指标")
        for (field, value) in [("名称", "体重"), ("单位", "kg"), ("当前", "80"), ("目标", "75")] {
            let f = app.textFields[field].firstMatch
            reveal(f).tap()
            f.typeText(value)
        }
        app.buttons["create-add"].tap()
        exists(app.navigationBars["今天"])
        tab("plan")
        reveal(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", title)).firstMatch).tap()
        exists(app.navigationBars[title])
        exists(text("UIT 游泳"))
        reveal(app.buttons["objective-add-reading"])
        exists(any("体重"))
    }

    func testKindsAreTodoEventProjectGoal() throws {
        try launch()
        openNew()
        let kinds = app.segmentedControls["create-kind"]
        exists(kinds)
        XCTAssertEqual(kinds.buttons.count, 4)
        for k in ["待办", "日程", "项目", "目标"] { XCTAssertTrue(kinds.buttons[k].exists, k) }
        XCTAssertFalse(kinds.buttons["笔记"].exists, "notes are written from 笔记, not 新建")
    }

    func testDraftSurvivesLeavingAndComingBack() throws {
        try launch()
        openNew()
        let add = app.buttons["create-add"]
        XCTAssertFalse(add.isEnabled, "添加 needs a title")
        let title = unique("UIT 草稿")
        type(title, into: app.textFields["create-title"])
        XCTAssertTrue(add.isEnabled)
        back()
        exists(app.navigationBars["今天"])
        openNew()
        XCTAssertEqual(app.textFields["create-title"].value as? String, title)
        exists(any("草稿已保存"))
        app.buttons["create-discard"].tap()
        let discard = app.buttons.matching(NSPredicate(format: "label == '丢弃草稿' AND identifier != 'create-discard'")).firstMatch
        exists(discard)
        discard.tap()
        eventually(5, "the draft is discarded") { (app.textFields["create-title"].value as? String) != title }
        XCTAssertFalse(app.buttons["create-add"].isEnabled)
    }

    func testCreatingFromAProjectFilesItThere() throws {
        try launch()
        openProject("菲律宾旅行")
        app.buttons["project-add"].tap()
        let item = app.buttons["新建待办"].firstMatch
        exists(item)
        item.tap()
        let title = unique("UIT 项目里的")
        type(title, into: app.textFields["create-title"])
        exists(app.buttons.matching(NSPredicate(format: "label BEGINSWITH '项目'")).matching(NSPredicate(format: "label CONTAINS '菲律宾旅行'")).firstMatch)
        app.buttons["create-add"].tap()
        exists(app.navigationBars["菲律宾旅行"])
        reveal(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", title)).firstMatch)
    }
}
