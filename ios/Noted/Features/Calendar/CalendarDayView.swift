import SwiftUI

// 日历 · 日: the week strip and 全天 row (in the fixed header), and the scrolling hour grid.

/// 一 … 日 with the dates of `day`'s week: today in the accent, the selected day filled, and up to
/// three dots for days with events or open to-dos.
struct WeekStrip: View {
    @Environment(AppData.self) private var data
    let selected: Date
    let today: Date
    let onSelect: (Date) -> Void

    var body: some View {
        let monday = selected.startOfWeek
        HStack(spacing: 0) {
            ForEach(0..<7, id: \.self) { i in
                let d = monday.adding(days: i)
                cell(d)
            }
        }
        .padding(.horizontal, 8)
        .padding(.bottom, 8)
    }

    private func cell(_ d: Date) -> some View {
        let isSel = d.isSameDay(as: selected)
        let isToday = d.isSameDay(as: today)
        let n = min(3, data.occurrences(on: d).count + data.tasks(on: d).filter { !$0.completed }.count)
        return Button { onSelect(d) } label: {
            VStack(spacing: 3) {
                Text(Fmt.weekdayShort(d))
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(.secondaryText)
                Text("\(Fmt.calendar.component(.day, from: d))")
                    .font(.system(size: 19, weight: isToday ? .bold : .medium))
                    .monospacedDigit()
                    .foregroundStyle(isSel ? AnyShapeStyle(.white) : (isToday ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary)))
                    .frame(width: 34, height: 34)
                    .background {
                        if isSel { Circle().fill(isToday ? AnyShapeStyle(.tint) : AnyShapeStyle(Color.primary)) }
                    }
                HStack(spacing: 2) {
                    ForEach(0..<n, id: \.self) { _ in Circle().fill(.tertiaryText).frame(width: 4, height: 4) }
                }
                .frame(height: 5)
            }
            .padding(.vertical, 2)
            .frame(maxWidth: .infinity)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(Fmt.mdw(d))\(isToday ? "，今天" : "")\(n > 0 ? "，有安排" : "")")
        .accessibilityAddTraits(isSel ? .isSelected : [])
        .accessibilityIdentifier("cal-day-\(Fmt.ymd(d))")
    }
}

/// 全天: the day's all-day events (tinted) and date-only to-dos (gray, with a check).
struct AllDayRow: View {
    @Environment(Router.self) private var router
    let events: [Noted_V1_Occurrence]
    let tasks: [Noted_V1_Task]
    let onDelete: (Noted_V1_Event) -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            Text("全天")
                .font(.system(size: 11))
                .foregroundStyle(.secondaryText)
                .frame(width: 44, height: 24, alignment: .leading)
            WrapLayout {
                ForEach(events, id: \.key) { o in
                    Button { router.push(.event(o.event.id, occurrence: o.start)) } label: {
                        Text(o.event.title)
                            .font(.system(size: 13, weight: .medium))
                            .lineLimit(1)
                            .padding(.horizontal, 8)
                            .frame(height: 24)
                            .foregroundStyle(o.event.space.tint)
                            .background(o.event.space.tint.wash(0.13), in: RoundedRectangle(cornerRadius: 5))
                    }
                    .buttonStyle(BlockPressStyle())
                    .contextMenu {
                        Button(role: .destructive) { onDelete(o.event) } label: {
                            Label(o.event.rrule.isEmpty ? "删除" : "删除整个重复日程", systemImage: "trash")
                        }
                    }
                    .accessibilityLabel("全天日程 \(o.event.title)")
                    .accessibilityIdentifier("event-block-\(o.event.id)")
                }
                ForEach(tasks, id: \.id) { t in
                    Button { router.push(.todo(t.id)) } label: {
                        HStack(spacing: 5) {
                            ZStack {
                                if t.completed {
                                    Circle().fill(.tint)
                                    Image(systemName: "checkmark").font(.system(size: 7, weight: .bold)).foregroundStyle(.white)
                                } else {
                                    Circle().strokeBorder(Color.secondaryText, lineWidth: 1.3)
                                }
                            }
                            .frame(width: 12, height: 12)
                            Text(t.title)
                                .strikethrough(t.completed)
                                .foregroundStyle(t.completed ? AnyShapeStyle(.secondaryText) : AnyShapeStyle(.primary))
                                .lineLimit(1)
                        }
                        .font(.system(size: 13, weight: .medium))
                        .padding(.horizontal, 8)
                        .frame(height: 24)
                        .background(.fillTrack, in: RoundedRectangle(cornerRadius: 5))
                    }
                    .buttonStyle(BlockPressStyle())
                    .accessibilityLabel("待办 \(t.title)\(t.completed ? "，已完成" : "")")
                    .accessibilityIdentifier("todo-block-\(t.id)")
                }
            }
        }
        .padding(.leading, 12)
        .padding(.trailing, 12)
        .padding(.vertical, 6)
        .overlay(alignment: .top) { Rectangle().fill(.hairline).frame(height: 0.5) }
    }
}

/// The scrolling 24-hour grid of one day. Tapping an empty hour opens 新建 with an event at that hour.
struct DayGridView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Session.self) private var session
    let day: Date
    let now: Date
    /// Bumped to scroll to now (today) or the day's first item.
    let scrollRequest: Int
    let onSwipe: (Int) -> Void
    let onDelete: (Noted_V1_Event) -> Void

    static let hourHeight: CGFloat = 52
    private static let top: CGFloat = 8
    private static let gutter: CGFloat = 54
    private static let headerHeight: CGFloat = 30

    @State private var position = ScrollPosition(edge: .top)

    var body: some View {
        let items = TimedItem.layout(day: day, occurrences: data.occurrences(on: day), tasks: data.tasks(on: day))
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                Text(day.isSameDay(as: now) ? "\(Fmt.mdw(day)) · 今天" : Fmt.mdw(day))
                    .font(.footnote)
                    .foregroundStyle(.secondaryText)
                    .padding(.horizontal, 16)
                    .frame(height: Self.headerHeight, alignment: .bottom)
                grid(items)
            }
        }
        .scrollPosition($position)
        .onHorizontalSwipe(onSwipe)
        .onAppear { scrollToFocus(items, animated: false) }
        .onChange(of: scrollRequest) { scrollToFocus(items, animated: true) }
    }

    private func grid(_ items: [TimedItem]) -> some View {
        let hh = Self.hourHeight, top = Self.top, gutter = Self.gutter
        return GeometryReader { g in
            let width = g.size.width - gutter - 8
            ZStack(alignment: .topLeading) {
                ForEach(0..<24, id: \.self) { h in
                    HStack(spacing: 0) {
                        Text(h == 0 ? "" : String(format: "%02d:00", h))
                            .font(.system(size: 11)).monospacedDigit()
                            .foregroundStyle(.secondaryText)
                            .frame(width: 46, alignment: .trailing)
                            .padding(.trailing, 6)
                        Rectangle().fill(.hairline).frame(height: 0.5)
                    }
                    .frame(height: 14)
                    .offset(y: top + CGFloat(h) * hh - 7)
                    .accessibilityHidden(true)
                }
                ForEach(0..<24, id: \.self) { h in
                    Button {
                        router.push(.create(CreateSpec(kind: .event, date: day, start: day.startOfDay.at(hour: h), space: session.mode.space)))
                    } label: {
                        Color.clear.contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .frame(width: g.size.width - gutter, height: hh)
                    .offset(x: gutter, y: top + CGFloat(h) * hh)
                    .accessibilityLabel("\(String(format: "%02d:00", h)) 新建日程")
                    .accessibilityIdentifier("cal-slot-\(h)")
                }
                TimedItemsLayer(items: items, hourHeight: hh, top: top, x: gutter, width: width, now: now, onDelete: onDelete)
                if day.isSameDay(as: now) {
                    GridNowLine(now: now, hourHeight: hh, top: top, x: 46)
                }
            }
            .frame(width: g.size.width, height: g.size.height, alignment: .topLeading)
        }
        .frame(height: 24 * hh + 2 * top)
    }

    private func scrollToFocus(_ items: [TimedItem], animated: Bool) {
        let minute: Int
        if day.isSameDay(as: now) {
            minute = max(0, now.minuteOfDay - 90)
        } else if let first = items.map(\.start).min() {
            minute = max(0, first - 30)
        } else {
            minute = 7 * 60 + 30
        }
        let y = Self.headerHeight + Self.top + CGFloat(minute) / 60 * Self.hourHeight
        if animated {
            withAnimation(.snappy) { position.scrollTo(y: y) }
        } else {
            // After the first layout, or the offset is lost.
            Task { @MainActor in position.scrollTo(y: y) }
        }
    }
}
