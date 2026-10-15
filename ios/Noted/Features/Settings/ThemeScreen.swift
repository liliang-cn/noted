import SwiftUI

/// 主题: the accent color, saved to the "ui.theme" preference so every device shows it.
struct ThemeScreen: View {
    @Environment(Session.self) private var session
    @State private var demoOn = true

    private var selected: AccentChoice {
        AccentChoice(rawValue: session.theme.accent ?? AccentChoice.blue.rawValue) ?? .blue
    }

    var body: some View {
        List {
            Section("强调色") {
                ForEach(AccentChoice.allCases) { c in
                    Button { pick(c) } label: {
                        HStack(spacing: 14) {
                            Circle().fill(c.color).frame(width: 22, height: 22)
                            Text(c.label).foregroundStyle(.primary)
                            Spacer()
                            if c == selected {
                                Image(systemName: "checkmark").font(.body.weight(.semibold)).foregroundStyle(.tint)
                            }
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(c.label)
                    .accessibilityAddTraits(c == selected ? .isSelected : [])
                    .accessibilityIdentifier("accent-\(c.label)")
                }
            }
            Section("预览") {
                HStack(spacing: 12) {
                    Button("按钮") {}
                        .buttonStyle(.wide(.filled, small: true))
                        .accessibilityLabel("示例按钮")
                    Image(systemName: "checkmark")
                        .font(.system(size: 12, weight: .bold))
                        .foregroundStyle(.white)
                        .frame(width: 24, height: 24)
                        .background(Circle().fill(.tint))
                        .accessibilityHidden(true)
                    Toggle("示例开关", isOn: $demoOn).labelsHidden().tint(.switchOn)
                }
                .padding(.vertical, 6)
            }
        }
        .navigationTitle("主题")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func pick(_ c: AccentChoice) {
        var t = session.theme
        // Only differences from the default are stored.
        t.accent = c == .blue ? nil : c.rawValue
        withAnimation(.snappy) { session.saveTheme(t) }
    }
}
