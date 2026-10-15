import SwiftUI

/// The short confirmation pill at the top of the screen ("已添加待办", "已删除"), and the
/// place errors go. AppData reports every failed call here; screens call `show` after an action.
///
///     @Environment(Toaster.self) private var toaster
///     toaster.show("已完成")
///     toaster.show("已删除", systemImage: "trash")
///
/// The shell hosts it. A sheet is its own presentation, so a sheet that wants to show one on top
/// of itself adds `.toastHost()` to its root view.
@MainActor @Observable
final class Toaster {
    struct Toast: Identifiable, Equatable {
        let id = UUID()
        var text: String
        var systemImage: String
        var isError: Bool
    }

    private(set) var current: Toast?
    private var hide: Task<Void, Never>?

    func show(_ text: String, systemImage: String = "checkmark", isError: Bool = false) {
        let t = Toast(text: text, systemImage: systemImage, isError: isError)
        withAnimation(.snappy) { current = t }
        hide?.cancel()
        hide = Task { [weak self] in
            try? await Task.sleep(for: .seconds(isError ? 3 : 1.7))
            guard !Task.isCancelled else { return }
            withAnimation(.easeOut(duration: 0.25)) { if self?.current?.id == t.id { self?.current = nil } }
        }
    }

    func error(_ error: Error) { show(describe(error), systemImage: "exclamationmark.circle.fill", isError: true) }
    func error(_ message: String) { show(message, systemImage: "exclamationmark.circle.fill", isError: true) }

    func dismiss() { withAnimation(.easeOut(duration: 0.2)) { current = nil } }
}

struct ToastView: View {
    let toast: Toaster.Toast

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: toast.systemImage)
                .font(.system(size: 16, weight: .semibold))
                .foregroundStyle(toast.isError ? Color.danger : Color.success)
            Text(toast.text)
                .font(.subheadline.weight(.semibold))
                .lineLimit(2)
        }
        .padding(.leading, 14).padding(.trailing, 18)
        .frame(minHeight: 44)
        .background(.white.opacity(0.97), in: Capsule())
        .shadow(color: .black.opacity(0.14), radius: 15, y: 10)
        .overlay(Capsule().stroke(.black.opacity(0.05), lineWidth: 0.5))
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("toast")
    }
}

private struct ToastHost: ViewModifier {
    @Environment(Toaster.self) private var toaster

    func body(content: Content) -> some View {
        content.overlay(alignment: .top) {
            if let t = toaster.current {
                ToastView(toast: t)
                    .padding(.horizontal, 24).padding(.top, 6)
                    .transition(.move(edge: .top).combined(with: .opacity))
                    .onTapGesture { toaster.dismiss() }
                    .id(t.id)
            }
        }
    }
}

extension View {
    /// Shows the Toaster's current toast over this view. The shell already has one.
    func toastHost() -> some View { modifier(ToastHost()) }
}
