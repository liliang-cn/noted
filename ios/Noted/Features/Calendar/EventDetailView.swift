import SwiftProtobuf
import SwiftUI

/// 日程 (route .event): the title and space tag, then 全天 · 开始 · 结束, 地点, 提醒 · 重复,
/// 项目 · 空间 and 备注. Every change saves itself shortly after (masked update).
///
/// Opened on an instance of a recurring event, the times shown are that instance's; changing them
/// moves the whole series by the same amount (the server edits a series as a whole).
struct EventDetailView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router

    let eventID: String
    let occurrence: Date?

    private enum Expanded: Hashable { case startDate, startTime, endDate, endTime }

    @State private var draft: Noted_V1_Event?
    /// The shown instance's start minus the series' start.
    @State private var shift: TimeInterval = 0
    @State private var pending: Set<String> = []
    @State private var saveTask: Task<Void, Never>?
    @State private var expanded: Expanded?
    @State private var deleting: Noted_V1_Event?
    @State private var missing = false

    init(eventID: String, occurrence: Date? = nil) {
        self.eventID = eventID
        self.occurrence = occurrence
    }

    var body: some View {
        Group {
            if let e = draft {
                form(e)
            } else if missing {
                List { Section { EmptyState(text: "日程不存在或已删除") } }
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(Color.groupedBackground)
        .navigationTitle("日程")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let e = draft {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Button(role: .destructive) { deleting = e } label: { Label("删除", systemImage: "trash") }
                            .accessibilityIdentifier("event-more-delete")
                    } label: {
                        Image(systemName: "ellipsis.circle")
                    }
                    .accessibilityLabel("更多")
                    .accessibilityIdentifier("event-more")
                }
            }
        }
        .confirmEventDelete($deleting) {
            saveTask?.cancel()
            pending = []
            router.pop()
        }
        .task { await start() }
        .onDisappear { flushNow() }
    }

    // MARK: form

    private func form(_ e: Noted_V1_Event) -> some View {
        List {
            Section { header(e) }
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets(top: 0, leading: 4, bottom: 4, trailing: 4))

            Section {
                Toggle(isOn: Binding(get: { live.allDay }, set: { setAllDay($0) })) {
                    FieldLabel("全天", icon: "sun.max", tile: .orange)
                }
                .tint(.switchOn)
                .accessibilityIdentifier("event-allday")
                startRows(e)
                endRows(e)
            }

            Section {
                HStack(spacing: 12) {
                    IconTile(systemName: "mappin.and.ellipse", tile: .green)
                    TextField("地点", text: textBinding(\.location, "location"))
                        .accessibilityIdentifier("event-location")
                }
            }

            Section {
                ReminderPicker(lead: Binding(get: { live.reminderLead }, set: { lead in
                    edit(["remind_before_minutes"]) { $0.setReminder(lead) }
                }))
                RepeatPicker(rule: Binding(get: { live.repeatRule }, set: { rule in
                    edit(["rrule"]) { $0.rrule = Self.merge(rrule: $0.rrule, with: rule) }
                }), defaultWeekday: Fmt.calendar.component(.weekday, from: shownStart(e)))
                if let extra = Self.repeatExtras(e.rrule) {
                    Text(extra).font(.footnote).foregroundStyle(.secondaryText)
                }
            }

            Section {
                ProjectPicker(projectID: Binding(get: { live.projectID }, set: { id in
                    edit(["project_id"]) { $0.projectID = id }
                }), space: Binding(get: { live.space }, set: { s in
                    guard s != live.space else { return }
                    edit(["space"]) { $0.space = s }
                }))
                SpaceSegmentRow(space: Binding(get: { live.space }, set: { s in
                    edit(["space"]) { $0.space = s }
                }))
            }

            Section {
                NotesField(text: textBinding(\.description_p, "description"))
                    .accessibilityIdentifier("event-notes")
            }

            Section {
                Button(role: .destructive) { deleting = e } label: {
                    Text("删除日程").frame(maxWidth: .infinity)
                }
                .foregroundStyle(.danger)
                .accessibilityIdentifier("event-delete")
            }
        }
        .listStyle(.insetGrouped)
        .scrollDismissesKeyboard(.interactively)
    }

    private func header(_ e: Noted_V1_Event) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top, spacing: 10) {
                Capsule().fill(e.space.tint).frame(width: 5, height: 28).padding(.top, 5)
                TextField("标题", text: textBinding(\.title, "title"), axis: .vertical)
                    .font(.system(size: 26, weight: .bold))
                    .accessibilityIdentifier("event-title")
            }
            HStack(spacing: 8) {
                SpaceTag(space: e.space)
                if let p = data.project(e.projectID) {
                    Button { router.push(.project(p.id)) } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "folder").font(.system(size: 12))
                            Text(p.title).lineLimit(1)
                            Image(systemName: "chevron.right").font(.system(size: 10, weight: .semibold))
                        }
                    }
                    .buttonStyle(PillButtonStyle(height: 24))
                    .accessibilityLabel("项目 \(p.title)")
                }
                Text(whenText(e))
                    .font(.subheadline)
                    .foregroundStyle(.secondaryText)
                    .lineLimit(1)
                    .monospacedDigit()
            }
        }
        .padding(.top, 4)
    }

    private func whenText(_ e: Noted_V1_Event) -> String {
        let s = shownStart(e), end = shownEnd(e)
        if e.allDay {
            let last = end.addingTimeInterval(-1)
            return last.isSameDay(as: s) ? "\(Fmt.day(s)) 全天" : "\(Fmt.day(s)) – \(Fmt.day(last)) 全天"
        }
        return end.isSameDay(as: s) ? "\(Fmt.day(s)) \(Fmt.hm(s)) – \(Fmt.hm(end))" : "\(Fmt.dayTime(s)) – \(Fmt.dayTime(end))"
    }

    // MARK: 开始 / 结束

    @ViewBuilder
    private func startRows(_ e: Noted_V1_Event) -> some View {
        let s = shownStart(e)
        HStack(spacing: 8) {
            FieldLabel(e.allDay ? "日期" : "开始", icon: "calendar", tile: .red)
            Spacer(minLength: 4)
            pill(Fmt.mdw(s), .startDate, label: "开始日期")
            if !e.allDay { pill(Fmt.hm(s), .startTime, label: "开始时间") }
        }
        if expanded == .startDate {
            DatePicker("", selection: Binding(get: { s }, set: { setStart(day: $0) }), displayedComponents: .date)
                .datePickerStyle(.graphical)
                .labelsHidden()
                .environment(\.locale, Fmt.zh)
                .environment(\.calendar, Fmt.calendar)
        }
        if expanded == .startTime && !e.allDay {
            DatePicker("", selection: Binding(get: { s }, set: { setStart($0) }), displayedComponents: .hourAndMinute)
                .datePickerStyle(.wheel)
                .labelsHidden()
                .frame(maxWidth: .infinity)
                .environment(\.locale, Locale(identifier: "en_GB"))
        }
    }

    @ViewBuilder
    private func endRows(_ e: Noted_V1_Event) -> some View {
        let s = shownStart(e), end = shownEnd(e)
        if e.allDay {
            let last = max(end.addingTimeInterval(-1).startOfDay, s.startOfDay)
            HStack(spacing: 8) {
                FieldLabel("结束", icon: "clock", tile: .blue)
                Spacer(minLength: 4)
                pill(Fmt.mdw(last), .endDate, label: "结束日期")
            }
            if expanded == .endDate {
                DatePicker("", selection: Binding(get: { last }, set: { setAllDayEnd($0) }), in: s.startOfDay..., displayedComponents: .date)
                    .datePickerStyle(.graphical)
                    .labelsHidden()
                    .environment(\.locale, Fmt.zh)
                    .environment(\.calendar, Fmt.calendar)
            }
        } else {
            HStack(spacing: 8) {
                FieldLabel("结束", icon: "clock", tile: .blue)
                Spacer(minLength: 4)
                pill(end.isSameDay(as: s) ? Fmt.hm(end) : "\(Fmt.md(end)) \(Fmt.hm(end))", .endTime, label: "结束时间")
            }
            if expanded == .endTime {
                DatePicker("", selection: Binding(get: { end }, set: { setEnd($0) }), in: s..., displayedComponents: [.date, .hourAndMinute])
                    .datePickerStyle(.wheel)
                    .labelsHidden()
                    .frame(maxWidth: .infinity)
                    .environment(\.locale, Locale(identifier: "en_GB"))
            }
        }
    }

    private func pill(_ text: String, _ which: Expanded, label: String) -> some View {
        Button {
            withAnimation(.snappy) { expanded = expanded == which ? nil : which }
        } label: {
            Text(text).monospacedDigit()
        }
        .buttonStyle(PillButtonStyle(accent: expanded == which))
        .accessibilityLabel("\(label) \(text)")
        .accessibilityIdentifier("event-\(String(describing: which))")
    }

    /// The current draft (bindings read it live: a picker may set twice in one go).
    private var live: Noted_V1_Event { draft ?? Noted_V1_Event() }

    private func shownStart(_ e: Noted_V1_Event) -> Date { e.start.addingTimeInterval(shift) }
    private func shownEnd(_ e: Noted_V1_Event) -> Date { e.end.addingTimeInterval(shift) }

    /// A new start time; the length stays.
    private func setStart(_ newShown: Date) {
        edit(["start_time", "end_time"]) { e in
            let length = e.end.timeIntervalSince(e.start)
            let s = newShown.addingTimeInterval(-shift)
            e.startTime = Google_Protobuf_Timestamp(date: s)
            e.endTime = Google_Protobuf_Timestamp(date: s.addingTimeInterval(length))
        }
    }

    /// A new day from the calendar: keeps the time of day (and the length).
    private func setStart(day: Date) {
        guard let e = draft else { return }
        let cur = shownStart(e)
        let m = cur.minuteOfDay
        setStart(day.startOfDay.at(hour: m / 60, minute: m % 60))
    }

    private func setEnd(_ newShown: Date) {
        guard let e = draft else { return }
        let end = max(newShown, shownStart(e))
        edit(["end_time"]) { $0.endTime = Google_Protobuf_Timestamp(date: end.addingTimeInterval(-shift)) }
    }

    /// The last day of an all-day event (shown inclusive, stored as the next midnight).
    private func setAllDayEnd(_ lastDay: Date) {
        guard let e = draft else { return }
        let last = max(lastDay.startOfDay, shownStart(e).startOfDay)
        edit(["end_time"]) { $0.endTime = Google_Protobuf_Timestamp(date: last.adding(days: 1).addingTimeInterval(-shift)) }
    }

    private func setAllDay(_ on: Bool) {
        guard let e = draft, on != e.allDay else { return }
        let s = shownStart(e)
        let newStart: Date, newEnd: Date
        if on {
            newStart = s.startOfDay
            let days = max(1, Fmt.dayDiff(shownEnd(e).addingTimeInterval(-1), from: s) + 1)
            newEnd = newStart.adding(days: days)
        } else {
            newStart = s.startOfDay.at(hour: 9)
            newEnd = newStart.addingTimeInterval(3600)
        }
        withAnimation(.snappy) {
            expanded = nil
            edit(["all_day", "start_time", "end_time"]) { e in
                e.allDay = on
                e.startTime = Google_Protobuf_Timestamp(date: newStart.addingTimeInterval(-shift))
                e.endTime = Google_Protobuf_Timestamp(date: newEnd.addingTimeInterval(-shift))
            }
        }
    }

    // MARK: 重复

    /// The new FREQ/BYDAY with the old rule's INTERVAL, COUNT and UNTIL kept.
    static func merge(rrule old: String, with rule: RepeatRule) -> String {
        let base = rule.rrule
        guard !base.isEmpty else { return "" }
        let kept = parts(old).filter { ["INTERVAL", "COUNT", "UNTIL"].contains($0.key) }
        return ([base] + kept.map { "\($0.key)=\($0.value)" }).joined(separator: ";")
    }

    private static func parts(_ rrule: String) -> [(key: String, value: String)] {
        rrule.uppercased().replacingOccurrences(of: "RRULE:", with: "").split(separator: ";").compactMap { p in
            let kv = p.split(separator: "=", maxSplits: 1)
            return kv.count == 2 ? (String(kv[0]), String(kv[1])) : nil
        }
    }

    /// "每 2 周 · 共 10 次 · 至 12月31日" for what the 重复 menu does not show.
    static func repeatExtras(_ rrule: String) -> String? {
        let p = Dictionary(parts(rrule).map { ($0.key, $0.value) }, uniquingKeysWith: { a, _ in a })
        var out: [String] = []
        if let i = p["INTERVAL"].flatMap(Int.init), i > 1 {
            let unit = ["DAILY": "天", "WEEKLY": "周", "MONTHLY": "个月", "YEARLY": "年"][p["FREQ"] ?? ""] ?? ""
            out.append("每 \(i) \(unit)")
        }
        if let c = p["COUNT"].flatMap(Int.init) { out.append("共 \(c) 次") }
        if let u = p["UNTIL"], let d = untilDate(u) { out.append("至 \(Fmt.md(d))") }
        return out.isEmpty ? nil : out.joined(separator: " · ")
    }

    private static func untilDate(_ s: String) -> Date? {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        for fmt in ["yyyyMMdd'T'HHmmss'Z'", "yyyyMMdd'T'HHmmss", "yyyyMMdd"] {
            f.dateFormat = fmt
            f.timeZone = fmt.hasSuffix("'Z'") ? TimeZone(identifier: "UTC") : .current
            if let d = f.date(from: s) { return d }
        }
        return nil
    }

    // MARK: saving

    private func textBinding(_ kp: WritableKeyPath<Noted_V1_Event, String>, _ field: String) -> Binding<String> {
        Binding(get: { draft?[keyPath: kp] ?? "" }, set: { v in
            guard draft?[keyPath: kp] != v else { return }
            edit([field], delay: 0.8) { $0[keyPath: kp] = v }
        })
    }

    /// Changes the draft and saves the named fields after `delay` without further changes.
    private func edit(_ fields: [String], delay: Double = 0.35, _ change: (inout Noted_V1_Event) -> Void) {
        guard var e = draft else { return }
        change(&e)
        draft = e
        pending.formUnion(fields)
        saveTask?.cancel()
        saveTask = Task {
            try? await Task.sleep(for: .seconds(delay))
            guard !Task.isCancelled else { return }
            await save()
        }
    }

    private func takePending() -> (Noted_V1_Event, [String])? {
        guard var e = draft, !pending.isEmpty else { return nil }
        var fields = pending
        if e.title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { fields.remove("title") }
        guard !fields.isEmpty else { return nil }
        pending.subtract(fields)
        if e.timeZone.isEmpty { e.timeZone = TimeZone.current.identifier }
        return (e, Array(fields))
    }

    private func save() async {
        guard let p = takePending() else { return }
        if await data.updateEvent(p.0, fields: p.1) == nil, pending.isEmpty, let server = data.event(eventID) {
            draft = server
        }
    }

    /// Leaving the page: save what is still waiting.
    private func flushNow() {
        saveTask?.cancel()
        guard let p = takePending() else { return }
        let data = data
        Task { await data.updateEvent(p.0, fields: p.1) }
    }

    private func start() async {
        if draft == nil, let e = data.event(eventID) { adopt(e) }
        if draft == nil {
            if let e = await data.loadEvent(eventID) { adopt(e) } else { missing = true }
        }
    }

    private func adopt(_ e: Noted_V1_Event) {
        draft = e
        shift = occurrence.map { $0.timeIntervalSince(e.start) } ?? 0
    }
}
