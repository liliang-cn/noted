import SwiftUI

/// A client asking for access (Claude Code after /mcp → 登录): who it is, the code its browser
/// shows, and 允许 / 拒绝. Pushed from 接入 AI 助手, a scanned QR code, or a `noted://pair` link.
struct PairScreen: View {
    let code: String

    @Environment(AppData.self) private var data
    @Environment(Session.self) private var session
    @Environment(Toaster.self) private var toaster
    @Environment(Router.self) private var router

    private enum Phase {
        case loading
        case ready(Noted_V1_Pairing)
        case done(Noted_V1_Pairing)
        /// `gone`: the code is unknown, expired or used, so only another code helps.
        case failed(String, gone: Bool)
    }

    @State private var phase = Phase.loading
    @State private var working = false
    @State private var entering = false

    var body: some View {
        Group {
            switch phase {
            case .loading:
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            case .ready(let p):
                request(p)
            case .done(let p):
                done(p)
            case .failed(let message, let gone):
                failed(message, gone: gone)
            }
        }
        .background(.groupedBackground)
        .navigationTitle("配对")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .pairCodeEntry(isPresented: $entering) { new in
            if new == code { Task { await load() } } else { router.replaceTop(.pair(new)) }
        }
    }

    // MARK: states

    private func request(_ p: Noted_V1_Pairing) -> some View {
        List {
            Section {
                VStack(spacing: 6) {
                    IconTile(systemName: "terminal", tile: .graphite, size: 60)
                        .padding(.bottom, 8)
                    Text(clientName(p))
                        .font(.title.weight(.bold))
                        .multilineTextAlignment(.center)
                        .accessibilityIdentifier("pair-client")
                    Text("想要访问你的 noted").foregroundStyle(.secondaryText)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 8)
                .listRowBackground(Color.clear)
            }
            Section {
                LabeledContent("配对码") {
                    Text(PairCode.display(p.code.isEmpty ? code : p.code))
                        .font(.system(.body, design: .monospaced).weight(.semibold))
                        .foregroundStyle(.primary)
                        .accessibilityIdentifier("pair-code")
                }
                LabeledContent("账号", value: accountName)
                LabeledContent("服务器", value: serverAddress)
                if p.hasExpireTime {
                    LabeledContent("有效期") {
                        TimelineView(.periodic(from: .now, by: 1)) { ctx in
                            Text(Self.left(p.expireTime.date.timeIntervalSince(ctx.date)))
                                .monospacedDigit()
                        }
                    }
                }
            } footer: {
                Text("允许后它可以读写你的数据，可随时在“已签发的令牌”里吊销")
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            BottomBar {
                Button("拒绝") { deny() }
                    .buttonStyle(.wide(.gray))
                    .accessibilityIdentifier("pair-deny")
                Button { approve(p) } label: {
                    if working { ProgressView().tint(.white) } else { Text("允许") }
                }
                .buttonStyle(.wide)
                .accessibilityIdentifier("pair-allow")
            }
            .disabled(working)
        }
        .task(id: p.expireTime) {
            // Past its expiry the server refuses it anyway; say so before the person taps 允许.
            guard p.hasExpireTime else { return }
            let wait = p.expireTime.date.timeIntervalSinceNow
            if wait > 0 { try? await Task.sleep(for: .seconds(wait)) }
            guard !Task.isCancelled, case .ready = phase else { return }
            phase = .failed("配对码已过期，请在电脑上重新登录", gone: true)
        }
    }

    private func done(_ p: Noted_V1_Pairing) -> some View {
        VStack(spacing: 10) {
            Spacer()
            Image(systemName: "checkmark.circle.fill")
                .font(.system(size: 60))
                .foregroundStyle(.success)
                .padding(.bottom, 6)
            Text("已连接，回到电脑上继续")
                .font(.title3.weight(.semibold))
            Text("已允许 \(clientName(p)) 访问你的 noted")
                .font(.subheadline).foregroundStyle(.secondaryText)
            Spacer()
            Button("完成") { router.pop() }
                .buttonStyle(.wide)
                .accessibilityIdentifier("pair-done")
        }
        .multilineTextAlignment(.center)
        .padding(.horizontal, 24).padding(.bottom, 16)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func failed(_ message: String, gone: Bool) -> some View {
        VStack(spacing: 10) {
            Spacer()
            Image(systemName: "exclamationmark.circle")
                .font(.system(size: 54))
                .foregroundStyle(.warning)
                .padding(.bottom, 6)
            Text(message)
                .font(.headline)
                .accessibilityIdentifier("pair-error")
            Text(PairCode.display(code))
                .font(.system(.subheadline, design: .monospaced))
                .foregroundStyle(.secondaryText)
            Spacer()
            if !gone {
                Button("重试") { Task { await load() } }
                    .buttonStyle(.wide)
                    .accessibilityIdentifier("pair-retry")
            }
            Button("输入配对码") { entering = true }
                .buttonStyle(.wide(gone ? .filled : .gray))
                .accessibilityIdentifier("pair-enter-code")
        }
        .multilineTextAlignment(.center)
        .padding(.horizontal, 24).padding(.bottom, 16)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: actions

    private func load() async {
        phase = .loading
        switch await data.getPairing(code) {
        case .success(let p): phase = p.approved ? .done(p) : .ready(p)
        case .failure(let e): phase = .failed(e.message, gone: e.gone)
        }
        if data.account == nil { await data.loadAccount() }
    }

    private func approve(_ p: Noted_V1_Pairing) {
        working = true
        Task {
            defer { working = false }
            switch await data.approvePairing(code) {
            case .success(let got):
                phase = .done(got.code.isEmpty ? p : got)
                toaster.show("已连接 \(clientName(p))", systemImage: "checkmark")
            case .failure(let e):
                if e.gone { phase = .failed(e.message, gone: true) } else { toaster.error(e.message) }
            }
        }
    }

    private func deny() {
        working = true
        Task {
            defer { working = false }
            if let e = await data.denyPairing(code) {
                toaster.error(e.message)
            } else {
                toaster.show("已拒绝", systemImage: "xmark")
                router.pop()
            }
        }
    }

    // MARK: text

    private func clientName(_ p: Noted_V1_Pairing) -> String {
        p.clientName.isEmpty ? "未知客户端" : p.clientName
    }

    private var accountName: String {
        guard let a = data.account else { return "—" }
        return a.userName.isEmpty ? (a.email.isEmpty ? "—" : a.email) : a.userName
    }

    private var serverAddress: String {
        session.tls && session.port == "443" ? session.host : "\(session.host):\(session.port)"
    }

    /// "04:59 后过期", "已过期".
    static func left(_ seconds: TimeInterval) -> String {
        guard seconds > 0 else { return "已过期" }
        let s = Int(seconds.rounded(.up))
        return String(format: "%02d:%02d 后过期", s / 60, s % 60)
    }
}

extension View {
    /// The 输入配对码 alert: one field that takes "K7QM-29XD", "k7qm29xd" or "K7QM 29XD", and
    /// hands the normalised code to `onCode`.
    func pairCodeEntry(isPresented: Binding<Bool>, onCode: @escaping (String) -> Void) -> some View {
        modifier(PairCodeEntry(isPresented: isPresented, onCode: onCode))
    }
}

private struct PairCodeEntry: ViewModifier {
    @Binding var isPresented: Bool
    let onCode: (String) -> Void
    @Environment(Toaster.self) private var toaster
    @State private var text = ""

    func body(content: Content) -> some View {
        content
            .alert("输入配对码", isPresented: $isPresented) {
                TextField("K7QM-29XD", text: $text)
                    .textInputAutocapitalization(.characters)
                    .autocorrectionDisabled()
                    .accessibilityIdentifier("pair-code-field")
                Button("取消", role: .cancel) { text = "" }
                Button("继续") {
                    let typed = text
                    text = ""
                    if let code = PairCode.normalize(typed) { onCode(code) } else { toaster.error("配对码不对，再看一下电脑上显示的") }
                }
                .accessibilityIdentifier("pair-code-submit")
            } message: {
                Text("浏览器里显示的 8 位配对码")
            }
    }
}
