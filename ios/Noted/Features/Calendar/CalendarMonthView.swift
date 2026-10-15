import SwiftUI

// 日历 · 月: the month grid (fixed, swipe for the next or previous month) and the selected day's
// events and to-dos in a list under it.

/// 一 … 日 and the weeks of `month`: days outside it grayed, today in the accent, the selected day
/// filled, and dots for events (space colors, up to three) and open to-dos (gray).
struct MonthGrid: View {
    @Environment(AppData.self) private var data
    let month: Date
    let selected: Date
    let today: Date
    let onSelect: (Date) -> Void

    var body: some View {
        let start = month.startOfWeek
        let daysInMonth = Fmt.calendar.range(of: .day, in: .month, for: month)?.count ?? 30
        let lead = Fmt.dayDiff(month, from: start)
        let rows = (lead + daysInMonth + 6) / 7
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                ForEach(RepeatRule.weekOrder, id: \.self) { w in
                    Text(Fmt.weekdayChar(w))
                        .font(.system(size: 11, weight: .medium))
                        .foregroundStyle(.secondaryText)
                        .frame(maxWidth: .infinity)
                }
            }
            .padding(.top, 2)
            .padding(.bottom, 6)
            .accessibilityHidden(true)
            ForEach(0..<rows, id: \.self) { r in
                HStack(spacing: 0) {
                    ForEach(0..<7, id: \.self) { c in cell(start.adding(days: r * 7 + c)) }
                }
                .overlay(alignment: .top) { Rectangle().fill(.hairline).frame(height: 0.5) }
            }
        }
        .padding(.horizontal, 8)
        .padding(.bottom, 6)
    }

    private func cell(_ d: Date) -> some View {
        let inMonth = Fmt.calendar.isDate(d, equalTo: month, toGranularity: .month)
        let isSel = d.isSameDay(as: selected)
        let isToday = d.isSameDay(as: today)
        let events = data.occurrences(on: d)
        let openTasks = data.tasks(on: d).filter { !$0.completed }
        var dots = events.prefix(3).map(\.event.space.tint)
        if dots.count < 3 && !openTasks.isEmpty { dots.append(.tertiaryText) }
        let total = events.count + openTasks.count
        return Button { onSelect(d) } label: {
            VStack(spacing: 3) {
                Text("\(Fmt.calendar.component(.day, from: d))")
                    .font(.system(size: 17, weight: isToday ? .bold : .regular)).monospacedDigit()
                    .foregroundStyle(isSel ? AnyShapeStyle(.white)
                                     : isToday ? AnyShapeStyle(.tint)
                                     : inMonth ? AnyShapeStyle(.primary) : AnyShapeStyle(.tertiaryText))
                    .frame(width: 30, height: 30)
                    .background {
                        if isSel { Circle().fill(isToday ? AnyShapeStyle(.tint) : AnyShapeStyle(Color.primary)) }
                    }
                HStack(spacing: 3) {
                    ForEach(dots.indices, id: \.self) { i in Circle().fill(dots[i]).frame(width: 5, height: 5) }
                }
                .frame(height: 5)
            }
            .padding(.top, 4)
            .frame(maxWidth: .infinity)
            .frame(height: 50, alignment: .top)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(Fmt.mdw(d))\(isToday ? "，今天" : "")\(total > 0 ? "，\(total) 项" : "")")
        .accessibilityAddTraits(isSel ? .isSelected : [])
        .accessibilityIdentifier("cal-month-day-\(Fmt.ymd(d))")
    }
}

/// The selected day's events and to-dos, by time (all-day events first, date-only to-dos last).
struct MonthDayList: View {
    @Environment(AppData.self) private var data
    let day: Date
    let now: Date
    let onDelete: (Noted_V1_Event) -> Void

    private enum Entry: Identifiable {
        case event(Noted_V1_Occurrence)
        case todo(Noted_V1_Task)
        var id: String {
            switch self {
            case .event(let o): "e-\(o.key)"
            case .todo(let t): "t-\(t.id)"
            }
        }
    }

    var body: some View {
        let entries = entries
        List {
            Section {
                if entries.isEmpty {
                    EmptyState(text: "没有安排")
                } else {
                    ForEach(entries) { entry in
                        switch entry {
                        case .event(let o):
                            EventTimelineRow(occurrence: o, now: now)
                                .contextMenu {
                                    Button(role: .destructive) { onDelete(o.event) } label: {
                                        Label(o.event.rrule.isEmpty ? "删除" : "删除整个重复日程", systemImage: "trash")
                                    }
                                }
                                .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                                    Button { onDelete(o.event) } label: { Label("删除", systemImage: "trash") }
                                        .tint(.danger)
                                }
                        case .todo(let t):
                            TodoTimelineRow(task: t, now: now)
                                .taskSwipeActions(t)
                        }
                    }
                }
            } header: {
                Text(day.isSameDay(as: now) ? "\(Fmt.mdw(day)) · 今天" : Fmt.mdw(day))
                    .font(.footnote)
                    .foregroundStyle(.secondaryText)
                    .textCase(nil)
            }
        }
        .listStyle(.insetGrouped)
        .listSectionSpacing(.compact)
        .contentMargins(.top, 8, for: .scrollContent)
        .scrollContentBackground(.hidden)
        .background(Color.groupedBackground)
        .refreshable { await data.refreshAll() }
    }

    private var entries: [Entry] {
        var keyed: [(Int, Entry)] = data.occurrences(on: day).map { o in
            (o.event.allDay ? -1 : max(0, Int(o.start.timeIntervalSince(day.startOfDay) / 60)), .event(o))
        }
        keyed += data.tasks(on: day).map { t in (t.hasClockTime ? t.dueTime.date.minuteOfDay : 24 * 60 + 1, .todo(t)) }
        return keyed.enumerated()
            .sorted { $0.element.0 != $1.element.0 ? $0.element.0 < $1.element.0 : $0.offset < $1.offset }
            .map(\.element.1)
    }
}
