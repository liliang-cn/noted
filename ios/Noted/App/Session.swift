import Foundation
import Observation
import SwiftUI
import GRPCCore

@MainActor @Observable
final class Session {
    var api: API?
    var mode: Mode = .all
    var theme = ThemeSpec()
    var layout = Layout()
    let pro = Pro()
    let reminders = ReminderCenter()
    var mute = MuteRules()
    var palette: Palette { theme.palette }
    var connectError: String?
    var connecting = false
    var aiChat = false
    var aiNoteTools = false

    /// The hosted service: the app opens pointing here, so signing in with Apple is one tap.
    static let hostedHost = "noted.superleo.cn"

    var host: String
    var port: String
    var tls: Bool

    init() {
        let d = UserDefaults.standard
        host = d.string(forKey: "host") ?? ""
        port = d.string(forKey: "port") ?? "43872"
        tls = d.bool(forKey: "tls")
        if host.isEmpty, d.object(forKey: "port") == nil {
            host = Session.hostedHost; port = "443"; tls = true
        }
        if let raw = d.string(forKey: "mode"), let m = Mode(rawValue: raw) { mode = m }
        #if DEBUG
        let env = ProcessInfo.processInfo.environment
        if env["NOTED_RESET"] == "1" {
            Keychain.remove("token")
            for k in ["host", "port", "tls", "mode"] { d.removeObject(forKey: k) }
            host = ""; port = "43872"; tls = false; mode = .all
        }
        // UI tests start without the 新建 and note drafts an earlier test left behind.
        if env["NOTED_FRESH_DRAFTS"] == "1" {
            for k in d.dictionaryRepresentation().keys where k.hasPrefix("create.draft.") || k.hasPrefix("draft.note.") {
                d.removeObject(forKey: k)
            }
        }
        if let h = env["NOTED_HOST"], let t = env["NOTED_TOKEN"] {
            host = h
            if let m = env["NOTED_MODE"], let mm = Mode(rawValue: m) { mode = mm }
            port = env["NOTED_PORT"] ?? port
            tls = env["NOTED_TLS"] == "1"
            api = try? API(host: h, port: Int(port) ?? 43872, tls: tls, token: t)
            return
        }
        #endif
        if !host.isEmpty, let token = Keychain.get("token") {
            api = try? API(host: host, port: Int(port) ?? 43872, tls: tls, token: token)
        }
    }

    /// What the server's AI layer offers, so the app only shows entry points that work.
    func refreshAI() async {
        guard let api else { return }
        guard let st = try? await api.ai.getStatus(Noted_V1_GetStatusRequest()), st.enabled, st.chat else {
            aiChat = false; aiNoteTools = false
            return
        }
        aiChat = true
        let f = try? await api.ai.getAIFeatures(Noted_V1_GetAIFeaturesRequest())
        aiNoteTools = f?.noteTools ?? true
    }

    /// The server can grant Pro outside the App Store; this is where the app learns of it.
    func refreshAccount() async {
        guard let api, let a = try? await api.account.getAccount(Noted_V1_GetAccountRequest()) else { return }
        pro.setServerGrant(a.pro)
    }

    /// Theme and layout live on the server so every device shows the same app.
    func loadPreferences() async {
        guard let api else { return }
        func read<T: Decodable>(_ key: String, as: T.Type) async -> T? {
            var r = Noted_V1_GetPreferenceRequest(); r.key = key
            guard let p = try? await api.prefs.getPreference(r) else { return nil }
            return try? JSONDecoder().decode(T.self, from: Data(p.value.utf8))
        }
        if let t = await read("ui.theme", as: ThemeSpec.self) { theme = t }
        if let l = await read("ui.layout", as: Layout.self) { layout = l }
        if let m = await read("ui.mute", as: MuteRules.self) { mute = m }
    }

    func saveTheme(_ t: ThemeSpec) {
        theme = t
        write("ui.theme", t)
    }

    func saveMute(_ m: MuteRules) {
        mute = m
        write("ui.mute", m)
        Task { await syncReminders() }
    }

    /// Everything that mirrors the server outside the app: pending reminders, widgets, the watch.
    func syncExtras() async {
        #if DEBUG
        debugLog("syncExtras")
        #endif
        await syncReminders()
        await SnapshotBuilder.refresh(session: self)
    }

    func syncReminders() async {
        guard let api else { return }
        await reminders.sync(api: api, mode: mode, mute: mute)
    }

    func saveLayout(_ l: Layout) {
        layout = l
        write("ui.layout", l)
    }

    private func write<T: Encodable>(_ key: String, _ value: T) {
        guard let api, let data = try? JSONEncoder().encode(value) else { return }
        Task {
            var r = Noted_V1_SetPreferenceRequest()
            r.key = key; r.value = String(decoding: data, as: UTF8.self)
            _ = try? await api.prefs.setPreference(r)
        }
    }

    func setMode(_ m: Mode) {
        mode = m
        Task { await syncExtras() }
        UserDefaults.standard.set(m.rawValue, forKey: "mode")
    }

    /// Checks the server answers with this token before keeping the connection.
    func connect(token: String) async {
        connecting = true
        connectError = nil
        defer { connecting = false }
        let h = host.trimmingCharacters(in: .whitespaces)
        guard !h.isEmpty, let p = Int(port) else {
            connectError = "地址或端口不对"
            return
        }
        do {
            let candidate = try API(host: h, port: p, tls: tls, token: token)
            var req = Noted_V1_ListNotesRequest()
            req.pageSize = 1
            _ = try await candidate.notes.listNotes(req)
            let d = UserDefaults.standard
            d.set(h, forKey: "host")
            d.set(port, forKey: "port")
            d.set(tls, forKey: "tls")
            Keychain.set(token, for: "token")
            host = h
            api = candidate
        } catch {
            connectError = describe(error)
        }
    }

    /// Trades an Apple identity token for this server's own token, then connects as usual.
    func signInWithApple(_ proof: AppleProof) async {
        connecting = true
        connectError = nil
        let h = host.trimmingCharacters(in: .whitespaces)
        guard !h.isEmpty, let p = Int(port) else {
            connecting = false
            connectError = "地址或端口不对"
            return
        }
        do {
            let anon = try API(host: h, port: p, tls: tls, token: "")
            var req = Noted_V1_SignInWithAppleRequest()
            req.identityToken = proof.identityToken
            req.nonce = proof.nonce
            req.deviceLabel = UIDevice.current.name
            let resp = try await anon.account.signInWithApple(req)
            anon.close()
            connecting = false
            await connect(token: resp.token)
        } catch {
            connecting = false
            connectError = describe(error)
        }
    }

    /// Signs in (or, with `create`, signs up) with email and password on the chosen server, then connects.
    func signIn(email: String, password: String, create: Bool) async {
        connecting = true
        connectError = nil
        let h = host.trimmingCharacters(in: .whitespaces)
        guard !h.isEmpty, let p = Int(port) else {
            connecting = false
            connectError = "地址或端口不对"
            return
        }
        do {
            let anon = try API(host: h, port: p, tls: tls, token: "")
            defer { anon.close() }
            let token: String
            if create {
                var r = Noted_V1_SignUpRequest()
                r.email = email; r.password = password; r.deviceLabel = UIDevice.current.name
                token = try await anon.account.signUp(r).token
            } else {
                var r = Noted_V1_SignInRequest()
                r.email = email; r.password = password; r.deviceLabel = UIDevice.current.name
                token = try await anon.account.signIn(r).token
            }
            connecting = false
            await connect(token: token)
        } catch {
            connecting = false
            connectError = describeSignIn(error, signUp: create)
        }
    }

    /// Points the app back at the hosted service.
    func useHostedServer() {
        host = Session.hostedHost; port = "443"; tls = true
    }

    var isHostedServer: Bool { host == Session.hostedHost && port == "443" && tls }

    /// The accent: the 主题 page's choice, else the default blue.
    var accent: Color { theme.accent.map { Color(hex: $0) } ?? Color(hex: 0x1F5BFF) }

    func disconnect() {
        reminders.stopWatching()
        api?.close()
        api = nil
        Keychain.remove("token")
    }
}

/// Sign-in errors in the words the sign-in page needs.
private func describeSignIn(_ error: Error, signUp: Bool) -> String {
    guard let e = error as? RPCError else { return error.localizedDescription }
    switch e.code {
    case .unauthenticated: return "邮箱或密码不对"
    case .alreadyExists: return "这个邮箱已经注册过了"
    case .resourceExhausted: return "尝试次数太多,请 10 分钟后再试"
    case .failedPrecondition: return signUp ? "这台服务器不开放注册" : describe(error)
    case .invalidArgument: return signUp ? "密码至少 8 位" : describe(error)
    default: return describe(error)
    }
}
