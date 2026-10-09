import XCTest

final class ReminderUITests: NotedUITestCase {
    /// The system asks once for permission to send notifications.
    private func allowNotifications() {
        addUIInterruptionMonitor(withDescription: "notifications") { alert in
            for label in ["允许", "Allow"] where alert.buttons[label].exists {
                alert.buttons[label].tap()
                return true
            }
            return false
        }
    }

    func testAnEventWithAReminderIsScheduledAsALocalNotification() throws {
        try skipLateAtNight()
        try? FileManager.default.removeItem(atPath: Self.pendingDump)
        allowNotifications()
        try launch()
        app.staticTexts["概览"].firstMatch.tap()   // somewhere harmless; lets the interruption monitor answer the permission alert

        let title = unique("UIT remind")
        openCapture()
        app.segmentedControls.buttons["日程"].tap()
        type(title, into: app.textFields["要做什么"])
        let picker = app.buttons["remind-picker"].exists ? app.buttons["remind-picker"] : app.otherElements["remind-picker"]
        exists(picker)
        picker.tap()
        app.buttons["提前 10 分钟"].tap()
        button("保存").tap()
        exists(text(title), 10)

        let deadline = Date().addingTimeInterval(20)
        var ids: [String] = []
        while Date() < deadline {
            ids = pendingNotificationIDs()
            if ids.contains(where: { $0.hasPrefix("noted.e.") }) { break }
            usleep(500_000)
        }
        XCTAssertTrue(ids.contains { $0.hasPrefix("noted.e.") }, "the event's reminder was scheduled; pending: \(ids)")
    }

    func testAnEventWithoutAReminderSchedulesNothingNew() throws {
        try skipLateAtNight()
        allowNotifications()
        try launch()
        app.staticTexts["概览"].firstMatch.tap()
        let before = Set(pendingNotificationIDs())
        let title = unique("UIT quiet")
        openCapture()
        app.segmentedControls.buttons["日程"].tap()
        type(title, into: app.textFields["要做什么"])
        button("保存").tap()
        exists(text(title), 10)
        sleep(3)
        XCTAssertEqual(Set(pendingNotificationIDs()), before.union(Set(pendingNotificationIDs()).filter { _ in false }), "no reminder was asked for")
    }

    /// The server scheduler fires a reminder for an event the script created a minute ago.
    func testAReminderThatFiresWhileTheAppIsOpenShowsABanner() throws {
        try XCTSkipUnless(env["NOTED_EXPECT_BANNER"] == "1", "needs run-ui-tests.sh to create the event")
        try launch()
        // The banner is the only thing that says "N:NN 开始": the list rows show just the time.
        let banner = app.descendants(matching: .any)["reminder-banner"]
        XCTAssertTrue(banner.waitForExistence(timeout: 120), "no reminder banner appeared")
        let close = app.buttons["关闭提醒"]
        exists(close, 3)
        close.tap()
        gone(banner, 5)
    }
}
