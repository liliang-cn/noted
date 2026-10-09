import SwiftUI

/// A staged change from the assistant: nothing in it has been done yet. The
/// person picks what to keep, fills what the assistant could not know, then accepts.
struct ProposalPanel: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    let proposal: Noted_V1_Proposal
    var acceptLabel: (Int) -> String = { "采纳所选 \($0) 条" }
    var onDone: (Noted_V1_Change) -> Void

    @State private var picked: Set<Int>
    @State private var dates: [String: Date] = [:]
    @State private var chosen: Set<String> = []
    @State private var error: String?
    @State private var busy = false

    init(proposal: Noted_V1_Proposal, acceptLabel: ((Int) -> String)? = nil, onDone: @escaping (Noted_V1_Change) -> Void) {
        self.proposal = proposal
        if let acceptLabel { self.acceptLabel = acceptLabel }
        self.onDone = onDone
        _picked = State(initialValue: Set(proposal.operations.indices))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Eyebrow(text: "拟执行 · 还没有写入")
                Spacer()
                Text("\(proposal.operations.count) 步").font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2)
            }
            if !proposal.inputs.isEmpty {
                VStack(spacing: 0) {
                    ForEach(Array(proposal.inputs.enumerated()), id: \.element.name) { i, input in
                        if i > 0 { Divider().overlay(p.line) }
                        HStack {
                            Text(input.label).font(.system(size: 14)).foregroundStyle(p.ink2)
                            Spacer()
                            if input.type == "date" {
                                if chosen.contains(input.name) {
                                    DatePicker("", selection: dateBinding(input.name), displayedComponents: .date)
                                        .labelsHidden().environment(\.locale, Fmt.zh)
                                } else {
                                    Button { chosen.insert(input.name) } label: {
                                        Text("请选一个").font(.system(size: 14, weight: .medium)).foregroundStyle(p.ink3)
                                    }
                                }
                            }
                            if input.required && !chosen.contains(input.name) {
                                Text("必填").font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
                                    .background(Color(hex: 0xF8EBCB), in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(p.warn)
                            }
                        }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                    }
                }
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
            }
            VStack(spacing: 0) {
                ForEach(Array(proposal.operations.enumerated()), id: \.offset) { i, op in
                    if i > 0 { Divider().overlay(p.line) }
                    Button { if picked.contains(i) { picked.remove(i) } else { picked.insert(i) } } label: {
                        HStack(spacing: 12) {
                            Image(systemName: picked.contains(i) ? "checkmark.square.fill" : "square")
                                .font(.system(size: 20)).foregroundStyle(picked.contains(i) ? p.ok : p.ink3)
                            Text(op.label).font(.system(size: 15, weight: .medium)).foregroundStyle(p.ink).multilineTextAlignment(.leading)
                            Spacer()
                        }.padding(.horizontal, 14).frame(minHeight: p.rowHeight).contentShape(Rectangle())
                    }.buttonStyle(.plain)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
            ErrorLine(text: error)
            Button { Task { await accept() } } label: {
                Text(busy ? "写入中…" : acceptLabel(picked.count)).font(.system(size: 16, weight: .semibold)).foregroundStyle(.white)
                    .frame(maxWidth: .infinity).frame(height: 48)
                    .background(session.mode.accent(p), in: RoundedRectangle(cornerRadius: 12))
            }
            .disabled(picked.isEmpty || busy || missingRequired).opacity(picked.isEmpty || missingRequired ? 0.5 : 1)
        }
    }

    private var missingRequired: Bool {
        proposal.inputs.contains { $0.required && !chosen.contains($0.name) }
    }

    private func dateBinding(_ name: String) -> Binding<Date> {
        Binding(get: { dates[name] ?? Date().addingTimeInterval(14 * 86400) }, set: { dates[name] = $0 })
    }

    private func accept() async {
        guard let api = session.api else { return }
        busy = true
        defer { busy = false }
        var r = Noted_V1_AcceptProposalRequest()
        r.id = proposal.id
        for input in proposal.inputs where chosen.contains(input.name) {
            r.inputs[input.name] = Fmt.ymd(dates[input.name] ?? Date().addingTimeInterval(14 * 86400))
        }
        if picked.count < proposal.operations.count { r.selection.indexes = picked.sorted().map { Int32($0) } }
        do { onDone(try await api.suggest.acceptProposal(r).change) } catch { self.error = describe(error) }
    }
}
