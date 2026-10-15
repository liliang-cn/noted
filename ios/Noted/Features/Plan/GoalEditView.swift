import SwiftUI

/// 编辑目标: title, 周期, target + 计量, 累计总量 (name + total), 项目, 空间, 备注. 完成 saves.
struct GoalEditView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let goalID: String

    @State private var title = ""
    @State private var notes = ""
    @State private var period: Noted_V1_GoalPeriod = .week
    @State private var target = 3
    @State private var minutes = false
    @State private var hasCounter = false
    @State private var counterName = "课时"
    @State private var counterTarget = 10
    @State private var projectID = ""
    @State private var space: Noted_V1_Space = .life
    @State private var isDimension = false
    @State private var filled = false
    @State private var saving = false

    var body: some View {
        List {
            Section {
                TextField("目标名称", text: $title)
                    .font(.title3.weight(.semibold))
                    .accessibilityIdentifier("goal-edit-title")
            }
            Section {
                HStack {
                    FieldLabel("周期", icon: "repeat", tile: .gray)
                    Spacer(minLength: 8)
                    Picker("周期", selection: $period) {
                        ForEach(Noted_V1_GoalPeriod.planChoices, id: \.self) { Text($0.every).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .frame(width: 168)
                }
                HStack {
                    IconTile(systemName: "target", tile: .blue)
                    (Text("\(period.every) ") + Text("\(target)").bold().monospacedDigit() + Text(minutes ? " 分钟" : " 次"))
                    Spacer(minLength: 8)
                    CompactStepper(onMinus: { target = max(1, target - step) }, onPlus: { target = min(10_000, target + step) })
                }
                .accessibilityElement(children: .contain)
                HStack {
                    FieldLabel("计量", icon: "clock", tile: .orange)
                    Spacer(minLength: 8)
                    Picker("计量", selection: Binding(get: { minutes }, set: { on in
                        minutes = on
                        if on && target < 5 { target = 30 }
                        if !on && target > 20 { target = 3 }
                    })) {
                        Text("次数").tag(false)
                        Text("分钟").tag(true)
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .frame(width: 132)
                }
            }
            Section {
                Toggle(isOn: $hasCounter.animation(.snappy)) { FieldLabel("累计总量", icon: "chart.bar", tile: .teal) }
                    .tint(.switchOn)
                if hasCounter {
                    HStack {
                        Text("名称")
                        TextField("课时", text: $counterName).multilineTextAlignment(.trailing)
                    }
                    .padding(.leading, 42)
                    HStack {
                        Text("总数 ") + Text("\(counterTarget)").bold().monospacedDigit()
                        Spacer()
                        CompactStepper(onMinus: { counterTarget = max(1, counterTarget - 5) }, onPlus: { counterTarget += 5 })
                    }
                    .padding(.leading, 42)
                }
            }
            Section {
                if !isDimension { ProjectPicker(projectID: $projectID, space: $space) }
                SpaceSegmentRow(space: $space)
            }
            Section {
                NotesField(text: $notes)
            }
        }
        .listStyle(.insetGrouped)
        .navigationTitle("编辑目标")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("完成") { save() }
                    .fontWeight(.semibold)
                    .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty || saving)
                    .accessibilityIdentifier("goal-edit-done")
            }
        }
        .onAppear(perform: fill)
    }

    private var step: Int { minutes ? 5 : 1 }

    private func fill() {
        guard !filled, let g = data.goal(goalID) else { return }
        filled = true
        title = g.title
        notes = g.notes
        period = g.period.planNormalized
        target = max(1, Int(g.target.rounded()))
        minutes = g.isMinutes
        hasCounter = g.hasCounter
        if g.hasCounter { counterName = g.counterUnit; counterTarget = Int(g.counterTarget.rounded()) }
        projectID = g.projectID
        space = g.space == .work ? .work : .life
        isDimension = !g.objectiveID.isEmpty
    }

    private func save() {
        guard var g = data.goal(goalID) else { router.pop(); return }
        var fields: [String] = []
        let t = title.trimmingCharacters(in: .whitespacesAndNewlines)
        if t != g.title { g.title = t; fields.append("title") }
        if notes != g.notes { g.notes = notes; fields.append("notes") }
        if period != g.period.planNormalized || g.period == .unspecified { g.period = period; fields.append("period") }
        if Double(target) != g.target { g.target = Double(target); fields.append("target") }
        if minutes != g.isMinutes { g.unit = minutes ? "min" : "times"; fields.append("unit") }
        let newCounter = hasCounter ? Double(counterTarget) : 0
        if newCounter != g.counterTarget { g.counterTarget = newCounter; fields.append("counter_target") }
        let name = hasCounter ? counterName.trimmingCharacters(in: .whitespaces) : ""
        if name != g.counterUnit { g.counterUnit = name; fields.append("counter_unit") }
        if !isDimension && projectID != g.projectID { g.projectID = projectID; fields.append("project_id") }
        if space != g.space { g.space = space; fields.append("space") }
        guard !fields.isEmpty else { router.pop(); return }
        saving = true
        Task {
            defer { saving = false }
            if await data.updateGoal(g, fields: fields) != nil {
                router.pop()
                toaster.show("已保存")
            }
        }
    }
}
