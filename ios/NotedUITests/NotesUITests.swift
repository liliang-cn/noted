import XCTest

final class NotesUITests: NotedUITestCase {
    func testNoteLifecycle() throws {
        try launch()
        let title = unique("UIT note")
        openCapture()
        app.segmentedControls.buttons["笔记"].tap()
        type(title, into: app.textFields["标题"])
        let body = app.textViews.firstMatch
        body.tap(); body.typeText("first body")
        button("保存").tap()

        tab("笔记")
        exists(text(title), 10)

        // search finds it, and a miss shows the empty state
        let search = app.textFields["搜索"]
        type("UIT note", into: search)
        exists(text(title), 10)
        search.clearText()
        type("zzzqqq", into: search)
        exists(text("没有匹配的笔记"))
        search.clearText()
        exists(text(title), 10)

        // edit
        text(title).tap()
        let editor = app.textViews.firstMatch
        exists(editor)
        editor.tap(); editor.typeText(" and more")
        button("保存").tap()
        exists(text("and more"), 10)

        // delete
        text(title).tap()
        let del = button("删除")
        exists(del)
        del.tap()
        gone(text(title), 10)
    }

    func testModeFiltersNotes() throws {
        try launch()
        tab("笔记")
        exists(text("旅行清单"))
        exists(text("周会纪要"))
        button("工作").tap()
        exists(text("周会纪要"))
        gone(text("旅行清单"))
        button("生活").tap()
        exists(text("旅行清单"))
        gone(text("周会纪要"))
    }

    func testNewNoteTakesTheCurrentMode() throws {
        try launch(mode: "work")
        let title = unique("UIT work note")
        openCapture()
        app.segmentedControls.buttons["笔记"].tap()
        type(title, into: app.textFields["标题"])
        button("保存").tap()
        tab("笔记")
        button("工作").tap()
        exists(text(title), 10)
        button("生活").tap()
        gone(text(title))
    }
}

extension XCUIElement {
    func clearText() {
        guard let s = value as? String, !s.isEmpty else { return }
        tap()
        typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: s.count))
    }
}

final class NoteShareUITests: NotedUITestCase {
    private func openNote() {
        tab("笔记")
        text("旅行清单").tap()
        exists(button("分享"))
    }

    func testShareMenuOffersTextAndPictureOptions() throws {
        try launch()
        openNote()
        button("分享").tap()
        exists(app.buttons["复制文字"])
        exists(app.buttons["分享文字"])
        exists(app.buttons["分享为图片"])
        exists(app.buttons["保存到相册"])
    }

    func testCopyingTheTextSaysSo() throws {
        try launch()
        openNote()
        button("分享").tap()
        app.buttons["复制文字"].tap()
        exists(text("已复制"))
    }

    func testSharingAsAPictureOpensTheSystemShareSheet() throws {
        try launch()
        openNote()
        button("分享").tap()
        app.buttons["分享为图片"].tap()
        // the system share sheet shows what can be done with a picture
        let sheet = app.otherElements["ActivityListView"]
        let save = app.buttons["Save Image"].exists ? app.buttons["Save Image"] : app.cells["Save Image"]
        XCTAssertTrue(sheet.waitForExistence(timeout: 10) || save.waitForExistence(timeout: 10) || app.buttons["Copy"].waitForExistence(timeout: 10), "the share sheet did not appear")
    }

    func testSavingAsAPictureReportsTheOutcome() throws {
        addUIInterruptionMonitor(withDescription: "photos") { alert in
            for label in ["允许", "Allow", "好", "OK"] where alert.buttons[label].exists { alert.buttons[label].tap(); return true }
            return false
        }
        try launch()
        openNote()
        button("分享").tap()
        app.buttons["保存到相册"].tap()
        app.tap()   // gives the interruption monitor a chance to answer the permission alert
        XCTAssertTrue(text("已保存到相册").waitForExistence(timeout: 15) || text("没有保存").waitForExistence(timeout: 1),
                      "the outcome of saving should be shown")
        XCTAssertFalse(text("没有保存").exists, "saving to the photo library failed")
    }
}
