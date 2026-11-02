import SwiftUI

/// 罗盘: the card at the top of 目标 (tap to edit) and its editor sheet. One per space.
struct CompassCard: View {
    @Environment(AppData.self) private var data
    @Environment(Session.self) private var session
    @State private var editing = false

    var body: some View {
        let c = data.compass(for: session.mode.space)
        Button { editing = true } label: {
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    Image(systemName: "safari").foregroundStyle(.tint)
                    Text("罗盘").font(.headline)
                    Spacer()
                    Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(.tertiary)
                }
                CompassLine(label: "拒绝的生活", text: c?.antiVision ?? "")
                CompassLine(label: "正在建造", text: c?.vision ?? "")
                CompassLine(label: "我是那种……的人", text: c?.identity ?? "")
                CompassLine(label: "不牺牲", text: (c?.constraints ?? []).joined(separator: " · "))
            }
            .card()
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("compass-card")
        .task(id: session.mode) { await data.loadCompass(session.mode.space) }
        .sheet(isPresented: $editing) {
            CompassEditView(space: session.mode.space)
        }
    }
}

private struct CompassLine: View {
    let label: String
    let text: String
    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label).font(.footnote).foregroundStyle(.secondaryText)
            Text(text.isEmpty ? "—" : text)
                .font(.subheadline)
                .foregroundStyle(text.isEmpty ? .tertiary : .primary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }
}

struct CompassEditView: View {
    @Environment(AppData.self) private var data
    @Environment(\.dismiss) private var dismiss
    let space: Noted_V1_Space

    @State private var antiVision = ""
    @State private var vision = ""
    @State private var identity = ""
    @State private var constraints: [String] = []
    @State private var filled = false
    @State private var saving = false

    var body: some View {
        NavigationStack {
            List {
                Section("拒绝的生活") {
                    TextField("一句话", text: $antiVision, axis: .vertical).accessibilityIdentifier("compass-anti")
                }
                Section("正在建造") {
                    TextField("一句话", text: $vision, axis: .vertical).accessibilityIdentifier("compass-vision")
                }
                Section("我是那种……的人") {
                    TextField("我是那种……的人", text: $identity, axis: .vertical).accessibilityIdentifier("compass-identity")
                }
                Section("不牺牲") {
                    ForEach(constraints.indices, id: \.self) { i in
                        TextField("不牺牲", text: $constraints[i], axis: .vertical)
                    }
                    .onDelete { constraints.remove(atOffsets: $0) }
                    Button { constraints.append("") } label: {
                        Label("添加", systemImage: "plus.circle.fill")
                    }
                    .accessibilityIdentifier("compass-add-constraint")
                }
            }
            .listStyle(.insetGrouped)
            .navigationTitle("罗盘")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") { save() }
                        .disabled(saving)
                        .accessibilityIdentifier("compass-save")
                }
            }
            .onAppear(perform: fill)
        }
    }

    private func fill() {
        guard !filled else { return }
        filled = true
        if let c = data.compass(for: space) {
            antiVision = c.antiVision; vision = c.vision; identity = c.identity; constraints = c.constraints
        }
    }

    private func save() {
        saving = true
        Task {
            let ok = await data.saveCompass(space, antiVision: antiVision, vision: vision, identity: identity,
                                            constraints: constraints.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty })
            saving = false
            if ok { dismiss() }
        }
    }
}
