import XCTest

/// Drives the real app against a real noted server seeded with the prototype's sample data
/// (and a stand-in model server). `ios/Tests/run-ui-tests.sh` starts both, seeds them with
/// Tests/seed.py and passes NOTED_TOKEN. Items whose titles start with "UIT " belong to one test each.
class NotedUITestCase: XCTestCase {
    var app: XCUIApplication!
    static let pendingDump = "/tmp/noted-ui-pending.json"
    static let exportCopy = "/tmp/noted-ui-export.zip"

    override func setUp() {
        continueAfterFailure = false
    }

    /// A failed test leaves a picture and the screen's element tree behind.
    override func tearDown() {
        if let run = testRun, run.totalFailureCount > 0, let app {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.name = "failure"; shot.lifetime = .keepAlways
            add(shot)
            let tree = XCTAttachment(string: app.debugDescription)
            tree.name = "tree"; tree.lifetime = .keepAlways
            add(tree)
            print("UITREE >>>\n\(app.debugDescription)\n<<< UITREE")
        }
        super.tearDown()
    }

    var env: [String: String] { ProcessInfo.processInfo.environment }
    var token: String { env["NOTED_TOKEN"] ?? "" }
    var port: String { env["NOTED_PORT"] ?? "43901" }

    /// Starts the app signed in to the test server (or, with `connected: false`, on the sign-in page).
    func launch(mode: String = "all", tab: String? = nil, connected: Bool = true, reset: Bool = false, extra: [String: String] = [:]) throws {
        try XCTSkipIf(token.isEmpty, "NOTED_TOKEN is not set: run ios/Tests/run-ui-tests.sh")
        app = XCUIApplication()
        var e = ["NOTED_MODE": mode, "NOTED_DUMP_PENDING": Self.pendingDump, "NOTED_PROVISIONAL_NOTIFICATIONS": "1",
                 "NOTED_FRESH_DRAFTS": "1", "NOTED_EXPORT_COPY": Self.exportCopy]
        if connected {
            e["NOTED_HOST"] = env["NOTED_HOST"] ?? "127.0.0.1"
            e["NOTED_PORT"] = port
            e["NOTED_TOKEN"] = token
        }
        if let tab { e["NOTED_TAB"] = tab }
        if reset { e["NOTED_RESET"] = "1" }
        e.merge(extra) { $1 }
        app.launchEnvironment = e
        app.launch()
        if connected { exists(app.buttons["tab-today"], 15) }
    }

    /// Quits and starts again with the same settings (everything shown is then read from the server).
    func relaunch() {
        app.terminate()
        app.launchEnvironment["NOTED_FRESH_DRAFTS"] = "0"
        app.launch()
        exists(app.buttons["tab-today"], 15)
    }

    // MARK: finding things

    func button(_ label: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
    }

    func text(_ label: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
    }

    func any(_ label: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
    }

    /// Toasts only stay for a moment; this looks for one without failing when it is already gone.
    @discardableResult
    func sawToast(_ text: String) -> Bool { any(text).waitForExistence(timeout: 2) }

    /// Swipes a list row right: a full swipe runs the leading action, a short one only shows its
    /// button (to the left of the row, which slides over); then that button is tapped.
    func swipeRight(_ row: XCUIElement, action: String) {
        row.swipeRight()
        RunLoop.current.run(until: Date().addingTimeInterval(0.8))
        let rowLeft = row.exists ? row.frame.minX : 0
        let matches = app.buttons.matching(NSPredicate(format: "identifier == %@ OR label == %@", action, action)).allElementsBoundByIndex
        if let b = matches.first(where: { $0.isHittable && $0.frame.maxX <= rowLeft + 1 }) { b.tap() }
    }

    func byID(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    /// A row (list cell) that shows `title`.
    func cell(_ title: String) -> XCUIElement {
        app.cells.containing(NSPredicate(format: "label CONTAINS %@", title)).firstMatch
    }

    /// A to-do row on a timeline (今天, the calendar's day list).
    func todoRow(_ title: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'todo-' AND label CONTAINS %@", title)).firstMatch
    }

    func eventRow(_ title: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'event-' AND label CONTAINS %@", title)).firstMatch
    }

    func exists(_ e: XCUIElement, _ timeout: TimeInterval = 8, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(e.waitForExistence(timeout: timeout), "expected to see \(e)", file: file, line: line)
    }

    func gone(_ e: XCUIElement, _ timeout: TimeInterval = 8, file: StaticString = #filePath, line: UInt = #line) {
        let p = NSPredicate(format: "exists == false")
        let r = XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: p, object: e)], timeout: timeout)
        XCTAssertEqual(r, .completed, "expected \(e) to disappear", file: file, line: line)
    }

    /// Waits until `condition` holds (polling the UI).
    func eventually(_ timeout: TimeInterval = 8, _ what: String, file: StaticString = #filePath, line: UInt = #line, _ condition: () -> Bool) {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if condition() { return }
            RunLoop.current.run(until: Date().addingTimeInterval(0.25))
        }
        XCTAssertTrue(condition(), what, file: file, line: line)
    }

    /// Scrolls the page until `e` sits in the visible part of the screen (above the keyboard and
    /// the bottom bar): lists only build the rows near the screen, so a row further down does not
    /// exist until scrolled to.
    @discardableResult
    func reveal(_ e: XCUIElement, up: Bool = true, file: StaticString = #filePath, line: UInt = #line) -> XCUIElement {
        _ = e.waitForExistence(timeout: 3)
        for i in 0..<16 {
            if e.exists && e.isHittable && inSafeArea(e) { return e }
            if keyboardUp { dismissKeyboard(); continue }
            // Below the fold first, then back up.
            drag(up: (i < 8) == up)
        }
        XCTAssertTrue(e.exists && e.isHittable, "could not scroll to \(e)", file: file, line: line)
        return e
    }

    /// A keyboard dismissed by dragging can stay in the tree, parked below the screen.
    var keyboardUp: Bool {
        let k = app.keyboards.firstMatch
        return k.exists && k.frame.height > 0 && k.frame.minY < app.windows.firstMatch.frame.height - 1
    }

    /// Pulls the page down into the keyboard (the pages dismiss it interactively).
    func dismissKeyboard() {
        guard keyboardUp else { return }
        let w = app.windows.firstMatch
        let from = w.coordinate(withNormalizedOffset: CGVector(dx: 0.06, dy: 0.3))
        let to = w.coordinate(withNormalizedOffset: CGVector(dx: 0.06, dy: 0.99))
        from.press(forDuration: 0.05, thenDragTo: to, withVelocity: .slow, thenHoldForDuration: 0.1)
        _ = XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: app.keyboards.firstMatch)], timeout: 2)
    }

    /// Taps a day in the open graphical date picker (its days are labeled in English).
    func pickDay(_ date: Date) {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "MMMM d"
        let cal = app.datePickers.firstMatch
        exists(cal)
        let day = cal.buttons.matching(NSPredicate(format: "label ENDSWITH %@", ", \(f.string(from: date))")).firstMatch
        if !day.waitForExistence(timeout: 1) { cal.buttons["DatePicker.NextMonth"].tap() }
        exists(day)
        day.tap()
    }

    /// The lowest point that is not under the keyboard or a bottom bar.
    var visibleBottom: CGFloat {
        let h = app.windows.firstMatch.frame.height
        var limit = h - 40
        let tabs = app.buttons["tab-today"]
        // The tab bar is on the tab roots only (a pushed page has a back button); a hidden root's
        // bar still reports itself hittable.
        if tabs.exists && tabs.isHittable && !app.navigationBars.buttons["BackButton"].exists {
            limit = min(limit, tabs.frame.minY - 4)
        }
        // A page's own bottom bar (it floats above the keyboard).
        for id in ["create-add", "project-add", "holding-buy", "link-confirm"] {
            let b = app.buttons[id]
            if b.exists && b.frame.minY > h / 3 { limit = min(limit, b.frame.minY - 14) }
        }
        if keyboardUp { limit = min(limit, app.keyboards.firstMatch.frame.minY - 4) }
        return limit
    }

    /// Clear of the bars at the top and the bottom (a row under the tab bar reports hittable).
    func inSafeArea(_ e: XCUIElement) -> Bool {
        let f = e.frame
        return f.minY > 110 && f.maxY < visibleBottom
    }

    /// One slow drag on whatever is on screen (scrolls the page without flinging it).
    func drag(up: Bool) {
        let w = app.windows.firstMatch
        let h = w.frame.height
        let low = (visibleBottom - 20) / h, high = 220 / h
        let from = w.coordinate(withNormalizedOffset: CGVector(dx: 0.06, dy: up ? low : high))
        let to = w.coordinate(withNormalizedOffset: CGVector(dx: 0.06, dy: up ? high : low))
        from.press(forDuration: 0.05, thenDragTo: to, withVelocity: .slow, thenHoldForDuration: 0.15)
    }

    /// Taps the switch of a Toggle row whose label contains `label`.
    func toggle(_ label: String) {
        let s = app.switches.matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
        reveal(s)
        s.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
    }

    func tab(_ id: String) {
        let b = app.buttons["tab-\(id)"]
        exists(b)
        b.tap()
    }

    func back() { app.navigationBars.buttons.element(boundBy: 0).tap() }

    func type(_ s: String, into f: XCUIElement) {
        exists(f)
        f.tap()
        f.typeText(s)
    }

    func replace(_ s: String, in f: XCUIElement) {
        exists(f)
        f.clearText()
        f.typeText(s)
    }

    func unique(_ prefix: String) -> String { "\(prefix) \(Int(Date().timeIntervalSince1970) % 100_000)" }

    /// Tests that put something at "the next hour" need that to still be today.
    func skipLateAtNight() throws {
        let h = Calendar.current.component(.hour, from: .now)
        try XCTSkipIf(h >= 23, "the next hour would fall on tomorrow")
    }

    /// Today's to-do row, scrolled to and opened (tapping the title, not the check).
    func openTodo(_ title: String) {
        exists(app.collectionViews["today-list"], 10)
        let row = todoRow(title)
        reveal(row)
        row.coordinate(withNormalizedOffset: CGVector(dx: 0.6, dy: 0.5)).tap()
        exists(app.navigationBars["待办"])
    }

    /// 计划 → a project card.
    func openProject(_ title: String) {
        tab("plan")
        let card = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'project-' AND label CONTAINS %@", title)).firstMatch
        reveal(card).tap()
        exists(app.navigationBars[title])
    }

    func openSettings() {
        let b = app.buttons["settings"].firstMatch
        exists(b)
        b.tap()
        exists(app.navigationBars["设置"])
    }

    /// Picks `option` from a menu-style Picker row whose label contains `row`.
    func pick(_ option: String, inMenu row: String) {
        let b = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", row)).firstMatch
        exists(b)
        reveal(b).tap()
        let item = app.buttons[option].firstMatch
        exists(item)
        item.tap()
    }
}

extension XCUIElement {
    /// Empties a text field: the cursor goes to the end of the text, then everything is deleted.
    func clearText() {
        // An empty field reports its placeholder as its value, so delete that many anyway.
        guard let s = value as? String, !s.isEmpty else { tap(); return }
        coordinate(withNormalizedOffset: CGVector(dx: 0.99, dy: 0.5)).tap()
        typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: s.count + 1))
        if let left = value as? String, !left.isEmpty, left != placeholderValue {
            // The cursor landed elsewhere: select a word at a time and delete it.
            for _ in 0..<6 {
                guard let v = value as? String, !v.isEmpty, v != placeholderValue else { break }
                doubleTap()
                typeText(XCUIKeyboardKey.delete.rawValue)
            }
        }
    }
}
