import XCTest

/// 计划: project pages (contents, 关联已有, 移出项目, 编辑, 删除), a goal, an objective, a holding, 投资.
final class PlanUITests: NotedUITestCase {
    private func row(_ title: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label CONTAINS %@", title)).firstMatch
    }

    func testProjectPageShowsItsItems() throws {
        try launch()
        openProject("菲律宾旅行")
        for t in ["办理签证", "订酒店", "订机票"] { exists(row(t)) }
        exists(any("待办 · "))
        reveal(row("航班 CTU→MNL"))
        reveal(row("行程想法"))
    }

    func testLinkAnExistingTodo() throws {
        try launch()
        openProject("UIT 关联")
        app.buttons["project-add"].tap()
        let link = app.buttons["关联已有…"].firstMatch
        exists(link)
        link.tap()
        exists(app.navigationBars["关联已有"])
        let candidate = row("UIT 待关联")
        reveal(candidate).tap()
        XCTAssertTrue(candidate.isSelected)
        app.buttons["link-confirm"].tap()
        exists(app.navigationBars["UIT 关联"])
        reveal(row("UIT 待关联"))
    }

    func testNewNoteFromAProjectIsFiledThere() throws {
        try launch()
        openProject("UIT 关联")
        app.buttons["project-add"].tap()
        let item = app.buttons["新建笔记"].firstMatch
        exists(item)
        item.tap()
        let title = unique("UIT 项目笔记")
        let field = byID("note-title")
        exists(field)
        field.tap()
        field.typeText(title)
        let body = app.textViews["note-body"]
        body.tap()
        body.typeText("记在项目里")
        app.buttons["note-done"].tap()
        back()
        exists(app.navigationBars["UIT 关联"])
        reveal(row(title))
    }

    func testRemoveFromProject() throws {
        try launch()
        openProject("UIT 关联")
        let item = cell("UIT 移出")
        reveal(item).buttons["更多"].tap()
        let out = app.buttons["移出项目"].firstMatch
        exists(out)
        out.tap()
        sawToast("已移出项目")
        gone(cell("UIT 移出"))
    }

    func testEditProjectTitle() throws {
        try launch()
        openProject("UIT 改名")
        app.buttons["project-more"].tap()
        let edit = app.buttons["编辑"].firstMatch
        exists(edit)
        edit.tap()
        exists(app.navigationBars["编辑项目"])
        replace("UIT 新名字", in: app.textFields["project-edit-title"])
        app.buttons["project-edit-done"].tap()
        exists(app.navigationBars["UIT 新名字"])
        back()
        exists(app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'project-' AND label CONTAINS 'UIT 新名字'")).firstMatch)
    }

    func testDeleteProjectKeepingItsItems() throws {
        try launch()
        openProject("UIT 删项目")
        app.buttons["project-more"].tap()
        let delete = app.buttons["删除项目"].firstMatch
        exists(delete)
        delete.tap()
        let keep = app.buttons["只删项目，保留内容"].firstMatch
        exists(keep)
        keep.tap()
        exists(app.navigationBars["计划"])
        gone(app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'project-' AND label CONTAINS 'UIT 删项目'")).firstMatch)
        // The to-do stayed: it can be linked into another project.
        openProject("UIT 关联")
        app.buttons["project-add"].tap()
        app.buttons["关联已有…"].firstMatch.tap()
        reveal(row("UIT 留下"))
    }

    func testGoalPageCheckIn() throws {
        try launch(tab: "plan")
        let goal = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '健身'")).firstMatch
        reveal(goal).tap()
        exists(app.navigationBars["健身"])
        let ring = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS '/3'")).firstMatch
        exists(ring)
        let before = ring.label
        let checkIn = button("打卡 +1")
        exists(checkIn)
        checkIn.tap()
        sawToast("健身 +1")
        eventually(8, "the ring counts it") { app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS '/3'")).firstMatch.label != before }
        exists(text("打卡记录"))
    }

    func testObjectiveRecordWeight() throws {
        try launch(tab: "plan")
        let o = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '减肥'")).firstMatch
        reveal(o).tap()
        exists(app.navigationBars["减肥"])
        exists(text("游泳"))
        exists(text("跑步"))
        let add = app.buttons["objective-add-reading"]
        reveal(add).tap()
        let field = app.textFields["记一次体重"]
        exists(field)
        replace("78.2", in: field)
        app.buttons["number-sheet-save"].tap()
        sawToast("已记录 78.2 kg")
        exists(text("78.2"))
        exists(any("已减 1.8 kg"))
    }

    func testHoldingBuyUpdatesPosition() throws {
        try launch(tab: "plan")
        let voo = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'VOO'")).firstMatch
        reveal(voo).tap()
        exists(app.navigationBars["VOO"])
        exists(any("10"))
        app.buttons["holding-buy"].tap()
        exists(app.navigationBars["记一笔 · VOO"])
        let shares = app.textFields.matching(NSPredicate(format: "placeholderValue == '0'")).firstMatch
        exists(shares)
        shares.tap()
        shares.typeText("2")
        app.buttons["trade-save"].tap()
        sawToast("已记录买入")
        exists(any("523.33"))
        exists(any("12"))
    }

    func testInvestSummary() throws {
        try launch(tab: "plan")
        let all = app.buttons["plan-invest-all"]
        reveal(all).tap()
        exists(app.navigationBars["投资"])
        for s in ["QQQM", "VOO", "OKLO"] { exists(any(s)) }
        exists(any("持仓"))
        reveal(any("定投计划"))
    }
}
