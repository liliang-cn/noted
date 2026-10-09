import SwiftUI

struct SuggestionsView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var proposals: [Noted_V1_Proposal] = []
    @State private var error: String?
    @State private var loaded = false
    @State private var undo: Noted_V1_Change?
    @State private var inputs: [String: [String: Date]] = [:]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Button { dismiss() } label: { Label("概览", systemImage: "chevron.left").font(.system(size: 14, weight: .semibold)) }
                    Spacer()
                }
                VStack(alignment: .leading, spacing: 6) {
                    Text("建议").font(.system(size: 30, weight: .bold)).foregroundStyle(p.ink)
                    Text("\(proposals.count) 条 · 你不点,就不会执行").font(.system(size: 13)).foregroundStyle(p.ink2)
                }
                ErrorLine(text: error)
                if let u = undo {
                    HStack {
                        Text(u.summary).font(.system(size: 13)).foregroundStyle(p.ink).lineLimit(2)
                        Spacer()
                        Button("撤销") { Task { await undoChange(u) } }.font(.system(size: 13, weight: .semibold))
                    }.card(padding: 12)
                }
                if loaded && proposals.isEmpty {
                    Text("现在没有建议").font(.system(size: 14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 80).card()
                }
                ForEach(proposals, id: \.id) { pr in card(pr) }
            }
            .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 24)
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
    }

    private func kindLabel(_ k: String) -> String {
        switch k {
        case "goal_slot": "目标"
        case "project_dates": "项目"
        case "conflict": "冲突"
        case "overdue": "逾期"
        case "schedule": "排期"
        case "weekly": "每周"
        case "extract": "提取"
        default: "建议"
        }
    }

    private func card(_ pr: Noted_V1_Proposal) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                SpaceTag(space: pr.space)
                Spacer()
                Text(kindLabel(pr.kind)).font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
                    .background(pr.kind == "conflict" || pr.kind == "overdue" ? p.badBg : p.surface2, in: RoundedRectangle(cornerRadius: 6))
                    .foregroundStyle(pr.kind == "conflict" || pr.kind == "overdue" ? p.bad : p.ink2)
            }
            Text(pr.title).font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink).accessibilityIdentifier("proposal-title")
            if !pr.reason.isEmpty { Text(pr.reason).font(.system(size: 13)).foregroundStyle(p.ink2) }
            ForEach(Array(pr.operations.enumerated()), id: \.offset) { _, op in
                Text("· \(op.label)").font(.system(size: 13)).foregroundStyle(p.ink2)
            }
            ForEach(pr.inputs, id: \.name) { input in
                if input.type == "date" {
                    DatePicker(input.label, selection: dateBinding(pr.id, input.name), displayedComponents: .date)
                        .environment(\.locale, Fmt.zh).font(.system(size: 14))
                }
            }
            HStack(spacing: 8) {
                Button { Task { await accept(pr) } } label: {
                    Text("采纳").font(.system(size: 13, weight: .semibold)).padding(.horizontal, 14).frame(height: 34)
                        .background(session.mode.accent(p), in: RoundedRectangle(cornerRadius: 8)).foregroundStyle(.white)
                }
                Button { Task { await dismissProposal(pr) } } label: {
                    Text("忽略").font(.system(size: 13, weight: .semibold)).padding(.horizontal, 14).frame(height: 34)
                        .background(p.surface2, in: RoundedRectangle(cornerRadius: 8)).foregroundStyle(p.ink)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }

    private func dateBinding(_ id: String, _ name: String) -> Binding<Date> {
        Binding(get: { inputs[id]?[name] ?? Date().addingTimeInterval(7 * 86400) },
                set: { inputs[id, default: [:]][name] = $0 })
    }

    private func load() async {
        guard let api = session.api else { return }
        var r = Noted_V1_ListProposalsRequest(); r.space = session.mode.space
        do { proposals = try await api.suggest.listProposals(r).proposals; error = nil } catch { self.error = describe(error) }
        loaded = true
    }

    private func accept(_ pr: Noted_V1_Proposal) async {
        guard let api = session.api else { return }
        var r = Noted_V1_AcceptProposalRequest()
        r.id = pr.id
        for input in pr.inputs where input.type == "date" {
            let d = inputs[pr.id]?[input.name] ?? Date().addingTimeInterval(7 * 86400)
            r.inputs[input.name] = Fmt.ymd(d)
        }
        do {
            undo = try await api.suggest.acceptProposal(r).change
            await load()
        } catch { self.error = describe(error) }
    }

    private func dismissProposal(_ pr: Noted_V1_Proposal) async {
        var r = Noted_V1_DismissProposalRequest(); r.id = pr.id
        _ = try? await session.api?.suggest.dismissProposal(r)
        await load()
    }

    private func undoChange(_ c: Noted_V1_Change) async {
        var r = Noted_V1_UndoChangeRequest(); r.id = c.id
        _ = try? await session.api?.suggest.undoChange(r)
        undo = nil
        await load()
    }
}
