import SwiftUI

// 日历 · 周: the weekday header (in the fixed header) and the scrolling 7-column hour grid.

/// 一 5 · 二 6 … with today circled in the accent. Tapping a day opens it in 日 view.
struct WeekHeader: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    let monday: Date
    let today: Date
    let onOpenDay: (Date) -> Void
    let onDelete: (Noted_V1_Event) -> Void

    var body: some View {
        let days = (0..<7).map { monday.adding(days: $0) }
        let allDay = days.map { d in data.occurrences(on: d).filter(\.event.allDay) }
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                // The hour-label gutter. Height 1: a bare Color.clear grows to fill the header.
                Color.clear.frame(width: 44, height: 1)
                ForEach(days, id: \.self) { d in
                    let isToday = d.isSameDay(as: today)
                    Button { onOpenDay(d) } label: {
                        VStack(spacing: 2) {
                            Text(Fmt.weekdayShort(d))
                                .font(.system(size: 11))
                                .foregroundStyle(.secondaryText)
                            Text("\(Fmt.calendar.component(.day, from: d))")
                                .font(.system(size: 17, weight: .medium)).monospacedDigit()
                                .foregroundStyle(isToday ? AnyShapeStyle(.white) : AnyShapeStyle(.primary))
                                .frame(width: 28, height: 28)
                                .background { if isToday { Circle().fill(.tint) } }
                        }
                        .frame(maxWidth: .infinity)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(Fmt.mdw(d))\(isToday ? "，今天" : "")")
                    .accessibilityHint("打开这一天")
                    .accessibilityIdentifier("cal-week-day-\(Fmt.ymd(d))")
                }
            }
            .padding(.trailing, 4)
            .padding(.bottom, 6)
            if allDay.contains(where: { !$0.isEmpty }) {
                HStack(alignment: .top, spacing: 0) {
                    Text("全天")
                        .font(.system(size: 10))
                        .foregroundStyle(.secondaryText)
                        .frame(width: 44, height: 18)
                    ForEach(0..<7, id: \.self) { i in
                        VStack(spacing: 2) {
                            ForEach(allDay[i].prefix(2), id: \.key) { o in
                                Button { router.push(.event(o.event.id, occurrence: o.start)) } label: {
                                    Text(o.event.title)
                                        .font(.system(size: 10, weight: .medium))
                                        .lineLimit(1)
                                        .padding(.horizontal, 3)
                                        .frame(maxWidth: .infinity, minHeight: 18, alignment: .leading)
                                        .foregroundStyle(o.event.space.calendarInk)
                                        .background(o.event.space.tint.wash(0.13), in: RoundedRectangle(cornerRadius: 4))
                                }
                                .buttonStyle(BlockPressStyle())
                                .contextMenu {
                                    Button(role: .destructive) { onDelete(o.event) } label: {
                                        Label(o.event.rrule.isEmpty ? "删除" : "删除整个重复日程", systemImage: "trash")
                                    }
                                }
                                .accessibilityLabel("全天日程 \(o.event.title)")
                            }
                            if allDay[i].count > 2 {
                                Button { onOpenDay(days[i]) } label: {
                                    Text("+\(allDay[i].count - 2)")
                                        .font(.system(size: 10, weight: .medium))
                                        .foregroundStyle(.secondaryText)
                                        .frame(maxWidth: .infinity)
                                }
                                .buttonStyle(.plain)
                                .accessibilityLabel("还有 \(allDay[i].count - 2) 个全天日程")
                            }
                        }
                        .padding(.horizontal, 1)
                        .frame(maxWidth: .infinity)
                    }
                }
                .padding(.trailing, 4)
                .padding(.vertical, 5)
                .overlay(alignment: .top) { Rectangle().fill(.hairline).frame(height: 0.5) }
            }
        }
    }
}

/// Seven day columns on a 24-hour grid. Tapping an empty hour of a day opens 新建 with an event then.
struct WeekGridView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Session.self) private var session
    let monday: Date
    let now: Date
    let scrollRequest: Int
    let onSwipe: (Int) -> Void
    let onDelete: (Noted_V1_Event) -> Void

    static let hourHeight: CGFloat = 46
    private static let top: CGFloat = 8
    private static let gutter: CGFloat = 44

    @State private var position = ScrollPosition(edge: .top)

    var body: some View {
        let days = (0..<7).map { monday.adding(days: $0) }
        let items = days.map { d in TimedItem.layout(day: d, occurrences: data.occurrences(on: d), tasks: data.tasks(on: d)) }
        ScrollView {
            grid(days: days, items: items)
        }
        .scrollPosition($position)
        .onHorizontalSwipe(onSwipe)
        .onAppear { scrollToFocus(items, animated: false) }
        .onChange(of: scrollRequest) { scrollToFocus(items, animated: true) }
    }

    private func grid(days: [Date], items: [[TimedItem]]) -> some View {
        let hh = Self.hourHeight, top = Self.top, gutter = Self.gutter
        return GeometryReader { g in
            let colW = (g.size.width - gutter - 2) / 7
            ZStack(alignment: .topLeading) {
                ForEach(0..<24, id: \.self) { h in
                    HStack(spacing: 0) {
                        Text(h == 0 ? "" : String(format: "%02d", h))
                            .font(.system(size: 11)).monospacedDigit()
                            .foregroundStyle(.secondaryText)
                            .frame(width: 36, alignment: .trailing)
                            .padding(.trailing, 8)
                        Rectangle().fill(.hairline).frame(height: 0.5)
                    }
                    .frame(height: 14)
                    .offset(y: top + CGFloat(h) * hh - 7)
                    .accessibilityHidden(true)
                }
                ForEach(1..<7, id: \.self) { i in
                    Rectangle().fill(.hairline)
                        .frame(width: 0.5, height: g.size.height)
                        .offset(x: gutter + colW * CGFloat(i))
                        .accessibilityHidden(true)
                }
                ForEach(0..<7, id: \.self) { i in
                    let d = days[i]
                    ForEach(0..<24, id: \.self) { h in
                        Button {
                            router.push(.create(CreateSpec(kind: .event, date: d, start: d.startOfDay.at(hour: h), space: session.mode.space)))
                        } label: {
                            Color.clear.contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .frame(width: colW, height: hh)
                        .offset(x: gutter + colW * CGFloat(i), y: top + CGFloat(h) * hh)
                        .accessibilityLabel("\(Fmt.mdw(d)) \(String(format: "%02d:00", h)) 新建日程")
                    }
                }
                ForEach(0..<7, id: \.self) { i in
                    TimedItemsLayer(items: items[i], hourHeight: hh, top: top, x: gutter + colW * CGFloat(i),
                                    width: colW, compact: true, now: now, onDelete: onDelete)
                }
                if days.contains(where: { $0.isSameDay(as: now) }) {
                    GridNowLine(now: now, hourHeight: hh, top: top, x: gutter, showsLabel: false)
                }
            }
            .frame(width: g.size.width, height: g.size.height, alignment: .topLeading)
        }
        .frame(height: 24 * hh + 2 * top)
    }

    private func scrollToFocus(_ items: [[TimedItem]], animated: Bool) {
        let minute: Int
        if (0..<7).contains(where: { monday.adding(days: $0).isSameDay(as: now) }) {
            minute = max(0, now.minuteOfDay - 120)
        } else if let first = items.flatMap({ $0.map(\.start) }).min() {
            minute = max(0, first - 30)
        } else {
            minute = 7 * 60 + 30
        }
        let y = Self.top + CGFloat(minute) / 60 * Self.hourHeight
        if animated {
            withAnimation(.snappy) { position.scrollTo(y: y) }
        } else {
            // After the first layout, or the offset is lost.
            Task { @MainActor in position.scrollTo(y: y) }
        }
    }
}
