import Foundation
import SwiftUI
import UIKit
import WidgetKit

enum SnapshotBuilder {
    static func make(focus: Noted_V1_GetFocusResponse, goals: [Noted_V1_Goal], objectives: [Noted_V1_Objective] = [], holdings: [Noted_V1_Holding] = [],
                     palette p: Palette, pro: Bool, now: Date = .now) -> Snapshot {
        var s = Snapshot()
        s.generated = now
        s.pro = pro
        s.colors = colors(p)
        s.items = focus.items.compactMap { f in
            switch f.item {
            case .event(let o):
                return Snapshot.Item(id: "e\(o.event.id)", title: o.event.title, time: o.startTime.date,
                                     allDay: o.event.allDay, work: o.event.space == .work)
            case .task(let t):
                return Snapshot.Item(id: "t\(t.id)", title: t.title, time: t.hasDueTime ? t.dueTime.date : nil,
                                     isTask: true, overdue: f.overdue, work: t.space == .work)
            case nil:
                return nil
            }
        }
        func goal(_ g: Noted_V1_Goal) -> Snapshot.Goal {
            Snapshot.Goal(id: g.id, title: g.title, done: g.progress.done, target: g.progress.target,
                          unit: g.unit, behind: g.progress.behind, work: g.space == .work)
        }
        s.goals = goals.filter { $0.objectiveID.isEmpty }.map(goal)
        s.plans = objectives.map { o in
            let pr = o.progress
            return Snapshot.Plan(id: o.id, title: o.title, work: o.space == .work, metricName: o.metricName, unit: o.metricUnit,
                                 current: pr.hasReading_p ? pr.metricCurrent : nil, start: o.metricStart, target: o.metricTarget,
                                 percent: pr.percent, daysLeft: pr.hasDaysLeft ? Int(pr.daysLeft) : nil,
                                 offTrack: pr.hasMetric_p && pr.hasReading_p && !pr.onTrack, dims: o.goals.map(goal))
        }
        s.holdings = holdings.map { h in
            let pos = h.position
            return Snapshot.Holding(id: h.id, symbol: h.symbol, planDay: Int(h.dcaDay), amount: h.dcaAmount, currency: h.currency,
                                    doneThisMonth: pos.dca.doneThisMonth, next: pos.dca.active ? pos.dca.nextTime.date : nil,
                                    shares: pos.shares, gainPercent: pos.hasPrice_p && pos.costBasis > 0 ? pos.unrealizedPercent : nil,
                                    streak: Int(pos.dca.streakMonths))
        }
        return s
    }

    static func colors(_ p: Palette) -> Snapshot.Colors {
        Snapshot.Colors(bg: hex(p.bg), surface: hex(p.surface), ink: hex(p.ink), ink2: hex(p.ink2), line: hex(p.line),
                        accent: hex(p.accentDef), work: hex(p.work), life: hex(p.life), bad: hex(p.bad))
    }

    static func hex(_ c: Color) -> UInt32 {
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        UIColor(c).getRed(&r, green: &g, blue: &b, alpha: &a)
        return (UInt32((r * 255).rounded()) << 16) | (UInt32((g * 255).rounded()) << 8) | UInt32((b * 255).rounded())
    }

    /// What the watch asked for, done here against the server.
    @MainActor static func perform(_ action: String, id: String, session: Session) async -> Snapshot? {
        guard let api = session.api else { return nil }
        do {
            switch action {
            case "complete":
                var r = Noted_V1_UpdateTaskRequest()
                r.task.id = id; r.task.completed = true; r.updateMask = fieldMask("completed")
                _ = try await api.calendar.updateTask(r)
            case "checkin":
                var r = Noted_V1_RecordCheckInRequest()
                r.goalID = id; r.amount = 1
                _ = try await api.goals.recordCheckIn(r)
            default:
                return nil
            }
        } catch { return nil }
        return await refresh(session: session)
    }

    /// Fetches today and the goals, then hands the result to the widgets and the watch.
    @MainActor @discardableResult
    static func refresh(session: Session) async -> Snapshot? {
        guard let api = session.api else { return nil }
        var f = Noted_V1_GetFocusRequest()
        f.horizon = .today
        f.timeZone = TimeZone.current.identifier
        guard let focus = try? await api.focus.getFocus(f) else { return nil }
        let goals = (try? await api.goals.listGoals(Noted_V1_ListGoalsRequest()).goals) ?? []
        var oreq = Noted_V1_ListObjectivesRequest(); oreq.space = .unspecified
        let objectives = (try? await api.objectives.listObjectives(oreq).objectives) ?? []
        let holdings = (try? await api.holdings.listHoldings(Noted_V1_ListHoldingsRequest()).holdings) ?? []
        let snap = make(focus: focus, goals: goals, objectives: objectives, holdings: holdings, palette: session.palette, pro: session.pro.isPro)
        SnapshotStore.save(snap)
        WidgetCenter.shared.reloadAllTimelines()
        WatchBridge.shared.push(snap)
        return snap
    }
}
