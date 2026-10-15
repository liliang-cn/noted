import SwiftUI

// Navigation-bar pieces shared by the tab roots:
//
//   .toolbar {
//       ToolbarItem(placement: .topBarTrailing) { SpaceFilterMenu() }
//       ToolbarItem(placement: .topBarTrailing) { SettingsAvatarButton() }
//   }
//   .navigationTitle("今天").largeSubtitle("10月10日 周六")

/// 全部 / 个人 / 工作, bound to Session.mode. Shows a filter icon for 全部 and "● 个人" when filtered.
struct SpaceFilterMenu: View {
    @Environment(Session.self) private var session

    var body: some View {
        Menu {
            Picker("空间", selection: Binding(get: { session.mode }, set: { session.setMode($0) })) {
                Text("全部").tag(Mode.all)
                Section {
                    Label { Text("个人") } icon: { Image(uiImage: .dot(Noted_V1_Space.life.tint)) }.tag(Mode.life)
                    Label { Text("工作") } icon: { Image(uiImage: .dot(Noted_V1_Space.work.tint)) }.tag(Mode.work)
                }
            }
            .pickerStyle(.inline)
        } label: {
            switch session.mode {
            case .all:
                Image(systemName: "line.3.horizontal.decrease.circle")
                    .accessibilityLabel("空间")
            case .life, .work:
                HStack(spacing: 5) {
                    SpaceDot(space: session.mode.space)
                    Text(session.mode.label).font(.subheadline.weight(.medium)).foregroundStyle(.primary)
                }
                .accessibilityElement(children: .combine)
                .accessibilityLabel("空间 \(session.mode.label)")
            }
        }
        .accessibilityIdentifier("space-filter")
    }
}

/// The initials avatar that opens 设置.
struct SettingsAvatarButton: View {
    @Environment(Router.self) private var router
    @Environment(AppData.self) private var data

    var body: some View {
        Button { router.push(.settings) } label: {
            Avatar(name: data.account?.userName ?? "")
        }
        .buttonStyle(.plain)
        .accessibilityLabel("设置")
        .accessibilityIdentifier("settings")
    }
}

extension View {
    /// The gray line under a large title ("10月10日 周六"). iOS 26 shows it in the bar; earlier
    /// systems ignore it (put the text in the first section instead if it matters there).
    @ViewBuilder
    func largeSubtitle(_ text: String) -> some View {
        if #available(iOS 26.0, *) {
            navigationSubtitle(text)
        } else {
            self
        }
    }
}
