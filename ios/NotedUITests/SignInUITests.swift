import XCTest

/// The page before connecting: Apple, 邮箱, 自己的服务器; a wrong token, then the right one.
final class SignInUITests: NotedUITestCase {
    func testSignInOffersAppleEmailAndOwnServer() throws {
        try launch(connected: false, reset: true)
        exists(app.buttons["apple-sign-in"], 10)
        exists(app.buttons["email-sign-in"])
        exists(app.buttons["own-server"])
        XCTAssertTrue(app.buttons["email-sign-in"].label.contains("邮箱"))
        XCTAssertTrue(app.buttons["own-server"].label.contains("自己的服务器"))
        app.buttons["email-sign-in"].tap()
        exists(app.textFields["email"])
        exists(app.secureTextFields["password"])
    }

    func testWrongTokenShowsChineseErrorAndRightTokenOpensToday() throws {
        try launch(connected: false, reset: true)
        let own = app.buttons["own-server"]
        exists(own, 10)
        own.tap()
        replace("127.0.0.1", in: app.textFields["host"])
        replace(port, in: app.textFields["port"])
        XCTAssertEqual(app.textFields["host"].value as? String, "127.0.0.1")
        XCTAssertEqual(app.textFields["port"].value as? String, port)
        type("noted_0000wrong0000", into: app.secureTextFields["token"])
        app.buttons["connect"].tap()
        let error = app.staticTexts["connect-error"]
        exists(error, 10)
        XCTAssertTrue(error.label.contains("令牌无效"), error.label)

        replace(token, in: app.secureTextFields["token"])
        app.buttons["connect"].tap()
        exists(app.buttons["tab-today"], 15)
        exists(app.navigationBars["今天"])
    }
}
