import SwiftUI

/// Pro: choose which blocks the overview shows, and their order.
struct ModulesView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var layout = Layout()
    @State private var mode: Mode = .all

    var body: some View {
        let mods = layout.modules(for: mode)
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Button("取消") { dismiss() }
                Spacer()
                Text("页面模块").font(.system(size: 16, weight: .semibold))
                Spacer()
                Button("完成") { session.saveLayout(layout); dismiss() }.fontWeight(.semibold)
            }
            Eyebrow(text: "正在编辑 · 概览页")
            HStack(spacing: 4) {
                ForEach(Mode.allCases) { m in
                    Button { mode = m } label: {
                        Text(m.label).font(.system(size: 14, weight: .semibold)).frame(maxWidth: .infinity).frame(height: 38)
                            .foregroundStyle(mode == m ? .white : p.ink2)
                            .background(mode == m ? m.accent(p) : .clear, in: RoundedRectangle(cornerRadius: 9))
                    }
                }
            }
            .padding(4).background(p.surface, in: RoundedRectangle(cornerRadius: 12)).overlay(RoundedRectangle(cornerRadius: 12).stroke(p.line))
            HStack {
                Text("三种模式各用各的布局").font(.system(size: 14)).foregroundStyle(p.ink)
                Spacer()
                Toggle("", isOn: $layout.perMode).labelsHidden().accessibilityLabel("三种模式各用各的布局")
            }.padding(.horizontal, 14).frame(minHeight: p.rowHeight).tapsToToggle($layout.perMode).card(padding: 0)
            Eyebrow(text: "拖动排序 · 关掉的不显示")
            List {
                ForEach(Array(mods.enumerated()), id: \.element.id) { _, m in
                    let info = Layout.catalog.first { $0.id == m.id }
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(info?.title ?? m.id).font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink)
                            Text(info?.detail ?? "").font(.system(size: 12)).foregroundStyle(p.ink2)
                        }
                        Spacer()
                        Toggle("", isOn: Binding(get: { m.on }, set: { v in
                            var next = layout.modules(for: mode)
                            if let i = next.firstIndex(where: { $0.id == m.id }) { next[i].on = v }
                            layout.set(next, for: mode)
                        })).labelsHidden().accessibilityLabel(info?.title ?? m.id)
                    }
                    .listRowBackground(p.surface)
                }
                .onMove { from, to in
                    var next = layout.modules(for: mode)
                    next.move(fromOffsets: from, toOffset: to)
                    layout.set(next, for: mode)
                }
            }
            .listStyle(.plain).scrollContentBackground(.hidden)
            .environment(\.editMode, .constant(.active))
            .clipShape(RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
            Button("还原默认") { layout.set(Layout.standard, for: mode) }
                .font(.system(size: 14, weight: .semibold)).foregroundStyle(p.ink2)
        }
        .padding(16)
        .background(p.bg.ignoresSafeArea())
        .onAppear { layout = session.layout }
    }
}
