import XCTest

final class AIUITests: NotedUITestCase {
    private func openAsk() {
        let b = app.buttons["提问"]
        exists(b, 12)
        b.tap()
    }

    private func send(_ s: String) {
        type(s, into: app.textFields["ask-input"])
        app.buttons["ask-send"].tap()
    }

    func testAskShowsTheRowsTheAnswerIsBasedOn() throws {
        try launch()
        openAsk()
        send("what are my todo items")
        exists(text("依据"), 20)
        exists(text("交房租"))
        exists(text("待办"))
    }

    func testAssistantChangesWaitForConfirmation() throws {
        try launch()
        openAsk()
        send("please create a todo")
        exists(text("还没有写入"), 20)
        exists(text("买牛奶"))
        let accept = button("采纳所选")
        exists(accept)
        accept.tap()
        exists(text("已写入"), 10)
        gone(button("采纳所选"))
    }

    func testUncheckingAStepKeepsItOut() throws {
        try launch()
        openAsk()
        send("please create a todo")
        exists(text("买牛奶"), 20)
        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "买牛奶")).firstMatch
        row.tap()
        let accept = button("采纳所选 0 条")
        exists(accept)
        XCTAssertFalse(accept.isEnabled, "nothing selected: nothing to accept")
    }

    func testPlanNeedsADateTheUserDidNotGive() throws {
        try launch(mode: "life")
        openAsk()
        app.segmentedControls.buttons["拆成项目"].tap()
        send("trip to the Philippines next month")
        exists(text("必填"), 20)
        let create = button("创建所选")
        exists(create)
        XCTAssertFalse(create.isEnabled, "the departure date is required and was not given")
        button("请选一个").tap()
        XCTAssertTrue(create.waitForExistence(timeout: 5))
        let enabled = NSPredicate(format: "isEnabled == true")
        XCTAssertEqual(XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: enabled, object: create)], timeout: 5), .completed)
        create.tap()
        exists(text("已写入"), 10)
        // the project now exists
        app.buttons["关闭"].tap()
        tab("项目")
        exists(text("UIT 东京行"), 10)
    }

    func testExtractingTodosFromANote() throws {
        try launch()
        tab("笔记")
        text("旅行清单").tap()
        let extract = button("提取待办")
        exists(extract)
        extract.tap()
        exists(text("笔记里有 3 件事"), 15)
        exists(text("订大阪机票"))
        let add = button("加入 3 项待办")
        exists(add)
        add.tap()
        exists(text("已加入"), 10)
    }

    func testTurningNoteToolsOffHidesExtraction() throws {
        try launch()
        openSettings()
        button("智能").tap()
        let toggle = app.switches["笔记摘要、标签、提取待办"]
        exists(toggle)
        XCTAssertEqual(toggle.value as? String, "1")
        toggle.tap()
        XCTAssertEqual(toggle.value as? String, "0")

        try launch()
        tab("笔记")
        text("旅行清单").tap()
        exists(app.buttons["关闭"])
        XCTAssertFalse(button("提取待办").waitForExistence(timeout: 3), "the server switch is honored by the app")

        // put it back
        try launch()
        openSettings()
        button("智能").tap()
        let again = app.switches["笔记摘要、标签、提取待办"]
        exists(again)
        XCTAssertEqual(again.value as? String, "0", "the switch persisted on the server")
        again.tap()
        XCTAssertEqual(again.value as? String, "1")
    }

    func testWorkContentIsKeptFromTheAssistantWhenTurnedOff() throws {
        try launch()
        openSettings()
        button("智能").tap()
        let work = app.switches["工作的内容"]
        exists(work)
        work.tap()
        XCTAssertEqual(work.value as? String, "0")

        try launch()
        openAsk()
        let tag = "PRIV\(Int(Date().timeIntervalSince1970) % 100_000)"
        send("what are my todo items \(tag)")
        exists(text("依据"), 20)
        // What the model was actually sent for this question.
        let sent = (try? String(contentsOfFile: "/tmp/noted-fake-llm/\(tag).log", encoding: .utf8)) ?? ""
        XCTAssertFalse(sent.isEmpty, "the stand-in model saw no request for \(tag)")
        XCTAssertTrue(sent.contains("交房租"), "life items are still available to the assistant")
        XCTAssertFalse(sent.contains("UIT work only"), "work items must not reach the model")

        // restore
        try launch()
        openSettings()
        button("智能").tap()
        let again = app.switches["工作的内容"]
        exists(again)
        if again.value as? String == "0" { again.tap() }
        XCTAssertEqual(again.value as? String, "1")
    }
}
