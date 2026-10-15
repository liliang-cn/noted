import SwiftUI
import UIKit

/// 接入 AI 助手: pair Claude Code by signing in through the browser (scan or type the code it shows),
/// or mint a token (shown once) with ready-to-paste setup for Claude Code and Codex; and the tokens
/// issued so far with 吊销 (a paired client's token carries its name).
struct AIConnectScreen: View {
    @Environment(AppData.self) private var data
    @Environment(Session.self) private var session
    @Environment(Toaster.self) private var toaster
    @Environment(Router.self) private var router

    /// The token minted on this visit: its secret is never shown again once the page closes.
    @State private var fresh: (secret: String, info: Noted_V1_TokenInfo)?
    @State private var reveal = false
    @State private var naming = false
    @State private var newLabel = "AI 助手"
    @State private var creating = false
    @State private var revoking: Noted_V1_TokenInfo?
    @State private var scanning = false
    @State private var scanned: String?
    @State private var enteringCode = false

    var body: some View {
        List {
            pairSection
            tokenSection
            Section("Claude Code · 用令牌") {
                CodeBlock(text: claudeTokenCommand(shownToken)) { copy(claudeTokenCommand(realToken)) }
            }
            Section("Codex · 写入配置") {
                CodeBlock(text: codexConfig) { copy(codexConfig) }
                CodeBlock(text: codexExport(shownToken)) { copy(codexExport(realToken)) }
            }
            Section("已签发的令牌") {
                if data.tokens.isEmpty {
                    EmptyState(text: "没有令牌")
                } else {
                    ForEach(data.tokens, id: \.id) { t in tokenRow(t) }
                }
            }
        }
        .navigationTitle("接入 AI 助手")
        .navigationBarTitleDisplayMode(.inline)
        .task { await data.loadTokens() }
        .reloadOnReturn { await data.loadTokens() }
        .refreshable { await data.loadTokens() }
        .fullScreenCover(isPresented: $scanning, onDismiss: {
            if let code = scanned { scanned = nil; router.push(.pair(code)) }
        }) {
            PairScanner { scanned = $0 }
        }
        .pairCodeEntry(isPresented: $enteringCode) { router.push(.pair($0)) }
        .alert("生成令牌", isPresented: $naming) {
            TextField("名称", text: $newLabel)
                .accessibilityIdentifier("token-label")
            Button("取消", role: .cancel) {}
            Button("生成") { generate() }
                .accessibilityIdentifier("token-create")
        }
        .alert(revoking.map { "吊销“\($0.label.isEmpty ? "未命名" : $0.label)”？" } ?? "",
               isPresented: Binding(get: { revoking != nil }, set: { if !$0 { revoking = nil } }),
               presenting: revoking) { t in
            Button("取消", role: .cancel) {}
            Button("吊销", role: .destructive) { revoke(t) }
                .accessibilityIdentifier("token-revoke-confirm")
        } message: { t in
            Text(t.current ? "这台设备会立即退出登录" : "使用它的客户端会立即断开")
        }
    }

    // MARK: pairing

    private var pairSection: some View {
        Section("连接 Claude Code") {
            Step(number: 1, text: "在电脑终端运行")
            CodeBlock(text: claudePairCommand) { copy(claudePairCommand) }
            Step(number: 2, text: "在 Claude Code 里输入 /mcp，选 noted → 登录，浏览器会显示配对码")
            HStack(spacing: 10) {
                Button { scanning = true } label: {
                    Label("扫码配对", systemImage: "qrcode.viewfinder")
                }
                .buttonStyle(.wide(.filled, small: true))
                .accessibilityIdentifier("pair-scan")
                Button { enteringCode = true } label: {
                    Label("输入配对码", systemImage: "keyboard")
                }
                .buttonStyle(.wide(.tinted, small: true))
                .accessibilityIdentifier("pair-enter-code")
            }
            .padding(.vertical, 4)
        }
    }

    // MARK: token

    @ViewBuilder
    private var tokenSection: some View {
        if let fresh {
            Section {
                VStack(spacing: 12) {
                    HStack(spacing: 8) {
                        Text(reveal ? fresh.secret : Self.mask(fresh.secret))
                            .font(.system(.body, design: .monospaced))
                            .lineLimit(reveal ? nil : 1)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        Button { reveal.toggle() } label: {
                            Image(systemName: reveal ? "eye.slash" : "eye")
                                .font(.system(size: 18))
                                .foregroundStyle(.tint)
                                .frame(width: 34, height: 34)
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(reveal ? "隐藏令牌" : "显示令牌")
                        .accessibilityIdentifier("token-reveal")
                    }
                    .padding(.leading, 12).padding(.trailing, 6).padding(.vertical, 8)
                    .frame(minHeight: 54)
                    .background(.fillQuiet, in: RoundedRectangle(cornerRadius: 12))

                    HStack(spacing: 10) {
                        Button { copy(fresh.secret) } label: {
                            Label("复制", systemImage: "doc.on.doc")
                        }
                        .buttonStyle(.wide(.tinted, small: true))
                        .accessibilityLabel("复制令牌")
                        .accessibilityIdentifier("token-copy")
                        Button { askLabel() } label: {
                            Label("生成新令牌", systemImage: "key")
                        }
                        .buttonStyle(.wide(.gray, small: true))
                        .accessibilityIdentifier("token-generate")
                    }
                }
                .padding(.vertical, 6)
            } header: {
                Text("用令牌连接")
            } footer: {
                Text("\(fresh.info.label) · \(Fmt.md(fresh.info.createTime.date)) 创建")
            }
        } else {
            Section {
                Button { askLabel() } label: {
                    if creating { ProgressView() } else { Label("生成令牌", systemImage: "key") }
                }
                .buttonStyle(.wide(.gray))
                .disabled(creating)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
                .accessibilityIdentifier("token-generate")
            } header: {
                Text("用令牌连接")
            }
        }
    }

    private func tokenRow(_ t: Noted_V1_TokenInfo) -> some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(t.label.isEmpty ? "未命名" : t.label).lineLimit(1)
                    if t.current { tag("当前设备") }
                    else if t.id == fresh?.info.id { tag("新") }
                }
                Text("\(Fmt.md(t.createTime.date)) 创建 · 最近使用 \(lastUsed(t))")
                    .font(.footnote).foregroundStyle(.secondaryText).monospacedDigit()
            }
            Spacer(minLength: 8)
            Button { revoking = t } label: {
                Text("吊销")
                    .font(.subheadline)
                    .foregroundStyle(.danger)
                    .padding(.horizontal, 12)
                    .frame(height: 30)
                    .background(.fillTrack, in: RoundedRectangle(cornerRadius: 8))
            }
            .buttonStyle(.plain)
            .accessibilityLabel("吊销 \(t.label)")
            .accessibilityIdentifier("token-revoke")
        }
        .padding(.vertical, 2)
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            Button("吊销") { revoking = t }.tint(.danger)
        }
    }

    private func tag(_ text: String) -> some View {
        Text(text)
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.tint)
            .padding(.horizontal, 7).frame(height: 20)
            .background(.tint.opacity(0.12), in: Capsule())
    }

    // MARK: actions

    private func askLabel() {
        newLabel = "AI 助手"
        naming = true
    }

    private func generate() {
        let label = newLabel.trimmingCharacters(in: .whitespacesAndNewlines)
        creating = true
        Task {
            defer { creating = false }
            guard let made = await data.createToken(label: label.isEmpty ? "AI 助手" : label) else { return }
            fresh = made
            reveal = true
            toaster.show("已生成令牌", systemImage: "key")
        }
    }

    private func revoke(_ t: Noted_V1_TokenInfo) {
        Task {
            guard await data.revokeToken(t.id) else { return }
            if fresh?.info.id == t.id { fresh = nil; reveal = false }
            toaster.show("已吊销", systemImage: "key")
            if t.current { data.signOut() }
        }
    }

    private func copy(_ text: String) {
        UIPasteboard.general.string = text
        toaster.show("已复制", systemImage: "doc.on.doc")
    }

    // MARK: text

    private var realToken: String { fresh?.secret ?? "<令牌>" }
    private var shownToken: String {
        guard let fresh else { return "<令牌>" }
        return reveal ? fresh.secret : Self.mask(fresh.secret)
    }

    /// https://host/mcp behind TLS (the reverse proxy serves MCP on the same name), else http.
    private var mcpURL: String { "\(session.tls ? "https" : "http")://\(session.host)/mcp" }

    private var claudePairCommand: String { "claude mcp add --scope user --transport http noted \(mcpURL)" }

    private func claudeTokenCommand(_ token: String) -> String {
        "claude mcp add --scope user --transport http noted \(mcpURL) --header \"Authorization: Bearer \(token)\""
    }

    private var codexConfig: String {
        """
        # ~/.codex/config.toml
        [mcp_servers.noted]
        url = "\(mcpURL)"
        bearer_token_env_var = "NOTED_TOKEN"
        """
    }

    private func codexExport(_ token: String) -> String { "export NOTED_TOKEN=\"\(token)\"" }

    static func mask(_ t: String) -> String {
        guard t.count > 10 else { return String(repeating: "•", count: t.count) }
        return "\(t.prefix(6))••••••••••••\(t.suffix(4))"
    }

    private func lastUsed(_ t: Noted_V1_TokenInfo) -> String {
        guard t.hasLastUsedTime else { return "未使用" }
        let d = t.lastUsedTime.date
        let ago = Date.now.timeIntervalSince(d)
        if ago < 120 { return "刚刚" }
        if ago < 3600 { return "\(Int(ago / 60)) 分钟前" }
        return Fmt.stamp(d)
    }
}

/// "① 在电脑终端运行": a numbered step above or below a code block.
private struct Step: View {
    let number: Int
    let text: String

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Text("\(number)")
                .font(.caption.weight(.bold))
                .foregroundStyle(.tint)
                .frame(width: 20, height: 20)
                .background(.tint.opacity(0.12), in: Circle())
            Text(text).font(.subheadline)
                .frame(minHeight: 20)
        }
        .padding(.vertical, 2)
    }
}

/// A monospaced, selectable block with a copy button in its top-right corner.
private struct CodeBlock: View {
    let text: String
    let copy: () -> Void

    var body: some View {
        Text(text)
            .font(.system(size: 13, design: .monospaced))
            .lineSpacing(3)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.trailing, 40)
            .padding(.vertical, 6)
            .overlay(alignment: .topTrailing) {
                Button(action: copy) {
                    Image(systemName: "doc.on.doc")
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(.tint)
                        .frame(width: 34, height: 34)
                        .background(.white, in: RoundedRectangle(cornerRadius: 9))
                        .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(.hairline, lineWidth: 0.5))
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("复制")
            }
            .listRowBackground(Color(hex: 0xF7F7F9))
    }
}
