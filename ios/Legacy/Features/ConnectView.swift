import SwiftUI

struct ConnectView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @State private var token = ""
    @State private var advanced = ProcessInfo.processInfo.environment["NOTED_RESET"] == "1"

    var body: some View {
        @Bindable var s = session
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                Spacer(minLength: 60)
                Eyebrow(text: "NOTED")
                Text(advanced ? "连接你的服务器" : "欢迎使用 Noted").font(.ui(30, .bold)).foregroundStyle(p.ink)
                Text(advanced ? "地址和令牌来自 `noted user add`。" : "笔记、日历、目标和定投,在所有设备上同步。")
                    .font(.ui(14)).foregroundStyle(p.ink2)

                if !advanced {
                    AppleButton(type: .signIn) { result in
                        switch result {
                        case .success(let proof): Task { await session.signInWithApple(proof) }
                        case .failure(let e): if !e.isAppleCancel { session.connectError = e.localizedDescription }
                        }
                    }
                    .frame(height: 52)
                    .disabled(session.connecting)
                    .accessibilityIdentifier("apple-sign-in")
                    if session.connecting { ProgressView().frame(maxWidth: .infinity) }
                }

                if advanced {
                    VStack(spacing: 0) {
                        field("地址", text: $s.host, prompt: "192.168.1.10")
                        Divider().overlay(p.line)
                        field("端口", text: $s.port, prompt: "43872", keyboard: .numberPad)
                        Divider().overlay(p.line)
                        HStack {
                            Text("TLS").foregroundStyle(p.ink)
                            Spacer()
                            Toggle("", isOn: $s.tls).labelsHidden()
                        }.padding(.horizontal, 14).frame(height: p.rowHeight)
                        Divider().overlay(p.line)
                        HStack {
                            Text("令牌").foregroundStyle(p.ink).frame(width: 56, alignment: .leading)
                            SecureField("noted_…", text: $token).textInputAutocapitalization(.never).autocorrectionDisabled()
                        }.padding(.horizontal, 14).frame(height: p.rowHeight)
                    }
                    .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                    .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))
                }

                if let e = session.connectError {
                    Text(e).font(.ui(13)).foregroundStyle(p.bad)
                }

                if advanced {
                    Button {
                        Task { await session.connect(token: token.trimmingCharacters(in: .whitespacesAndNewlines)) }
                    } label: {
                        Text(session.connecting ? "连接中…" : "连接")
                            .font(.ui(16, .semibold)).frame(maxWidth: .infinity).frame(height: 50)
                            .background(Mode.all.accent(p), in: RoundedRectangle(cornerRadius: 10))
                            .foregroundStyle(.white)
                    }
                    .disabled(session.connecting || token.isEmpty || session.host.isEmpty)
                }

                Button { withAnimation { advanced.toggle() } } label: {
                    Text(advanced ? "返回 Apple 登录" : "用令牌或自己的服务器连接")
                        .font(.ui(13)).foregroundStyle(p.ink2)
                        .frame(maxWidth: .infinity).frame(height: 36)
                }
                .accessibilityIdentifier("advanced-toggle")
                Spacer(minLength: 20)
            }
            .padding(20)
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(p.bg.ignoresSafeArea())
    }

    private func field(_ label: String, text: Binding<String>, prompt: String, keyboard: UIKeyboardType = .URL) -> some View {
        HStack {
            Text(label).foregroundStyle(p.ink).frame(width: 56, alignment: .leading)
            TextField(prompt, text: text).keyboardType(keyboard)
                .textInputAutocapitalization(.never).autocorrectionDisabled()
        }.padding(.horizontal, 14).frame(height: p.rowHeight)
    }
}
