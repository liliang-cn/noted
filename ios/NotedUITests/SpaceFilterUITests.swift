import XCTest

/// The 全部 / 个人 / 工作 filter: 工作 hides 个人 items on 今天, 计划 and 笔记.
final class SpaceFilterUITests: NotedUITestCase {
    func testWorkSpaceHidesPersonalItems() throws {
        try launch()
        exists(todoRow("交房租"), 10)
        exists(eventRow("团队站会"))
        app.buttons["space-filter"].firstMatch.tap()
        let work = app.buttons["工作"].firstMatch
        exists(work)
        work.tap()
        gone(todoRow("交房租"))
        gone(eventRow("牙医复诊"))
        exists(eventRow("团队站会"))
        reveal(todoRow("写周报"))

        tab("plan")
        exists(app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'project-' AND label CONTAINS 'Q4 产品发布'")).firstMatch)
        gone(app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'project-' AND label CONTAINS '菲律宾旅行'")).firstMatch)
        gone(any("健身"))

        tab("notes")
        reveal(app.buttons.matching(NSPredicate(format: "identifier == 'note-row' AND label BEGINSWITH '周会纪要'")).firstMatch)
        gone(app.buttons.matching(NSPredicate(format: "identifier == 'note-row' AND label BEGINSWITH '旅行清单'")).firstMatch)

        app.buttons["space-filter"].firstMatch.tap()
        app.buttons["全部"].firstMatch.tap()
        reveal(app.buttons.matching(NSPredicate(format: "identifier == 'note-row' AND label BEGINSWITH '旅行清单'")).firstMatch)
    }
}
