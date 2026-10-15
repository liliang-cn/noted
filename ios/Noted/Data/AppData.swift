import Foundation
import Observation
import SwiftProtobuf

/// Everything the screens show, loaded once and kept in memory, plus every write the app makes.
///
/// Read it from the environment and use the arrays and lookups directly; they are observable,
/// so a view redraws when a write lands:
///
///     @Environment(AppData.self) private var data
///     ForEach(data.tasks(on: day)) { ... }               // space-filtered by Session.mode
///     Button("完成") { Task { await data.setCompleted(task.id, true) } }
///
/// Lists (`tasks`, `notes`, `projects`, …) hold every space; the helpers below filter by the
/// 全部/个人/工作 menu (Session.mode). Use `inSpace(_:)` when filtering a list yourself.
///
/// Writes are `async`, never throw, and report failures through the Toaster (so a screen only
/// shows its own success toast). They return the server's object, or nil / false on failure.
/// After every write the reminders, widgets and watch are resynced (Session.syncExtras).
/// For a call not wrapped here use `perform { api in ... }`.
@MainActor @Observable
final class AppData {
    let session: Session
    let toaster: Toaster

    // MARK: state

    /// Every task, open and completed.
    var tasks: [Noted_V1_Task] = []
    /// Every unarchived note, newest first.
    var notes: [Noted_V1_Note] = []
    /// Every project, archived ones included.
    var projects: [Noted_V1_Project] = []
    /// Every unarchived goal, the dimensions of objectives included (objectiveID non-empty).
    var goals: [Noted_V1_Goal] = []
    /// Every unarchived objective (larger goals with dimensions and a metric).
    var objectives: [Noted_V1_Objective] = []
    /// Every unarchived holding.
    var holdings: [Noted_V1_Holding] = []
    /// Event occurrences of the months loaded with `loadMonth`, ordered by start.
    var occurrences: [Noted_V1_Occurrence] = []
    var account: Noted_V1_Account?
    /// The account's API tokens, newest first (load with `loadTokens`).
    var tokens: [Noted_V1_TokenInfo] = []

    /// Per-item extras, filled by the matching `load…` call.
    var projectDetails: [String: Noted_V1_ProjectDetail] = [:]
    var taskWork: [String: [Noted_V1_TaskWork]] = [:]      // task id → sittings, newest first
    var checkIns: [String: [Noted_V1_CheckIn]] = [:]       // goal id → newest first
    var measurements: [String: [Noted_V1_Measurement]] = [:] // objective id → oldest first
    var trades: [String: [Noted_V1_Trade]] = [:]           // holding id → newest first

    /// True once the first `refreshAll` finished (show placeholders before).
    private(set) var isLoaded = false
    private(set) var isRefreshing = false

    /// Events fetched one by one (opened from a project, a note reference …).
    var looseEvents: [String: Noted_V1_Event] = [:]
    private var loadedMonths: Set<Date> = []

    init(session: Session, toaster: Toaster) {
        self.session = session
        self.toaster = toaster
    }

    // MARK: space filter

    /// Whether something in `space` shows under the current 全部/个人/工作 choice.
    func inSpace(_ space: Noted_V1_Space) -> Bool {
        switch session.mode {
        case .all: true
        case .life: space != .work
        case .work: space == .work
        }
    }

    // MARK: lookups (not space-filtered)

    func task(_ id: String) -> Noted_V1_Task? { tasks.first { $0.id == id } ?? projectDetails.values.lazy.flatMap(\.tasks).first { $0.id == id } }
    func note(_ id: String) -> Noted_V1_Note? { notes.first { $0.id == id } ?? projectDetails.values.lazy.flatMap(\.notes).first { $0.id == id } }
    func project(_ id: String) -> Noted_V1_Project? { id.isEmpty ? nil : projects.first { $0.id == id } }
    func projectTitle(_ id: String) -> String? { project(id)?.title }
    func goal(_ id: String) -> Noted_V1_Goal? { goals.first { $0.id == id } ?? objectives.lazy.flatMap(\.goals).first { $0.id == id } }
    func objective(_ id: String) -> Noted_V1_Objective? { objectives.first { $0.id == id } }
    func holding(_ id: String) -> Noted_V1_Holding? { holdings.first { $0.id == id } }
    func event(_ id: String) -> Noted_V1_Event? {
        occurrences.first { $0.event.id == id }?.event ?? looseEvents[id]
            ?? projectDetails.values.lazy.flatMap(\.events).first { $0.id == id }
    }

    // MARK: derived lists (space-filtered)

    /// Tasks due on `day`: timed ones by time, then date-only ones, open before done.
    func tasks(on day: Date) -> [Noted_V1_Task] {
        tasks.filter { inSpace($0.space) && $0.hasDueTime && $0.dueTime.date.isSameDay(as: day) }
            .sorted { a, b in
                if a.hasClockTime != b.hasClockTime { return a.hasClockTime }
                if a.hasClockTime { return a.dueTime.date < b.dueTime.date }
                if a.completed != b.completed { return !a.completed }
                return a.createTime.date < b.createTime.date
            }
    }

    /// Open tasks due on a day before `now`'s day, oldest first.
    func overdueTasks(now: Date = .now) -> [Noted_V1_Task] {
        tasks.filter { !$0.completed && inSpace($0.space) && $0.hasDueTime && $0.dueTime.date.startOfDay < now.startOfDay }
            .sorted { $0.dueTime.date < $1.dueTime.date }
    }

    /// Open tasks with work logged on them (进行中), most recently worked first.
    var inProgressTasks: [Noted_V1_Task] {
        tasks.filter { $0.isInProgress && inSpace($0.space) }.sorted { $0.lastWorkedTime.date > $1.lastWorkedTime.date }
    }

    /// Open tasks without a date.
    var undatedTasks: [Noted_V1_Task] { tasks.filter { !$0.completed && !$0.hasDueTime && inSpace($0.space) } }

    /// Occurrences on `day` (call `loadMonth` for it first): all-day ones first, then by start.
    func occurrences(on day: Date) -> [Noted_V1_Occurrence] {
        let start = day.startOfDay, end = start.adding(days: 1)
        return occurrences.filter { o in
            inSpace(o.event.space) && o.start < end && (o.end > start || (o.start >= start && o.start < end))
        }
        .sorted { a, b in a.event.allDay != b.event.allDay ? a.event.allDay : a.start < b.start }
    }

    /// Unarchived projects, pinned first (server order).
    var activeProjects: [Noted_V1_Project] { projects.filter { !$0.archived && inSpace($0.space) } }
    var archivedProjects: [Noted_V1_Project] { projects.filter { $0.archived && inSpace($0.space) } }
    var pinnedProjects: [Noted_V1_Project] { activeProjects.filter(\.pinned) }
    /// Goals that are not a dimension of an objective.
    var standaloneGoals: [Noted_V1_Goal] { goals.filter { $0.objectiveID.isEmpty && inSpace($0.space) } }
    var visibleObjectives: [Noted_V1_Objective] { objectives.filter { inSpace($0.space) } }
    var visibleNotes: [Noted_V1_Note] { notes.filter { inSpace($0.space) } }
    /// Tags used on any note, most used first.
    var noteTags: [String] {
        var count: [String: Int] = [:]
        for n in notes { for t in n.tags { count[t, default: 0] += 1 } }
        return count.sorted { $0.value != $1.value ? $0.value > $1.value : $0.key < $1.key }.map(\.key)
    }

    // MARK: loading

    /// Loads every list (and the current month's events). Call on launch, on return to the
    /// foreground and from pull-to-refresh.
    func refreshAll() async {
        guard let api = session.api else { return }
        isRefreshing = true
        defer { isRefreshing = false; isLoaded = true }
        async let t = Self.fetchTasks(api)
        async let n = Self.fetchNotes(api)
        async let p = Self.fetchProjects(api)
        async let g = Self.fetchGoals(api)
        async let o = Self.fetchObjectives(api)
        async let h = Self.fetchHoldings(api)
        async let a = try? api.account.getAccount(Noted_V1_GetAccountRequest())
        var failure: Error?
        do { tasks = try await t } catch { failure = error }
        do { notes = try await n } catch { failure = error }
        do { projects = try await p } catch { failure = error }
        do { goals = try await g } catch { failure = error }
        do { objectives = try await o } catch { failure = error }
        do { holdings = try await h } catch { failure = error }
        if let acc = await a { account = acc; session.pro.setServerGrant(acc.pro) }
        let months = loadedMonths.isEmpty ? [Date.now.startOfMonth] : Array(loadedMonths)
        loadedMonths = []
        for m in months { await loadMonth(m) }
        if let failure { toaster.error(failure) }
    }

    func refreshTasks() async { if let v = await perform(Self.fetchTasks) { tasks = v } }
    func refreshNotes() async { if let v = await perform(Self.fetchNotes) { notes = v } }
    func refreshProjects() async { if let v = await perform(Self.fetchProjects) { projects = v } }
    func refreshGoals() async {
        async let g = perform(Self.fetchGoals)
        async let o = perform(Self.fetchObjectives)
        if let v = await g { goals = v }
        if let v = await o { objectives = v }
    }
    func refreshHoldings() async { if let v = await perform(Self.fetchHoldings) { holdings = v } }

    /// Loads the event occurrences of the month containing `date`, padded to whole weeks
    /// (what a month grid shows). Cached; pass `force` to reload.
    func loadMonth(_ date: Date, force: Bool = false) async {
        let month = date.startOfMonth
        guard force || !loadedMonths.contains(month) else { return }
        let from = month.startOfWeek
        let to = (Fmt.calendar.date(byAdding: .month, value: 1, to: month) ?? month).startOfWeek.adding(days: 7)
        var r = Noted_V1_ListEventsRequest()
        r.from = Google_Protobuf_Timestamp(date: from)
        r.to = Google_Protobuf_Timestamp(date: to)
        guard let got = await perform({ try await $0.calendar.listEvents(r).occurrences }) else { return }
        loadedMonths.insert(month)
        var byKey = Dictionary(occurrences.filter { $0.start < from || $0.start >= to }.map { ($0.key, $0) }, uniquingKeysWith: { a, _ in a })
        for o in got { byKey[o.key] = o }
        occurrences = byKey.values.sorted { $0.start < $1.start }
    }

    /// Reloads every month loaded so far (after an event changed).
    func reloadEvents() async {
        let months = loadedMonths.isEmpty ? [Date.now.startOfMonth] : Array(loadedMonths)
        for m in months { await loadMonth(m, force: true) }
    }

    /// Fetches one project with its tasks, events, notes, goals and objectives into `projectDetails`.
    @discardableResult
    func loadProject(_ id: String) async -> Noted_V1_ProjectDetail? {
        var r = Noted_V1_GetProjectRequest(); r.id = id
        guard let d = await perform({ try await $0.projects.getProject(r) }) else { return nil }
        projectDetails[id] = d
        upsert(d.project)
        return d
    }

    @discardableResult
    func loadTask(_ id: String) async -> Noted_V1_Task? {
        var r = Noted_V1_GetTaskRequest(); r.id = id
        guard let t = await perform({ try await $0.calendar.getTask(r) }) else { return nil }
        upsert(t)
        return t
    }

    @discardableResult
    func loadEvent(_ id: String) async -> Noted_V1_Event? {
        var r = Noted_V1_GetEventRequest(); r.id = id
        guard let e = await perform({ try await $0.calendar.getEvent(r) }) else { return nil }
        looseEvents[id] = e
        replaceEvent(e)
        return e
    }

    @discardableResult
    func loadNote(_ id: String) async -> Noted_V1_Note? {
        var r = Noted_V1_GetNoteRequest(); r.id = id
        guard let n = await perform({ try await $0.notes.getNote(r) }) else { return nil }
        upsert(n)
        return n
    }

    func loadTaskWork(_ taskID: String) async {
        var r = Noted_V1_ListTaskWorkRequest(); r.taskID = taskID
        if let v = await perform({ try await $0.calendar.listTaskWork(r).entries }) { taskWork[taskID] = v }
    }

    func loadCheckIns(_ goalID: String, limit: Int32 = 100) async {
        var r = Noted_V1_ListCheckInsRequest(); r.goalID = goalID; r.limit = limit
        if let v = await perform({ try await $0.goals.listCheckIns(r).checkIns }) { checkIns[goalID] = v }
    }

    func loadMeasurements(_ objectiveID: String) async {
        var r = Noted_V1_ListMeasurementsRequest(); r.objectiveID = objectiveID
        if let v = await perform({ try await $0.objectives.listMeasurements(r).measurements }) { measurements[objectiveID] = v }
    }

    func loadTrades(_ holdingID: String) async {
        var r = Noted_V1_ListTradesRequest(); r.holdingID = holdingID
        if let v = await perform({ try await $0.holdings.listTrades(r).trades }) { trades[holdingID] = v.reversed() }
    }

    func loadAccount() async {
        if let a = await perform({ try await $0.account.getAccount(Noted_V1_GetAccountRequest()) }) {
            account = a
            session.pro.setServerGrant(a.pro)
        }
    }

    func loadTokens() async {
        if let v = await perform({ try await $0.account.listTokens(Noted_V1_ListTokensRequest()).tokens }) { tokens = v }
    }

    /// Forgets everything (after signing out).
    func reset() {
        tasks = []; notes = []; projects = []; goals = []; objectives = []; holdings = []
        occurrences = []; account = nil; tokens = []
        projectDetails = [:]; taskWork = [:]; checkIns = [:]; measurements = [:]; trades = [:]
        looseEvents = [:]; loadedMonths = []; isLoaded = false
    }

    // MARK: calling the server

    /// Runs one call against the server. On failure the error is shown as a toast and nil comes back.
    func perform<T: Sendable>(_ op: (API) async throws -> T) async -> T? {
        guard let api = session.api else { return nil }
        do {
            return try await op(api)
        } catch {
            toaster.error(error)
            return nil
        }
    }

    /// Mirrors a change outside the app: reminders, widgets, the watch.
    func didWrite() {
        Task { await session.syncExtras() }
    }

    // MARK: fetching

    nonisolated static func fetchTasks(_ api: API) async throws -> [Noted_V1_Task] {
        var out: [Noted_V1_Task] = []
        var r = Noted_V1_ListTasksRequest()
        r.filter = .all; r.pageSize = 200
        for _ in 0..<25 {
            let resp = try await api.calendar.listTasks(r)
            out += resp.tasks
            guard !resp.nextPageToken.isEmpty else { break }
            r.pageToken = resp.nextPageToken
        }
        return out
    }

    nonisolated static func fetchNotes(_ api: API) async throws -> [Noted_V1_Note] {
        var out: [Noted_V1_Note] = []
        var r = Noted_V1_ListNotesRequest()
        r.pageSize = 200
        for _ in 0..<25 {
            let resp = try await api.notes.listNotes(r)
            out += resp.notes
            guard !resp.nextPageToken.isEmpty else { break }
            r.pageToken = resp.nextPageToken
        }
        return out.sorted { $0.updateTime.date > $1.updateTime.date }
    }

    nonisolated static func fetchProjects(_ api: API) async throws -> [Noted_V1_Project] {
        var r = Noted_V1_ListProjectsRequest(); r.includeArchived = true
        return try await api.projects.listProjects(r).projects
    }

    nonisolated static func fetchGoals(_ api: API) async throws -> [Noted_V1_Goal] {
        try await api.goals.listGoals(Noted_V1_ListGoalsRequest()).goals
    }

    nonisolated static func fetchObjectives(_ api: API) async throws -> [Noted_V1_Objective] {
        try await api.objectives.listObjectives(Noted_V1_ListObjectivesRequest()).objectives
    }

    nonisolated static func fetchHoldings(_ api: API) async throws -> [Noted_V1_Holding] {
        try await api.holdings.listHoldings(Noted_V1_ListHoldingsRequest()).holdings
    }

    // MARK: local cache upkeep

    func upsert(_ t: Noted_V1_Task) {
        if let i = tasks.firstIndex(where: { $0.id == t.id }) { tasks[i] = t } else { tasks.append(t) }
        for (k, d) in projectDetails {
            if let i = d.tasks.firstIndex(where: { $0.id == t.id }) {
                if t.projectID == k { projectDetails[k]?.tasks[i] = t } else { projectDetails[k]?.tasks.remove(at: i) }
            } else if t.projectID == k {
                projectDetails[k]?.tasks.append(t)
            }
        }
    }

    func upsert(_ n: Noted_V1_Note) {
        if let i = notes.firstIndex(where: { $0.id == n.id }) { notes[i] = n } else { notes.insert(n, at: 0) }
        notes.sort { $0.updateTime.date > $1.updateTime.date }
        for (k, d) in projectDetails {
            if let i = d.notes.firstIndex(where: { $0.id == n.id }) {
                if n.projectID == k { projectDetails[k]?.notes[i] = n } else { projectDetails[k]?.notes.remove(at: i) }
            } else if n.projectID == k {
                projectDetails[k]?.notes.insert(n, at: 0)
            }
        }
    }

    func upsert(_ p: Noted_V1_Project) {
        if let i = projects.firstIndex(where: { $0.id == p.id }) { projects[i] = p } else { projects.insert(p, at: 0) }
        if projectDetails[p.id] != nil { projectDetails[p.id]?.project = p }
    }

    func upsert(_ g: Noted_V1_Goal) {
        if let i = goals.firstIndex(where: { $0.id == g.id }) { goals[i] = g } else { goals.append(g) }
        for (i, o) in objectives.enumerated() {
            if let j = o.goals.firstIndex(where: { $0.id == g.id }) { objectives[i].goals[j] = g }
        }
        for (k, d) in projectDetails {
            if let i = d.goals.firstIndex(where: { $0.id == g.id }) { projectDetails[k]?.goals[i] = g }
        }
    }

    func upsert(_ o: Noted_V1_Objective) {
        if let i = objectives.firstIndex(where: { $0.id == o.id }) { objectives[i] = o } else { objectives.append(o) }
        for g in o.goals {
            if let i = goals.firstIndex(where: { $0.id == g.id }) { goals[i] = g }
        }
        for (k, d) in projectDetails {
            if let i = d.objectives.firstIndex(where: { $0.id == o.id }) { projectDetails[k]?.objectives[i] = o }
        }
    }

    func upsert(_ h: Noted_V1_Holding) {
        if let i = holdings.firstIndex(where: { $0.id == h.id }) { holdings[i] = h } else { holdings.append(h) }
    }

    /// Puts a changed event into every occurrence of it and the other caches.
    func replaceEvent(_ e: Noted_V1_Event) {
        for i in occurrences.indices where occurrences[i].event.id == e.id { occurrences[i].event = e }
        if looseEvents[e.id] != nil { looseEvents[e.id] = e }
        for (k, d) in projectDetails {
            if let i = d.events.firstIndex(where: { $0.id == e.id }) {
                if e.projectID == k { projectDetails[k]?.events[i] = e } else { projectDetails[k]?.events.remove(at: i) }
            } else if e.projectID == k {
                projectDetails[k]?.events.append(e)
            }
        }
    }

    /// Reloads what shows a project's progress after something in it changed.
    func projectChanged(_ ids: String...) {
        let ids = Set(ids.filter { !$0.isEmpty })
        guard !ids.isEmpty else { return }
        Task {
            await refreshProjects()
            for id in ids where projectDetails[id] != nil { await loadProject(id) }
        }
    }
}
