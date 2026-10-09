import XCTest

/// Drives the real app against a real noted server (and a stand-in model server).
/// The script `ios/Tests/run-ui-tests.sh` starts both and passes NOTED_TOKEN.
class NotedUITestCase: XCTestCase {
    var app: XCUIApplication!
    static let pendingDump = "/tmp/noted-ui-pending.json"

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
        }
        super.tearDown()
    }

    var env: [String: String] { ProcessInfo.processInfo.environment }

    func launch(pro: Bool = false, mode: String = "all", connected: Bool = true, reset: Bool = false, extra: [String: String] = [:]) throws {
        let token = try XCTUnwrap(env["NOTED_TOKEN"], "NOTED_TOKEN is not set: run ios/Tests/run-ui-tests.sh")
        app = XCUIApplication()
        var e = ["NOTED_MODE": mode, "NOTED_PRO": pro ? "1" : "", "NOTED_DUMP_PENDING": Self.pendingDump, "NOTED_PROVISIONAL_NOTIFICATIONS": "1", "NOTED_CAPTURE_MINUTES": "20", "NOTED_DEBUG_LOG": "/tmp/noted-ui-debug.log"]
        if connected {
            e["NOTED_HOST"] = env["NOTED_HOST"] ?? "127.0.0.1"
            e["NOTED_PORT"] = env["NOTED_PORT"] ?? "43901"
            e["NOTED_TOKEN"] = token
        }
        if reset { e["NOTED_RESET"] = "1" }
        e.merge(extra) { $1 }
        app.launchEnvironment = e
        app.launch()
    }

    // MARK: finding things

    func button(_ label: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
    }

    func text(_ label: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
    }

    func exists(_ e: XCUIElement, _ timeout: TimeInterval = 8, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(e.waitForExistence(timeout: timeout), "expected to see \(e)", file: file, line: line)
    }

    func gone(_ e: XCUIElement, _ timeout: TimeInterval = 8, file: StaticString = #filePath, line: UInt = #line) {
        let p = NSPredicate(format: "exists == false")
        let r = XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: p, object: e)], timeout: timeout)
        XCTAssertEqual(r, .completed, "expected \(e) to disappear", file: file, line: line)
    }

    func tab(_ name: String) {
        let b = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", name)).element(boundBy: 0)
        exists(b)
        b.tap()
    }

    func openCapture() { let b = app.buttons["记一笔"]; exists(b); b.tap() }

    func openSettings() { let b = app.buttons["设置"]; exists(b); b.tap() }

    func type(_ s: String, into f: XCUIElement) {
        exists(f)
        f.tap()
        f.typeText(s)
    }

    func unique(_ prefix: String) -> String { "\(prefix) \(Int(Date().timeIntervalSince1970) % 1_000_000)" }

    /// New items default to 20 minutes from now in tests; that must still be today.
    func skipLateAtNight() throws {
        let c = Calendar.current.dateComponents([.hour, .minute], from: .now)
        try XCTSkipIf((c.hour ?? 0) == 23 && (c.minute ?? 0) >= 35, "twenty minutes from now would fall on tomorrow")
    }

    func pendingNotificationIDs() -> [String] {
        guard let d = try? Data(contentsOf: URL(fileURLWithPath: Self.pendingDump)),
              let ids = try? JSONDecoder().decode([String].self, from: d) else { return [] }
        return ids
    }
}
