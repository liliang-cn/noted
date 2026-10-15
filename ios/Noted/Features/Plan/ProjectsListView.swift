import SwiftUI

/// 项目 · 全部: 进行中 / 已归档, cards with swipes for 归档 and 删除. "+" makes a new project.
struct ProjectsListView: View {
    @Environment(AppData.self) private var data
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    @Environment(Session.self) private var session

    enum Shelf: String, CaseIterable { case active, archived }
    @State private var tab: Shelf = .active
    @State private var deleting: Noted_V1_Project?

    var body: some View {
        let list = tab == .active ? data.activeProjects : data.archivedProjects
        List {
            Section {
                Picker("项目", selection: $tab) {
                    Text("进行中").tag(Shelf.active)
                    Text("已归档").tag(Shelf.archived)
                }
                .pickerStyle(.segmented)
                .planCardRow(bottom: 4)
                .accessibilityIdentifier("projects-tab")
            }
            Section {
                if list.isEmpty {
                    EmptyState(text: tab == .active ? "没有进行中的项目" : "没有已归档的项目")
                        .background(.white, in: RoundedRectangle(cornerRadius: 14))
                        .planCardRow()
                }
                ForEach(list, id: \.id) { p in
                    ProjectCard(project: p)
                        .planCardRow()
                        .swipeActions(edge: .trailing) {
                            Button { deleting = p } label: { Label("删除", systemImage: "trash") }
                                .tint(.danger)
                            Button { archive(p) } label: {
                                Label(p.archived ? "取消归档" : "归档", systemImage: "archivebox")
                            }
                            .tint(.warning)
                        }
                        .contextMenu {
                            Button { archive(p) } label: { Label(p.archived ? "取消归档" : "归档", systemImage: "archivebox") }
                            Button(role: .destructive) { deleting = p } label: { Label("删除项目", systemImage: "trash") }
                        }
                }
            }
        }
        .listStyle(.insetGrouped)
        .refreshable { await data.refreshProjects() }
        .navigationTitle("项目")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { router.push(.create(CreateSpec(kind: .project, space: session.mode.space))) } label: {
                    Image(systemName: "plus")
                }
                .accessibilityLabel("新建项目")
                .accessibilityIdentifier("projects-new")
            }
        }
        .confirmationDialog("删除“\(deleting?.title ?? "")”？", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { p in
            Button("只删项目，保留内容", role: .destructive) { delete(p, items: false) }
            Button("连同内容一起删除", role: .destructive) { delete(p, items: true) }
            Button("取消", role: .cancel) {}
        }
    }

    private func archive(_ p: Noted_V1_Project) {
        let on = !p.archived
        Task { if await data.setArchived(project: p.id, on) { toaster.show(on ? "已归档" : "已取消归档", systemImage: "archivebox") } }
    }

    private func delete(_ p: Noted_V1_Project, items: Bool) {
        Task {
            if await data.deleteProject(p.id, deleteItems: items) {
                toaster.show(items ? "已删除项目及内容" : "已删除项目", systemImage: "trash")
            }
        }
    }
}
