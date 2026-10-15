import SwiftUI

enum CalendarMode: String, CaseIterable, Identifiable {
    case day, week, month
    var id: String { rawValue }
    var label: String {
        switch self {
        case .day: "日"
        case .week: "周"
        case .month: "月"
        }
    }
}

/// 日历 (tab root): 日 / 周 / 月 in the navigation bar, "10月 2026" with ‹ › under it, then the
/// week strip and hour grid, the week grid, or the month grid with the selected day's list.
/// ‹ › and swipes on the strip or header move a week (a month in 月); swipes on the day grid move a day.
struct CalendarScreen: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @AppStorage("calendar.mode") private var mode: CalendarMode = .day
    @State private var selected = Date.now.startOfDay
    @State private var month = Date.now.startOfMonth
    @State private var scrollRequest = 0
    @State private var deleting: Noted_V1_Event?

    var body: some View {
        TimelineView(.everyMinute) { ctx in
            let now = ctx.date
            VStack(spacing: 0) {
                header(now: now)
                content(now: now)
            }
        }
        .background(Color.white)
        .navigationTitle("日历")
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(Color.white, for: .navigationBar)
        .toolbarBackground(.visible, for: .navigationBar)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Button("今天") { goToToday() }
                    .accessibilityIdentifier("calendar-today")
            }
            ToolbarItem(placement: .principal) {
                Picker("视图", selection: Binding(get: { mode }, set: { setMode($0) })) {
                    ForEach(CalendarMode.allCases) { Text($0.label).tag($0) }
                }
                .pickerStyle(.segmented)
                .frame(width: 150)
                .accessibilityIdentifier("calendar-mode")
            }
            ToolbarItem(placement: .topBarTrailing) { SpaceFilterMenu() }
        }
        .confirmEventDelete($deleting)
        .task(id: loadKey) { await load() }
        .onChange(of: router.scrollToTopRequest) { goToToday() }
    }

    // MARK: header

    private func header(now: Date) -> some View {
        let shown = mode == .month ? month : selected
        return VStack(spacing: 0) {
            HStack(spacing: 0) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(Fmt.month(shown)).font(.system(size: 30, weight: .bold))
                    Text(verbatim: "\(Fmt.calendar.component(.year, from: shown))")
                        .font(.system(size: 17))
                        .foregroundStyle(.secondaryText)
                }
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(.isHeader)
                Spacer()
                Button { step(-1) } label: {
                    Image(systemName: "chevron.left").font(.system(size: 20, weight: .semibold))
                        .frame(width: 44, height: 44).contentShape(Rectangle())
                }
                .accessibilityLabel(mode == .month ? "上个月" : "上一周")
                .accessibilityIdentifier("calendar-prev")
                Button { step(1) } label: {
                    Image(systemName: "chevron.right").font(.system(size: 20, weight: .semibold))
                        .frame(width: 44, height: 44).contentShape(Rectangle())
                }
                .accessibilityLabel(mode == .month ? "下个月" : "下一周")
                .accessibilityIdentifier("calendar-next")
            }
            .padding(.leading, 16)
            .padding(.trailing, 6)
            .padding(.bottom, 4)

            switch mode {
            case .day:
                WeekStrip(selected: selected, today: now) { d in
                    selected = d
                    month = d.startOfMonth
                }
                .id(selected.startOfWeek)
                .transition(.opacity)
                .onHorizontalSwipe { step($0) }
                let allDayEvents = data.occurrences(on: selected).filter(\.event.allDay)
                let dateOnly = data.tasks(on: selected).filter { !$0.hasClockTime }
                if !allDayEvents.isEmpty || !dateOnly.isEmpty {
                    AllDayRow(events: allDayEvents, tasks: dateOnly) { deleting = $0 }
                }
            case .week:
                WeekHeader(monday: selected.startOfWeek, today: now, onOpenDay: openDay) { deleting = $0 }
                    .id(selected.startOfWeek)
                    .transition(.opacity)
                    .onHorizontalSwipe { step($0) }
            case .month:
                MonthGrid(month: month, selected: selected, today: now) { d in
                    withAnimation(.snappy(duration: 0.2)) {
                        selected = d
                        if !Fmt.calendar.isDate(d, equalTo: month, toGranularity: .month) { month = d.startOfMonth }
                    }
                }
                .id(month)
                .transition(.opacity)
                .onHorizontalSwipe { step($0) }
            }
        }
        .background(Color.white)
        .overlay(alignment: .bottom) { Rectangle().fill(.hairline).frame(height: 0.5) }
    }

    // MARK: content

    @ViewBuilder
    private func content(now: Date) -> some View {
        switch mode {
        case .day:
            DayGridView(day: selected, now: now, scrollRequest: scrollRequest,
                        onSwipe: { dir in withAnimation(.snappy(duration: 0.25)) { move(days: dir) } },
                        onDelete: { deleting = $0 })
                .refreshable { await data.refreshAll() }
        case .week:
            WeekGridView(monday: selected.startOfWeek, now: now, scrollRequest: scrollRequest,
                         onSwipe: { step($0) }, onDelete: { deleting = $0 })
                .refreshable { await data.refreshAll() }
        case .month:
            MonthDayList(day: selected, now: now) { deleting = $0 }
        }
    }

    // MARK: navigation

    private func setMode(_ m: CalendarMode) {
        withAnimation(.snappy(duration: 0.2)) {
            mode = m
            month = selected.startOfMonth
        }
        scrollRequest += 1
    }

    private func goToToday() {
        let today = Date.now.startOfDay
        withAnimation(.snappy(duration: 0.25)) {
            selected = today
            month = today.startOfMonth
        }
        scrollRequest += 1
    }

    /// From 周: open a day in 日.
    private func openDay(_ d: Date) {
        withAnimation(.snappy(duration: 0.2)) {
            selected = d.startOfDay
            month = d.startOfMonth
            mode = .day
        }
        scrollRequest += 1
    }

    /// ‹ / ›: a month in 月, a week otherwise.
    private func step(_ dir: Int) {
        withAnimation(.snappy(duration: 0.25)) {
            if mode == .month {
                month = Fmt.calendar.date(byAdding: .month, value: dir, to: month)?.startOfMonth ?? month
                if !Fmt.calendar.isDate(selected, equalTo: month, toGranularity: .month) {
                    let today = Date.now.startOfDay
                    selected = Fmt.calendar.isDate(today, equalTo: month, toGranularity: .month) ? today : month
                }
            } else {
                move(days: 7 * dir)
            }
        }
    }

    private func move(days n: Int) {
        selected = selected.adding(days: n).startOfDay
        month = selected.startOfMonth
    }

    // MARK: loading

    /// The visible range: the selected week, or the month.
    private var loadKey: Date { mode == .month ? month : selected.startOfWeek }

    private func load() async {
        if mode == .month {
            await data.loadMonth(month)
            for n in [1, -1] {
                if let m = Fmt.calendar.date(byAdding: .month, value: n, to: month) { await data.loadMonth(m) }
            }
        } else {
            let monday = selected.startOfWeek
            await data.loadMonth(monday)
            await data.loadMonth(monday.adding(days: 6))
            await data.loadMonth(monday.adding(days: 13))
            await data.loadMonth(monday.adding(days: -7))
        }
    }
}
