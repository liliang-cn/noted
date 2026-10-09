import SwiftUI

struct ReviewView: View {
    @Environment(Session.self) private var session
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @State private var review: Noted_V1_WeeklyReview?
    @State private var picked: Set<Int> = []
    @State private var error: String?
    @State private var done = false

    var body: some View {
        ZStack(alignment: .bottom) {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    HStack {
                        Button { dismiss() } label: { Label("概览", systemImage: "chevron.left").font(.system(size: 14, weight: .semibold)) }
                        Spacer()
                    }
                    if let r = review { content(r) }
                    ErrorLine(text: error)
                }
                .padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 90)
            }
            if let r = review, r.hasNextWeek, !r.nextWeek.operations.isEmpty, !done {
                Button { Task { await apply(r) } } label: {
                    Text("采纳所选 \(picked.count) 条").font(.system(size: 16, weight: .semibold)).foregroundStyle(.white)
                        .frame(maxWidth: .infinity).frame(height: 50)
                        .background(session.mode.accent(p), in: RoundedRectangle(cornerRadius: 12))
                }
                .disabled(picked.isEmpty).opacity(picked.isEmpty ? 0.5 : 1)
                .padding(.horizontal, 16).padding(.bottom, 12)
            }
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
    }

    @ViewBuilder private func content(_ r: Noted_V1_WeeklyReview) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Eyebrow(text: "\(Fmt.md(r.from.date)) – \(Fmt.md(r.to.date.addingTimeInterval(-1)))")
            Text("本周回顾").font(.system(size: 30, weight: .bold)).foregroundStyle(p.ink)
        }
        let achieved = r.goals.filter { $0.progress.achieved }.count
        HStack(spacing: 8) {
            stat("完成", "\(r.tasksDone)", "/\(r.tasksTotal)")
            stat("目标", "\(achieved)", "/\(r.goals.count)")
            stat("顺延", "\(r.carriedTasks.count)", "")
        }
        if !r.goals.isEmpty {
            Eyebrow(text: "目标")
            VStack(spacing: 0) {
                ForEach(Array(r.goals.enumerated()), id: \.element.id) { i, g in
                    if i > 0 { Divider().overlay(p.line) }
                    HStack(spacing: 12) {
                        Circle().fill(g.space.color(p)).frame(width: 8, height: 8)
                        Text(g.title).font(.system(size: 15, weight: .medium)).foregroundStyle(p.ink)
                        Spacer()
                        Text("\(Fmt.number(g.progress.done))/\(Fmt.number(g.progress.target))")
                            .font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2)
                        if g.progress.achieved {
                            tag("达标", p.ok, Color(hex: 0xE1F3EA))
                        } else {
                            tag(g.progress.done == 0 ? "未做" : "差 \(Fmt.number(g.progress.remaining))", p.bad, p.badBg)
                        }
                    }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
        if !r.carriedTasks.isEmpty {
            Eyebrow(text: "没做完的")
            VStack(spacing: 0) {
                ForEach(Array(r.carriedTasks.enumerated()), id: \.element.id) { i, t in
                    if i > 0 { Divider().overlay(p.line) }
                    HStack(spacing: 12) {
                        RoundedRectangle(cornerRadius: 6).strokeBorder(p.ink3, lineWidth: 1.75).frame(width: 20, height: 20)
                        Text(t.title).font(.system(size: 15, weight: .medium)).foregroundStyle(p.ink)
                        Spacer()
                        SpaceTag(space: t.space)
                    }.padding(.horizontal, 14).frame(minHeight: p.rowHeight)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        }
        if r.hasNextWeek, !r.nextWeek.operations.isEmpty {
            Eyebrow(text: done ? "已采纳" : "下周建议 · 勾选后采纳")
            VStack(spacing: 0) {
                ForEach(Array(r.nextWeek.operations.enumerated()), id: \.offset) { i, op in
                    if i > 0 { Divider().overlay(p.line) }
                    Button { if picked.contains(i) { picked.remove(i) } else { picked.insert(i) } } label: {
                        HStack(spacing: 12) {
                            Image(systemName: picked.contains(i) || done ? "checkmark.square.fill" : "square")
                                .font(.system(size: 20)).foregroundStyle(picked.contains(i) || done ? p.ok : p.ink3)
                            Text(op.label).font(.system(size: 15, weight: .medium)).foregroundStyle(p.ink).multilineTextAlignment(.leading)
                            Spacer()
                        }.padding(.horizontal, 14).frame(minHeight: p.rowHeight).contentShape(Rectangle())
                    }.buttonStyle(.plain).disabled(done)
                }
            }
            .background(p.surface, in: RoundedRectangle(cornerRadius: p.radius)).overlay(RoundedRectangle(cornerRadius: p.radius).stroke(p.line))
        } else {
            Text("下周没有要补的").font(.system(size: 14)).foregroundStyle(p.ink3).frame(maxWidth: .infinity, minHeight: 60).card()
        }
    }

    private func stat(_ label: String, _ big: String, _ small: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Eyebrow(text: label)
            (Text(big).foregroundStyle(p.ink) + Text(small).foregroundStyle(p.ink2)).font(.system(size: 26, weight: .semibold, design: .monospaced))
        }.frame(maxWidth: .infinity, alignment: .leading).card(padding: 12)
    }

    private func tag(_ t: String, _ fg: Color, _ bg: Color) -> some View {
        Text(t).font(.system(size: 11, weight: .semibold)).padding(.horizontal, 8).frame(height: 22)
            .background(bg, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(fg)
    }

    private func load() async {
        guard let api = session.api else { return }
        var r = Noted_V1_GetWeeklyReviewRequest()
        r.space = session.mode.space
        r.timeZone = TimeZone.current.identifier
        do {
            let v = try await api.suggest.getWeeklyReview(r)
            review = v
            picked = Set(v.nextWeek.operations.indices)
            error = nil
        } catch { self.error = describe(error) }
    }

    private func apply(_ r: Noted_V1_WeeklyReview) async {
        guard let api = session.api else { return }
        var req = Noted_V1_AcceptProposalRequest()
        req.id = r.nextWeek.id
        req.selection.indexes = picked.sorted().map { Int32($0) }
        do { _ = try await api.suggest.acceptProposal(req); done = true } catch { self.error = describe(error) }
    }
}
