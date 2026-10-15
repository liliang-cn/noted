import CryptoKit
import XCTest

/// 接入 AI 助手 → pairing a client that signs in through the browser. The test plays Claude Code:
/// it registers an OAuth client on the test server's HTTP port and opens the authorize page, which
/// shows the pairing code; then the app approves (or turns away) that code.
final class PairingUITests: NotedUITestCase {
    var httpPort: String { env["NOTED_MCP_PORT"] ?? "43921" }

    func testPairingCodeTypedInTheAppConnectsTheClient() throws {
        let client = unique("UIT Claude")
        let pairing = try startPairing(client)
        try launch()
        openSettings()
        app.buttons["settings-ai-connect"].tap()
        exists(app.navigationBars["接入 AI 助手"])
        app.buttons["pair-enter-code"].tap()
        // Typed the way people do: lower case, a space instead of the dash.
        let typed = "\(pairing.code.prefix(4)) \(pairing.code.dropFirst(4))".lowercased()
        enterCode(typed)

        exists(app.navigationBars["配对"])
        let name = app.staticTexts["pair-client"]
        exists(name, 10)
        XCTAssertEqual(name.label, client)
        exists(text(display(pairing.code)))
        app.buttons["pair-allow"].tap()
        let done = app.buttons["pair-done"]
        exists(done, 10)
        exists(text("已连接，回到电脑上继续"))
        try finishSignIn(pairing)
        done.tap()

        exists(app.navigationBars["接入 AI 助手"])
        // The client's token is labelled with its name.
        reveal(app.buttons["吊销 \(client)"])
        XCTAssertTrue(app.buttons["吊销 \(client)"].exists)
    }

    func testUnknownPairingCodeSaysSo() throws {
        try launch()
        openSettings()
        app.buttons["settings-ai-connect"].tap()
        app.buttons["pair-enter-code"].tap()
        enterCode("ZZZZ-2222")
        let error = app.staticTexts["pair-error"]
        exists(error, 10)
        XCTAssertEqual(error.label, "配对码无效或已过期")
        // Another code can be typed from there.
        exists(app.buttons["pair-enter-code"])
        XCTAssertFalse(app.buttons["pair-allow"].exists)
    }

    /// The system camera reads the QR code and opens the `noted://pair` link it holds.
    func testPairingLinkOpensTheConfirmPage() throws {
        let client = unique("UIT Linked")
        let pairing = try startPairing(client)
        try launch()
        open(pairing.link)
        let name = app.staticTexts["pair-client"]
        exists(name, 10)
        XCTAssertEqual(name.label, client)
        app.buttons["pair-deny"].tap()
        gone(app.buttons["pair-deny"])
        exists(app.buttons["tab-today"])

        // A code minted by another server is refused before anything is asked of this one.
        // (No waiting for a system prompt here: the error toast only stays for a few seconds.)
        open("noted://pair?code=ABCD2345&server=https://elsewhere.example", confirm: false)
        exists(any("这个配对码来自 elsewhere.example"), 4)
        XCTAssertFalse(app.staticTexts["pair-client"].exists)
    }

    // MARK: helpers

    private func enterCode(_ code: String) {
        let alert = app.alerts["输入配对码"]
        exists(alert)
        let field = alert.textFields.firstMatch
        exists(field)
        field.tap()
        field.typeText(code)
        alert.buttons["继续"].firstMatch.tap()
    }

    private func open(_ link: String, confirm: Bool = true) {
        XCUIDevice.shared.system.open(URL(string: link)!)
        guard confirm else { return }
        // iOS may ask before handing the link to the app.
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let ok = springboard.buttons.matching(NSPredicate(format: "label IN {'Open', '打开'}")).firstMatch
        if ok.waitForExistence(timeout: 2) { ok.tap() }
        XCTAssertTrue(app.wait(for: .runningForeground, timeout: 5))
    }

    private func display(_ code: String) -> String { "\(code.prefix(4))-\(code.dropFirst(4))" }

    /// What Claude Code does after `/mcp` → 登录: dynamic client registration, then the authorize
    /// page (PKCE, S256). The page shows the code and a QR code of `noted://pair?code=…&server=…`.
    /// A sign-in waiting on the pairing page, as Claude Code and its browser see it.
    struct Pending {
        var code: String        // K7QM29XD
        var link: String        // what the page's QR code holds
        var poll: String        // the page's own address: /oauth/pair?p=…
        var clientID: String
        var verifier: String
    }

    private var base: String { "http://127.0.0.1:\(httpPort)" }
    private let redirect = "http://127.0.0.1:43999/callback"

    /// What Claude Code does after `/mcp` → 登录: dynamic client registration, then the authorize
    /// step (PKCE, S256), which redirects the browser to the pairing page that shows the code.
    private func startPairing(_ clientName: String) throws -> Pending {
        var reg = URLRequest(url: URL(string: "\(base)/oauth/register")!)
        reg.httpMethod = "POST"
        reg.setValue("application/json", forHTTPHeaderField: "Content-Type")
        reg.httpBody = try JSONSerialization.data(withJSONObject: [
            "client_name": clientName, "redirect_uris": [redirect],
            "grant_types": ["authorization_code"], "response_types": ["code"],
            "token_endpoint_auth_method": "none",
        ])
        let (regBody, regStatus, _) = try fetch(reg)
        XCTAssertTrue((200..<300).contains(regStatus), "POST /oauth/register: \(regStatus) \(String(decoding: regBody, as: UTF8.self))")
        let registered = try XCTUnwrap(try JSONSerialization.jsonObject(with: regBody) as? [String: Any])
        let clientID = try XCTUnwrap(registered["client_id"] as? String, "\(registered)")

        let verifier = (0..<3).map { _ in UUID().uuidString }.joined().replacingOccurrences(of: "-", with: "")
        let challenge = Data(SHA256.hash(data: Data(verifier.utf8))).base64EncodedString()
            .replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
        var url = URLComponents(string: "\(base)/oauth/authorize")!
        url.queryItems = [
            .init(name: "response_type", value: "code"), .init(name: "client_id", value: clientID),
            .init(name: "redirect_uri", value: redirect), .init(name: "code_challenge", value: challenge),
            .init(name: "code_challenge_method", value: "S256"), .init(name: "state", value: "uit"),
            .init(name: "resource", value: "\(base)/mcp"),
        ]
        let (pageBody, pageStatus, landed) = try fetch(URLRequest(url: url.url!))
        let page = String(decoding: pageBody, as: UTF8.self)
        XCTAssertEqual(pageStatus, 200, "GET /oauth/authorize: \(page.prefix(300))")
        let poll = try XCTUnwrap(landed.flatMap { URLComponents(url: $0, resolvingAgainstBaseURL: false) }?
            .queryItems?.first { $0.name == "p" }?.value, "authorize did not land on the pairing page: \(landed?.absoluteString ?? "")")
        let shown = try XCTUnwrap(firstMatch(#"class="pair-code"[^>]*>([A-Z0-9]{4}-[A-Z0-9]{4})<"#, in: page),
                                  "no pairing code on the page: \(page.prefix(500))")
        let code = shown.replacingOccurrences(of: "-", with: "")
        var link = URLComponents(string: "noted://pair")!
        link.queryItems = [.init(name: "code", value: code), .init(name: "server", value: base)]
        return Pending(code: code, link: link.string!, poll: poll, clientID: clientID, verifier: verifier)
    }

    /// The browser notices the approval and sends the authorization code back to Claude Code,
    /// which trades it for a token (the one 已签发的令牌 then lists).
    private func finishSignIn(_ p: Pending) throws {
        var status: [String: Any] = [:]
        let end = Date().addingTimeInterval(10)
        while Date() < end {
            var q = URLComponents(string: "\(base)/oauth/pair/status")!
            q.queryItems = [.init(name: "p", value: p.poll)]
            let (body, _, _) = try fetch(URLRequest(url: q.url!))
            status = (try? JSONSerialization.jsonObject(with: body) as? [String: Any]) ?? [:]
            if status["status"] as? String != "pending" { break }
            Thread.sleep(forTimeInterval: 0.5)
        }
        XCTAssertEqual(status["status"] as? String, "approved", "\(status)")
        let to = try XCTUnwrap(status["redirect"] as? String, "\(status)")
        let authCode = try XCTUnwrap(URLComponents(string: to)?.queryItems?.first { $0.name == "code" }?.value, to)

        var form = URLComponents()
        form.queryItems = [
            .init(name: "grant_type", value: "authorization_code"), .init(name: "code", value: authCode),
            .init(name: "client_id", value: p.clientID), .init(name: "redirect_uri", value: redirect),
            .init(name: "code_verifier", value: p.verifier), .init(name: "resource", value: "\(base)/mcp"),
        ]
        var tok = URLRequest(url: URL(string: "\(base)/oauth/token")!)
        tok.httpMethod = "POST"
        tok.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        tok.httpBody = Data((form.percentEncodedQuery ?? "").replacingOccurrences(of: "+", with: "%2B").utf8)
        let (body, code, _) = try fetch(tok)
        let json = (try? JSONSerialization.jsonObject(with: body) as? [String: Any]) ?? [:]
        XCTAssertEqual(code, 200, "POST /oauth/token: \(String(decoding: body, as: UTF8.self))")
        XCTAssertTrue((json["access_token"] as? String)?.hasPrefix("noted_") == true, "\(json)")
    }

    private func firstMatch(_ pattern: String, in s: String) -> String? {
        guard let re = try? NSRegularExpression(pattern: pattern),
              let m = re.firstMatch(in: s, range: NSRange(s.startIndex..., in: s)),
              let r = Range(m.range(at: m.numberOfRanges > 1 ? 1 : 0), in: s) else { return nil }
        return String(s[r])
    }

    /// A plain blocking HTTP call (the test runner is not async).
    private func fetch(_ request: URLRequest) throws -> (Data, Int, URL?) {
        final class Box: @unchecked Sendable { var data = Data(); var status = 0; var url: URL?; var error: Error? }
        let box = Box()
        let done = DispatchSemaphore(value: 0)
        var r = request
        r.timeoutInterval = 10
        URLSession.shared.dataTask(with: r) { data, response, error in
            box.data = data ?? Data()
            box.status = (response as? HTTPURLResponse)?.statusCode ?? 0
            box.url = response?.url
            box.error = error
            done.signal()
        }.resume()
        guard done.wait(timeout: .now() + 15) == .success else { throw URLError(.timedOut) }
        if let e = box.error { throw e }
        return (box.data, box.status, box.url)
    }
}
