import Charts
import SwiftUI

/// A larger goal (减肥): the ring over its 维度 with 打卡, streak / month stats, the 维度 list with
/// their own check-ins and "+ 添加维度", the 里程碑 to tick, the 指标 card with a chart and
/// "+ 记一次体重", and the readings.
struct ObjectiveDetailView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let objectiveID: String

    @State private var chooseDim = false
    @State private var addDim = false
    @State private var addReading = false
    @State private var addMetric = false
    @State private var addMilestone = false
    @State private var minutesFor: MinutesTarget?
    @State private var askDelete = false
    @State private var deletingDim: Noted_V1_Goal?
    @State private var loaded = false

    var body: some View {
        Group {
            if let o = data.objective(objectiveID) {
                content(o)
            } else if loaded {
                List { Section { EmptyState(text: "目标已删除") } }
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle(data.objective(objectiveID)?.title ?? "目标")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            await load()
            loaded = true
        }
        .reloadOnReturn {
            await data.refreshGoals()
            await load()
        }
    }

    private func load() async {
        await data.loadMeasurements(objectiveID)
        for g in data.objective(objectiveID)?.goals ?? [] { await data.loadCheckIns(g.id, limit: 200) }
    }

    // MARK: content

    private func content(_ o: Noted_V1_Objective) -> some View {
        let dims = o.goals
        let done = dims.reduce(0.0) { $0 + min($1.done, $1.goalTarget) }
        let total = dims.reduce(0.0) { $0 + $1.goalTarget }
        let achieved = total > 0 && done >= total
        let monthStart = Date.now.startOfMonth
        let month = dims.reduce(0.0) { sum, g in
            sum + (data.checkIns[g.id] ?? []).filter { $0.time.date >= monthStart }.reduce(0.0) { $0 + $1.amount }
        }
        let readings = data.measurements[objectiveID] ?? []
        return List {
            Section {
                PlanDetailHeader(title: o.title, subtitle: subtitle(o)) {
                    SpaceTag(space: o.space)
                    if !o.projectID.isEmpty { PlanProjectChip(projectID: o.projectID) }
                }
            }
            if !dims.isEmpty {
                Section {
                    PlanRingCard(
                        fraction: total > 0 ? done / total : 0,
                        done: achieved,
                        value: Fmt.number(done),
                        total: Fmt.number(total),
                        caption: dims.first?.period.current ?? "本周",
                        buttonTitle: achieved ? "本期已达成 · 再记一次" : "打卡",
                        buttonIcon: achieved ? "checkmark" : "plus",
                        days: PlanSevenDayDots.days(from: dims.map(\.progress.recent))
                    ) {
                        if dims.count == 1, let g = dims.first { tapDim(g) } else { chooseDim = true }
                    }
                    .planCardRow(bottom: 0)
                }
                Section {
                    HStack(spacing: 12) {
                        PlanStatTile(title: "连续达成") {
                            HStack(alignment: .firstTextBaseline, spacing: 4) {
                                Image(systemName: "flame.fill").font(.system(size: 17)).foregroundStyle(.warning)
                                Text("\(o.streak)").font(.system(size: 22, weight: .bold)).foregroundStyle(.warning).monospacedDigit()
                                Text(dims.first?.period.unitName ?? "周").font(.subheadline).foregroundStyle(.secondaryText)
                            }
                        }
                        PlanStatTile(title: "本月") {
                            HStack(alignment: .firstTextBaseline, spacing: 4) {
                                Text(Fmt.number(month)).font(.system(size: 22, weight: .bold)).monospacedDigit()
                                Text("次").font(.subheadline).foregroundStyle(.secondaryText)
                            }
                        }
                    }
                    .planCardRow(bottom: 0)
                }
            }

            Section {
                ForEach(o.milestones, id: \.id) { m in
                    MilestoneRow(milestone: m) {
                        Task { await data.setObjectiveMilestoneDone(objectiveID, milestoneID: m.id, done: !m.done) }
                    }
                    .swipeActions(edge: .trailing) {
                        Button { removeMilestone(m, from: o) } label: { Label("删除", systemImage: "trash") }.tint(.danger)
                    }
                }
                PlanAddRow(title: "添加里程碑", systemImage: "flag") { addMilestone = true }
                    .accessibilityIdentifier("objective-add-milestone")
            } header: {
                Text(o.milestones.isEmpty ? "里程碑" : "里程碑 \(o.milestonesSummary)")
            }

            Section {
                ForEach(dims, id: \.id) { g in
                    DimensionRow(goal: g) { tapDim(g) }
                        .swipeActions(edge: .trailing) {
                            Button { deletingDim = g } label: { Label("删除", systemImage: "trash") }.tint(.danger)
                        }
                }
                PlanAddRow(title: "添加维度") { addDim = true }
                    .accessibilityIdentifier("objective-add-dimension")
            } header: { Text("维度") }

            if o.hasMetric {
                Section {
                    MetricCard(objective: o, readings: readings) { addReading = true }
                        .planCardRow(bottom: 0)
                } header: { Text("指标") }
                if !readings.isEmpty {
                    Section {
                        ForEach(Array(readings.enumerated().reversed()), id: \.element.id) { i, r in
                            ReadingRow(objective: o, reading: r, previous: i > 0 ? readings[i - 1] : nil)
                                .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                                    Button(role: .destructive) {
                                        Task { if await data.deleteMeasurement(r.id, objectiveID: objectiveID) { toaster.show("已删除", systemImage: "trash") } }
                                    } label: { Label("删除", systemImage: "trash") }
                                }
                        }
                    } header: { Text("记录") }
                }
            } else {
                Section {
                    PlanAddRow(title: "添加指标", systemImage: "chart.xyaxis.line") { addMetric = true }
                        .accessibilityIdentifier("objective-add-metric")
                }
            }
        }
        .listStyle(.insetGrouped)
        .refreshable {
            await data.refreshGoals()
            await load()
        }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Button(role: .destructive) { askDelete = true } label: { Label("删除目标", systemImage: "trash") }
                } label: { Image(systemName: "ellipsis.circle") }
                .accessibilityLabel("更多")
                .accessibilityIdentifier("objective-more")
            }
        }
        .confirmationDialog("\(o.title) · 打卡", isPresented: $chooseDim, titleVisibility: .visible) {
            ForEach(dims, id: \.id) { g in
                Button(g.isMinutes ? "\(g.title) 记录时长　（\(Fmt.number(g.done))/\(Fmt.number(g.goalTarget))）"
                                   : "\(g.title) +1　（\(Fmt.number(g.done))/\(Fmt.number(g.goalTarget))）") { tapDim(g) }
            }
            Button("取消", role: .cancel) {}
        }
        .confirmationDialog("删除目标？", isPresented: $askDelete, titleVisibility: .visible) {
            Button("删除目标", role: .destructive) {
                Task {
                    if await data.deleteObjective(objectiveID) {
                        router.pop()
                        toaster.show("已删除", systemImage: "trash")
                    }
                }
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text(dims.isEmpty ? "“\(o.title)”和它的记录会被删除" : "“\(o.title)”的维度会保留为独立目标")
        }
        .confirmationDialog("删除维度？", isPresented: Binding(get: { deletingDim != nil }, set: { if !$0 { deletingDim = nil } }),
                            titleVisibility: .visible, presenting: deletingDim) { g in
            Button("删除维度", role: .destructive) {
                Task { if await data.deleteGoal(g.id) { toaster.show("已删除", systemImage: "trash") } }
            }
            Button("取消", role: .cancel) {}
        } message: { g in
            Text("“\(g.title)”的打卡记录会一起删除")
        }
        .sheet(isPresented: $addDim) {
            AddDimensionSheet { title, period, target in
                Task {
                    var g = Noted_V1_Goal()
                    g.title = title; g.period = period; g.target = Double(target); g.unit = "times"
                    g.objectiveID = objectiveID; g.space = o.space
                    if await data.createGoal(g) != nil { toaster.show("已添加维度") }
                }
            }
        }
        .sheet(isPresented: $addReading) {
            PlanNumberSheet(title: "记一次\(o.metricName.isEmpty ? "指标" : o.metricName)",
                            caption: "\(o.metricUnit) · 今天",
                            initial: readings.last?.value ?? (o.metricStart > 0 ? o.metricStart : nil)) { v in
                Task {
                    if await data.recordMeasurement(objectiveID, value: v) != nil {
                        toaster.show("已记录 \(PlanFmt.value(v)) \(o.metricUnit)")
                    }
                }
            }
        }
        .sheet(isPresented: $addMilestone) {
            AddMilestoneSheet { title, due, criteria in
                Task {
                    var m = Noted_V1_Milestone()
                    m.title = title; m.criteria = criteria
                    if let due { m.dueTime = .init(date: due) }
                    var next = o
                    next.milestones.append(m)
                    if await data.updateObjective(next, fields: ["milestones"]) != nil { toaster.show("已添加里程碑") }
                }
            }
        }
        .sheet(isPresented: $addMetric) {
            AddMetricSheet { name, unit, start, target in
                Task {
                    var m = o
                    m.metricName = name; m.metricUnit = unit; m.metricStart = start; m.metricTarget = target
                    guard await data.updateObjective(m, fields: ["metric_name", "metric_unit", "metric_start", "metric_target"]) != nil else { return }
                    _ = await data.recordMeasurement(objectiveID, value: start)
                    toaster.show("已添加指标")
                }
            }
        }
        .sheet(item: $minutesFor) { item in
            let g = item.goal
            MinutesCheckInSheet(goal: g) { minutes, count in
                Task {
                    if await data.checkIn(g.id, amount: Double(minutes), count: count) != nil {
                        toaster.show("\(o.title) · \(g.title) +\(minutes) 分钟")
                    }
                }
            }
        }
    }

    /// Sends the list without `m`; the others keep their ids and so their done state.
    private func removeMilestone(_ m: Noted_V1_Milestone, from o: Noted_V1_Objective) {
        var next = o
        next.milestones.removeAll { $0.id == m.id }
        Task { if await data.updateObjective(next, fields: ["milestones"]) != nil { toaster.show("已删除", systemImage: "trash") } }
    }

    private func subtitle(_ o: Noted_V1_Objective) -> String {
        var parts = o.goals.map { "\($0.title) \($0.planRule)" }
        if o.hasDueTime { parts.append("截止 \(Fmt.md(o.dueTime.date))") }
        return parts.joined(separator: " · ")
    }

    private func tapDim(_ g: Noted_V1_Goal) {
        if g.isMinutes { minutesFor = MinutesTarget(goal: g); return }
        let title = data.objective(objectiveID)?.title ?? ""
        Task {
            if await data.checkIn(g.id, amount: 1, count: g.hasCounter ? 1 : 0) != nil {
                toaster.show("\(title) · \(g.title) +1")
            }
        }
    }
}

/// A dimension waiting for its minutes sheet.
private struct MinutesTarget: Identifiable {
    let goal: Noted_V1_Goal
    var id: String { goal.id }
}

// MARK: - rows

/// A dimension: ring, title, "1/2 本周", and its own +1.
private struct DimensionRow: View {
    @Environment(Router.self) private var router
    let goal: Noted_V1_Goal
    let checkIn: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Button { router.push(.goal(goal.id)) } label: {
                HStack(spacing: 12) {
                    GoalRing(fraction: goal.fraction, done: goal.isAchieved)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(goal.title).lineLimit(1)
                        Text("\(Fmt.number(goal.done))/\(Fmt.number(goal.goalTarget))\(goal.isMinutes ? " 分钟" : "") \(goal.period.current)")
                            .font(.footnote).foregroundStyle(.secondaryText).monospacedDigit()
                    }
                    Spacer(minLength: 0)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            CheckInButton(title: goal.isMinutes ? "+\(Fmt.number(goal.quickAmount))" : "+1", done: goal.isAchieved, action: checkIn)
                .buttonStyle(.borderless)
        }
        .padding(.vertical, 3)
        .accessibilityIdentifier("dimension-\(goal.id)")
    }
}

/// A milestone: its check, title, and what counts as done · 截止 date (red once overdue).
private struct MilestoneRow: View {
    let milestone: Noted_V1_Milestone
    let toggle: () -> Void

    var body: some View {
        let m = milestone
        let overdue = m.isOverdue()
        HStack(spacing: 12) {
            CheckButton(isOn: m.done, action: toggle)
                .buttonStyle(.borderless)
                .padding(.horizontal, -10).padding(.vertical, -10)
            VStack(alignment: .leading, spacing: 1) {
                Text(m.title)
                    .foregroundStyle(m.done ? Color.secondaryText : Color.primary)
                    .strikethrough(m.done, color: .tertiaryText)
                    .lineLimit(2)
                if let sub = subtitle(m, overdue: overdue) {
                    sub.font(.footnote).foregroundStyle(.secondaryText).lineLimit(2)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 4)
        .frame(minHeight: 44)
        .accessibilityIdentifier("milestone-\(m.id)")
    }

    private func subtitle(_ m: Noted_V1_Milestone, overdue: Bool) -> Text? {
        var parts: [Text] = []
        if m.done, m.hasDoneTime {
            parts.append(Text("\(Fmt.md(m.doneTime.date)) 完成"))
        } else if let due = m.due {
            parts.append(overdue ? Text("截止 \(Fmt.md(due)) · 已逾期").foregroundColor(.danger) : Text("截止 \(Fmt.md(due))"))
        }
        if !m.criteria.isEmpty { parts.append(Text(m.criteria)) }
        guard var t = parts.first else { return nil }
        for p in parts.dropFirst() { t = t + Text(" · ") + p }
        return t
    }
}

/// One reading: day, value, change from the one before (green toward the target).
private struct ReadingRow: View {
    let objective: Noted_V1_Objective
    let reading: Noted_V1_Measurement
    let previous: Noted_V1_Measurement?

    var body: some View {
        HStack(spacing: 12) {
            Text(Fmt.mdw(reading.time.date))
            Spacer(minLength: 8)
            Text("\(PlanFmt.value(reading.value)) \(objective.metricUnit)").font(.system(size: 15, design: .monospaced))
            Group {
                if let previous {
                    let d = ((reading.value - previous.value) * 10).rounded() / 10
                    Text(d > 0 ? "+\(PlanFmt.value(d))" : (d < 0 ? "−\(PlanFmt.value(-d))" : "0"))
                        .foregroundStyle(d == 0 ? Color.secondaryText : (towardTarget(d) ? Color.success : Color.danger))
                } else {
                    Text("起始").foregroundStyle(.secondaryText)
                }
            }
            .font(.system(size: 13, design: .monospaced))
            .frame(width: 56, alignment: .trailing)
        }
        .accessibilityElement(children: .combine)
    }

    private func towardTarget(_ delta: Double) -> Bool {
        objective.metricTarget < objective.metricStart ? delta < 0 : delta > 0
    }
}

// MARK: - metric card

/// 指标: current value, start / target, 已减 X · 还差 Y, a line chart with the target line, "+ 记一次…".
private struct MetricCard: View {
    let objective: Noted_V1_Objective
    let readings: [Noted_V1_Measurement]
    let onAdd: () -> Void

    var body: some View {
        let o = objective
        let current = readings.last?.value ?? (o.progress.hasReading_p ? o.progress.metricCurrent : o.metricStart)
        let change = ((current - o.metricStart) * 10).rounded() / 10
        let left = ((o.metricTarget - current) * 10).rounded() / 10
        let goingDown = o.metricTarget < o.metricStart
        let good = goingDown ? change <= 0 : change >= 0
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 0) {
                    Text(o.metricName.isEmpty ? "指标" : o.metricName).font(.footnote).foregroundStyle(.secondaryText)
                    HStack(alignment: .firstTextBaseline, spacing: 2) {
                        Text(PlanFmt.value(current)).font(.system(size: 34, weight: .bold, design: .monospaced))
                        Text(o.metricUnit).font(.system(size: 20, weight: .semibold)).foregroundStyle(.secondaryText)
                    }
                }
                Spacer(minLength: 8)
                VStack(alignment: .trailing, spacing: 2) {
                    Text("起始 \(PlanFmt.value(o.metricStart)) · 目标 \(PlanFmt.value(o.metricTarget))")
                    HStack(spacing: 0) {
                        Text(changeText(change, down: goingDown)).fontWeight(.semibold).foregroundStyle(good ? Color.success : Color.danger)
                        if (goingDown && left < 0) || (!goingDown && left > 0) {
                            Text(" · 还差 \(PlanFmt.value(abs(left)))")
                        }
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondaryText)
                .monospacedDigit()
            }
            .padding(.bottom, 8)
            if readings.count > 0 {
                MetricChart(readings: readings, target: o.metricTarget, start: o.metricStart)
                    .frame(height: 160)
            }
            Button(action: onAdd) {
                Label("记一次\(o.metricName.isEmpty ? "" : o.metricName)", systemImage: "plus")
            }
            .buttonStyle(.wide(.tinted, small: true))
            .padding(.top, 10)
            .accessibilityIdentifier("objective-add-reading")
        }
        .card()
    }

    private func changeText(_ change: Double, down: Bool) -> String {
        let unit = objective.metricUnit
        if change == 0 { return "持平" }
        if change < 0 { return "已减 \(PlanFmt.value(-change)) \(unit)" }
        return "增加 \(PlanFmt.value(change)) \(unit)"
    }
}

/// The readings as a line over a soft area, a dot on each, the last one filled, and a dashed target line.
private struct MetricChart: View {
    let readings: [Noted_V1_Measurement]
    let target: Double
    let start: Double

    var body: some View {
        let values = readings.map(\.value) + [target, start]
        let lo = (values.min() ?? 0) - 0.5
        let hi = (values.max() ?? 1) + 0.3
        let lastID = readings.last?.id
        Chart {
            ForEach(readings, id: \.id) { r in
                AreaMark(x: .value("日期", r.time.date), yStart: .value("下限", lo), yEnd: .value("值", r.value))
                    .foregroundStyle(LinearGradient(colors: [Color.accentColor.opacity(0.18), Color.accentColor.opacity(0)],
                                                    startPoint: .top, endPoint: .bottom))
                    .interpolationMethod(.linear)
                LineMark(x: .value("日期", r.time.date), y: .value("值", r.value))
                    .foregroundStyle(Color.accentColor)
                    .lineStyle(StrokeStyle(lineWidth: 2.2, lineCap: .round, lineJoin: .round))
                    .interpolationMethod(.linear)
                PointMark(x: .value("日期", r.time.date), y: .value("值", r.value))
                    .symbol {
                        Circle()
                            .fill(r.id == lastID ? Color.accentColor : Color.white)
                            .overlay(Circle().stroke(Color.accentColor, lineWidth: 2))
                            .frame(width: r.id == lastID ? 9 : 6, height: r.id == lastID ? 9 : 6)
                    }
            }
            RuleMark(y: .value("目标", target))
                .foregroundStyle(Color.success)
                .lineStyle(StrokeStyle(lineWidth: 1.2, dash: [4, 4]))
                .annotation(position: .top, alignment: .trailing) {
                    Text("目标 \(PlanFmt.value(target))").font(.system(size: 10)).foregroundStyle(.success)
                }
        }
        .chartYScale(domain: lo...hi)
        .chartXAxis {
            AxisMarks(values: xTicks) { v in
                AxisValueLabel {
                    if let d = v.as(Date.self) { Text(Fmt.md(d)).font(.system(size: 10)) }
                }
            }
        }
        .chartYAxis {
            AxisMarks(position: .leading, values: .automatic(desiredCount: 4)) { v in
                AxisGridLine().foregroundStyle(Color.hairline)
                AxisValueLabel {
                    if let d = v.as(Double.self) { Text(PlanFmt.value(d)).font(.system(size: 10)) }
                }
            }
        }
        .accessibilityLabel("变化图")
    }

    private var xTicks: [Date] {
        guard let first = readings.first?.time.date else { return [] }
        guard let last = readings.last?.time.date, readings.count > 1 else { return [first] }
        return [first, last]
    }
}

// MARK: - sheets

/// 添加维度: name, 每天 / 每周 / 每月, and how many times.
private struct AddDimensionSheet: View {
    @Environment(\.dismiss) private var dismiss
    let onAdd: (String, Noted_V1_GoalPeriod, Int) -> Void
    @State private var title = ""
    @State private var period: Noted_V1_GoalPeriod = .week
    @State private var target = 2
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextField("如 游泳", text: $title)
                        .font(.title3)
                        .focused($focused)
                        .accessibilityLabel("维度名称")
                }
                Section {
                    Picker("周期", selection: $period) {
                        ForEach(Noted_V1_GoalPeriod.planChoices, id: \.self) { Text($0.every).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    .planCardRow(bottom: 0)
                }
                Section {
                    HStack {
                        Text("目标 ") + Text("\(target)").bold().monospacedDigit() + Text(" 次")
                        Spacer()
                        CompactStepper(onMinus: { target = max(1, target - 1) }, onPlus: { target += 1 })
                    }
                }
            }
            .navigationTitle("添加维度")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("添加") {
                        onAdd(title.trimmingCharacters(in: .whitespaces), period, target)
                        dismiss()
                    }
                    .fontWeight(.semibold)
                    .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty)
                    .accessibilityIdentifier("dimension-add-save")
                }
            }
        }
        .presentationDetents([.medium])
        .onAppear { focused = true }
    }
}

/// 添加里程碑: title, an optional due date, and what counts as done.
private struct AddMilestoneSheet: View {
    @Environment(\.dismiss) private var dismiss
    let onAdd: (String, Date?, String) -> Void
    @State private var title = ""
    @State private var hasDue = false
    @State private var due = Calendar.current.date(byAdding: .month, value: 1, to: .now) ?? .now
    @State private var criteria = ""
    @FocusState private var focused: Bool

    private var trimmed: String { title.trimmingCharacters(in: .whitespaces) }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextField("如 通过 A2 考试", text: $title)
                        .font(.title3)
                        .focused($focused)
                        .accessibilityLabel("里程碑名称")
                }
                Section {
                    Toggle("截止日期", isOn: $hasDue.animation())
                    if hasDue {
                        DatePicker("截止日期", selection: $due, displayedComponents: .date)
                            .datePickerStyle(.compact)
                    }
                    TextField("完成标准（可选）", text: $criteria, axis: .vertical)
                        .lineLimit(1...3)
                        .accessibilityLabel("完成标准")
                }
            }
            .navigationTitle("添加里程碑")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("添加") {
                        // The end of the chosen day, so it is not overdue on the day itself.
                        let end = hasDue ? Calendar.current.date(bySettingHour: 23, minute: 59, second: 59, of: due) : nil
                        onAdd(trimmed, end, criteria.trimmingCharacters(in: .whitespacesAndNewlines))
                        dismiss()
                    }
                    .fontWeight(.semibold)
                    .disabled(trimmed.isEmpty)
                    .accessibilityIdentifier("milestone-add-save")
                }
            }
        }
        .presentationDetents([.medium])
        .onAppear { focused = true }
    }
}

/// 添加指标: name, unit, current value and target.
private struct AddMetricSheet: View {
    @Environment(\.dismiss) private var dismiss
    let onAdd: (String, String, Double, Double) -> Void
    @State private var name = ""
    @State private var unit = ""
    @State private var start = ""
    @State private var target = ""
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            List {
                Section {
                    row("名称", "体重", $name).focused($focused)
                    row("单位", "kg", $unit)
                    row("当前", "80", $start, decimal: true)
                    row("目标", "75", $target, decimal: true)
                }
            }
            .navigationTitle("添加指标")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("添加") {
                        guard let s = PlanFmt.parse(start) else { return }
                        onAdd(name.trimmingCharacters(in: .whitespaces), unit.trimmingCharacters(in: .whitespaces), s, PlanFmt.parse(target) ?? s)
                        dismiss()
                    }
                    .fontWeight(.semibold)
                    .disabled(name.trimmingCharacters(in: .whitespaces).isEmpty || PlanFmt.parse(start) == nil)
                }
            }
        }
        .presentationDetents([.medium])
        .onAppear { focused = true }
    }

    private func row(_ label: String, _ placeholder: String, _ text: Binding<String>, decimal: Bool = false) -> some View {
        HStack {
            Text(label)
            TextField(placeholder, text: text)
                .multilineTextAlignment(.trailing)
                .keyboardType(decimal ? .decimalPad : .default)
                .font(decimal ? .body.monospacedDigit() : .body)
        }
    }
}
