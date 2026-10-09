import SwiftUI

struct ConnectView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @State private var token = ""

    var body: some View {
        @Bindable var s = session
        VStack(alignment: .leading, spacing: 18) {
            Spacer()
            Eyebrow(text: "NOTED")
            Text("连接你的服务器").font(.system(size: 30, weight: .bold)).foregroundStyle(p.ink)
            Text("地址和令牌来自 `noted user add`。").font(.system(size: 14)).foregroundStyle(p.ink2)

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

            if let e = session.connectError {
                Text(e).font(.system(size: 13)).foregroundStyle(p.bad)
            }

            Button {
                Task { await session.connect(token: token.trimmingCharacters(in: .whitespacesAndNewlines)) }
            } label: {
                Text(session.connecting ? "连接中…" : "连接")
                    .font(.system(size: 16, weight: .semibold)).frame(maxWidth: .infinity).frame(height: 50)
                    .background(Mode.all.accent(p), in: RoundedRectangle(cornerRadius: 10))
                    .foregroundStyle(.white)
            }
            .disabled(session.connecting || token.isEmpty || session.host.isEmpty)
            Spacer()
        }
        .padding(20)
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
