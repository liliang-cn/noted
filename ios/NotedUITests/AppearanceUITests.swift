import XCTest

final class AppearanceUITests: NotedUITestCase {
    func testThemeChoiceIsSavedOnTheServerAndComesBack() throws {
        try launch()
        openSettings()
        let paper = app.buttons["主题 纸本"]
        exists(paper)
        paper.tap()
        XCTAssertEqual(paper.value as? String, "已选")

        try launch()
        openSettings()
        XCTAssertEqual(app.buttons["主题 纸本"].value as? String, "已选", "a new launch reads the theme back from the server")
        app.buttons["主题 日光"].tap()
        XCTAssertEqual(app.buttons["主题 日光"].value as? String, "已选")
    }

    func testCustomThemeAndModulesAreProOnly() throws {
        try launch()
        openSettings()
        button("自定义主题").tap()
        exists(app.buttons["subscribe"], 8)
        exists(text("桌面、锁屏小组件与 Apple Watch 表盘"))
        app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "设置")).firstMatch.tap()
        button("页面模块").tap()
        exists(text("桌面、锁屏小组件与 Apple Watch 表盘"))
    }

    func testProUserCanEditTheTheme() throws {
        try launch(pro: true)
        openSettings()
        button("自定义主题").tap()
        exists(text("预览"))
        app.segmentedControls.buttons["宽松"].tap()
        button("保存").tap()
        exists(text("设置"))

        try launch(pro: true)
        openSettings()
        button("自定义主题").tap()
        XCTAssertTrue(app.segmentedControls.buttons["宽松"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.segmentedControls.buttons["宽松"].isSelected, "the saved density comes back")
        app.segmentedControls.buttons["标准"].tap()
        button("保存").tap()
    }

    func testProUserCanChangeWhatTheOverviewShows() throws {
        try launch(pro: true)
        openSettings()
        button("页面模块").tap()
        let recent = app.switches["最近笔记"]
        exists(recent)
        if recent.value as? String == "0" { recent.tap() }
        XCTAssertEqual(recent.value as? String, "1")
        button("完成").tap()
        app.buttons["back"].tap()
        exists(text("最近笔记"), 10)

        // and back
        openSettings()
        button("页面模块").tap()
        let again = app.switches["最近笔记"]
        exists(again)
        XCTAssertEqual(again.value as? String, "1", "the layout was saved on the server")
        again.tap()
        button("完成").tap()
    }

    func testMutingIsSavedOnTheServer() throws {
        try launch()
        openSettings()
        let mute = app.switches["工作模式下静音生活提醒"]
        exists(mute)
        if mute.value as? String == "1" { mute.tap() }
        mute.tap()
        XCTAssertEqual(mute.value as? String, "1")

        try launch()
        openSettings()
        let again = app.switches["工作模式下静音生活提醒"]
        exists(again)
        XCTAssertEqual(again.value as? String, "1")
        again.tap()
        XCTAssertEqual(again.value as? String, "0")
    }

    func testPaywallShowsWhatProUnlocks() throws {
        try launch()
        openSettings()
        button("Noted Pro").tap()
        exists(text("自定义主题与配色"))
        exists(text("页面模块与顺序"))
        exists(text("桌面、锁屏小组件与 Apple Watch 表盘"))
        exists(button("恢复购买"))
    }
}
