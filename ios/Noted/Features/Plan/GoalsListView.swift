import SwiftUI

/// 目标 · 全部: every goal and objective in the current space, swipe to delete, "+" for a new goal.
struct GoalsListView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    @Environment(Session.self) private var session

    /// A goal or objective waiting for its delete confirmation.
    private struct Pending: Identifiable {
        let id: String
        let title: String
        let isObjective: Bool
    }

    @State private var deleting: Pending?

    var body: some View {
        let goals = data.standaloneGoals
        let objectives = data.visibleObjectives
        List {
            Section {
                if goals.isEmpty && objectives.isEmpty { EmptyState(text: "没有目标") }
                ForEach(goals, id: \.id) { g in
                    GoalRow(goal: g)
                        .swipeActions(edge: .trailing) {
                            Button { deleting = Pending(id: g.id, title: g.title, isObjective: false) } label: {
                                Label("删除", systemImage: "trash")
                            }
                            .tint(.danger)
                        }
                }
                ForEach(objectives, id: \.id) { o in
                    ObjectiveRow(objective: o)
                        .swipeActions(edge: .trailing) {
                            Button { deleting = Pending(id: o.id, title: o.title, isObjective: true) } label: {
                                Label("删除", systemImage: "trash")
                            }
                            .tint(.danger)
                        }
                }
            }
        }
        .listStyle(.insetGrouped)
        .refreshable { await data.refreshGoals() }
        .navigationTitle("目标")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { router.push(.create(CreateSpec(kind: .goal, space: session.mode.space))) } label: {
                    Image(systemName: "plus")
                }
                .accessibilityLabel("新建目标")
                .accessibilityIdentifier("goals-new")
            }
        }
        .confirmationDialog("删除目标？", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { p in
            Button("删除目标", role: .destructive) {
                Task {
                    let ok: Bool
                    if p.isObjective { ok = await data.deleteObjective(p.id) } else { ok = await data.deleteGoal(p.id) }
                    if ok { toaster.show("已删除", systemImage: "trash") }
                }
            }
            Button("取消", role: .cancel) {}
        } message: { p in
            Text(p.isObjective ? "“\(p.title)”的维度会保留为独立目标" : "“\(p.title)”的打卡记录会一起删除")
        }
    }
}
