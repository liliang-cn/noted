import XCTest

final class WatchUITests: XCTestCase {
    override func setUp() { continueAfterFailure = false }

    private func launch() -> XCUIApplication {
        let app = XCUIApplication()
        app.launchEnvironment = ["NOTED_SAMPLE": "1"]
        app.launch()
        return app
    }

    func testTodayListShowsTheDay() {
        let app = launch()
        XCTAssertTrue(app.staticTexts["团队站会"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["牙医复诊"].exists)
        XCTAssertTrue(app.staticTexts["交房租"].exists)
    }

    func testCompletingWithoutThePhoneExplainsWhy() {
        let app = launch()
        let box = app.buttons["完成 交房租"]
        XCTAssertTrue(box.waitForExistence(timeout: 10))
        box.tap()
        XCTAssertTrue(app.staticTexts["手机不在身边"].waitForExistence(timeout: 5))
    }

    func testGoalsPageHasCheckIn() {
        let app = launch()
        XCTAssertTrue(app.staticTexts["团队站会"].waitForExistence(timeout: 10))
        // the pages sit one under the other: scroll to the second with the crown
        for _ in 0..<4 where !app.staticTexts["运动"].exists {
            XCUIDevice.shared.rotateDigitalCrown(delta: 0.6)
            _ = app.staticTexts["运动"].waitForExistence(timeout: 2)
        }
        XCTAssertTrue(app.staticTexts["运动"].exists || app.buttons["打卡 +1"].exists, "the goals page did not come up")
    }

    func testCheckingInWithoutThePhoneExplainsWhy() {
        let app = launch()
        XCTAssertTrue(app.staticTexts["团队站会"].waitForExistence(timeout: 10))
        for _ in 0..<4 where !app.buttons["打卡 +1"].exists {
            XCUIDevice.shared.rotateDigitalCrown(delta: 0.6)
            _ = app.buttons["打卡 +1"].waitForExistence(timeout: 2)
        }
        let b = app.buttons["打卡 +1"].firstMatch
        XCTAssertTrue(b.exists)
        b.tap()
        XCTAssertTrue(app.staticTexts["手机不在身边"].waitForExistence(timeout: 5) || app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "手机")).firstMatch.exists)
    }
}

/// Needs a paired iPhone simulator running the app against a real server: see Tests/run-watch-roundtrip.sh.
final class WatchRoundTripUITests: XCTestCase {
    override func setUp() { continueAfterFailure = false }

    func testFinishingATaskOnTheWatchFinishesItOnTheServer() throws {
        let title = try XCTUnwrap(ProcessInfo.processInfo.environment["NOTED_WATCH_TASK"], "run Tests/run-watch-roundtrip.sh")
        let app = XCUIApplication()
        app.launch()
        // what the phone sent arrives on the watch
        let box = app.buttons["完成 \(title)"]
        XCTAssertTrue(app.staticTexts.firstMatch.waitForExistence(timeout: 60), "the watch never received today's list from the phone")
        // the list is lazy: the task may sit below the first screen
        for _ in 0..<10 where !box.exists {
            XCUIDevice.shared.rotateDigitalCrown(delta: 0.6)
            _ = box.waitForExistence(timeout: 2)
        }
        XCTAssertTrue(box.exists, "the task made on the server never showed up on the watch")
        box.tap()
        // the phone does it against the server and sends the new list back
        let gone = NSPredicate(format: "exists == false")
        XCTAssertEqual(XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: gone, object: box)], timeout: 30), .completed,
                       "the task is still on the watch after finishing it")
    }
}
