import XCTest

/// 笔记: search, tag chips, swipes, the editor's autosave, ticking a checklist, "@" references.
final class NotesUITests: NotedUITestCase {
    private func note(_ title: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier == 'note-row' AND label BEGINSWITH %@", title)).firstMatch
    }

    private func open(_ title: String) {
        reveal(note(title)).tap()
        exists(byID("note-preview"))
    }

    func testLargeTitleAndSearch() throws {
        try launch(tab: "notes")
        let bar = app.navigationBars["笔记"]
        exists(bar)
        let title = bar.staticTexts["笔记"]
        exists(title)
        XCTAssertLessThan(title.frame.minX, 40, "笔记 shows a large (leading) title like the other tabs, not an inline one")
        XCTAssertGreaterThan(title.frame.height, 30)
        exists(note("周会纪要"))
        let search = app.searchFields.firstMatch
        exists(search)
        search.tap()
        search.typeText("德语")
        exists(note("德语单词"))
        gone(note("旅行清单"))
        gone(note("周会纪要"))
        search.typeText("zzz")
        exists(any("没有找到"))
    }

    func testTagChipFilters() throws {
        try launch(tab: "notes")
        let chip = app.buttons["#德语"].firstMatch
        exists(chip)
        chip.tap()
        reveal(note("德语单词"))
        gone(note("旅行清单"))
        app.buttons["#travel"].firstMatch.tap()
        reveal(note("旅行清单"))
        gone(note("德语单词"))
        app.buttons["notes-tag-all"].tap()
        reveal(note("德语单词"))
        reveal(note("旅行清单"))
    }

    func testSwipeToPinAndToDelete() throws {
        try launch(tab: "notes")
        let pin = note("UIT 置顶我")
        swipeRight(reveal(pin), action: "置顶")
        sawToast("已置顶")
        // Pinned notes come first, with 周会纪要, above 今天.
        drag(up: false); drag(up: false)
        let today = app.collectionViews.staticTexts["今天"].firstMatch
        exists(today)
        eventually(5, "the note moves into 置顶") { note("UIT 置顶我").exists && note("UIT 置顶我").frame.maxY < today.frame.minY }

        let del = note("UIT 删除我")
        reveal(del).swipeLeft()
        let b = app.buttons["删除"].firstMatch
        exists(b)
        b.tap()
        let confirm = app.buttons["删除"].firstMatch
        exists(confirm)
        confirm.tap()
        gone(note("UIT 删除我"))
    }

    func testEditAutosavesAndReopens() throws {
        try launch(tab: "notes")
        open("UIT 编辑笔记")
        byID("note-preview").tap()
        let body = app.textViews["note-body"]
        exists(body)
        body.typeText("\n自动保存的第二行")
        eventually(10, "it saves while typing") { (byID("note-status").label).contains("已保存") }
        relaunch()
        tab("notes")
        open("UIT 编辑笔记")
        exists(any("自动保存的第二行"))
    }

    func testTickAChecklistInPreview() throws {
        try launch(tab: "notes")
        open("UIT 清单")
        let item = app.buttons["完成 买水"]
        exists(item)
        item.tap()
        exists(app.buttons["标为未完成 买水"])
        back()
        open("UIT 清单")
        exists(app.buttons["标为未完成 买水"])
        exists(app.buttons["完成 买面包"])
    }

    func testMentionInsertsAProjectReferenceThatOpensIt() throws {
        try launch(tab: "notes")
        open("UIT 引用")
        byID("note-preview").tap()
        let body = app.textViews["note-body"]
        exists(body)
        app.buttons["kb-mention"].tap()
        exists(byID("mention-panel"))
        let project = app.buttons.matching(NSPredicate(format: "identifier == 'mention-project' AND label CONTAINS '菲律宾旅行'")).firstMatch
        exists(project)
        project.tap()
        XCTAssertTrue(((body.value as? String) ?? "").contains("[@菲律宾旅行](noted://project/"), (body.value as? String) ?? "")
        app.buttons["note-done"].tap()
        let link = app.links.matching(NSPredicate(format: "identifier BEGINSWITH 'noted://project/'")).firstMatch
        exists(link)
        link.tap()
        exists(app.navigationBars["菲律宾旅行"])
    }
}
