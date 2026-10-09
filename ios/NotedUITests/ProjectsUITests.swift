import XCTest

final class ProjectsUITests: NotedUITestCase {
    func testAddingATaskToAProject() throws {
        try launch()
        tab("项目")
        text("菲律宾旅行").tap()
        let add = button("添加到这个项目")
        exists(add)
        add.tap()
        let title = unique("UIT visa")
        type(title, into: app.textFields["要做什么"])
        button("保存").tap()
        exists(text(title), 10)
    }

    func testTasksInAProjectCanBeTicked() throws {
        try launch()
        tab("项目")
        text("菲律宾旅行").tap()
        let add = button("添加到这个项目")
        exists(add)
        add.tap()
        let title = unique("UIT tick")
        type(title, into: app.textFields["要做什么"])
        button("保存").tap()
        exists(text(title), 10)
        app.buttons["完成 \(title)"].tap()
        exists(app.buttons["取消完成 \(title)"], 10)
        app.buttons["取消完成 \(title)"].tap()
        exists(app.buttons["完成 \(title)"], 10)
    }

    func testGoalCheckInRaisesTheCount() throws {
        try launch()
        tab("项目")
        button("目标").tap()
        let count = app.staticTexts.matching(NSPredicate(format: "label MATCHES %@", "[0-9.]+/3 次")).firstMatch
        exists(count)
        func done(_ s: String) -> Double { Double(s.split(separator: "/").first.map(String.init) ?? "") ?? -1 }
        let before = done(count.label)
        app.buttons.matching(NSPredicate(format: "label == %@", "打卡 +1")).element(boundBy: 1).tap()
        let deadline = Date().addingTimeInterval(8)
        while Date() < deadline, done(count.label) <= before { usleep(300_000) }
        XCTAssertGreaterThan(done(count.label), before)
    }

    func testGoalsCarryTheirSecondCounter() throws {
        try launch()
        tab("项目")
        button("目标").tap()
        exists(text("课时"))
    }

    func testModeFiltersProjects() throws {
        try launch()
        tab("项目")
        exists(text("菲律宾旅行"))
        exists(text("Q4 产品发布"))
        button("工作").tap()
        exists(text("Q4 产品发布"))
        gone(text("菲律宾旅行"))
    }
}
