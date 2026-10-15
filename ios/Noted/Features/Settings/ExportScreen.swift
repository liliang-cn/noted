import SwiftUI

/// 导出数据: everything as one zip from the server, handed to the share sheet.
struct ExportScreen: View {
    @Environment(AppData.self) private var data
    @Environment(Toaster.self) private var toaster
    @State private var exporting = false
    @State private var file: URL?

    var body: some View {
        List {
            Section("包含") {
                FieldLabel("笔记", icon: "doc.text.fill", tile: .orange, value: "Markdown")
                FieldLabel("日程与待办", icon: "calendar", tile: .red, value: "iCalendar")
                FieldLabel("全部数据", icon: "curlybraces", tile: .blue, value: "JSON")
                FieldLabel("交易记录", icon: "chart.line.uptrend.xyaxis", tile: .green, value: "CSV")
            }
            if let file {
                Section {
                    FieldLabel(file.lastPathComponent, icon: "doc.zipper", tile: .graphite, value: size(file))
                }
            }
            Section {
                VStack(spacing: 10) {
                    if let file {
                        ShareLink(item: file) {
                            Label("分享", systemImage: "square.and.arrow.up")
                        }
                        .buttonStyle(.wide)
                        .accessibilityIdentifier("export-share")
                        Button("重新导出", action: run)
                            .font(.subheadline)
                            .disabled(exporting)
                            .accessibilityIdentifier("export")
                    } else {
                        Button(action: run) {
                            if exporting { ProgressView().tint(.white) } else { Label("导出", systemImage: "square.and.arrow.up") }
                        }
                        .buttonStyle(.wide)
                        .disabled(exporting)
                        .accessibilityIdentifier("export")
                    }
                }
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            }
        }
        .navigationTitle("导出数据")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func run() {
        exporting = true
        Task {
            defer { exporting = false }
            guard let url = await data.exportAll() else { return }
            #if DEBUG
            if let copy = ProcessInfo.processInfo.environment["NOTED_EXPORT_COPY"] {
                try? FileManager.default.removeItem(atPath: copy)
                try? FileManager.default.copyItem(at: url, to: URL(fileURLWithPath: copy))
            }
            #endif
            withAnimation(.snappy) { file = url }
            toaster.show("已导出 \(url.lastPathComponent)", systemImage: "square.and.arrow.up")
        }
    }

    private func size(_ url: URL) -> String? {
        guard let n = try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize else { return nil }
        return ByteCountFormatter.string(fromByteCount: Int64(n), countStyle: .file)
    }
}
