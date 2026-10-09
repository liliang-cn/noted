import SwiftUI
import WidgetKit

struct NotedWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: Entry

    var body: some View {
        let s = entry.snapshot
        Group {
            if !s.pro {
                LockedView(c: s.colors)
            } else {
                switch family {
                case .systemSmall: SmallSnapshotView(s: s, now: entry.date)
                case .systemMedium: MediumSnapshotView(s: s)
                #if os(iOS)
                case .accessoryRectangular: RectangularSnapshotView(s: s, now: entry.date)
                case .accessoryCircular: CircularSnapshotView(s: s)
                case .accessoryInline: InlineSnapshotView(s: s, now: entry.date)
                #else
                case .accessoryRectangular: RectangularSnapshotView(s: s, now: entry.date)
                case .accessoryCircular: CircularSnapshotView(s: s)
                case .accessoryInline: InlineSnapshotView(s: s, now: entry.date)
                case .accessoryCorner: InlineSnapshotView(s: s, now: entry.date)
                #endif
                default: SmallSnapshotView(s: s, now: entry.date)
                }
            }
        }
        .containerBackground(for: .widget) { Color(rgb: s.colors.bg) }
    }
}

struct NotedWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "NotedToday", provider: Provider()) { NotedWidgetView(entry: $0) }
            .configurationDisplayName("今天")
            .description("今天的日程、待办和目标。")
            #if os(iOS)
            .supportedFamilies([.systemSmall, .systemMedium, .accessoryRectangular, .accessoryCircular, .accessoryInline])
            #else
            .supportedFamilies([.accessoryRectangular, .accessoryCircular, .accessoryInline, .accessoryCorner])
            #endif
    }
}

@main
struct NotedWidgetBundle: WidgetBundle {
    var body: some Widget { NotedWidget() }
}
