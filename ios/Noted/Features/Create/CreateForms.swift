import SwiftUI

// The sections under the title on 新建, one set per kind. They edit the kind's draft in place.

// MARK: - 待办

struct TodoFormSections: View {
    @Binding var draft: CreateDraft
    /// 选择… opened the calendar.
    @Binding var pickDay: Bool

    var body: some View {
        Section {
            DateToggleRows(date: $draft.due, expanded: $pickDay)
            if let due = draft.due {
                TimeToggleRows(date: Binding(get: { draft.due ?? due }, set: { draft.due = $0 }))
                // A reminder needs a clock time to lead from.
                if due.minuteOfDay != 0 { ReminderPicker(lead: $draft.remind) }
            }
            // Repeating needs a date to count from.
            RepeatPicker(rule: $draft.repeatRule, defaultWeekday: Fmt.calendar.component(.weekday, from: draft.due ?? .now))
                .disabled(draft.due == nil)
        }
        Section {
            ProjectPicker(projectID: $draft.projectID, space: $draft.space)
            SpaceSegmentRow(space: $draft.space)
            PriorityPicker(priority: $draft.priority)
        }
        Section { NotesField(text: $draft.notes) }
    }
}

// MARK: - 日程

struct EventFormSections: View {
    @Binding var draft: CreateDraft
    @Binding var pickDay: Bool

    var body: some View {
        Section {
            Toggle(isOn: $draft.allDay.animation(.snappy)) { FieldLabel("全天", icon: "sun.max", tile: .orange) }
                .tint(.switchOn)
            HStack {
                FieldLabel(draft.allDay ? "日期" : "开始", icon: "calendar", tile: .red)
                Spacer(minLength: 8)
                DatePicker("开始", selection: start, displayedComponents: draft.allDay ? [.date] : [.date, .hourAndMinute])
                    .labelsHidden()
                    .environment(\.locale, Fmt.zh)
            }
            if pickDay {
                DatePicker("日期", selection: day, displayedComponents: .date)
                    .datePickerStyle(.graphical)
                    .labelsHidden()
                    .environment(\.locale, Fmt.zh)
                    .environment(\.calendar, Fmt.calendar)
            }
            if !draft.allDay {
                HStack {
                    FieldLabel("结束", icon: "clock", tile: .blue)
                    Spacer(minLength: 8)
                    DatePicker("结束", selection: endTime, displayedComponents: .hourAndMinute)
                        .labelsHidden()
                        .environment(\.locale, Fmt.zh)
                }
            }
        }
        Section {
            HStack(spacing: 12) {
                IconTile(systemName: "mappin", tile: .green)
                TextField("地点", text: $draft.location)
                    .alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
            }
        }
        Section {
            ReminderPicker(lead: $draft.remind)
            RepeatPicker(rule: $draft.repeatRule, defaultWeekday: Fmt.calendar.component(.weekday, from: draft.start))
        }
        Section {
            ProjectPicker(projectID: $draft.projectID, space: $draft.space)
            SpaceSegmentRow(space: $draft.space)
        }
        Section { NotesField(text: $draft.notes) }
    }

    /// A new start keeps the length.
    private var start: Binding<Date> {
        Binding(get: { draft.start }, set: { new in
            let length = max(draft.end.timeIntervalSince(draft.start), 15 * 60)
            draft.start = new
            draft.end = new.addingTimeInterval(length)
        })
    }

    /// The calendar under 开始 moves the day only.
    private var day: Binding<Date> {
        Binding(get: { draft.start }, set: { new in
            let m = draft.start.minuteOfDay
            start.wrappedValue = new.startOfDay.at(hour: m / 60, minute: m % 60)
            withAnimation(.snappy) { pickDay = false }
        })
    }

    /// 结束 is a time on the start's day; earlier than the start means the next day.
    private var endTime: Binding<Date> {
        Binding(get: { draft.end }, set: { new in
            let m = new.minuteOfDay
            var e = draft.start.startOfDay.at(hour: m / 60, minute: m % 60)
            if e <= draft.start { e = e.adding(days: 1) }
            draft.end = e
        })
    }
}

// MARK: - 项目

struct ProjectFormSections: View {
    @Binding var draft: CreateDraft

    var body: some View {
        Section {
            HStack {
                FieldLabel("开始", icon: "calendar", tile: .red)
                Spacer(minLength: 8)
                DatePicker("开始", selection: Binding(get: { draft.projectStart }, set: { new in
                    draft.projectStart = new.startOfDay
                    if draft.projectDue < draft.projectStart { draft.projectDue = draft.projectStart }
                }), displayedComponents: .date)
                .labelsHidden()
                .environment(\.locale, Fmt.zh)
            }
            HStack {
                FieldLabel("截止", icon: "flag", tile: .rust)
                Spacer(minLength: 8)
                let n = Fmt.dayDiff(draft.projectDue)
                if n >= 0 { Text("\(n) 天后").foregroundStyle(.secondaryText).monospacedDigit() }
                DatePicker("截止", selection: Binding(get: { draft.projectDue }, set: { draft.projectDue = $0.startOfDay }),
                           in: draft.projectStart..., displayedComponents: .date)
                .labelsHidden()
                .environment(\.locale, Fmt.zh)
            }
        }
        Section {
            SpaceSegmentRow(space: $draft.space)
            Toggle(isOn: $draft.pinned) { FieldLabel("置顶", icon: "pin", tile: .orange) }
                .tint(.switchOn)
        }
        Section { NotesField(text: $draft.notes) }
    }
}

// MARK: - 目标

struct GoalFormSections: View {
    @Binding var draft: CreateDraft
    let addDimension: () -> Void

    var body: some View {
        if draft.dimensions.isEmpty {
            Section {
                HStack {
                    FieldLabel("周期", icon: "repeat", tile: .gray)
                    Spacer(minLength: 8)
                    PeriodSegment(period: $draft.period).frame(width: 168)
                }
                TargetRow(period: draft.period, unit: draft.unitLabel, step: draft.goalUnit.step, text: $draft.target)
                Picker(selection: Binding(get: { draft.goalUnit }, set: { setUnit($0) })) {
                    ForEach(GoalUnit.allCases) { Text($0.label).tag($0) }
                } label: {
                    FieldLabel("单位", icon: "ruler", tile: .orange)
                }
                .pickerStyle(.menu)
                if draft.goalUnit == .custom {
                    SubFieldRow(title: "名称", placeholder: "如 杯", text: $draft.customUnit)
                }
            }
            Section {
                Toggle(isOn: $draft.hasCounter.animation(.snappy)) {
                    FieldLabel("累计总量", icon: "chart.bar.fill", tile: .teal)
                }
                .tint(.switchOn)
                if draft.hasCounter {
                    TargetRow(label: "总数", step: 5, text: $draft.counterTarget)
                    SubFieldRow(title: "单位", placeholder: "课时", text: $draft.counterUnit)
                }
            }
        }
        Section {
            ForEach(draft.dimensions) { d in
                HStack {
                    Text(d.title).lineLimit(1)
                    Spacer(minLength: 8)
                    Text(d.summary).foregroundStyle(.secondaryText).monospacedDigit()
                }
                .swipeActions {
                    Button(role: .destructive) {
                        withAnimation { draft.dimensions.removeAll { $0.id == d.id } }
                    } label: { Label("删除", systemImage: "trash") }
                }
            }
            Button(action: addDimension) {
                Label("添加维度", systemImage: "plus").frame(maxWidth: .infinity)
            }
            .accessibilityIdentifier("create-add-dimension")
        } header: {
            if !draft.dimensions.isEmpty { Text("维度") }
        }
        Section {
            Toggle(isOn: $draft.hasMetric.animation(.snappy)) {
                FieldLabel("追踪指标", icon: "chart.xyaxis.line", tile: .green)
            }
            .tint(.switchOn)
            if draft.hasMetric {
                SubFieldRow(title: "名称", placeholder: "体重", text: $draft.metricName)
                SubFieldRow(title: "单位", placeholder: "kg", text: $draft.metricUnit)
                SubFieldRow(title: "当前", placeholder: "80", text: $draft.metricStart, decimal: true)
                SubFieldRow(title: "目标", placeholder: "75", text: $draft.metricTarget, decimal: true)
            }
        }
        Section {
            ProjectPicker(projectID: $draft.projectID, space: $draft.space)
            SpaceSegmentRow(space: $draft.space)
        }
    }

    private func setUnit(_ u: GoalUnit) {
        let n = draft.target.parsedNumber ?? 0
        if u == .minutes && n < 5 { draft.target = "30" }
        if draft.goalUnit == .minutes && u != .minutes && n > 20 { draft.target = "3" }
        draft.goalUnit = u
    }
}

/// 每天 / 每周 / 每月
struct PeriodSegment: View {
    @Binding var period: Noted_V1_GoalPeriod
    var body: some View {
        Picker("周期", selection: $period) {
            Text("每天").tag(Noted_V1_GoalPeriod.day)
            Text("每周").tag(Noted_V1_GoalPeriod.week)
            Text("每月").tag(Noted_V1_GoalPeriod.month)
        }
        .pickerStyle(.segmented)
        .labelsHidden()
    }
}

/// "每周 [3] 次" with − / +; the number can also be typed.
struct TargetRow: View {
    var period: Noted_V1_GoalPeriod?
    var unit = ""
    var label: String?
    var step: Double = 1
    @Binding var text: String

    init(period: Noted_V1_GoalPeriod, unit: String, step: Double, text: Binding<String>) {
        self.period = period; self.unit = unit; self.step = step; _text = text
    }
    init(label: String, step: Double, text: Binding<String>) {
        self.label = label; self.step = step; _text = text
    }

    var body: some View {
        HStack(spacing: 12) {
            if let period {
                IconTile(systemName: "target", tile: .blue)
                Text(period.every).alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
            } else {
                Color.clear.frame(width: 30, height: 1)
                Text(label ?? "").alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
            }
            TextField("0", text: $text)
                .keyboardType(.decimalPad)
                .font(.body.weight(.semibold).monospacedDigit())
                .fixedSize()
                .accessibilityLabel(label ?? "目标数")
            if !unit.isEmpty { Text(unit) }
            Spacer(minLength: 8)
            CompactStepper(onMinus: { bump(-1) }, onPlus: { bump(1) })
        }
    }

    private func bump(_ dir: Double) {
        let n = text.parsedNumber ?? 0
        let next = dir > 0 ? (n / step).rounded(.down) * step + step : (n / step).rounded(.up) * step - step
        text = Fmt.number(max(step, next))
    }
}

/// An indented row under a switch: a label and a field on the right.
struct SubFieldRow: View {
    let title: String
    let placeholder: String
    @Binding var text: String
    var decimal = false

    var body: some View {
        HStack(spacing: 12) {
            Color.clear.frame(width: 30, height: 1)
            Text(title).alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
            TextField(placeholder, text: $text)
                .multilineTextAlignment(.trailing)
                .keyboardType(decimal ? .decimalPad : .default)
                .accessibilityLabel(title)
        }
    }
}

// MARK: - 添加维度

/// One step: name, period, target and unit of a dimension.
struct DimensionSheet: View {
    @Environment(\.dismiss) private var dismiss
    let onAdd: (CreateDraft.Dimension) -> Void

    @State private var title = ""
    @State private var period = Noted_V1_GoalPeriod.week
    @State private var target = "2"
    @State private var unit = GoalUnit.times
    @State private var customUnit = ""
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextField("如 游泳", text: $title)
                        .font(.system(size: 22, weight: .semibold))
                        .focused($focused)
                        .submitLabel(.done)
                        .onSubmit(add)
                }
                Section {
                    HStack {
                        FieldLabel("周期", icon: "repeat", tile: .gray)
                        Spacer(minLength: 8)
                        PeriodSegment(period: $period).frame(width: 168)
                    }
                    TargetRow(period: period, unit: unitLabel, step: unit.step, text: $target)
                    Picker(selection: $unit) {
                        ForEach(GoalUnit.allCases) { Text($0.label).tag($0) }
                    } label: {
                        FieldLabel("单位", icon: "ruler", tile: .orange)
                    }
                    .pickerStyle(.menu)
                    if unit == .custom { SubFieldRow(title: "名称", placeholder: "如 杯", text: $customUnit) }
                }
            }
            .navigationTitle("添加维度")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("添加", action: add)
                        .disabled(!valid)
                        .accessibilityIdentifier("dimension-add")
                }
            }
            .onAppear { focused = true }
        }
        .presentationDetents([.medium, .large])
    }

    private var unitLabel: String { unit == .custom ? (customUnit.trimmedText.isEmpty ? "单位" : customUnit.trimmedText) : unit.label }
    private var valid: Bool {
        !title.trimmedText.isEmpty && (target.parsedNumber ?? 0) > 0 && (unit != .custom || !customUnit.trimmedText.isEmpty)
    }

    private func add() {
        guard valid else { return }
        onAdd(.init(title: title.trimmedText, periodRaw: period.rawValue, target: target.parsedNumber ?? 1,
                    unit: unit == .custom ? customUnit.trimmedText : unit.rawValue))
        dismiss()
    }
}
