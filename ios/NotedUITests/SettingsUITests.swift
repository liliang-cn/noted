import XCTest

/// 设置: an AI assistant token (made, shown with the Claude Code command, revoked), 导出数据, 退出登录.
final class SettingsUITests: NotedUITestCase {
    func testAIAssistantTokenCanBeMadeAndRevoked() throws {
        try launch()
        openSettings()
        app.buttons["settings-ai-connect"].tap()
        exists(app.navigationBars["接入 AI 助手"])
        reveal(app.buttons["token-generate"].firstMatch).tap()
        let alert = app.alerts["生成令牌"]
        exists(alert)
        let label = unique("UIT 令牌")
        let name = alert.textFields.firstMatch
        name.clearText()
        name.typeText(label)
        alert.buttons["生成"].firstMatch.tap()
        let secret = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'noted_'")).firstMatch
        exists(secret, 10)
        // The pairing command (no token) comes first; this is the one with the token.
        let command = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'claude mcp add --scope user' AND label CONTAINS 'Bearer'")).firstMatch
        exists(command)
        XCTAssertTrue(command.label.contains(secret.label), command.label)
        let revoke = app.buttons["吊销 \(label)"]
        reveal(revoke).tap()
        let confirm = app.alerts.buttons["吊销"].firstMatch
        exists(confirm)
        confirm.tap()
        sawToast("已吊销")
        gone(app.buttons["吊销 \(label)"])
        gone(secret)
    }

    func testExportMakesAFile() throws {
        try? FileManager.default.removeItem(atPath: Self.exportCopy)
        try launch()
        openSettings()
        reveal(app.buttons["settings-export"]).tap()
        exists(app.navigationBars["导出数据"])
        app.buttons["export"].tap()
        exists(app.buttons["export-share"], 20)
        exists(any(".zip"))
        eventually(5, "the zip was copied out for verify.py") { FileManager.default.fileExists(atPath: Self.exportCopy) }
    }

    func testSignOutReturnsToSignIn() throws {
        try launch()
        openSettings()
        let out = app.buttons["sign-out"]
        reveal(out).tap()
        let confirm = app.buttons.matching(NSPredicate(format: "label == '退出登录' AND identifier != 'sign-out'")).firstMatch
        exists(confirm)
        confirm.tap()
        exists(app.buttons["apple-sign-in"], 10)
        exists(app.buttons["own-server"])
    }
}
