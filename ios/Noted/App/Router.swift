// Navigation: four tabs, each its own NavigationStack with its own Router.
//
// Who owns what (each folder belongs to one screen agent; shared pieces live in App/, Data/, Design/):
//   Features/Today/     TodayView (tab root)          routes: .todo
//   Features/Calendar/  CalendarScreen (tab root)     routes: .event
//   Features/Plan/      PlanView (tab root)           routes: .project, .projectEdit, .projects, .linkExisting,
//                                                             .goal, .goalEdit, .goals, .objective, .holding, .invest
//   Features/Notes/     NotesScreen (tab root)        routes: .note
//   Features/Create/    CreateView                    routes: .create
//   Features/Settings/  SettingsScreen                routes: .settings, .aiConnect, .pair, .theme, .export, .pro
//   Features/SignIn/    SignInView (shown before connecting; not a route)
//
// Using it in a screen:
//   @Environment(Router.self) private var router     // the stack this screen is on
//   router.push(.project(id))   router.pop()   router.replaceTop(.project(id))   router.popToRoot()
//   router.select(.calendar)                          // switch tab (e.g. "日历 ›" on 今天)
//   NavigationLink(value: Route.todo(task.id)) { … }  // also works: the stack resolves Route values
//
// Adding a screen: add a case to `Route` (ids, not whole messages, so the page reads the live copy
// from AppData), then one line in `RouteDestination.body`. That's all.

import SwiftUI
import UIKit

/// The four tabs, in tab-bar order.
enum AppTab: String, CaseIterable, Identifiable, Hashable {
    case today, calendar, plan, notes
    var id: String { rawValue }
    var title: String {
        switch self {
        case .today: "今天"
        case .calendar: "日历"
        case .plan: "计划"
        case .notes: "笔记"
        }
    }
    var symbol: String {
        switch self {
        case .today: "sun.max"
        case .calendar: "calendar"
        case .plan: "target"
        case .notes: "note.text"
        }
    }
}

/// Every page that can be pushed.
enum Route: Hashable {
    // Today
    case todo(String)                         // task id
    // Calendar
    case event(String, occurrence: Date? = nil) // event id; the tapped instance's start for recurring events
    // Plan
    case project(String)
    case projectEdit(String)
    case projects                             // 项目 · 全部
    case linkExisting(projectID: String)      // 关联已有
    case goal(String)
    case goalEdit(String)
    case goals                                // 目标 · 全部
    case objective(String)
    case holding(String)
    case invest                               // 投资 · 全部
    // Notes
    case note(String)
    // Create
    case create(CreateSpec)
    // Settings
    case settings
    case aiConnect
    case pair(String)                         // a client's pairing code (normalised)
    case theme
    case export
    case pro
}

/// What the 新建 page opens with.
struct CreateSpec: Hashable {
    var kind: CreateKind = .todo
    /// The day to start on (a calendar day); nil: today.
    var date: Date?
    /// A time slot tapped on the calendar: an event starting then (kind becomes .event).
    var start: Date?
    /// Create inside this project; its space wins.
    var projectID = ""
    /// .unspecified: from the space filter (全部 → 个人).
    var space: Noted_V1_Space = .unspecified
}

enum CreateKind: String, CaseIterable, Identifiable, Hashable {
    case todo, event, project, goal
    var id: String { rawValue }
    /// 待办 / 日程 / 项目 / 目标
    var label: String {
        switch self {
        case .todo: "待办"
        case .event: "日程"
        case .project: "项目"
        case .goal: "目标"
        }
    }
}

/// One tab's stack.
@MainActor @Observable
final class Router {
    let tab: AppTab
    var path: [Route] = []
    /// Bumped when the person taps the tab they are already on, at its root: scroll to the top.
    private(set) var scrollToTopRequest = 0
    @ObservationIgnored weak var navigation: AppNavigation?

    init(tab: AppTab) { self.tab = tab }

    func push(_ r: Route) { path.append(r) }

    func pop() { if !path.isEmpty { path.removeLast() } }

    func popToRoot() { path.removeAll() }

    /// Swaps the top page for another without stacking, e.g. 新建 turning into the note it created.
    func replaceTop(_ r: Route) {
        if path.isEmpty { path.append(r) } else { path[path.count - 1] = r }
    }

    /// Switches to another tab (its stack stays as it was).
    func select(_ tab: AppTab) { navigation?.select(tab) }

    fileprivate func requestScrollToTop() { scrollToTopRequest += 1 }
}

/// The tab selection and the four routers. Screens normally use `Router`; this is for the shell.
@MainActor @Observable
final class AppNavigation {
    var tab: AppTab
    let routers: [AppTab: Router]

    init() {
        var t = AppTab.today
        #if DEBUG
        if let raw = ProcessInfo.processInfo.environment["NOTED_TAB"], let v = AppTab(rawValue: raw) { t = v }
        #endif
        tab = t
        routers = Dictionary(uniqueKeysWithValues: AppTab.allCases.map { ($0, Router(tab: $0)) })
        for r in routers.values { r.navigation = self }
    }

    func router(_ t: AppTab) -> Router { routers[t]! }
    var current: Router { router(tab) }

    /// Tab-bar tap: switch, or on the current tab go back to its root, or scroll the root to the top.
    func select(_ t: AppTab) {
        guard t == tab else { tab = t; return }
        let r = router(t)
        if r.path.isEmpty { r.requestScrollToTop() } else { r.popToRoot() }
    }

    /// The "+" button: 新建 on the current tab's stack.
    func create(_ spec: CreateSpec) { current.push(.create(spec)) }

    /// A pairing link opened from outside (the system camera): its page on the current tab's stack.
    func openPairing(_ code: String) {
        let r = current
        if case .pair(let shown) = r.path.last, shown == code { return }
        r.push(.pair(code))
    }
}

/// Maps a route to its page. Pages own their title, toolbar and data loading.
struct RouteDestination: View {
    let route: Route

    var body: some View {
        switch route {
        case .todo(let id): TodoDetailView(id: id)
        case .event(let id, let occurrence): EventDetailView(eventID: id, occurrence: occurrence)
        case .project(let id): ProjectDetailView(projectID: id)
        case .projectEdit(let id): ProjectEditView(projectID: id)
        case .projects: ProjectsListView()
        case .linkExisting(let id): LinkExistingView(projectID: id)
        case .goal(let id): GoalDetailView(goalID: id)
        case .goalEdit(let id): GoalEditView(goalID: id)
        case .goals: GoalsListView()
        case .objective(let id): ObjectiveDetailView(objectiveID: id)
        case .holding(let id): HoldingDetailView(holdingID: id)
        case .invest: InvestView()
        case .note(let id): NoteScreen(id: id)
        case .create(let spec): CreateView(spec: spec)
        case .settings: SettingsScreen()
        case .aiConnect: AIConnectScreen()
        case .pair(let code): PairScreen(code: code)
        case .theme: ThemeScreen()
        case .export: ExportScreen()
        case .pro: ProScreen()
        }
    }
}

/// Stands in for a page that is not built yet.
struct RoutePlaceholder: View {
    let title: String
    var detail = ""

    var body: some View {
        List {
            Section { EmptyState(text: detail.isEmpty ? title : "\(title) · \(detail)") }
        }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
    }
}

extension View {
    /// Runs `action` each time the page comes back into view after something pushed on top of it
    /// is popped, but not on the first appearance (its own `.task` covers that).
    func reloadOnReturn(_ action: @escaping @MainActor () async -> Void) -> some View {
        modifier(ReloadOnReturn(action: action))
    }
}

private struct ReloadOnReturn: ViewModifier {
    let action: @MainActor () async -> Void
    @State private var seen = false

    func body(content: Content) -> some View {
        content.onAppear {
            if seen { Task { await action() } } else { seen = true }
        }
    }
}
