import SwiftUI

/// To-dos the assistant found in a note. Nothing is added until they accept.
struct ExtractView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let noteID: String
    @State private var proposal: Noted_V1_Proposal?
    @State private var empty = false
    @State private var done = false
    @State private var error: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Text(proposal.map { "笔记里有 \($0.operations.count) 件事" } ?? "正在看这条笔记…")
                        .font(.system(size: 17, weight: .semibold)).foregroundStyle(p.ink)
                    Spacer()
                    Button("不用了") { dismiss() }.font(.system(size: 13, weight: .semibold))
                }
                ErrorLine(text: error)
                if empty { Text("没有找到可以加入的待办").font(.system(size: 14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 60).card() }
                if let pr = proposal {
                    if done {
                        Text("已加入").font(.system(size: 14)).foregroundStyle(p.ok).card(padding: 12)
                    } else {
                        ProposalPanel(proposal: pr, acceptLabel: { "加入 \($0) 项待办" }) { _ in done = true }
                    }
                }
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
        .presentationDetents([.medium, .large])
        .task { await load() }
    }

    private func load() async {
        guard let api = session.api else { return }
        var r = Noted_V1_ExtractTasksRequest(); r.noteID = noteID
        do {
            let pr = try await api.ai.extractTasks(r)
            if pr.operations.isEmpty { empty = true } else { proposal = pr }
        } catch { self.error = describe(error) }
    }
}
