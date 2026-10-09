import XCTest

final class ConnectUITests: NotedUITestCase {
    private func fill(host: String, port: String, token: String) {
        let h = app.textFields["192.168.1.10"]
        exists(h)
        h.tap(); h.typeText(host)
        let p = app.textFields["43872"]
        p.tap()
        p.press(forDuration: 1.0)
        if app.menuItems["Select All"].waitForExistence(timeout: 1) { app.menuItems["Select All"].tap() }
        p.typeText(port)
        let t = app.secureTextFields["noted_…"]
        t.tap(); t.typeText(token)
    }

    func testWrongTokenIsRejectedThenTheRightOneConnects() throws {
        try launch(connected: false, reset: true)
        exists(text("连接你的服务器"))
        fill(host: "127.0.0.1", port: "43901", token: "noted_not_a_real_token")
        button("连接").tap()
        exists(text("令牌无效或已被撤销"))

        let t = app.secureTextFields["noted_…"]
        t.tap()
        if let old = t.value as? String, !old.isEmpty {
            t.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: old.count))
        }
        t.typeText(try XCTUnwrap(env["NOTED_TOKEN"]))
        button("连接").tap()
        exists(text("置顶项目"), 15)
        exists(app.buttons["记一笔"])
    }

    func testUnreachableServerSaysSo() throws {
        try launch(connected: false, reset: true)
        fill(host: "127.0.0.1", port: "1", token: try XCTUnwrap(env["NOTED_TOKEN"]))
        button("连接").tap()
        exists(text("连不上服务器"), 20)
    }

    func testDisconnectReturnsToTheConnectScreen() throws {
        try launch()
        openSettings()
        let b = button("断开连接")
        exists(b)
        b.tap()
        exists(text("连接你的服务器"))
    }
}
