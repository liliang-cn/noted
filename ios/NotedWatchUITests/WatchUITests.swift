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
        // the sections sit one under the other: scroll down with the crown
        scroll(app, to: app.staticTexts["运动"])
        XCTAssertTrue(app.staticTexts["运动"].exists || app.buttons["打卡 +1"].exists, "the goals page did not come up")
    }

    /// The list is long: crown down until the thing shows up.
    private func scroll(_ app: XCUIApplication, to e: XCUIElement) {
        // one turn of 0.6 moves about a screen, so small steps: nothing is skipped over
        for delta in [0.15, -0.15] {
            for _ in 0..<40 where !e.exists {
                XCUIDevice.shared.rotateDigitalCrown(delta: delta)
                _ = e.waitForExistence(timeout: 0.5)
            }
        }
    }

    func testPlansShowTheResultAndEachDimension() {
        let app = launch()
        XCTAssertTrue(app.staticTexts["团队站会"].waitForExistence(timeout: 10))
        let title = app.staticTexts["减肥"]
        scroll(app, to: title)
        XCTAssertTrue(title.exists, "the plan did not come up")
        let reading = app.staticTexts["73.2"]
        scroll(app, to: reading)
        XCTAssertTrue(reading.exists, "the latest reading shows")
        scroll(app, to: app.buttons["打卡 游泳"])
        XCTAssertTrue(app.buttons["打卡 游泳"].exists, "a dimension can be checked in from the watch")
    }

    func testCheckingInADimensionWithoutThePhoneExplainsWhy() {
        let app = launch()
        XCTAssertTrue(app.staticTexts["团队站会"].waitForExistence(timeout: 10))
        let b = app.buttons["打卡 游泳"]
        scroll(app, to: b)
        XCTAssertTrue(b.exists)
        b.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "手机")).firstMatch.waitForExistence(timeout: 5))
    }

    func testInvestingShowsWhichPlansAreStillToDoThisMonth() {
        let app = launch()
        XCTAssertTrue(app.staticTexts["团队站会"].waitForExistence(timeout: 10))
        let sym = app.staticTexts["QQQM"]
        scroll(app, to: sym)
        XCTAssertTrue(sym.exists, "the holding did not come up")
        XCTAssertTrue(app.staticTexts["待投"].exists, "QQQM has not been bought this month")
        scroll(app, to: app.staticTexts["已投"])
        XCTAssertTrue(app.staticTexts["已投"].exists, "VOO has")
    }

    func testCheckingInWithoutThePhoneExplainsWhy() {
        let app = launch()
        XCTAssertTrue(app.staticTexts["团队站会"].waitForExistence(timeout: 10))
        scroll(app, to: app.buttons["打卡 +1"])
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

    func testCheckingInADimensionOnTheWatchRecordsItOnTheServer() throws {
        let dim = try XCTUnwrap(ProcessInfo.processInfo.environment["NOTED_WATCH_DIM"], "run Tests/run-watch-roundtrip.sh")
        let app = XCUIApplication()
        app.launch()
        let b = app.buttons["打卡 \(dim)"]
        XCTAssertTrue(app.staticTexts.firstMatch.waitForExistence(timeout: 60), "the watch never received the plan from the phone")
        for _ in 0..<14 where !b.exists {
            XCUIDevice.shared.rotateDigitalCrown(delta: 0.6)
            _ = b.waitForExistence(timeout: 2)
        }
        XCTAssertTrue(b.exists, "the dimension made on the server never showed up on the watch")
        b.tap()
        // the new count comes back from the phone: 1 of 2
        XCTAssertTrue(app.staticTexts["1/2"].waitForExistence(timeout: 30), "the watch did not get the new count back")
    }
}

