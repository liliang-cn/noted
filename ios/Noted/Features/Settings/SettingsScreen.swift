import SwiftUI

/// 设置 (pushed from the avatar on 今天): account, AI access, appearance, reminders, export, Pro, server.
struct SettingsScreen: View {
    @Environment(AppData.self) private var data
    @Environment(Session.self) private var session
    @Environment(Toaster.self) private var toaster

    @State private var ai: AppData.AISettings?
    @State private var aiLoaded = false
    @State private var editingEmail = false
    @State private var confirmUnlink = false
    @State private var confirmSignOut = false

    var body: some View {
        List {
            accountSection
            Section {
                NavigationLink(value: Route.aiConnect) {
                    FieldLabel("接入 AI 助手", icon: "sparkles", tile: .blue,
                               value: data.tokens.isEmpty ? nil : "\(data.tokens.count) 个令牌")
                }
                .accessibilityIdentifier("settings-ai-connect")
            }
            aiSection
            Section {
                NavigationLink(value: Route.theme) {
                    FieldLabel("主题", icon: "paintpalette", tile: .orange, value: accentLabel)
                }
                .accessibilityIdentifier("settings-theme")
                muteToggle("工作模式下静音个人提醒", icon: "bell.slash", tile: .gray, \.lifeInWork)
                muteToggle("个人模式下静音工作提醒", icon: "bell.slash", tile: .graphite, \.workInLife)
                NavigationLink(value: Route.export) {
                    FieldLabel("导出数据", icon: "square.and.arrow.up", tile: .green)
                }
                .accessibilityIdentifier("settings-export")
            }
            Section {
                NavigationLink(value: Route.pro) {
                    FieldLabel("Noted Pro", icon: "star.fill", tile: .rust, value: session.pro.isPro ? "已开通" : nil)
                }
                .accessibilityIdentifier("settings-pro")
            }
            Section("服务器") {
                FieldLabel("地址", icon: "server.rack", tile: .graphite, value: serverAddress)
                FieldLabel("状态", icon: session.tls ? "lock.fill" : "bolt.horizontal.fill", tile: .green,
                           value: session.tls ? "已连接 · TLS" : "已连接")
            }
            Section {
                Button("退出登录", role: .destructive) { confirmSignOut = true }
                    .frame(maxWidth: .infinity)
                    .accessibilityIdentifier("sign-out")
            } footer: {
                Text("Noted \(version)").frame(maxWidth: .infinity).padding(.top, 12)
            }
        }
        .navigationTitle("设置")
        .navigationBarTitleDisplayMode(.large)
        .task { await load() }
        .refreshable { await load() }
        .sheet(isPresented: $editingEmail) { EmailPasswordSheet(currentEmail: data.account?.email ?? "") }
        .confirmationDialog("解除绑定 Apple ID？", isPresented: $confirmUnlink, titleVisibility: .visible) {
            Button("解除绑定", role: .destructive) {
                Task { if await data.unlinkApple() { toaster.show("已解除绑定", systemImage: "apple.logo") } }
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text("之后不能再用这个 Apple ID 登录")
        }
        .confirmationDialog("退出登录？", isPresented: $confirmSignOut, titleVisibility: .visible) {
            Button("退出登录", role: .destructive) { data.signOut() }
            Button("取消", role: .cancel) {}
        }
    }

    // MARK: sections

    @ViewBuilder
    private var accountSection: some View {
        let account = data.account
        Section {
            HStack(spacing: 14) {
                Avatar(name: account?.userName ?? "", size: 58)
                VStack(alignment: .leading, spacing: 2) {
                    Text(account?.userName.isEmpty == false ? account!.userName : "Noted")
                        .font(.title3.weight(.semibold))
                    Text(accountLine).font(.footnote).foregroundStyle(.secondaryText).lineLimit(1)
                }
            }
            .padding(.vertical, 4)
            .accessibilityElement(children: .combine)

            Button { editingEmail = true } label: {
                HStack {
                    FieldLabel(account?.email.isEmpty == false ? "修改密码" : "绑定邮箱",
                               icon: "envelope.fill", tile: .blue,
                               value: account?.email.isEmpty == false ? account!.email : nil)
                    Image(systemName: "chevron.right").font(.system(size: 13, weight: .semibold)).foregroundStyle(.tertiaryText)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("settings-email")

            if let account, account.appleEnabled {
                if account.appleLinked {
                    Button { confirmUnlink = true } label: {
                        HStack {
                            FieldLabel("Apple ID", icon: "apple.logo", tile: .graphite, value: "已绑定")
                            Image(systemName: "chevron.right").font(.system(size: 13, weight: .semibold)).foregroundStyle(.tertiaryText)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Apple ID 已绑定，解除绑定")
                    .accessibilityIdentifier("apple-unlink")
                } else {
                    HStack {
                        FieldLabel("Apple ID", icon: "apple.logo", tile: .graphite)
                        Spacer(minLength: 8)
                        AppleButton(type: .continue) { result in
                            switch result {
                            case .success(let proof):
                                Task { if await data.linkApple(proof) { toaster.show("已绑定 Apple ID", systemImage: "apple.logo") } }
                            case .failure(let e):
                                if !e.isAppleCancel { toaster.error("Apple 登录没有完成") }
                            }
                        }
                        .frame(width: 150, height: 34)
                        .clipShape(RoundedRectangle(cornerRadius: 8))
                        .accessibilityLabel("绑定 Apple ID")
                        .accessibilityIdentifier("apple-link")
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var aiSection: some View {
        Section {
            FieldLabel("服务器智能", icon: "cpu", tile: .teal, value: aiStatusText)
            if let ai, ai.status.enabled {
                aiToggle("读取个人内容", icon: "person.fill", tile: .rust, \.allowLife)
                aiToggle("读取工作内容", icon: "briefcase.fill", tile: .blue, \.allowWork)
                featureToggle("每日简报", icon: "sun.horizon.fill", tile: .orange, \.dailyBriefing)
                featureToggle("建议", icon: "lightbulb.fill", tile: .orange, \.suggestions)
                featureToggle("每周回顾", icon: "calendar.badge.clock", tile: .red, \.weeklyReview)
                featureToggle("笔记摘要与标签", icon: "text.badge.star", tile: .green, \.noteTools)
            }
        } header: {
            Text("智能")
        }
    }

    // MARK: rows

    private func muteToggle(_ title: String, icon: String, tile: TileColor, _ key: WritableKeyPath<MuteRules, Bool>) -> some View {
        Toggle(isOn: Binding(get: { session.mute[keyPath: key] }, set: { v in
            var m = session.mute
            m[keyPath: key] = v
            session.saveMute(m)
            toaster.show(v ? "已开启提醒静音" : "已关闭提醒静音", systemImage: v ? "bell.slash" : "bell")
        })) {
            FieldLabel(title, icon: icon, tile: tile)
        }
        .tint(.switchOn)
        .accessibilityLabel(title)
    }

    private func aiToggle(_ title: String, icon: String, tile: TileColor, _ key: WritableKeyPath<Noted_V1_AIAccess, Bool>) -> some View {
        Toggle(isOn: Binding(get: { ai?.access[keyPath: key] ?? false }, set: { v in
            guard var cur = ai else { return }
            let old = cur.access
            cur.access[keyPath: key] = v
            ai = cur
            Task {
                if let got = await data.setAIAccess(cur.access) { ai?.access = got } else { ai?.access = old }
            }
        })) {
            FieldLabel(title, icon: icon, tile: tile)
        }
        .tint(.switchOn)
        .accessibilityLabel(title)
    }

    private func featureToggle(_ title: String, icon: String, tile: TileColor, _ key: WritableKeyPath<Noted_V1_AIFeatures, Bool>) -> some View {
        Toggle(isOn: Binding(get: { ai?.features[keyPath: key] ?? false }, set: { v in
            guard var cur = ai else { return }
            let old = cur.features
            cur.features[keyPath: key] = v
            ai = cur
            Task {
                if let got = await data.setAIFeatures(cur.features) { ai?.features = got } else { ai?.features = old }
            }
        })) {
            FieldLabel(title, icon: icon, tile: tile)
        }
        .tint(.switchOn)
        .accessibilityLabel(title)
    }

    // MARK: values

    private var accountLine: String {
        guard let a = data.account else { return "" }
        var parts: [String] = []
        if a.appleLinked { parts.append("Apple ID 登录") }
        if !a.email.isEmpty { parts.append(a.email) }
        if parts.isEmpty { parts.append("令牌登录") }
        return parts.joined(separator: " · ")
    }

    private var accentLabel: String {
        AccentChoice(rawValue: session.theme.accent ?? AccentChoice.blue.rawValue)?.label ?? "自定义"
    }

    private var aiStatusText: String {
        guard aiLoaded else { return "…" }
        guard let ai else { return "读取失败" }
        guard ai.status.enabled, ai.status.chat else { return "未开启" }
        return ai.status.model.isEmpty ? "已开启" : ai.status.model
    }

    private var serverAddress: String {
        session.tls && session.port == "443" ? session.host : "\(session.host):\(session.port)"
    }

    private var version: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
    }

    private func load() async {
        async let account: Void = data.loadAccount()
        async let tokens: Void = data.loadTokens()
        async let settings = data.loadAISettings()
        _ = await (account, tokens)
        ai = await settings
        aiLoaded = true
    }
}

/// 绑定邮箱 / 修改密码: one form, saved with SetEmailPassword.
private struct EmailPasswordSheet: View {
    let currentEmail: String
    @Environment(AppData.self) private var data
    @Environment(Toaster.self) private var toaster
    @Environment(\.dismiss) private var dismiss
    @State private var email = ""
    @State private var current = ""
    @State private var password = ""
    @State private var saving = false
    @FocusState private var focus: Field?

    enum Field { case email, current, password }

    private var hasEmail: Bool { !currentEmail.isEmpty }
    private var emailOK: Bool {
        let e = email.trimmingCharacters(in: .whitespaces)
        return e.contains("@") && e.contains(".")
    }
    private var ready: Bool { emailOK && password.count >= 8 && (!hasEmail || !current.isEmpty) && !saving }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextField("邮箱", text: $email)
                        .keyboardType(.emailAddress)
                        .textContentType(.username)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($focus, equals: .email)
                        .accessibilityIdentifier("settings-email-field")
                }
                Section {
                    if hasEmail {
                        SecureField("当前密码", text: $current)
                            .textContentType(.password)
                            .focused($focus, equals: .current)
                            .accessibilityIdentifier("settings-current-password")
                    }
                    SecureField("新密码（至少 8 位）", text: $password)
                        .textContentType(.newPassword)
                        .focused($focus, equals: .password)
                        .accessibilityIdentifier("settings-new-password")
                } footer: {
                    if !password.isEmpty && password.count < 8 {
                        Text("密码至少 8 位").foregroundStyle(.danger)
                    }
                }
            }
            .navigationTitle(hasEmail ? "修改密码" : "绑定邮箱")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存", action: save)
                        .fontWeight(.semibold)
                        .disabled(!ready)
                        .accessibilityIdentifier("settings-email-save")
                }
            }
            .onAppear {
                email = currentEmail
                focus = hasEmail ? .current : .email
            }
        }
        .toastHost()
    }

    private func save() {
        saving = true
        Task {
            let ok = await data.setEmailPassword(email: email.trimmingCharacters(in: .whitespaces),
                                                 password: password, currentPassword: current)
            saving = false
            if ok {
                dismiss()
                toaster.show(hasEmail ? "已修改密码" : "已绑定邮箱", systemImage: "envelope")
            }
        }
    }
}
