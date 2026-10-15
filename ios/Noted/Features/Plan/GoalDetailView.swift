import SwiftUI

/// 目标: the period's ring with the check-in button and last 7 days, streak and month stats,
/// the running total (课时 19/40) with − / +, and the check-in history (swipe to undo).
struct GoalDetailView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let goalID: String

    @State private var askMinutes = false
    @State private var askDelete = false
    @State private var loaded = false

    var body: some View {
        Group {
            if let g = data.goal(goalID) {
                content(g)
            } else if loaded {
                List { Section { EmptyState(text: "目标已删除") } }
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle(data.goal(goalID)?.title ?? "目标")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            await data.loadCheckIns(goalID, limit: 200)
            loaded = true
        }
        .reloadOnReturn {
            await data.refreshGoals()
            await data.loadCheckIns(goalID, limit: 200)
        }
    }

    private func content(_ g: Noted_V1_Goal) -> some View {
        let history = data.checkIns[goalID] ?? []
        let monthStart = Date.now.startOfMonth
        let month = history.filter { $0.time.date >= monthStart }.reduce(0.0) { $0 + $1.amount }
        return List {
            Section {
                PlanDetailHeader(title: g.title, subtitle: subtitle(g)) {
                    SpaceTag(space: g.space)
                    if !g.projectID.isEmpty { PlanProjectChip(projectID: g.projectID) }
                }
            }
            Section {
                PlanRingCard(
                    fraction: g.fraction,
                    done: g.isAchieved,
                    value: Fmt.number(g.done),
                    total: Fmt.number(g.goalTarget),
                    caption: g.isMinutes ? "\(g.period.current) · 分钟" : g.period.current,
                    buttonTitle: g.isAchieved ? "本期已达成 · 再记一次" : (g.isMinutes ? "记录时长" : "打卡 +1"),
                    buttonIcon: g.isAchieved ? "checkmark" : (g.isMinutes ? "clock" : "plus"),
                    days: PlanSevenDayDots.days(from: [g.progress.recent])
                ) {
                    if g.isMinutes { askMinutes = true } else { Task { await checkIn(g, amount: 1, count: g.hasCounter ? 1 : 0) } }
                }
                .planCardRow(bottom: 0)
            }
            Section {
                HStack(spacing: 12) {
                    PlanStatTile(title: "连续达成") {
                        HStack(alignment: .firstTextBaseline, spacing: 4) {
                            Image(systemName: "flame.fill").font(.system(size: 17)).foregroundStyle(.warning)
                            Text("\(g.progress.streak)").font(.system(size: 22, weight: .bold)).foregroundStyle(.warning).monospacedDigit()
                            Text(g.period.unitName).font(.subheadline).foregroundStyle(.secondaryText)
                        }
                    }
                    PlanStatTile(title: "本月") {
                        HStack(alignment: .firstTextBaseline, spacing: 4) {
                            Text(Fmt.number(month)).font(.system(size: 22, weight: .bold)).monospacedDigit()
                            Text(g.isMinutes ? "分钟" : g.unitLabel).font(.subheadline).foregroundStyle(.secondaryText)
                        }
                    }
                }
                .planCardRow(bottom: 0)
            }
            if g.hasCounter {
                Section { counterCard(g, history: history).planCardRow(bottom: 0) }
            }
            Section {
                if history.isEmpty { EmptyState(text: "还没有打卡") }
                ForEach(history, id: \.id) { c in
                    CheckInRow(goal: g, checkIn: c)
                        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                            Button(role: .destructive) { undo(c) } label: { Label("撤销", systemImage: "arrow.uturn.backward") }
                        }
                }
            } header: {
                Text("打卡记录")
            }
        }
        .listStyle(.insetGrouped)
        .refreshable {
            await data.refreshGoals()
            await data.loadCheckIns(goalID, limit: 200)
        }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Button { router.push(.goalEdit(goalID)) } label: { Label("编辑", systemImage: "pencil") }
                    Divider()
                    Button(role: .destructive) { askDelete = true } label: { Label("删除目标", systemImage: "trash") }
                } label: { Image(systemName: "ellipsis.circle") }
                .accessibilityLabel("更多")
                .accessibilityIdentifier("goal-more")
            }
        }
        .sheet(isPresented: $askMinutes) {
            MinutesCheckInSheet(goal: g) { minutes, count in
                Task { await checkIn(g, amount: Double(minutes), count: count) }
            }
        }
        .confirmationDialog("删除目标？", isPresented: $askDelete, titleVisibility: .visible) {
            Button("删除目标", role: .destructive) {
                Task {
                    if await data.deleteGoal(goalID) {
                        router.pop()
                        toaster.show("已删除", systemImage: "trash")
                    }
                }
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text("“\(g.title)”的打卡记录会一起删除")
        }
    }

    private func subtitle(_ g: Noted_V1_Goal) -> String {
        var s = g.planRule
        if g.hasCounter { s += " · \(g.planCounterName) \(Fmt.number(g.counterTarget))" }
        let notes = g.notes.trimmingCharacters(in: .whitespacesAndNewlines)
        if !notes.isEmpty { s += " · \(notes)" }
        return s
    }

    private func counterCard(_ g: Noted_V1_Goal, history: [Noted_V1_CheckIn]) -> some View {
        let done = g.progress.counterDone
        let left = max(g.counterTarget - done, 0)
        return VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .center) {
                VStack(alignment: .leading, spacing: 0) {
                    Text("累计\(g.planCounterName)").font(.footnote).foregroundStyle(.secondaryText)
                    HStack(alignment: .firstTextBaseline, spacing: 0) {
                        Text(Fmt.number(done)).font(.system(size: 34, weight: .bold, design: .monospaced))
                            .contentTransition(.numericText())
                        Text("/\(Fmt.number(g.counterTarget))").font(.system(size: 22, weight: .medium, design: .monospaced)).foregroundStyle(.secondaryText)
                    }
                }
                Spacer()
                CompactStepper(onMinus: { decrementCounter(g, history: history) },
                               onPlus: { Task { await adjustCounter(g) } })
                    .accessibilityIdentifier("goal-counter-stepper")
            }
            ProgressBar(value: g.progress.counterPercent, height: ProgressBar.large).padding(.top, 10)
            Text(left > 0 ? "还差 \(Fmt.number(left)) \(g.planCounterName)" : "已完成")
                .font(.footnote).foregroundStyle(.secondaryText)
                .padding(.top, 8)
        }
        .card()
        .animation(.snappy, value: done)
    }

    // MARK: actions

    private func checkIn(_ g: Noted_V1_Goal, amount: Double, count: Double) async {
        guard await data.checkIn(g.id, amount: amount, count: count) != nil else { return }
        toaster.show(g.isMinutes ? "\(g.title) +\(Fmt.number(amount)) 分钟" : "\(g.title) +1")
    }

    /// + on the running total: a check-in that only counts toward it.
    private func adjustCounter(_ g: Noted_V1_Goal) async {
        guard g.progress.counterDone < g.counterTarget else { return }
        if await data.checkIn(g.id, amount: 0, count: 1) != nil {
            toaster.show("\(g.planCounterName) +1")
        }
    }

    /// − on the running total: takes back the latest +1 made with the stepper.
    private func decrementCounter(_ g: Noted_V1_Goal, history: [Noted_V1_CheckIn]) {
        guard let c = history.first(where: { $0.count > 0 && $0.amount == 0 }) else {
            toaster.error("在打卡记录里左滑撤销")
            return
        }
        Task { if await data.deleteCheckIn(c.id, goalID: g.id) { toaster.show("\(g.planCounterName) −1") } }
    }

    private func undo(_ c: Noted_V1_CheckIn) {
        Task { if await data.deleteCheckIn(c.id, goalID: goalID) { toaster.show("已撤销", systemImage: "arrow.uturn.backward") } }
    }
}

/// One check-in: when, how much, what it added to the running total, and its note.
private struct CheckInRow: View {
    let goal: Noted_V1_Goal
    let checkIn: Noted_V1_CheckIn

    var body: some View {
        let c = checkIn
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 1) {
                Text("\(Fmt.mdw(c.time.date)) \(Fmt.hm(c.time.date))").monospacedDigit()
                if !c.note.isEmpty {
                    Text(c.note).font(.footnote).foregroundStyle(.secondaryText).lineLimit(2)
                }
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 1) {
                if c.amount > 0 {
                    Text(goal.isMinutes ? "+\(Fmt.number(c.amount)) 分钟" : "+\(Fmt.number(c.amount))")
                        .font(.system(size: 15, design: .monospaced))
                        .foregroundStyle(.success)
                }
                if c.count > 0 {
                    Text("\(goal.planCounterName) +\(Fmt.number(c.count))")
                        .font(.footnote).foregroundStyle(.secondaryText).monospacedDigit()
                }
            }
        }
        .accessibilityElement(children: .combine)
    }
}
