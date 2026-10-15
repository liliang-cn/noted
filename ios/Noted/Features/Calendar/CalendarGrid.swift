import SwiftUI
import UIKit

// Pieces shared by the calendar's day and week grids: placing timed items in columns, the event
// and to-do blocks, the now line, the horizontal swipe, and the delete confirmation.

/// A timed event occurrence or a timed to-do placed on an hour grid.
struct TimedItem: Identifiable {
    enum Kind {
        case event(Noted_V1_Occurrence)
        case todo(Noted_V1_Task)
    }

    let kind: Kind
    /// Minutes since the day's midnight.
    var start: Int
    var end: Int
    /// Column within its overlap group, and how many columns the group has.
    var col = 0
    var cols = 1

    var id: String {
        switch kind {
        case .event(let o): "e-\(o.key)"
        case .todo(let t): "t-\(t.id)"
        }
    }

    /// The day's timed occurrences and timed to-dos, laid out side by side where they overlap.
    static func layout(day: Date, occurrences: [Noted_V1_Occurrence], tasks: [Noted_V1_Task]) -> [TimedItem] {
        let dayStart = day.startOfDay
        var items: [TimedItem] = []
        for o in occurrences where !o.event.allDay {
            let s = max(0, Int(o.start.timeIntervalSince(dayStart) / 60))
            let e = min(24 * 60, Int(o.end.timeIntervalSince(dayStart) / 60))
            guard s < 24 * 60 else { continue }
            items.append(TimedItem(kind: .event(o), start: s, end: max(e, s + 20)))
        }
        for t in tasks where t.hasClockTime {
            let s = t.dueTime.date.minuteOfDay
            items.append(TimedItem(kind: .todo(t), start: s, end: s + 30))
        }
        items.sort { $0.start != $1.start ? $0.start < $1.start : $0.end > $1.end }
        var colEnds: [Int] = []
        var group: [Int] = []
        var groupEnd = -1
        func flush() {
            for i in group { items[i].cols = colEnds.count }
            group = []
            colEnds = []
        }
        for i in items.indices {
            if !group.isEmpty && items[i].start >= groupEnd { flush() }
            if let c = colEnds.firstIndex(where: { $0 <= items[i].start }) {
                colEnds[c] = items[i].end
                items[i].col = c
            } else {
                items[i].col = colEnds.count
                colEnds.append(items[i].end)
            }
            group.append(i)
            groupEnd = max(groupEnd, items[i].end)
        }
        flush()
        return items
    }
}

extension Noted_V1_Space {
    /// Text on a tinted calendar block: the space color darkened (70 % color, 30 % black).
    var calendarInk: Color {
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        UIColor(tint).getRed(&r, green: &g, blue: &b, alpha: &a)
        return Color(red: Double(r * 0.7), green: Double(g * 0.7), blue: Double(b * 0.7))
    }
}

/// An event on the hour grid: tinted block with a bar on the left, the title, and (when tall
/// enough, day view only) "14:00 · 地点".
struct EventBlock: View {
    let occurrence: Noted_V1_Occurrence
    let height: CGFloat
    var compact = false
    var now: Date = .now

    var body: some View {
        let e = occurrence.event
        let c = e.space.tint
        VStack(alignment: .leading, spacing: 0) {
            Text(e.title)
                .font(.system(size: compact ? 11 : 13, weight: .semibold))
                .lineLimit(compact ? nil : 1)
            if !compact && height > 38 {
                Text(e.location.isEmpty ? Fmt.hm(occurrence.start) : "\(Fmt.hm(occurrence.start)) · \(e.location)")
                    .font(.system(size: 12))
                    .opacity(0.85)
                    .lineLimit(1)
            }
        }
        .foregroundStyle(e.space.calendarInk)
        .padding(.leading, compact ? 4 : 8)
        .padding(.trailing, compact ? 2 : 6)
        .padding(.vertical, compact ? 2 : 3)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(c.wash(0.13))
        .overlay(alignment: .leading) { Rectangle().fill(c).frame(width: compact ? 2 : 3) }
        .clipShape(RoundedRectangle(cornerRadius: compact ? 4 : 6))
        .opacity(occurrence.isPast(now: now) ? 0.55 : 1)
        .contentShape(RoundedRectangle(cornerRadius: 6))
    }
}

/// A timed to-do on the hour grid: a thin dashed row with a small check and the title.
struct TodoMarker: View {
    let task: Noted_V1_Task
    var compact = false

    var body: some View {
        let c = task.space.tint
        HStack(spacing: 5) {
            if !compact {
                ZStack {
                    if task.completed {
                        Circle().fill(.tint)
                        Image(systemName: "checkmark").font(.system(size: 7, weight: .bold)).foregroundStyle(.white)
                    } else {
                        Circle().strokeBorder(Color.secondaryText, lineWidth: 1.3)
                    }
                }
                .frame(width: 12, height: 12)
            }
            Text(task.title)
                .font(.system(size: compact ? 11 : 13, weight: .semibold))
                .strikethrough(task.completed)
                .opacity(task.completed ? 0.6 : 1)
                .lineLimit(1)
        }
        .foregroundStyle(task.space.calendarInk)
        .padding(.leading, compact ? 4 : 8)
        .padding(.trailing, 2)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .background(.white)
        .overlay(RoundedRectangle(cornerRadius: compact ? 4 : 6).strokeBorder(c.wash(0.55), style: StrokeStyle(lineWidth: 1, dash: [3, 2])))
        .overlay(alignment: .leading) { Rectangle().fill(c).frame(width: compact ? 2 : 3) }
        .clipShape(RoundedRectangle(cornerRadius: compact ? 4 : 6))
        .contentShape(Rectangle())
    }
}

/// Places the blocks of one day column on the grid and makes them tappable.
struct TimedItemsLayer: View {
    let items: [TimedItem]
    let hourHeight: CGFloat
    let top: CGFloat
    /// The column's left edge and width.
    let x: CGFloat
    let width: CGFloat
    var compact = false
    let now: Date
    let onDelete: (Noted_V1_Event) -> Void

    @Environment(Router.self) private var router

    var body: some View {
        ForEach(items) { item in
            let y = top + CGFloat(item.start) / 60 * hourHeight
            let colW = width / CGFloat(item.cols)
            let left = x + colW * CGFloat(item.col) + 1
            let w = max(colW - 3, 4)
            switch item.kind {
            case .event(let o):
                let h = max(22, CGFloat(item.end - item.start) / 60 * hourHeight - 2)
                Button { router.push(.event(o.event.id, occurrence: o.start)) } label: {
                    EventBlock(occurrence: o, height: h, compact: compact, now: now)
                }
                .buttonStyle(BlockPressStyle())
                .frame(width: w, height: h)
                .contextMenu {
                    Button(role: .destructive) { onDelete(o.event) } label: {
                        Label(o.event.rrule.isEmpty ? "删除" : "删除整个重复日程", systemImage: "trash")
                    }
                }
                .offset(x: left, y: y)
                .accessibilityLabel("\(o.event.title)，\(Fmt.hm(o.start)) 到 \(Fmt.hm(o.end))")
                .accessibilityIdentifier("event-block-\(o.event.id)")
            case .todo(let t):
                Button { router.push(.todo(t.id)) } label: { TodoMarker(task: t, compact: compact) }
                    .buttonStyle(BlockPressStyle())
                    .frame(width: w, height: compact ? 18 : 22)
                    .offset(x: left, y: y)
                    .accessibilityLabel("待办 \(t.title)，\(Fmt.hm(t.dueTime.date))\(t.completed ? "，已完成" : "")")
                    .accessibilityIdentifier("todo-block-\(t.id)")
            }
        }
    }
}

/// Blocks darken a little while pressed.
struct BlockPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.brightness(configuration.isPressed ? -0.06 : 0)
    }
}

/// The red line across the grid at the current time.
struct GridNowLine: View {
    let now: Date
    let hourHeight: CGFloat
    let top: CGFloat
    /// Where the line starts.
    let x: CGFloat
    /// Show "11:00" in the hour-label gutter.
    var showsLabel = true

    var body: some View {
        let y = top + CGFloat(now.minuteOfDay) / 60 * hourHeight
        ZStack(alignment: .topLeading) {
            Rectangle().fill(Color.danger)
                .frame(height: 1.5)
                .padding(.leading, x)
                .offset(y: y - 0.75)
            Circle().fill(Color.danger)
                .frame(width: 8, height: 8)
                .offset(x: x - 4, y: y - 4)
            if showsLabel {
                Text(Fmt.hm(now))
                    .font(.system(size: 11, weight: .bold)).monospacedDigit()
                    .foregroundStyle(.danger)
                    .frame(width: 40, alignment: .trailing)
                    .background(.white)
                    .offset(x: 4, y: y - 7)
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .allowsHitTesting(false)
        .accessibilityElement()
        .accessibilityLabel("现在 \(Fmt.hm(now))")
    }
}

// MARK: - horizontal swipe

/// A UIKit swipe recognizer, so a left/right swipe works on top of a vertical ScrollView without
/// getting in the way of scrolling or of the buttons inside.
private struct SwipeRecognizer: UIGestureRecognizerRepresentable {
    let direction: UISwipeGestureRecognizer.Direction
    let action: () -> Void

    func makeUIGestureRecognizer(context: Context) -> UISwipeGestureRecognizer {
        let g = UISwipeGestureRecognizer()
        g.direction = direction
        g.delegate = context.coordinator
        return g
    }

    func handleUIGestureRecognizerAction(_ recognizer: UISwipeGestureRecognizer, context: Context) {
        if recognizer.state == .ended { action() }
    }

    func makeCoordinator(converter: CoordinateSpaceConverter) -> Coordinator { Coordinator() }

    @MainActor
    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        func gestureRecognizer(_ gestureRecognizer: UIGestureRecognizer,
                               shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool { true }
    }
}

extension View {
    /// Calls `action(1)` on a swipe to the left (next) and `action(-1)` on a swipe to the right.
    func onHorizontalSwipe(_ action: @escaping (Int) -> Void) -> some View {
        gesture(SwipeRecognizer(direction: .left) { action(1) })
            .gesture(SwipeRecognizer(direction: .right) { action(-1) })
    }
}

// MARK: - deleting an event

extension View {
    /// Asks before deleting `event` (set it to ask). The server deletes a recurring event as a whole
    /// series, so a recurring one is offered only that. `onDeleted` runs after it is gone.
    func confirmEventDelete(_ event: Binding<Noted_V1_Event?>, onDeleted: @escaping () -> Void = {}) -> some View {
        modifier(EventDeleteConfirmation(event: event, onDeleted: onDeleted))
    }
}

private struct EventDeleteConfirmation: ViewModifier {
    @Environment(AppData.self) private var data
    @Environment(Toaster.self) private var toaster
    @Binding var event: Noted_V1_Event?
    let onDeleted: () -> Void

    func body(content: Content) -> some View {
        content.confirmationDialog(
            event.map { "删除“\($0.title)”？" } ?? "",
            isPresented: Binding(get: { event != nil }, set: { if !$0 { event = nil } }),
            titleVisibility: .visible,
            presenting: event
        ) { e in
            Button(e.rrule.isEmpty ? "删除日程" : "删除整个重复日程", role: .destructive) {
                Task {
                    if await data.deleteEvent(e.id) {
                        toaster.show("已删除", systemImage: "trash")
                        onDeleted()
                    }
                }
            }
            .accessibilityIdentifier("event-delete-confirm")
        } message: { e in
            if !e.rrule.isEmpty { Text("这是重复日程，所有重复都会删除") }
        }
    }
}

// MARK: - wrapping row

/// Lays children out left to right and wraps to the next line (the 全天 chips).
struct WrapLayout: SwiftUI.Layout {
    var spacing: CGFloat = 6
    var lineSpacing: CGFloat = 6

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let maxW = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, lineH: CGFloat = 0, widest: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(ProposedViewSize(width: maxW, height: nil))
            if x > 0 && x + s.width > maxW {
                y += lineH + lineSpacing
                x = 0
                lineH = 0
            }
            x += s.width + spacing
            lineH = max(lineH, s.height)
            widest = max(widest, x - spacing)
        }
        return CGSize(width: min(widest, maxW), height: y + lineH)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, lineH: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(ProposedViewSize(width: bounds.width, height: nil))
            if x > bounds.minX && x + s.width > bounds.maxX {
                y += lineH + lineSpacing
                x = bounds.minX
                lineH = 0
            }
            v.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(width: min(s.width, bounds.width), height: s.height))
            x += s.width + spacing
            lineH = max(lineH, s.height)
        }
    }
}
