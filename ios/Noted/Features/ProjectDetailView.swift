import SwiftUI

struct ProjectDetailView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let id: String
    @State private var detail: Noted_V1_ProjectDetail?
    @State private var error: String?
    @State private var adding = false

    var body: some View {
        ZStack(alignment: .bottom) {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    HStack {
                        Button { dismiss() } label: {
                            Label("返回", systemImage: "chevron.left").font(.system(size: 14, weight: .semibold))
                        }
                        Spacer()
                        if let d = detail {
                            Button { Task { await togglePin(d.project) } } label: {
                                Text(d.project.pinned ? "已置顶" : "置顶").font(.system(size: 12, weight: .semibold))
                                    .padding(.horizontal, 10).frame(height: 24)
                                    .background(d.project.space == .work ? p.workBg : p.lifeBg, in: RoundedRectangle(cornerRadius: 6))
                                    .foregroundStyle(d.project.space.color(p))
                            }
                        }
                    }
                    ErrorLine(text: error)
                    if let d = detail { content(d) }
                }
                .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 90)
            }
            Button { adding = true } label: {
                Text("+ 添加到这个项目").font(.system(size: 16, weight: .semibold)).foregroundStyle(.white)
                    .frame(maxWidth: .infinity).frame(height: 50)
                    .background(accent, in: RoundedRectangle(cornerRadius: 12))
            }
            .padding(.horizontal, 16).padding(.bottom, 12)
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .sheet(isPresented: $adding, onDismiss: { Task { await load() } }) { CaptureView(projectID: id, initialSpace: detail?.project.space) }
    }

    private var accent: Color { detail?.project.space.color(p) ?? p.life }

    @ViewBuilder private func content(_ d: Noted_V1_ProjectDetail) -> some View {
        let pr = d.project.progress
        VStack(alignment: .leading, spacing: 8) {
            SpaceTag(space: d.project.space)
            Text(d.project.title).font(.system(size: 30, weight: .bold)).foregroundStyle(p.ink)
        }
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 6) {
                Eyebrow(text: "进度")
                (Text("\(pr.tasksDone)").foregroundStyle(p.ink) + Text("/\(pr.tasksTotal)").foregroundStyle(p.ink2))
                    .font(.system(size: 28, weight: .semibold, design: .monospaced))
                ProgressBar(value: pr.tasksTotal > 0 ? Double(pr.tasksDone) / Double(pr.tasksTotal) : 0, tint: accent)
            }
            Spacer(minLength: 24)
            VStack(alignment: .trailing, spacing: 6) {
                Eyebrow(text: dateLabel(d.project))
                if pr.hasDaysLeft {
                    (Text("\(pr.daysLeft)").foregroundStyle(accent) + Text(" 天").foregroundStyle(p.ink2))
                        .font(.system(size: 28, weight: .semibold, design: .monospaced))
                }
                if pr.tasksOverdue > 0 {
                    badge("逾期 \(pr.tasksOverdue)", p.bad, p.badBg)
                } else {
                    badge("没有逾期", p.ok, Color(hex: 0xE1F3EA))
                }
            }
        }
        .card()

        if !d.tasks.isEmpty {
            Eyebrow(text: "待办")
            VStack(spacing: 0) {
                ForEach(Array(d.tasks.enumerated()), id: \.element.id) { i, t in
                    if i > 0 { Divider().overlay(p.line) }
                    HStack(spacing: 12) {
                        Button { Task { await toggle(t) } } label: {
                            Image(systemName: t.completed ? "checkmark.square.fill" : "square")
                                .font(.system(size: 20)).foregroundStyle(t.completed ? p.ok : p.ink3)
                        }
                        .accessibilityLabel(t.completed ? "取消完成 \(t.title)" : "完成 \(t.title)")
                        Text(t.title).font(.system(size: 15, weight: t.completed ? .regular : .medium))
                            .strikethrough(t.completed).foregroundStyle(t.completed ? p.ink2 : p.ink)
                        Spacer()
                        if t.hasDueTime {
                            Text(Fmt.md(t.dueTime.date)).font(.system(size: 12, design: .monospaced)).foregroundStyle(t.completed ? p.ink3 : accent)
                        } else if !t.completed {
                            Text("没有日期").font(.system(size: 12)).foregroundStyle(p.ink3)
                        }
                    }
                    .padding(.horizontal, 14).frame(minHeight: p.rowHeight).opacity(t.completed ? 0.6 : 1)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
            .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
        if !d.events.isEmpty {
            Eyebrow(text: "日程")
            VStack(spacing: 0) {
                ForEach(Array(d.events.enumerated()), id: \.element.id) { i, e in
                    if i > 0 { Divider().overlay(p.line) }
                    HStack(spacing: 12) {
                        Text(Fmt.md(e.startTime.date)).font(.system(size: 12, weight: .medium, design: .monospaced))
                            .foregroundStyle(p.ink2).fixedSize().frame(width: 46, alignment: .leading)
                        Text(e.title).font(.system(size: 15, weight: .medium)).foregroundStyle(p.ink)
                        Spacer()
                        if !e.allDay { Text(Fmt.hm(e.startTime.date)).font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2) }
                    }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
            .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
        if !d.notes.isEmpty {
            Eyebrow(text: "笔记")
            VStack(spacing: 0) {
                ForEach(Array(d.notes.enumerated()), id: \.element.id) { i, n in
                    if i > 0 { Divider().overlay(p.line) }
                    HStack(spacing: 10) {
                        Text(n.title.isEmpty ? "无标题" : n.title).font(.system(size: 15, weight: .medium)).foregroundStyle(p.ink)
                        Text(n.content).font(.system(size: 13)).foregroundStyle(p.ink2).lineLimit(1)
                        Spacer(minLength: 0)
                    }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius))
            .overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
        if d.tasks.isEmpty && d.events.isEmpty && d.notes.isEmpty {
            Text("项目里还没有内容").font(.system(size: 14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 60).card()
        }
    }

    private func dateLabel(_ pr: Noted_V1_Project) -> String {
        if pr.hasStartTime { return "\(Fmt.md(pr.startTime.date))出发" }
        if pr.hasDueTime { return "\(Fmt.md(pr.dueTime.date))截止" }
        return "没有日期"
    }

    private func badge(_ text: String, _ fg: Color, _ bg: Color) -> some View {
        Text(text).font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
            .background(bg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(fg)
    }

    private func load() async {
        guard let api = session.api else { return }
        do {
            var r = Noted_V1_GetProjectRequest(); r.id = id
            detail = try await api.projects.getProject(r)
            error = nil
        } catch { self.error = describe(error) }
    }

    private func toggle(_ t: Noted_V1_Task) async {
        var r = Noted_V1_UpdateTaskRequest()
        r.task = t; r.task.completed = !t.completed; r.updateMask = fieldMask("completed")
        _ = try? await session.api?.calendar.updateTask(r)
        await load()
    }

    private func togglePin(_ pr: Noted_V1_Project) async {
        var r = Noted_V1_UpdateProjectRequest()
        r.project = pr; r.project.pinned.toggle(); r.updateMask = fieldMask("pinned")
        _ = try? await session.api?.projects.updateProject(r)
        await load()
    }
}
