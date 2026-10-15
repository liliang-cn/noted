import SwiftUI

struct CalendarView: View {
    enum Range: String, CaseIterable { case month = "月", week = "周", day = "日" }

    @Environment(Session.self) private var session
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @State private var day = Calendar.current.startOfDay(for: .now)
    @State private var range: Range = .week
    @State private var occurrences: [Noted_V1_Occurrence] = []
    @State private var tasks: [Noted_V1_Task] = []
    @State private var error: String?
    @State private var pendingDelete: PendingDelete?

    private static let hourHeight: CGFloat = 48
    private var cal: Calendar {
        var c = Calendar.current
        c.firstWeekday = 2
        return c
    }
    private var weekDays: [Date] {
        let start = cal.dateInterval(of: .weekOfYear, for: day)?.start ?? day
        return (0..<7).compactMap { cal.date(byAdding: .day, value: $0, to: start) }
    }
    private var monthGrid: [Date] {
        guard let m = cal.dateInterval(of: .month, for: day), let first = cal.dateInterval(of: .weekOfYear, for: m.start)?.start else { return [] }
        let weeks = Int(ceil(Double(cal.dateComponents([.day], from: first, to: m.end).day ?? 35) / 7))
        return (0..<(weeks * 7)).compactMap { cal.date(byAdding: .day, value: $0, to: first) }
    }

    var body: some View {
        TabScreen(eyebrow: "\(cal.component(.year, from: day))", title: "\(cal.component(.month, from: day))月") {
            header
        } content: {
            HStack(spacing: 0) {
                ForEach(Range.allCases, id: \.self) { r in
                    Button { range = r } label: {
                        Text(r.rawValue).font(.ui(14, .semibold)).frame(maxWidth: .infinity).frame(height: 34)
                            .foregroundStyle(range == r ? p.bg : p.ink2)
                            .background(range == r ? p.ink : .clear, in: RoundedRectangle(cornerRadius: max(p.radiusSm - 2, 2)))
                            .contentShape(Rectangle())
                    }
                }
            }
            .padding(2).background(p.surface, in: RoundedRectangle(cornerRadius: p.radiusSm))
            .overlay(RoundedRectangle(cornerRadius: p.radiusSm).strokeBorder(p.line))

            switch range {
            case .month: monthView
            case .week: weekStrip; dayGrid
            case .day: dayStepper; dayGrid
            }
            ErrorLine(text: error)
            if range != .month, !tasks.isEmpty { taskList }
        }
        .task(id: "\(day.timeIntervalSince1970)-\(range.rawValue)-\(session.mode.rawValue)") { await load() }
        .refreshable { await load() }
        .reloadOnReturn { await load() }
        .confirmDelete($pendingDelete) { err in error = err; await load() }
    }

    private var header: some View {
        HStack(spacing: 8) {
            Button { day = cal.startOfDay(for: .now) } label: {
                Text("今天").font(.ui(13, .bold)).foregroundStyle(p.ink).padding(.horizontal, 12).frame(height: 32)
                    .overlay(RoundedRectangle(cornerRadius: p.radiusSm).strokeBorder(p.line))
                    .contentShape(Rectangle())
            }
            ModeSwitch(mini: true)
            Button { newEvent(at: nil) } label: {
                Image(systemName: "plus").font(.system(size: 16, weight: .semibold)).foregroundStyle(p.bg)
                    .frame(width: 34, height: 34).background(p.ink, in: RoundedRectangle(cornerRadius: p.radiusSm))
            }
            .accessibilityLabel("新建日程")
        }
    }

    /// Opens the create page on 日程, starting at the tapped hour, or at the next whole hour of
    /// the chosen day (9:00 on a day that is not today).
    private func newEvent(at hour: Int?) {
        let start: Date
        if let hour {
            start = cal.date(bySettingHour: hour, minute: 0, second: 0, of: day) ?? day
        } else if cal.isDateInToday(day) {
            let next = cal.component(.hour, from: .now) + 1
            start = cal.date(bySettingHour: min(next, 23), minute: 0, second: 0, of: day) ?? day
        } else {
            start = cal.date(bySettingHour: 9, minute: 0, second: 0, of: day) ?? day
        }
        router.push(.create(CreateSpec(kind: CaptureView.Kind.event.rawValue, space: session.mode.space, start: start)))
    }

    private var weekStrip: some View {
        HStack(spacing: 0) {
            ForEach(weekDays, id: \.self) { d in
                let on = cal.isDate(d, inSameDayAs: day)
                Button { day = d } label: {
                    VStack(spacing: 6) {
                        Text(d.formatted(.dateTime.weekday(.narrow).locale(Fmt.zh))).font(.mono(13))
                            .foregroundStyle(on ? p.bg.opacity(0.75) : p.ink2)
                        Text("\(cal.component(.day, from: d))").font(.mono(17, .semibold))
                            .foregroundStyle(on ? p.bg : p.ink)
                    }
                    .frame(width: 44).padding(.vertical, 6)
                    .background(on ? p.ink : .clear, in: RoundedRectangle(cornerRadius: p.radiusSm))
                    .frame(maxWidth: .infinity)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.vertical, -6)
    }

    private var dayStepper: some View {
        HStack {
            Button { day = cal.date(byAdding: .day, value: -1, to: day) ?? day } label: {
                Image(systemName: "chevron.left").font(.system(size: 15, weight: .semibold)).frame(width: 34, height: 34)
            }.accessibilityLabel("前一天")
            Spacer()
            Text("\(Fmt.md(day)) · \(day.formatted(.dateTime.weekday(.wide).locale(Fmt.zh)))").font(.ui(15, .bold)).foregroundStyle(p.ink)
            Spacer()
            Button { day = cal.date(byAdding: .day, value: 1, to: day) ?? day } label: {
                Image(systemName: "chevron.right").font(.system(size: 15, weight: .semibold)).frame(width: 34, height: 34)
            }.accessibilityLabel("后一天")
        }
        .foregroundStyle(p.ink2).frame(height: 40)
    }

    // MARK: month

    private var monthView: some View {
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                ForEach(["一", "二", "三", "四", "五", "六", "日"], id: \.self) {
                    Text($0).font(.mono(13)).foregroundStyle(p.ink2).frame(maxWidth: .infinity)
                }
            }.padding(.vertical, 10)
            let grid = monthGrid
            ForEach(0..<(grid.count / 7), id: \.self) { w in
                HStack(spacing: 0) {
                    ForEach(grid[(w * 7)..<(w * 7 + 7)], id: \.self) { d in monthCell(d) }
                }
            }
        }
        .padding(.horizontal, 6).padding(.bottom, 6)
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
        .overlay(RoundedRectangle(cornerRadius: p.radius).strokeBorder(p.line))
    }

    private func monthCell(_ d: Date) -> some View {
        let inMonth = cal.isDate(d, equalTo: day, toGranularity: .month)
        let today = cal.isDateInToday(d)
        let spaces = Array(Set(occurrences.filter { cal.isDate($0.startTime.date, inSameDayAs: d) }.map(\.event.space.rawValue))).sorted()
        return Button { day = d; range = .day } label: {
            VStack(spacing: 5) {
                Text("\(cal.component(.day, from: d))").font(.mono(15, .semibold))
                    .foregroundStyle(today ? p.bg : (inMonth ? p.ink : p.ink3))
                    .frame(width: 32, height: 32).background(today ? p.ink : .clear, in: RoundedRectangle(cornerRadius: p.radiusSm))
                HStack(spacing: 3) {
                    ForEach(spaces, id: \.self) { s in Circle().fill((Noted_V1_Space(rawValue: s) ?? .life).color(p)).frame(width: 5, height: 5) }
                }.frame(height: 5)
            }
            .frame(maxWidth: .infinity).frame(height: 52).contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(Fmt.md(d))")
    }

    // MARK: day grid

    /// Where the grid's top line sits, below the card's edge, so the first hour label has room.
    private static let gridTop: CGFloat = 14
    /// Event blocks start right of the hour labels.
    private static let blockLeft: CGFloat = 48

    /// Hours from the start of the chosen day to `d`, clamped to the day.
    private func hourOf(_ d: Date) -> Double {
        let m = d.timeIntervalSince(day) / 3600
        return min(max(m, 0), 24)
    }

    /// The hours the grid shows: from the first event's hour (or now, on today, if that is earlier)
    /// through the last one, at least ten rows.
    private var hours: ClosedRange<Int> {
        let timed = occurrences.filter { !$0.event.allDay }
        var lo = timed.map { Int(hourOf($0.startTime.date)) }.min() ?? 8
        if cal.isDateInToday(day) { lo = min(lo, Int(hourOf(.now))) }
        lo = min(lo, 14)
        let last = timed.map { Int(ceil(hourOf($0.endTime.date))) - 1 }.max() ?? lo
        return lo...min(max(last, lo + 9), 23)
    }

    private var dayGrid: some View {
        let hs = hours
        let timed = occurrences.filter { !$0.event.allDay }
        let allDay = occurrences.filter(\.event.allDay)
        let height = Self.gridTop + CGFloat(hs.count) * Self.hourHeight
        return VStack(alignment: .leading, spacing: 6) {
            if !allDay.isEmpty {
                VStack(spacing: 6) {
                    ForEach(allDay, id: \.event.id) { o in
                        eventBlock(o, height: 30)
                    }
                }
                .padding(.leading, Self.blockLeft).padding(.trailing, 12).padding(.top, 12)
            }
            GeometryReader { g in
                let width = g.size.width
                ZStack(alignment: .topLeading) {
                    ForEach(Array(hs), id: \.self) { h in
                        let top = Self.gridTop + CGFloat(h - hs.lowerBound) * Self.hourHeight
                        Color.clear
                            .frame(width: width, height: Self.hourHeight)
                            .contentShape(Rectangle())
                            .onTapGesture { newEvent(at: h) }
                            .accessibilityElement()
                            .accessibilityLabel("在 \(h) 点新建日程")
                            .accessibilityAddTraits(.isButton)
                            .offset(y: top)
                        Rectangle().fill(p.line).frame(width: width, height: 1).offset(y: top)
                            .allowsHitTesting(false)
                        Text(String(format: "%02d", h)).font(.mono(13)).foregroundStyle(p.ink3)
                            .padding(.trailing, 4).background(p.surface)
                            .frame(height: 18).offset(x: 12, y: top - 8)
                            .allowsHitTesting(false).accessibilityHidden(true)
                    }
                    ForEach(layout(timed), id: \.0.event.id) { o, col, cols in
                        block(o, col, cols, first: hs.lowerBound, width: width)
                    }
                    if cal.isDateInToday(day) { nowLine(first: hs.lowerBound, last: hs.upperBound, width: width) }
                }
            }
            .frame(height: height + 12)
            if occurrences.isEmpty { Text("这天没有日程").font(.ui(13)).foregroundStyle(p.ink3).frame(maxWidth: .infinity).padding(.bottom, 14) }
        }
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
        .clipShape(RoundedRectangle(cornerRadius: p.radius))
        .overlay(RoundedRectangle(cornerRadius: p.radius).strokeBorder(p.line))
    }

    private func y(_ d: Date, first: Int) -> CGFloat {
        Self.gridTop + CGFloat(hourOf(d) - Double(first)) * Self.hourHeight
    }

    private func block(_ o: Noted_V1_Occurrence, _ col: Int, _ cols: Int, first: Int, width: CGFloat) -> some View {
        let top = max(y(o.startTime.date, first: first), Self.gridTop) + 2
        let h = max(30, y(o.endTime.date, first: first) - top - 2)
        let w = (width - Self.blockLeft - 12) / CGFloat(cols)
        return eventBlock(o, height: h)
            .frame(width: w - (cols > 1 ? 2 : 0))
            .offset(x: Self.blockLeft + w * CGFloat(col), y: top)
    }

    /// An event as the design draws it: a tinted block with a bar down its left edge; the title,
    /// and below it where it is placed when the block is tall enough.
    private func eventBlock(_ o: Noted_V1_Occurrence, height h: CGFloat) -> some View {
        let color = o.event.space.color(p)
        let tall = h >= 44 && !o.event.location.isEmpty
        return Group {
            if tall {
                VStack(alignment: .leading, spacing: 0) {
                    Text(o.event.title).font(.ui(13, .bold)).lineLimit(1)
                    Text(o.event.location).font(.mono(13)).lineLimit(1)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            } else {
                HStack(spacing: 8) {
                    Text(o.event.title).font(.ui(13, .bold)).lineLimit(1)
                    Spacer(minLength: 0)
                    if !o.event.rrule.isEmpty { Text("重复").font(.mono(13)) }
                }
                .frame(maxHeight: .infinity)
            }
        }
        .foregroundStyle(color)
        .padding(.leading, 13).padding(.trailing, 10).padding(.vertical, h >= 44 ? 6 : 0)
        .frame(height: h)
        .background(o.event.space == .work ? p.workBg : p.lifeBg)
        .overlay(alignment: .leading) { Rectangle().fill(color).frame(width: 3) }
        .clipShape(RoundedRectangle(cornerRadius: 4))
        .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: 4))
        .contentShape(Rectangle())
        .onTapGesture { router.push(.event(o.event)) }
        .deleteMenu { pendingDelete = .event(o.event) }
    }

    private func nowLine(first: Int, last: Int, width: CGFloat) -> some View {
        let t = y(.now, first: first)
        return Group {
            if t >= Self.gridTop && t <= Self.gridTop + CGFloat(last - first + 1) * Self.hourHeight {
                ZStack(alignment: .leading) {
                    Rectangle().fill(p.bad).frame(width: width - 40, height: 2).offset(x: 40)
                    Circle().fill(p.bad).frame(width: 10, height: 10).offset(x: 34)
                }
                .frame(height: 10)
                .offset(y: t - 5)
                .allowsHitTesting(false)
            }
        }
    }

    /// Side-by-side columns for events that overlap in time.
    private func layout(_ list: [Noted_V1_Occurrence]) -> [(Noted_V1_Occurrence, Int, Int)] {
        var out: [(Noted_V1_Occurrence, Int, Int)] = []
        var group: [(Noted_V1_Occurrence, Int)] = []
        var groupEnd = Date.distantPast
        func flush() {
            let n = (group.map(\.1).max() ?? 0) + 1
            out += group.map { ($0.0, $0.1, n) }
            group = []
        }
        for o in list.sorted(by: { $0.startTime.date < $1.startTime.date }) {
            if o.startTime.date >= groupEnd { flush() }
            var col = 0
            while group.contains(where: { $0.1 == col && $0.0.endTime.date > o.startTime.date }) { col += 1 }
            group.append((o, col))
            groupEnd = max(groupEnd, o.endTime.date)
        }
        flush()
        return out
    }

    private var taskList: some View {
        EyebrowSection(title: "待办") {
            VStack(spacing: 0) {
                ForEach(Array(tasks.enumerated()), id: \.offset) { i, t in
                    if i > 0 { Rectangle().fill(p.line).frame(height: 1) }
                    HStack(spacing: 12) {
                        Button { Task { await complete(t) } } label: {
                            CheckBox(hot: Calendar.current.isDateInToday(t.dueTime.date) || t.dueTime.date < .now).contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("完成 \(t.title)")
                        Circle().fill(t.space.color(p)).frame(width: 8, height: 8)
                        Text(t.title).font(.ui(15)).foregroundStyle(p.ink).lineLimit(1)
                        Spacer()
                    }
                    .padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                    .background(p.surface)
                    .contentShape(Rectangle())
                    .onTapGesture { router.push(.task(t)) }
                    .deleteMenu { pendingDelete = .task(t) }
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
            .clipShape(RoundedRectangle(cornerRadius: p.radius))
            .overlay(RoundedRectangle(cornerRadius: p.radius).strokeBorder(p.line))
        }
    }

    private func complete(_ t: Noted_V1_Task) async {
        var r = Noted_V1_UpdateTaskRequest()
        r.task = t; r.task.completed = true; r.updateMask = fieldMask("completed")
        do { _ = try await session.api?.calendar.updateTask(r); await load() } catch { self.error = describe(error) }
    }

    private func load() async {
        guard let api = session.api else { return }
        let from: Date, to: Date
        if range == .month, let g = monthGrid.first, let last = monthGrid.last, let end = cal.date(byAdding: .day, value: 1, to: last) {
            from = g; to = end
        } else {
            from = day; to = cal.date(byAdding: .day, value: 1, to: day) ?? day
        }
        var ev = Noted_V1_ListEventsRequest()
        ev.from = .init(from); ev.to = .init(to); ev.space = session.mode.space
        var tk = Noted_V1_ListTasksRequest()
        tk.filter = .open; tk.pageSize = 200; tk.dueBefore = .init(to); tk.space = session.mode.space
        do {
            occurrences = try await api.calendar.listEvents(ev).occurrences
            tasks = try await api.calendar.listTasks(tk).tasks.filter { $0.hasDueTime && $0.dueTime.date >= from }
            error = nil
        } catch { if !(error is CancellationError) { self.error = describe(error) } }
    }
}

// MARK: - event

/// One event on its own page. Every field saves itself a moment after it changes.
struct EventDetailView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var event: Noted_V1_Event
    @State private var saved: Noted_V1_Event
    @State private var projects: [Noted_V1_Project] = []
    @State private var pendingSave: Task<Void, Never>?
    @State private var pendingDelete: PendingDelete?
    @State private var deleted = false
    @State private var error: String?

    private static let dayCodes = ["MO", "TU", "WE", "TH", "FR", "SA", "SU"]
    private static let remindChoices = [-1, 0, 5, 10, 30, 60]

    init(event: Noted_V1_Event) {
        _event = State(initialValue: event)
        _saved = State(initialValue: event)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                topBar
                VStack(spacing: 0) { form }
                    .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                    .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))
                VStack(alignment: .leading, spacing: 6) {
                    Eyebrow(text: "备注")
                    TextEditor(text: $event.description_p).scrollContentBackground(.hidden).frame(minHeight: 120)
                        .font(.ui(15)).foregroundStyle(p.ink).padding(8)
                        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
                        .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line, lineWidth: 1))
                        .accessibilityIdentifier("event-notes")
                }
                ErrorLine(text: error)
            }
            .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 32)
        }
        .scrollIndicators(.hidden)
        .scrollDismissesKeyboard(.interactively)
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .onChange(of: event) { _, _ in scheduleSave() }
        .onDisappear { flush() }
        .confirmDelete($pendingDelete) { err in
            if let err { error = err } else { deleted = true; dismiss() }
        }
    }

    private var topBar: some View {
        HStack(spacing: 10) {
            BackButton(title: "日历")
            Spacer()
            Menu {
                Button(role: .destructive) { pendingDelete = .event(event) } label: { Label("删除", systemImage: "trash") }
            } label: {
                Image(systemName: "ellipsis").font(.system(size: 16, weight: .semibold)).foregroundStyle(p.ink2)
                    .frame(width: 32, height: 28)
            }
            .accessibilityLabel("日程操作")
            .accessibilityIdentifier("event-menu")
        }
    }

    // MARK: form

    @ViewBuilder private var form: some View {
        let parts: DatePickerComponents = event.allDay ? .date : [.date, .hourAndMinute]
        row("标题") {
            TextField("要做什么", text: $event.title).font(.ui(16, .semibold)).foregroundStyle(p.ink)
                .accessibilityIdentifier("event-title")
        }
        divider
        row("开始") { DatePicker("", selection: start, displayedComponents: parts).labelsHidden().environment(\.locale, Fmt.zh) }
        divider
        row("结束") { DatePicker("", selection: end, in: event.startTime.date..., displayedComponents: parts).labelsHidden().environment(\.locale, Fmt.zh) }
        divider
        row("地点") { TextField("无", text: $event.location).font(.ui(15)).foregroundStyle(p.ink) }
        divider
        row("提醒") {
            Picker("", selection: remind) {
                ForEach(remindOptions, id: \.self) { Text(Self.remindLabel($0)).tag($0) }
            }.labelsHidden().accessibilityIdentifier("remind-picker")
            Spacer()
        }
        divider
        row("重复") {
            Picker("", selection: Binding(get: { repeatKind }, set: { setRule($0, weekdays) })) {
                ForEach(CaptureView.Repeat.allCases) { Text($0.rawValue).tag($0) }
            }.labelsHidden().accessibilityIdentifier("repeat-picker")
            Spacer()
        }
        if repeatKind == .weekly { divider; weekdayRow }
        divider
        row("项目") {
            Menu {
                Button { event.projectID = "" } label: { Label("不属于项目", systemImage: event.projectID.isEmpty ? "checkmark" : "") }
                ForEach(projects, id: \.id) { pr in
                    Button { event.projectID = pr.id } label: { Label(pr.title, systemImage: event.projectID == pr.id ? "checkmark" : "") }
                }
            } label: {
                HStack(spacing: 6) {
                    Text(projects.first { $0.id == event.projectID }?.title ?? "无").font(.ui(15))
                        .foregroundStyle(event.projectID.isEmpty ? p.ink3 : p.ink)
                    Image(systemName: "chevron.up.chevron.down").font(.system(size: 11, weight: .semibold)).foregroundStyle(p.ink3)
                }
            }
            .accessibilityIdentifier("project-picker")
            Spacer()
        }
        divider
        row("空间") {
            SpacePicker(selection: Binding(get: { event.space == .work ? .work : .life }, set: { event.space = $0 }))
            Spacer()
        }
    }

    private var divider: some View { Divider().overlay(p.line) }

    private func row<C: View>(_ label: String, @ViewBuilder _ content: () -> C) -> some View {
        HStack(spacing: 10) {
            Text(label).font(.ui(13, .medium)).foregroundStyle(p.ink2).frame(width: 44, alignment: .leading)
            content()
        }
        .padding(.horizontal, 14).frame(minHeight: p.rowHeight)
    }

    private var weekdayRow: some View {
        row("星期") {
            HStack(spacing: 6) {
                ForEach(Array(["一", "二", "三", "四", "五", "六", "日"].enumerated()), id: \.offset) { i, d in
                    let on = weekdays.contains(i + 1)
                    Button {
                        var days = weekdays
                        if on { if days.count > 1 { days.remove(i + 1) } } else { days.insert(i + 1) }
                        setRule(.weekly, days)
                    } label: {
                        Text(d).font(.ui(13, .semibold))
                            .frame(width: 30, height: 30)
                            .background(on ? session.mode.accent(p) : p.surface2, in: Circle())
                            .foregroundStyle(on ? Color.white : p.ink2)
                    }
                    .accessibilityLabel("周\(d)")
                }
            }
            Spacer()
        }
    }

    // MARK: fields

    /// Moving the start keeps the length, so the end moves with it.
    private var start: Binding<Date> {
        Binding(get: { event.startTime.date }, set: { d in
            var e = event
            let length = max(e.endTime.date.timeIntervalSince(e.startTime.date), 60)
            e.startTime = .init(d)
            e.endTime = .init(d.addingTimeInterval(length))
            event = e
        })
    }

    private var end: Binding<Date> {
        Binding(get: { event.endTime.date }, set: { event.endTime = .init(max($0, event.startTime.date)) })
    }

    private var remind: Binding<Int> {
        Binding(get: { event.hasRemindBeforeMinutes ? Int(event.remindBeforeMinutes) : -1 },
                set: { if $0 < 0 { event.clearRemindBeforeMinutes() } else { event.remindBeforeMinutes = Int32($0) } })
    }

    private var remindOptions: [Int] {
        let now = remind.wrappedValue
        return Self.remindChoices.contains(now) ? Self.remindChoices : (Self.remindChoices + [now]).sorted()
    }

    private static func remindLabel(_ m: Int) -> String {
        switch m {
        case ..<0: "不提醒"
        case 0: "开始时"
        case 60: "提前 1 小时"
        default: m % 60 == 0 ? "提前 \(m / 60) 小时" : "提前 \(m) 分钟"
        }
    }

    // MARK: repeat

    /// The rule's parts in order, e.g. FREQ=WEEKLY;BYDAY=MO,WE as [("FREQ", "WEEKLY"), ("BYDAY", "MO,WE")].
    private static func ruleParts(_ rule: String) -> [(String, String)] {
        rule.split(separator: ";").compactMap { part in
            let kv = part.split(separator: "=", maxSplits: 1).map(String.init)
            return kv.count == 2 ? (kv[0].uppercased(), kv[1]) : nil
        }
    }

    private var repeatKind: CaptureView.Repeat {
        switch Self.ruleParts(event.rrule).first(where: { $0.0 == "FREQ" })?.1.uppercased() {
        case "DAILY": .daily
        case "WEEKLY": .weekly
        case "MONTHLY": .monthly
        case "YEARLY": .yearly
        default: .none
        }
    }

    private var startWeekday: Int { (Calendar.current.component(.weekday, from: event.startTime.date) + 5) % 7 + 1 }

    /// 1 = Monday … 7 = Sunday. A weekly rule without days repeats on the start's weekday.
    private var weekdays: Set<Int> {
        let named = (Self.ruleParts(event.rrule).first { $0.0 == "BYDAY" }?.1 ?? "").split(separator: ",")
            .compactMap { d in Self.dayCodes.firstIndex(of: String(d.suffix(2)).uppercased()).map { $0 + 1 } }
        return named.isEmpty ? [startWeekday] : Set(named)
    }

    /// Writes the rule back, keeping the parts this page does not edit (INTERVAL, COUNT, UNTIL).
    private func setRule(_ kind: CaptureView.Repeat, _ days: Set<Int>) {
        let freq: String
        switch kind {
        case .none: event.rrule = ""; return
        case .daily: freq = "DAILY"
        case .weekly: freq = "WEEKLY"
        case .monthly: freq = "MONTHLY"
        case .yearly: freq = "YEARLY"
        }
        var out = ["FREQ=\(freq)"]
        if kind == .weekly {
            out.append("BYDAY=" + (days.isEmpty ? [startWeekday] : days.sorted()).map { Self.dayCodes[$0 - 1] }.joined(separator: ","))
        }
        out += Self.ruleParts(event.rrule).filter { $0.0 != "FREQ" && $0.0 != "BYDAY" }.map { "\($0.0)=\($0.1)" }
        event.rrule = out.joined(separator: ";")
    }

    // MARK: saving

    private func load() async {
        guard let api = session.api else { return }
        projects = (try? await api.projects.listProjects(Noted_V1_ListProjectsRequest()).projects) ?? []
        var g = Noted_V1_GetEventRequest(); g.id = event.id
        do {
            let fresh = try await api.calendar.getEvent(g)
            if event == saved { event = fresh; saved = fresh }
            error = nil
        } catch { if !(error is CancellationError) { self.error = describe(error) } }
    }

    private func scheduleSave() {
        pendingSave?.cancel()
        pendingSave = Task {
            try? await Task.sleep(for: .milliseconds(600))
            if Task.isCancelled { return }
            await persist()
        }
    }

    /// Saves now. Leaving the page calls it, so the last change is not lost.
    private func flush() {
        pendingSave?.cancel()
        Task { await persist() }
    }

    /// Sends only the fields that differ from the server's copy. An emptied title is not sent.
    private func persist() async {
        guard let api = session.api, !deleted, pendingDelete == nil else { return }
        let current = event
        var paths: [String] = []
        if current.title != saved.title && !current.title.trimmingCharacters(in: .whitespaces).isEmpty { paths.append("title") }
        if current.description_p != saved.description_p { paths.append("description") }
        if current.location != saved.location { paths.append("location") }
        if current.startTime != saved.startTime { paths.append("start_time") }
        if current.endTime != saved.endTime { paths.append("end_time") }
        if current.rrule != saved.rrule { paths.append("rrule") }
        if current.hasRemindBeforeMinutes != saved.hasRemindBeforeMinutes || current.remindBeforeMinutes != saved.remindBeforeMinutes {
            paths.append("remind_before_minutes")
        }
        if current.projectID != saved.projectID { paths.append("project_id") }
        if current.space != saved.space { paths.append("space") }
        guard !paths.isEmpty else { return }
        var r = Noted_V1_UpdateEventRequest()
        r.event = current
        r.updateMask.paths = paths
        do {
            _ = try await api.calendar.updateEvent(r)
            var s = current
            if !paths.contains("title") { s.title = saved.title }
            saved = s
            error = nil
            // Reminders, widgets and the watch follow the new time and title.
            Task { await session.syncExtras() }
        } catch { self.error = describe(error) }
    }
}
