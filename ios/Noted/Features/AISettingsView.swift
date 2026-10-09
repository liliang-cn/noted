import SwiftUI

struct AISettingsView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @State private var status: Noted_V1_GetStatusResponse?
    @State private var features = Noted_V1_AIFeatures()
    @State private var access = Noted_V1_AIAccess()
    @State private var changes: [Noted_V1_Change] = []
    @State private var error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("智能").font(.system(size: 30, weight: .bold)).foregroundStyle(p.ink)
            if let s = status, s.enabled {
                group {
                    row("状态") { Text(s.model).font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2); pill(s.chat) }
                    row("按含义搜索") { pill(s.semanticSearch) }
                }
                Eyebrow(text: "功能")
                group {
                    toggle("每日简报", $features.dailyBriefing)
                    toggle("建议", $features.suggestions)
                    toggle("每周回顾", $features.weeklyReview)
                    toggle("笔记摘要、标签、提取待办", $features.noteTools)
                }
                Eyebrow(text: "允许读取")
                group {
                    toggle("生活的内容", $access.allowLife, dot: p.life)
                    toggle("工作的内容", $access.allowWork, dot: p.work)
                }
                group {
                    row("写入前先确认") { Text("始终开启").font(.system(size: 13)).foregroundStyle(p.ink2) }
                    row("AI 的改动记录") { Text("\(changes.count) 条 · 可撤销").font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2) }
                }
            } else if status != nil {
                Text("这台服务器没有开启智能功能。笔记和日程不受影响。").font(.system(size: 14)).foregroundStyle(p.ink2).card()
            }
            ErrorLine(text: error)
            Spacer()
        }
        .padding(20)
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
    }

    private func group<C: View>(@ViewBuilder _ c: () -> C) -> some View {
        VStack(spacing: 0) {
            Group(subviews: c()) { subs in
                ForEach(subs.indices, id: \.self) { i in
                    if i > 0 { Divider().overlay(p.line) }
                    subs[i]
                }
            }
        }
        .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
    }

    private func row<C: View>(_ title: String, @ViewBuilder _ trailing: () -> C) -> some View {
        HStack(spacing: 10) {
            Text(title).font(.system(size: 15)).foregroundStyle(p.ink)
            Spacer()
            trailing()
        }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
    }

    private func toggle(_ title: String, _ value: Binding<Bool>, dot: Color? = nil) -> some View {
        let on = Binding(get: { value.wrappedValue }, set: { value.wrappedValue = $0; Task { await save() } })
        return HStack(spacing: 10) {
            if let dot { Circle().fill(dot).frame(width: 8, height: 8) }
            Text(title).font(.system(size: 15)).foregroundStyle(p.ink)
            Spacer()
            Toggle("", isOn: on).labelsHidden().accessibilityLabel(title)
        }.padding(.horizontal, 14).frame(minHeight: p.rowHeight).tapsToToggle(on)
    }

    private func pill(_ ok: Bool) -> some View {
        Text(ok ? "可用" : "未配置").font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
            .background(ok ? Color(hex: 0xE1F3EA) : p.surface2, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(ok ? p.ok : p.ink2)
    }

    private func load() async {
        guard let api = session.api else { return }
        do {
            status = try await api.ai.getStatus(Noted_V1_GetStatusRequest())
            guard status?.enabled == true else { return }
            features = try await api.ai.getAIFeatures(Noted_V1_GetAIFeaturesRequest())
            access = try await api.ai.getAIAccess(Noted_V1_GetAIAccessRequest())
            var c = Noted_V1_ListChangesRequest(); c.limit = 100
            changes = (try? await api.suggest.listChanges(c).changes) ?? []
        } catch { self.error = describe(error) }
    }

    private func save() async {
        guard let api = session.api else { return }
        do {
            var f = Noted_V1_SetAIFeaturesRequest()
            f.dailyBriefing = features.dailyBriefing; f.suggestions = features.suggestions
            f.weeklyReview = features.weeklyReview; f.noteTools = features.noteTools
            features = try await api.ai.setAIFeatures(f)
            var a = Noted_V1_SetAIAccessRequest()
            a.allowWork = access.allowWork; a.allowLife = access.allowLife
            access = try await api.ai.setAIAccess(a)
            await session.refreshAI()
            error = nil
        } catch { self.error = describe(error) }
    }
}
