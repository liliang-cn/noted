import SwiftUI

struct CalendarView: View {
    enum Range: String, CaseIterable { case month = "月", week = "周", day = "日" }

    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @State private var day = Calendar.current.startOfDay(for: .now)
    @State private var range: Range = .week
    @State private var occurrences: [Noted_V1_Occurrence] = []
    @State private var tasks: [Noted_V1_Task] = []
    @State private var error: String?

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
        Screen(eyebrow: "\(cal.component(.year, from: day))", title: "\(cal.component(.month, from: day))月", trailing: AnyView(header)) {
            HStack(spacing: 4) {
                ForEach(Range.allCases, id: \.self) { r in
                    Button { range = r } label: {
                        Text(r.rawValue).font(.system(size: 14, weight: .semibold)).frame(maxWidth: .infinity).frame(height: 34)
                            .foregroundStyle(range == r ? p.bg : p.ink2)
                            .background(range == r ? p.ink : .clear, in: RoundedRectangle(cornerRadius: 9))
                    }
                }
            }
            .padding(3).background(p.surface, in: RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12).stroke(p.line))

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
    }

    private var header: some View {
        HStack(spacing: 8) {
            Button { day = cal.startOfDay(for: .now) } label: {
                Text("今天").font(.system(size: 13, weight: .semibold)).foregroundStyle(p.ink).padding(.horizontal, 12).frame(height: 34)
                    .background(p.surface, in: RoundedRectangle(cornerRadius: 9)).overlay(RoundedRectangle(cornerRadius: 9).stroke(p.line))
            }
            ModeSwitch(mini: true)
        }
    }

    private var weekStrip: some View {
        HStack(spacing: 0) {
            ForEach(weekDays, id: \.self) { d in
                let on = cal.isDate(d, inSameDayAs: day)
                Button { day = d } label: {
                    VStack(spacing: 4) {
                        Text(d.formatted(.dateTime.weekday(.narrow).locale(Fmt.zh))).font(.system(size: 11)).foregroundStyle(on ? p.bg : p.ink3)
                        Text("\(cal.component(.day, from: d))").font(.system(size: 16, weight: .semibold, design: .monospaced))
                            .foregroundStyle(on ? p.bg : p.ink)
                    }
                    .frame(maxWidth: .infinity).frame(height: 52)
                    .background(on ? p.ink : .clear, in: RoundedRectangle(cornerRadius: 10))
                }
            }
        }
        .padding(4)
    }

    private var dayStepper: some View {
        HStack {
            Button { day = cal.date(byAdding: .day, value: -1, to: day) ?? day } label: { Image(systemName: "chevron.left") }.accessibilityLabel("前一天")
            Spacer()
            Text("\(Fmt.md(day)) · \(day.formatted(.dateTime.weekday(.wide).locale(Fmt.zh)))").font(.system(size: 15, weight: .semibold)).foregroundStyle(p.ink)
            Spacer()
            Button { day = cal.date(byAdding: .day, value: 1, to: day) ?? day } label: { Image(systemName: "chevron.right") }.accessibilityLabel("后一天")
        }
        .foregroundStyle(p.ink2).padding(.horizontal, 8).frame(height: 40)
    }

    // MARK: month

    private var monthView: some View {
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                ForEach(["一", "二", "三", "四", "五", "六", "日"], id: \.self) {
                    Text($0).font(.system(size: 11)).foregroundStyle(p.ink3).frame(maxWidth: .infinity)
                }
            }.padding(.vertical, 8)
            let grid = monthGrid
            ForEach(0..<(grid.count / 7), id: \.self) { w in
                HStack(spacing: 0) {
                    ForEach(grid[(w * 7)..<(w * 7 + 7)], id: \.self) { d in monthCell(d) }
                }
            }
        }
        .padding(.horizontal, 6).padding(.bottom, 6)
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
        .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
    }

    private func monthCell(_ d: Date) -> some View {
        let inMonth = cal.isDate(d, equalTo: day, toGranularity: .month)
        let today = cal.isDateInToday(d)
        let spaces = Array(Set(occurrences.filter { cal.isDate($0.startTime.date, inSameDayAs: d) }.map(\.event.space.rawValue))).sorted()
        return Button { day = d; range = .day } label: {
            VStack(spacing: 5) {
                Text("\(cal.component(.day, from: d))").font(.system(size: 15, weight: today ? .bold : .medium, design: .monospaced))
                    .foregroundStyle(today ? p.bg : (inMonth ? p.ink : p.ink3))
                    .frame(width: 30, height: 30).background(today ? p.ink : .clear, in: Circle())
                HStack(spacing: 3) {
                    ForEach(spaces, id: \.self) { s in Circle().fill((Noted_V1_Space(rawValue: s) ?? .life).color(p)).frame(width: 5, height: 5) }
                }.frame(height: 5)
            }
            .frame(maxWidth: .infinity).frame(height: 52).contentShape(Rectangle())
        }
        .accessibilityLabel("\(Fmt.md(d))")
    }

    // MARK: day grid

    private var hours: ClosedRange<Int> {
        var lo = 7, hi = 21
        for o in occurrences where !o.event.allDay {
            lo = min(lo, cal.component(.hour, from: o.startTime.date))
            hi = max(hi, cal.component(.hour, from: o.endTime.date))
        }
        return lo...min(hi, 23)
    }

    private var dayGrid: some View {
        let hs = hours
        let timed = occurrences.filter { !$0.event.allDay }
        let allDay = occurrences.filter(\.event.allDay)
        return VStack(alignment: .leading, spacing: 0) {
            ForEach(allDay, id: \.event.id) { o in
                Text(o.event.title).font(.system(size: 12, weight: .semibold)).foregroundStyle(o.event.space.color(p))
                    .padding(.horizontal, 10).frame(maxWidth: .infinity, minHeight: 26, alignment: .leading)
                    .background(o.event.space == .work ? p.workBg : p.lifeBg).padding(.horizontal, 8).padding(.top, 6)
            }
            ZStack(alignment: .topLeading) {
                VStack(spacing: 0) {
                    ForEach(Array(hs), id: \.self) { h in
                        HStack(alignment: .top, spacing: 8) {
                            Text(String(format: "%02d", h)).font(.system(size: 11, design: .monospaced)).foregroundStyle(p.ink3).frame(width: 24, alignment: .trailing)
                            Rectangle().fill(p.line).frame(height: 1)
                        }.frame(height: Self.hourHeight, alignment: .top)
                    }
                }
                ForEach(layout(timed), id: \.0.event.id) { o, col, cols in block(o, col, cols, first: hs.lowerBound) }
                if cal.isDateInToday(day) { nowLine(first: hs.lowerBound, last: hs.upperBound) }
            }
            .padding(.top, 8).padding(.horizontal, 8).padding(.bottom, 8)
            if occurrences.isEmpty { Text("这天没有日程").font(.system(size: 13)).foregroundStyle(p.ink3).frame(maxWidth: .infinity).padding(.bottom, 14) }
        }
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
        .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
    }

    private func y(_ d: Date, first: Int) -> CGFloat {
        let m = cal.component(.hour, from: d) * 60 + cal.component(.minute, from: d) - first * 60
        return CGFloat(m) / 60 * Self.hourHeight
    }

    private func block(_ o: Noted_V1_Occurrence, _ col: Int, _ cols: Int, first: Int) -> some View {
        let top = y(o.startTime.date, first: first)
        let h = max(24, y(o.endTime.date, first: first) - top - 2)
        let color = o.event.space.color(p)
        return GeometryReader { g in
            let w = (g.size.width - 32) / CGFloat(cols)
            VStack(alignment: .leading, spacing: 1) {
                Text(o.event.title).font(.system(size: 12, weight: .semibold)).lineLimit(1)
                if h > 40, !o.event.location.isEmpty { Text(o.event.location).font(.system(size: 11)).lineLimit(1) }
            }
            .foregroundStyle(color).padding(.horizontal, 8).padding(.vertical, 4)
            .frame(width: w - 2, height: h, alignment: .topLeading)
            .background(o.event.space == .work ? p.workBg : p.lifeBg)
            .overlay(alignment: .leading) { Rectangle().fill(color).frame(width: 3) }
            .offset(x: 32 + w * CGFloat(col), y: top)
        }
    }

    private func nowLine(first: Int, last: Int) -> some View {
        let t = y(.now, first: first)
        return Group {
            if t >= 0 && t <= CGFloat(last - first + 1) * Self.hourHeight {
                HStack(spacing: 0) {
                    Circle().fill(p.bad).frame(width: 8, height: 8)
                    Rectangle().fill(p.bad).frame(height: 1)
                }.padding(.leading, 28).offset(y: t - 4)
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
        VStack(alignment: .leading, spacing: 8) {
            Eyebrow(text: "待办")
            VStack(spacing: 0) {
                ForEach(Array(tasks.enumerated()), id: \.offset) { i, t in
                    if i > 0 { Divider().overlay(p.line) }
                    HStack(spacing: 12) {
                        RoundedRectangle(cornerRadius: 6).strokeBorder(p.ink3, lineWidth: 1.75).frame(width: 20, height: 20)
                            .frame(width: 46, alignment: .leading)
                        Circle().fill(t.space.color(p)).frame(width: 8, height: 8)
                        Text(t.title).font(.system(size: 15, weight: .medium)).foregroundStyle(p.ink)
                        Spacer()
                    }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
            .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
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
