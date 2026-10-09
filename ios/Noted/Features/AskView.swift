import SwiftUI

struct AskView: View {
    enum Kind: String, CaseIterable, Identifiable {
        case ask = "提问", plan = "拆成项目"
        var id: String { rawValue }
    }

    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var kind: Kind = .ask
    @State private var text = ""
    @State private var asked = ""
    @State private var reply: Noted_V1_AskResponse?
    @State private var plan: Noted_V1_Proposal?
    @State private var done: Noted_V1_Change?
    @State private var busy = false
    @State private var error: String?
    @State private var sessionID = ""

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    HStack {
                        Eyebrow(text: kind == .ask ? "提问" : "新内容")
                        Spacer()
                        Button("关闭") { dismiss() }.font(.system(size: 13, weight: .semibold))
                    }
                    Picker("", selection: $kind) { ForEach(Kind.allCases) { Text($0.rawValue).tag($0) } }
                        .pickerStyle(.segmented).onChange(of: kind) { reset() }
                    if !asked.isEmpty {
                        Text(asked).font(.system(size: 15)).foregroundStyle(p.ink).frame(maxWidth: .infinity, alignment: .leading).card()
                    }
                    if busy { Text("正在想…").font(.system(size: 14)).foregroundStyle(p.ink3) }
                    ErrorLine(text: error)
                    if let r = reply { answer(r) }
                    if let pl = plan { planBody(pl) }
                    if let d = done {
                        Text(d.summary.isEmpty ? "已写入" : "已写入 · \(d.summary)").font(.system(size: 14)).foregroundStyle(p.ok).card(padding: 12)
                    }
                }
                .padding(16)
            }
            HStack(spacing: 10) {
                TextField(kind == .ask ? "记点什么,或者问点什么" : "一句话说清要做的事", text: $text, axis: .vertical)
                    .lineLimit(1...3).accessibilityIdentifier("ask-input").submitLabel(.send).onSubmit { Task { await send() } }
                Button { Task { await send() } } label: {
                    Image(systemName: "arrow.up").font(.system(size: 15, weight: .bold)).foregroundStyle(.white)
                        .frame(width: 34, height: 34).background(session.mode.accent(p), in: Circle())
                }.accessibilityLabel("发送").accessibilityIdentifier("ask-send").disabled(text.trimmingCharacters(in: .whitespaces).isEmpty || busy)
            }
            .padding(.horizontal, 14).padding(.vertical, 8)
            .background(p.surface, in: RoundedRectangle(cornerRadius: 18)).overlay(RoundedRectangle(cornerRadius: 18).stroke(p.line))
            .padding(.horizontal, 16).padding(.bottom, 10)
        }
        .background(p.bg.ignoresSafeArea())
        #if DEBUG
        .task {
            let env = ProcessInfo.processInfo.environment
            if let q = env["NOTED_ASK"], !q.isEmpty {
                if env["NOTED_ASKKIND"] == "plan" { kind = .plan }
                text = q
                await send()
            }
        }
        #endif
    }

    @ViewBuilder private func answer(_ r: Noted_V1_AskResponse) -> some View {
        Text(r.reply).font(.system(size: 15)).foregroundStyle(p.ink)
        if !r.references.isEmpty {
            VStack(spacing: 0) {
                ForEach(Array(r.references.enumerated()), id: \.offset) { i, ref in
                    if i > 0 { Divider().overlay(p.line) }
                    HStack(spacing: 12) {
                        Text(label(ref.kind)).font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
                            .background(p.surface2, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(p.ink2)
                        Text(ref.title).font(.system(size: 15, weight: .medium)).foregroundStyle(p.ink)
                        Spacer()
                        if ref.hasTime { Text(Fmt.md(ref.time.date)).font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2) }
                    }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
            HStack(spacing: 8) {
                Eyebrow(text: "依据")
                Text(Set(r.references.map { label($0.kind) }).sorted().joined(separator: " · ")).font(.system(size: 13)).foregroundStyle(p.ink2)
            }
        }
        if r.hasProposal, !r.proposal.operations.isEmpty, done == nil {
            ProposalPanel(proposal: r.proposal) { done = $0 }
        }
    }

    @ViewBuilder private func planBody(_ pl: Noted_V1_Proposal) -> some View {
        if !pl.title.isEmpty { Text(pl.title).font(.system(size: 17, weight: .semibold)).foregroundStyle(p.ink) }
        if !pl.reason.isEmpty { Text(pl.reason).font(.system(size: 13)).foregroundStyle(p.ink2) }
        if done == nil { ProposalPanel(proposal: pl, acceptLabel: { "创建所选 \($0) 项" }) { done = $0 } }
    }

    private func label(_ kind: String) -> String {
        ["event": "日程", "task": "待办", "note": "笔记", "project": "项目", "goal": "目标"][kind] ?? kind
    }

    private func reset() { reply = nil; plan = nil; done = nil; error = nil; asked = "" }

    private func send() async {
        guard let api = session.api else { return }
        let q = text.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return }
        reset()
        asked = q; text = ""; busy = true
        defer { busy = false }
        do {
            if kind == .ask {
                var r = Noted_V1_AskRequest()
                r.message = q; r.sessionID = sessionID; r.space = session.mode.space
                let a = try await api.ai.ask(r)
                sessionID = a.sessionID
                reply = a
            } else {
                var r = Noted_V1_PlanFromTextRequest()
                r.text = q; r.space = session.mode.space
                plan = try await api.ai.planFromText(r)
            }
        } catch { self.error = describe(error) }
    }
}
