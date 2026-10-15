import SwiftUI

/// Before connecting: Sign in with Apple, email + password, or one's own server with a token.
/// Apple and email sign in on the server set below (the hosted one unless changed).
struct SignInView: View {
    @Environment(Session.self) private var session
    @State private var path: [Step] = []

    enum Step: Hashable { case email, server }

    var body: some View {
        NavigationStack(path: $path) {
            VStack(spacing: 0) {
                Spacer()
                VStack(spacing: 14) {
                    Image(systemName: "note.text")
                        .font(.system(size: 34, weight: .medium))
                        .foregroundStyle(.white)
                        .frame(width: 72, height: 72)
                        .background(.tint, in: RoundedRectangle(cornerRadius: 17))
                    Text("Noted").font(.largeTitle.weight(.bold))
                }
                Spacer()
                VStack(spacing: 12) {
                    if let e = session.connectError, path.isEmpty {
                        Text(signInMessage(e)).font(.footnote).foregroundStyle(.danger).multilineTextAlignment(.center)
                            .accessibilityIdentifier("connect-error")
                    }
                    AppleButton(type: .signIn) { result in
                        switch result {
                        case .success(let proof): Task { await session.signInWithApple(proof) }
                        case .failure(let e): if !e.isAppleCancel { session.connectError = "Apple 登录没有完成,请重试" }
                        }
                    }
                    .frame(height: 50)
                    .clipShape(RoundedRectangle(cornerRadius: 14))
                    .disabled(session.connecting)
                    .accessibilityLabel("使用 Apple 登录")
                    .accessibilityIdentifier("apple-sign-in")

                    Button { session.connectError = nil; path.append(.email) } label: {
                        Label("邮箱登录 / 注册", systemImage: "envelope.fill")
                    }
                    .buttonStyle(.wide(.gray))
                    .disabled(session.connecting)
                    .accessibilityIdentifier("email-sign-in")

                    Button { session.connectError = nil; path.append(.server) } label: {
                        Label("连接自己的服务器", systemImage: "server.rack")
                    }
                    .font(.subheadline)
                    .padding(.top, 4)
                    .disabled(session.connecting)
                    .accessibilityIdentifier("own-server")

                    if !session.isHostedServer {
                        HStack(spacing: 6) {
                            Text("服务器 \(session.host):\(session.port)").monospacedDigit()
                            Button("恢复默认") { session.useHostedServer() }
                                .accessibilityLabel("恢复默认服务器")
                        }
                        .font(.footnote).foregroundStyle(.secondaryText)
                    }
                    if session.connecting { ProgressView().padding(.top, 4) }
                }
                .padding(.horizontal, 24)
                .padding(.bottom, 28)
            }
            .frame(maxWidth: .infinity)
            .background(Color.groupedBackground.ignoresSafeArea())
            .navigationDestination(for: Step.self) { step in
                switch step {
                case .email: EmailSignInView()
                case .server: ServerConnectView()
                }
            }
        }
    }
}

/// 邮箱登录 / 注册.
struct EmailSignInView: View {
    @Environment(Session.self) private var session
    @State private var signUp = false
    @State private var email = ""
    @State private var password = ""
    @FocusState private var focus: Field?

    enum Field { case email, password }

    private var ready: Bool { email.contains("@") && password.count >= (signUp ? 8 : 1) && !session.connecting }

    /// What is still missing, once something has been typed.
    private var hint: String? {
        let e = email.trimmingCharacters(in: .whitespaces)
        if !e.isEmpty && !e.contains("@") { return "邮箱格式不对" }
        if signUp && !password.isEmpty && password.count < 8 { return "密码至少 8 位" }
        return nil
    }

    var body: some View {
        List {
            Section {
                Picker("登录或注册", selection: $signUp) {
                    Text("登录").tag(false)
                    Text("注册").tag(true)
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            }
            Section {
                TextField("邮箱", text: $email)
                    .keyboardType(.emailAddress)
                    .textContentType(.username)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .focused($focus, equals: .email)
                    .submitLabel(.next)
                    .onSubmit { focus = .password }
                    .accessibilityIdentifier("email")
                SecureField(signUp ? "密码（至少 8 位）" : "密码", text: $password)
                    .textContentType(signUp ? .newPassword : .password)
                    .focused($focus, equals: .password)
                    .submitLabel(.go)
                    .onSubmit { if ready { submit() } }
                    .accessibilityIdentifier("password")
            } footer: {
                if let e = session.connectError {
                    Text(signInMessage(e)).foregroundStyle(.danger).accessibilityIdentifier("connect-error")
                } else if let hint {
                    Text(hint).foregroundStyle(.secondaryText)
                }
            }
            Section {
                Button(action: submit) {
                    if session.connecting { ProgressView().tint(.white) } else { Text(signUp ? "注册" : "登录") }
                }
                .buttonStyle(.wide)
                .disabled(!ready)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
                .accessibilityIdentifier("email-submit")
            }
        }
        .navigationTitle(signUp ? "注册" : "邮箱登录")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear { focus = .email }
        .onChange(of: signUp) { session.connectError = nil }
    }

    private func submit() {
        focus = nil
        let e = email.trimmingCharacters(in: .whitespaces)
        Task { await session.signIn(email: e, password: password, create: signUp) }
    }
}

/// 连接自己的服务器: address, port, TLS and an API token.
struct ServerConnectView: View {
    @Environment(Session.self) private var session
    @State private var token = ""

    var body: some View {
        @Bindable var session = session
        List {
            Section("服务器") {
                LabeledContent("地址") {
                    TextField("noted.example.com", text: $session.host)
                        .multilineTextAlignment(.trailing)
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .accessibilityIdentifier("host")
                }
                LabeledContent("端口") {
                    TextField("43872", text: $session.port)
                        .multilineTextAlignment(.trailing)
                        .keyboardType(.numberPad)
                        .monospacedDigit()
                        .accessibilityIdentifier("port")
                }
                Toggle("TLS", isOn: $session.tls).tint(.switchOn)
                    .accessibilityIdentifier("tls")
            }
            Section {
                SecureField("noted_…", text: $token)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .accessibilityLabel("令牌")
                    .accessibilityIdentifier("token")
            } header: {
                Text("令牌")
            } footer: {
                if let e = session.connectError {
                    Text(signInMessage(e)).foregroundStyle(.danger).accessibilityIdentifier("connect-error")
                } else if !session.port.isEmpty && Int(session.port) == nil {
                    Text("端口只能是数字").foregroundStyle(.danger)
                }
            }
            Section {
                Button {
                    Task { await session.connect(token: token.trimmingCharacters(in: .whitespacesAndNewlines)) }
                } label: {
                    if session.connecting { ProgressView().tint(.white) } else { Text("连接") }
                }
                .buttonStyle(.wide)
                .disabled(token.isEmpty || session.host.isEmpty || Int(session.port) == nil || session.connecting)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
                .accessibilityIdentifier("connect")
            }
            if !session.isHostedServer {
                Section {
                    Button("恢复默认服务器") { session.useHostedServer() }
                }
            }
        }
        .navigationTitle("自己的服务器")
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// Sign-in errors always read in Chinese: the server's own (English) messages get a Chinese lead.
private func signInMessage(_ e: String) -> String {
    let chinese = e.unicodeScalars.contains { (0x4E00...0x9FFF).contains($0.value) }
    return chinese ? e : "出错了:\(e)"
}
