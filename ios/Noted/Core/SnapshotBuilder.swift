import Foundation
import SwiftUI
import UIKit
import WidgetKit

enum SnapshotBuilder {
    static func make(focus: Noted_V1_GetFocusResponse, goals: [Noted_V1_Goal], palette p: Palette, pro: Bool, now: Date = .now) -> Snapshot {
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
        s.goals = goals.map { g in
            Snapshot.Goal(id: g.id, title: g.title, done: g.progress.done, target: g.progress.target,
                          unit: g.unit, behind: g.progress.behind, work: g.space == .work)
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
        let snap = make(focus: focus, goals: goals, palette: session.palette, pro: session.pro.isPro)
        SnapshotStore.save(snap)
        WidgetCenter.shared.reloadAllTimelines()
        WatchBridge.shared.push(snap)
        return snap
    }
}
