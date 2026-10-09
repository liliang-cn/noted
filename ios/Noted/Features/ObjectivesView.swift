import SwiftUI
import Charts

/// A larger goal such as losing weight: a measured result plus the dimensions that get you there.
struct ObjectiveCard: View {
    @Environment(\.palette) private var p
    let objective: Noted_V1_Objective
    let onChange: () -> Void
    @State private var open = false

    var body: some View {
        let pr = objective.progress
        let tint = objective.space.color(p)
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                SpaceTag(space: objective.space)
                Spacer()
                if pr.hasMetric_p && pr.hasReading_p && !pr.onTrack {
                    Text("落后进度").font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
                        .background(p.badBg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(p.bad)
                }
                if pr.hasDaysLeft {
                    Text(pr.daysLeft >= 0 ? "\(pr.daysLeft) 天" : "已过期").font(.system(size: 13, weight: .semibold, design: .monospaced)).foregroundStyle(p.ink)
                }
            }
            Text(objective.title).font(.system(size: 18, weight: .semibold)).foregroundStyle(p.ink)
            if pr.hasMetric_p { metric(pr, tint) }
            else {
                ProgressBar(value: pr.goalsPercent, tint: tint)
            }
            Text(summary(pr)).font(.system(size: 13)).foregroundStyle(p.ink2)
            if !objective.goals.isEmpty {
                VStack(spacing: 0) {
                    ForEach(Array(objective.goals.enumerated()), id: \.element.id) { i, g in
                        if i > 0 { Divider().overlay(p.line) }
                        DimensionRow(goal: g, tint: tint, onChange: onChange)
                    }
                }
                .padding(.top, 2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card(padding: 14)
        .contentShape(Rectangle())
        .onTapGesture { open = true }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("objective-\(objective.title)")
        .sheet(isPresented: $open, onDismiss: onChange) { ObjectiveDetailView(id: objective.id) }
    }

    @ViewBuilder private func metric(_ pr: Noted_V1_ObjectiveProgress, _ tint: Color) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            if pr.hasReading_p {
                Text(Fmt.number(pr.metricCurrent)).font(.system(size: 28, weight: .semibold, design: .monospaced)).foregroundStyle(p.ink)
            } else {
                Text("—").font(.system(size: 28, weight: .semibold, design: .monospaced)).foregroundStyle(p.ink3)
            }
            Text(objective.metricUnit).font(.system(size: 14)).foregroundStyle(p.ink2)
            Spacer()
            Text("\(Fmt.number(objective.metricStart)) → \(Fmt.number(objective.metricTarget))")
                .font(.system(size: 13, design: .monospaced)).foregroundStyle(p.ink2)
        }
        ProgressBar(value: pr.metricPercent, tint: tint)
    }

    private func summary(_ pr: Noted_V1_ObjectiveProgress) -> String {
        var parts: [String] = []
        if pr.hasMetric_p {
            if pr.hasReading_p {
                let name = objective.metricName.isEmpty ? "结果" : objective.metricName
                parts.append("\(name) \(pr.metricChange > 0 ? "+" : "")\(Fmt.number((pr.metricChange * 10).rounded() / 10)) \(objective.metricUnit)")
            } else { parts.append("还没有记录") }
        }
        if pr.goalsTotal > 0 { parts.append("本期 \(pr.goalsAchieved)/\(pr.goalsTotal) 项达标") }
        return parts.joined(separator: " · ")
    }
}

/// One dimension of an objective, with a one-tap check-in.
struct DimensionRow: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    let goal: Noted_V1_Goal
    let tint: Color
    let onChange: () -> Void

    var body: some View {
        let g = goal.progress
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 5) {
                HStack {
                    Text(goal.title).font(.system(size: 14, weight: .medium)).foregroundStyle(p.ink)
                    Spacer()
                    Text("\(Fmt.number(g.done))/\(Fmt.number(g.target)) \(goal.unit)")
                        .font(.system(size: 12, design: .monospaced)).foregroundStyle(g.behind ? p.bad : p.ink2)
                }
                ProgressBar(value: g.percent, tint: g.achieved ? p.ok : tint)
            }
            Button {
                Task {
                    var req = Noted_V1_RecordCheckInRequest()
                    req.goalID = goal.id; req.amount = 1
                    _ = try? await session.api?.goals.recordCheckIn(req)
                    onChange()
                }
            } label: {
                Image(systemName: "plus").font(.system(size: 13, weight: .bold)).foregroundStyle(p.ink)
                    .frame(width: 32, height: 32).background(p.surface2, in: RoundedRectangle(cornerRadius: 8))
            }
            .accessibilityLabel("打卡 \(goal.title)")
        }
        .padding(.vertical, 8)
    }
}

struct ObjectiveDetailView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let id: String
    @State private var objective: Noted_V1_Objective?
    @State private var readings: [Noted_V1_Measurement] = []
    @State private var value = ""
    @State private var error: String?
    @State private var addingDimension = false
    @State private var confirmDelete = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Button { dismiss() } label: { Label("返回", systemImage: "chevron.left").font(.system(size: 14, weight: .semibold)) }
                    Spacer()
                    if objective != nil {
                        Menu {
                            Button("添加维度") { addingDimension = true }
                            Button("归档") { Task { await archive() } }
                            Button("删除目标", role: .destructive) { confirmDelete = true }
                        } label: { Image(systemName: "ellipsis").font(.system(size: 16)).frame(width: 32, height: 32) }
                        .accessibilityLabel("更多")
                    }
                }
                ErrorLine(text: error)
                if let o = objective { content(o) }
            }
            .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 24)
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .sheet(isPresented: $addingDimension, onDismiss: { Task { await load() } }) { NewDimensionView(objectiveID: id, space: objective?.space ?? .life) }
        .confirmationDialog("删除这个目标?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("删除,保留其中的维度", role: .destructive) { Task { await remove() } }
        } message: { Text("目标和它的记录会删除,里面的维度变成单独的目标。") }
    }

    @ViewBuilder private func content(_ o: Noted_V1_Objective) -> some View {
        let pr = o.progress
        let tint = o.space.color(p)
        VStack(alignment: .leading, spacing: 8) {
            SpaceTag(space: o.space)
            Text(o.title).font(.system(size: 30, weight: .bold)).foregroundStyle(p.ink)
        }
        if pr.hasMetric_p {
            VStack(alignment: .leading, spacing: 10) {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 4) {
                        Eyebrow(text: o.metricName.isEmpty ? "结果" : o.metricName)
                        HStack(alignment: .firstTextBaseline, spacing: 4) {
                            Text(pr.hasReading_p ? Fmt.number(pr.metricCurrent) : "—").font(.system(size: 32, weight: .semibold, design: .monospaced)).foregroundStyle(p.ink)
                            Text(o.metricUnit).font(.system(size: 14)).foregroundStyle(p.ink2)
                        }
                    }
                    Spacer()
                    VStack(alignment: .trailing, spacing: 4) {
                        Eyebrow(text: "目标")
                        Text("\(Fmt.number(o.metricTarget)) \(o.metricUnit)").font(.system(size: 18, weight: .semibold, design: .monospaced)).foregroundStyle(tint)
                        if pr.hasDaysLeft {
                            Text(pr.daysLeft >= 0 ? "还有 \(pr.daysLeft) 天" : "已过期").font(.system(size: 12)).foregroundStyle(p.ink2)
                        }
                    }
                }
                ProgressBar(value: pr.metricPercent, tint: tint)
                HStack {
                    Text("\(Int((pr.metricPercent * 100).rounded()))%").font(.system(size: 13, design: .monospaced)).foregroundStyle(p.ink2)
                    Spacer()
                    if pr.hasReading_p && !pr.onTrack {
                        Text("落后进度").font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
                            .background(p.badBg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(p.bad)
                    }
                }
                if readings.count >= 2 { chart(o, tint) }
                HStack(spacing: 8) {
                    TextField("今天的\(o.metricName.isEmpty ? "数值" : o.metricName)", text: $value)
                        .keyboardType(.decimalPad).padding(.horizontal, 12).frame(height: 40)
                        .background(p.surface2, in: RoundedRectangle(cornerRadius: 10))
                        .accessibilityIdentifier("reading-value")
                    Button { Task { await record() } } label: {
                        Text("记录").font(.system(size: 14, weight: .semibold)).foregroundStyle(.white).padding(.horizontal, 16).frame(height: 40)
                            .background(tint, in: RoundedRectangle(cornerRadius: 10))
                    }
                    .disabled(Double(value.replacingOccurrences(of: ",", with: ".")) == nil)
                    .accessibilityIdentifier("reading-save")
                }
            }
            .card(padding: 14)
        }

        Eyebrow(text: "维度 · 本期 \(pr.goalsAchieved)/\(pr.goalsTotal) 项达标")
        if o.goals.isEmpty {
            Text("还没有维度").font(.system(size: 14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 60).card()
        }
        ForEach(o.goals, id: \.id) { g in GoalRow(goal: g) { Task { await load() } } }

        if !readings.isEmpty {
            Eyebrow(text: "记录")
            VStack(spacing: 0) {
                ForEach(Array(readings.reversed().prefix(30).enumerated()), id: \.element.id) { i, m in
                    if i > 0 { Divider().overlay(p.line) }
                    HStack {
                        Text(Fmt.md(m.time.date)).font(.system(size: 13, design: .monospaced)).foregroundStyle(p.ink2)
                        Spacer()
                        Text("\(Fmt.number(m.value)) \(o.metricUnit)").font(.system(size: 15, weight: .medium, design: .monospaced)).foregroundStyle(p.ink)
                        Button { Task { await delete(m) } } label: { Image(systemName: "xmark").font(.system(size: 11, weight: .bold)).foregroundStyle(p.ink3).frame(width: 28, height: 28) }
                            .accessibilityLabel("删除 \(Fmt.md(m.time.date)) 的记录")
                    }.padding(.horizontal, 14).frame(minHeight: 44)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
    }

    private func chart(_ o: Noted_V1_Objective, _ tint: Color) -> some View {
        Chart {
            ForEach(readings, id: \.id) { m in
                LineMark(x: .value("日期", m.time.date), y: .value(o.metricUnit, m.value)).foregroundStyle(tint)
                PointMark(x: .value("日期", m.time.date), y: .value(o.metricUnit, m.value)).foregroundStyle(tint)
            }
            RuleMark(y: .value("目标", o.metricTarget)).foregroundStyle(p.ink3).lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 3]))
        }
        .chartYScale(domain: .automatic(includesZero: false))
        .frame(height: 150)
    }

    private func load() async {
        guard let api = session.api else { return }
        do {
            var g = Noted_V1_GetObjectiveRequest(); g.id = id
            objective = try await api.objectives.getObjective(g)
            if objective?.progress.hasMetric_p == true {
                var r = Noted_V1_ListMeasurementsRequest(); r.objectiveID = id; r.limit = 365
                readings = try await api.objectives.listMeasurements(r).measurements
            }
            error = nil
        } catch { if !(error is CancellationError) { self.error = describe(error) } }
    }

    private func record() async {
        guard let api = session.api, let v = Double(value.replacingOccurrences(of: ",", with: ".")) else { return }
        var r = Noted_V1_RecordMeasurementRequest(); r.objectiveID = id; r.value = v
        do { _ = try await api.objectives.recordMeasurement(r); value = ""; await load() } catch { self.error = describe(error) }
    }

    private func delete(_ m: Noted_V1_Measurement) async {
        guard let api = session.api else { return }
        var r = Noted_V1_DeleteMeasurementRequest(); r.id = m.id
        do { _ = try await api.objectives.deleteMeasurement(r); await load() } catch { self.error = describe(error) }
    }

    private func archive() async {
        guard let api = session.api else { return }
        var r = Noted_V1_UpdateObjectiveRequest(); r.objective.id = id; r.objective.archived = true; r.updateMask = fieldMask("archived")
        do { _ = try await api.objectives.updateObjective(r); dismiss() } catch { self.error = describe(error) }
    }

    private func remove() async {
        guard let api = session.api else { return }
        var r = Noted_V1_DeleteObjectiveRequest(); r.id = id
        do { _ = try await api.objectives.deleteObjective(r); dismiss() } catch { self.error = describe(error) }
    }
}

struct DimensionDraft: Identifiable {
    let id = UUID()
    var title = ""
    var period: Noted_V1_GoalPeriod = .week
    var target = ""
    var unit = "次"
}

private let periodNames: [(Noted_V1_GoalPeriod, String)] = [(.day, "每天"), (.week, "每周"), (.month, "每月")]

struct DimensionEditor: View {
    @Environment(\.palette) private var p
    @Binding var draft: DimensionDraft
    var onRemove: (() -> Void)?

    var body: some View {
        HStack(spacing: 8) {
            TextField("维度,如 游泳", text: $draft.title).frame(minWidth: 70)
            Menu {
                ForEach(periodNames, id: \.0) { per, name in Button(name) { draft.period = per } }
            } label: {
                Text(periodNames.first { $0.0 == draft.period }?.1 ?? "每周").font(.system(size: 13, weight: .medium))
                    .padding(.horizontal, 8).frame(height: 30).background(p.surface2, in: RoundedRectangle(cornerRadius: 8))
            }
            TextField("数量", text: $draft.target).keyboardType(.decimalPad).multilineTextAlignment(.trailing).frame(width: 44)
            TextField("单位", text: $draft.unit).frame(width: 34)
            if let onRemove {
                Button(action: onRemove) { Image(systemName: "minus.circle").foregroundStyle(p.ink3) }.accessibilityLabel("移除维度")
            }
        }
        .font(.system(size: 14))
        .padding(.horizontal, 12).frame(minHeight: 46)
    }
}

struct NewDimensionView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let objectiveID: String
    let space: Noted_V1_Space
    @State private var draft = DimensionDraft()
    @State private var error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Button("取消") { dismiss() }
                Spacer()
                Text("添加维度").font(.system(size: 16, weight: .semibold))
                Spacer()
                Button("保存") { Task { await save() } }.fontWeight(.semibold).disabled(draft.title.trimmingCharacters(in: .whitespaces).isEmpty || Double(draft.target) == nil)
            }
            DimensionEditor(draft: $draft).card(padding: 0)
            ErrorLine(text: error)
            Spacer()
        }
        .padding(16).background(p.bg.ignoresSafeArea())
        .presentationDetents([.height(220)])
    }

    private func save() async {
        guard let api = session.api, let t = Double(draft.target) else { return }
        var r = Noted_V1_CreateGoalRequest()
        r.goal.title = draft.title; r.goal.period = draft.period; r.goal.target = t; r.goal.unit = draft.unit; r.goal.objectiveID = objectiveID; r.goal.space = space
        do { _ = try await api.goals.createGoal(r); dismiss() } catch { self.error = describe(error) }
    }
}

struct NewObjectiveView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var title = ""
    @State private var work = false
    @State private var hasMetric = false
    @State private var metricName = ""
    @State private var metricUnit = ""
    @State private var metricStart = ""
    @State private var metricTarget = ""
    @State private var hasDue = false
    @State private var due = Calendar.current.date(byAdding: .day, value: 90, to: .now) ?? .now
    @State private var dims: [DimensionDraft] = []
    @State private var error: String?
    @State private var saving = false

    private var valid: Bool {
        !title.trimmingCharacters(in: .whitespaces).isEmpty
            && (!hasMetric || (!metricUnit.isEmpty && Double(metricStart) != nil && Double(metricTarget) != nil && metricStart != metricTarget))
            && dims.allSatisfy { !$0.title.trimmingCharacters(in: .whitespaces).isEmpty && Double($0.target) != nil }
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Button("取消") { dismiss() }
                    Spacer()
                    Text("新建目标").font(.system(size: 16, weight: .semibold))
                    Spacer()
                    Button("保存") { Task { await save() } }.fontWeight(.semibold).disabled(!valid || saving).accessibilityIdentifier("save-objective")
                }
                TextField("目标,如 减肥", text: $title).font(.system(size: 22, weight: .bold)).accessibilityIdentifier("objective-title")
                HStack(spacing: 8) {
                    chip("减肥", id: "template-weightloss") { weightLoss() }
                    chip("增肌", id: "template-muscle") { muscle() }
                    Spacer()
                    Picker("", selection: $work) { Text("生活").tag(false); Text("工作").tag(true) }.pickerStyle(.segmented).frame(width: 130)
                }

                Eyebrow(text: "结果指标")
                VStack(spacing: 0) {
                    Toggle("记录一个数值,如体重", isOn: $hasMetric).padding(.horizontal, 14).frame(minHeight: 48).tapsToToggle($hasMetric)
                    if hasMetric {
                        Divider().overlay(p.line)
                        HStack(spacing: 8) {
                            TextField("名称", text: $metricName).frame(minWidth: 50)
                            TextField("单位", text: $metricUnit).frame(width: 40).accessibilityIdentifier("metric-unit")
                            TextField("现在", text: $metricStart).keyboardType(.decimalPad).multilineTextAlignment(.trailing).frame(width: 56).accessibilityIdentifier("metric-start")
                            Image(systemName: "arrow.right").foregroundStyle(p.ink3)
                            TextField("目标", text: $metricTarget).keyboardType(.decimalPad).frame(width: 56).accessibilityIdentifier("metric-target")
                        }.font(.system(size: 14)).padding(.horizontal, 14).frame(minHeight: 48)
                    }
                    Divider().overlay(p.line)
                    Toggle("截止日期", isOn: $hasDue).padding(.horizontal, 14).frame(minHeight: 48).tapsToToggle($hasDue)
                    if hasDue {
                        Divider().overlay(p.line)
                        DatePicker("", selection: $due, displayedComponents: .date).labelsHidden().padding(.horizontal, 14).frame(minHeight: 48, alignment: .leading)
                    }
                }
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))

                Eyebrow(text: "维度 · 每个都有自己的周期和数量")
                VStack(spacing: 0) {
                    ForEach($dims) { $d in
                        DimensionEditor(draft: $d) { dims.removeAll { $0.id == d.id } }
                        Divider().overlay(p.line)
                    }
                    Button { dims.append(DimensionDraft()) } label: {
                        Text("+ 添加维度").font(.system(size: 14, weight: .semibold)).foregroundStyle(p.ink).frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 14).frame(minHeight: 46)
                    }.accessibilityIdentifier("add-dimension")
                }
                .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
                ErrorLine(text: error)
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
    }

    private func chip(_ label: String, id: String, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label).font(.system(size: 13, weight: .semibold)).foregroundStyle(p.ink).padding(.horizontal, 12).frame(height: 32)
                .background(p.surface, in: RoundedRectangle(cornerRadius: 9)).overlay(RoundedRectangle(cornerRadius: 9).stroke(p.line))
        }.accessibilityIdentifier(id)
    }

    private func weightLoss() {
        title = "减肥"; hasMetric = true; metricName = "体重"; metricUnit = "kg"; hasDue = true
        due = Calendar.current.date(byAdding: .day, value: 90, to: .now) ?? .now
        dims = [("游泳", 2, "次"), ("跑步", 3, "次"), ("健身", 3, "次"), ("轻食", 10, "餐"), ("控糖", 7, "天")]
            .map { DimensionDraft(title: $0.0, period: .week, target: "\($0.1)", unit: $0.2) }
    }

    private func muscle() {
        title = "增肌"; hasMetric = true; metricName = "体重"; metricUnit = "kg"; hasDue = true
        due = Calendar.current.date(byAdding: .day, value: 120, to: .now) ?? .now
        dims = [("力量训练", 4, "次"), ("蛋白质达标", 7, "天"), ("睡够 7 小时", 7, "天")]
            .map { DimensionDraft(title: $0.0, period: .week, target: "\($0.1)", unit: $0.2) }
    }

    private func save() async {
        guard let api = session.api else { return }
        saving = true; defer { saving = false }
        var r = Noted_V1_CreateObjectiveRequest()
        r.objective.title = title
        r.objective.space = work ? .work : .life
        if hasMetric {
            r.objective.metricName = metricName; r.objective.metricUnit = metricUnit
            r.objective.metricStart = Double(metricStart) ?? 0; r.objective.metricTarget = Double(metricTarget) ?? 0
        }
        r.objective.startTime = .init(Date.now)
        if hasDue { r.objective.dueTime = .init(due) }
        do {
            let o = try await api.objectives.createObjective(r)
            for d in dims {
                var g = Noted_V1_CreateGoalRequest()
                g.goal.title = d.title; g.goal.period = d.period; g.goal.target = Double(d.target) ?? 1; g.goal.unit = d.unit
                g.goal.objectiveID = o.id; g.goal.space = o.space
                _ = try await api.goals.createGoal(g)
            }
            dismiss()
        } catch { self.error = describe(error) }
    }
}
