import SwiftUI

/// 新建 (pushed by the "+", 日历's empty slots, a project's 添加 menu …): 待办 · 日程 · 项目 · 目标.
/// Each kind keeps an unsaved draft. Notes are written from 笔记, not here.
struct CreateView: View {
    @Environment(AppData.self) private var data
    @Environment(Session.self) private var session
    @Environment(Router.self) private var router
    @Environment(Toaster.self) private var toaster
    let spec: CreateSpec

    @State private var kind: CreateKind = .todo
    @State private var drafts: [CreateKind: CreateDraft] = [:]
    /// Kinds whose draft came back from an earlier visit (草稿已保存).
    @State private var restored: Set<CreateKind> = []
    @State private var started = false
    @State private var saving = false
    @State private var pickDay = false
    @State private var addingDimension = false
    @State private var confirmDiscard = false
    @FocusState private var titleFocused: Bool

    var body: some View {
        List {
            Section {
                Picker("类型", selection: kindSelection) {
                    ForEach(CreateKind.allCases) { Text($0.label).tag($0) }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
                .accessibilityIdentifier("create-kind")
            }
            .listSectionSpacing(16)

            titleSection
            if kind == .todo || kind == .event { chipsSection }
            switch kind {
            case .todo: TodoFormSections(draft: draft, pickDay: $pickDay)
            case .event: EventFormSections(draft: draft, pickDay: $pickDay)
            case .project: ProjectFormSections(draft: draft)
            case .goal: GoalFormSections(draft: draft) { addingDimension = true }
            }
        }
        .listStyle(.insetGrouped)
        .scrollDismissesKeyboard(.interactively)
        .navigationTitle("新建")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                VStack(spacing: 0) {
                    Text("新建").font(.headline)
                    if showsHint {
                        Text("草稿已保存").font(.caption2.weight(.semibold)).foregroundStyle(.success)
                    }
                }
                .accessibilityElement(children: .combine)
            }
            if showsHint {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("丢弃") { confirmDiscard = true }
                        .accessibilityLabel("丢弃草稿")
                        .accessibilityIdentifier("create-discard")
                }
            }
        }
        .confirmationDialog("丢弃这份草稿？", isPresented: $confirmDiscard, titleVisibility: .visible) {
            Button("丢弃草稿", role: .destructive, action: discard)
        }
        .safeAreaInset(edge: .bottom) {
            BottomBar {
                Button {
                    Task { await add() }
                } label: {
                    if saving { ProgressView().tint(.white) } else { Text("添加\(kind.label)") }
                }
                .buttonStyle(.wide)
                .disabled(draft.wrappedValue.title.trimmedText.isEmpty || saving)
                .accessibilityLabel("添加\(kind.label)")
                .accessibilityIdentifier("create-add")
            }
        }
        .sheet(isPresented: $addingDimension) {
            DimensionSheet { d in withAnimation { drafts[kind]?.dimensions.append(d) } }
        }
        .onAppear(perform: start)
        .onChange(of: drafts[kind]) { _, d in
            if let d { DraftStore.save(d, for: kind) }
        }
    }

    // MARK: sections

    private var titleSection: some View {
        Section {
            TextField(placeholder, text: draft.title)
                .font(.system(size: 22, weight: .semibold))
                .frame(minHeight: 40)
                .focused($titleFocused)
                .submitLabel(.done)
                .onSubmit { if draft.wrappedValue.title.trimmedText.isEmpty { titleFocused = false } else { Task { await add() } } }
                .accessibilityLabel("标题")
                .accessibilityIdentifier("create-title")
        }
    }

    private var chipsSection: some View {
        Section {
            QuickDateChips(date: chipDate, allowsNone: kind == .todo) {
                if kind == .todo, draft.wrappedValue.due == nil { draft.wrappedValue.due = Date.now.startOfDay }
                withAnimation(.snappy) { pickDay.toggle() }
            }
            .padding(.horizontal, -16)
            .listRowBackground(Color.clear)
            .listRowInsets(EdgeInsets())
        }
        .listSectionSpacing(16)
    }

    private var placeholder: String {
        switch kind {
        case .todo: "要做什么"
        case .event: "日程标题"
        case .project: "项目名称"
        case .goal: "目标名称，如 健身"
        }
    }

    // MARK: state

    private var draft: Binding<CreateDraft> {
        Binding(get: { drafts[kind] ?? CreateDraft() }, set: { drafts[kind] = $0 })
    }

    private var showsHint: Bool {
        restored.contains(kind) && !(drafts[kind]?.isEmpty ?? true)
    }

    /// The chips: a to-do's day (keeps its time), or the day an event starts on (keeps its times).
    private var chipDate: Binding<Date?> {
        Binding(get: {
            kind == .event ? draft.wrappedValue.start : draft.wrappedValue.due
        }, set: { new in
            withAnimation(.snappy) { pickDay = false }
            if kind == .event {
                guard let new else { return }
                var d = draft.wrappedValue
                let m = d.start.minuteOfDay
                let length = d.end.timeIntervalSince(d.start)
                d.start = new.startOfDay.at(hour: m / 60, minute: m % 60)
                d.end = d.start.addingTimeInterval(length)
                draft.wrappedValue = d
            } else {
                draft.wrappedValue.due = new
            }
        })
    }

    private var kindSelection: Binding<CreateKind> {
        Binding(get: { kind }, set: { new in
            let old = kind
            guard new != old, !saving else { return }
            if drafts[new] == nil { drafts[new] = loadOrFresh(new) }
            // What was typed follows to the new kind when it has no title of its own.
            if let t = drafts[old]?.title, !t.trimmedText.isEmpty, drafts[new]?.title.trimmedText.isEmpty ?? true {
                drafts[new]?.title = t
                drafts[old]?.title = ""
                if let o = drafts[old] { DraftStore.save(o, for: old) }
            }
            pickDay = false
            kind = new
        })
    }

    private func start() {
        guard !started else { return }
        started = true
        let k: CreateKind = spec.start != nil ? .event : spec.kind
        drafts[k] = loadOrFresh(k)
        kind = k
        Task {
            try? await Task.sleep(for: .milliseconds(450))
            titleFocused = true
        }
    }

    /// A blank draft from the spec: its day, slot, project (whose space wins) and space.
    private func fresh(_ k: CreateKind) -> CreateDraft {
        var d = CreateDraft()
        let today = Date.now.startOfDay
        let day = (spec.date ?? .now).startOfDay
        var space = spec.space != .unspecified ? spec.space : session.mode.space
        if space == .unspecified { space = .life }
        d.space = space
        if !spec.projectID.isEmpty {
            d.projectID = spec.projectID
            if let p = data.project(spec.projectID) { d.space = p.space }
        }
        d.due = day
        d.start = spec.start ?? defaultStart(on: day)
        d.end = d.start.addingTimeInterval(3600)
        d.projectStart = today
        d.projectDue = today.adding(days: 30)
        return d
    }

    /// The kind's saved draft (with the spec's day, slot and project applied), else a blank one.
    private func loadOrFresh(_ k: CreateKind) -> CreateDraft {
        guard var d = DraftStore.load(k) else { return fresh(k) }
        restored.insert(k)
        let today = Date.now.startOfDay
        if let day = spec.date?.startOfDay {
            if let due = d.due { d.due = day.at(hour: due.minuteOfDay / 60, minute: due.minuteOfDay % 60) }
            moveEvent(&d, to: day)
        }
        if let s = spec.start {
            d.start = s; d.end = s.addingTimeInterval(3600); d.allDay = false
        }
        if !spec.projectID.isEmpty {
            d.projectID = spec.projectID
            if let p = data.project(spec.projectID) { d.space = p.space }
        }
        // A draft left on an earlier day moves to today.
        if let due = d.due, due < today { d.due = today.at(hour: due.minuteOfDay / 60, minute: due.minuteOfDay % 60) }
        if d.start < today { moveEvent(&d, to: today) }
        if d.projectDue < d.projectStart { d.projectDue = d.projectStart }
        return d
    }

    private func moveEvent(_ d: inout CreateDraft, to day: Date) {
        let m = d.start.minuteOfDay
        let length = d.end.timeIntervalSince(d.start)
        d.start = day.at(hour: m / 60, minute: m % 60)
        d.end = d.start.addingTimeInterval(length)
    }

    /// Today: the next whole hour (at most 23:00); another day: 09:00.
    private func defaultStart(on day: Date) -> Date {
        guard day.isSameDay(as: .now) else { return day.at(hour: 9) }
        return day.at(hour: min(Date.now.minuteOfDay / 60 + 1, 23))
    }

    private func discard() {
        DraftStore.clear(kind)
        restored.remove(kind)
        withAnimation(.snappy) { drafts[kind] = fresh(kind) }
        pickDay = false
        titleFocused = true
    }

    // MARK: writes

    private func add() async {
        let d = draft.wrappedValue
        let title = d.title.trimmedText
        guard !title.isEmpty, !saving else { return }
        saving = true
        defer { saving = false }
        let k = kind
        switch k {
        case .todo:
            var t = Noted_V1_Task()
            t.title = title
            t.notes = d.notes.trimmedText
            t.priority = d.priority
            t.projectID = d.projectID
            t.space = d.space
            if let due = d.due {
                t.dueTime = .init(date: due)
                if due.minuteOfDay != 0 { t.setReminder(d.remind) }
                t.rrule = d.repeatRule.rrule
            }
            guard await data.createTask(t) != nil else { return }
            finish(k)

        case .event:
            var e = Noted_V1_Event()
            e.title = title
            e.description_p = d.notes.trimmedText
            e.location = d.location.trimmedText
            e.allDay = d.allDay
            if d.allDay {
                e.startTime = .init(date: d.start.startOfDay)
                e.endTime = .init(date: d.start.startOfDay.adding(days: 1))
            } else {
                e.startTime = .init(date: d.start)
                e.endTime = .init(date: max(d.end, d.start.addingTimeInterval(60)))
            }
            e.timeZone = TimeZone.current.identifier
            e.rrule = d.repeatRule.rrule
            e.setReminder(d.remind)
            e.projectID = d.projectID
            e.space = d.space
            guard await data.createEvent(e) != nil else { return }
            finish(k)

        case .project:
            var p = Noted_V1_Project()
            p.title = title
            p.notes = d.notes.trimmedText
            p.pinned = d.pinned
            p.startTime = .init(date: d.projectStart.startOfDay)
            p.dueTime = .init(date: max(d.projectDue, d.projectStart).startOfDay)
            p.space = d.space
            guard let made = await data.createProject(p) else { return }
            DraftStore.clear(k)
            restored.remove(k)
            router.replaceTop(.project(made.id))
            toaster.show("已添加项目")

        case .goal:
            guard await addGoal(d, title: title) else { return }
            finish(k)
        }
    }

    private func finish(_ k: CreateKind) {
        DraftStore.clear(k)
        restored.remove(k)
        titleFocused = false
        router.pop()
        toaster.show("已添加\(k.label)")
    }

    /// A plain goal; or, with dimensions or a metric, an objective whose dimensions are goals
    /// (without added dimensions, the period target above becomes its one dimension).
    private func addGoal(_ d: CreateDraft, title: String) async -> Bool {
        let zone = TimeZone.current.identifier
        var metricStart = 0.0, metricTarget = 0.0
        if d.hasMetric {
            guard !d.metricUnit.trimmedText.isEmpty, let s = d.metricStart.parsedNumber, let t = d.metricTarget.parsedNumber, s != t else {
                toaster.error("填写指标的单位、当前值和目标值")
                return false
            }
            metricStart = s; metricTarget = t
        }
        var single = Noted_V1_Goal()
        if d.dimensions.isEmpty {
            guard let target = d.target.parsedNumber, target > 0 else { toaster.error("目标数要大于 0"); return false }
            guard !d.serverUnit.isEmpty else { toaster.error("填写单位"); return false }
            single.title = title
            single.period = d.period
            single.target = target
            single.unit = d.serverUnit
            single.timeZone = zone
            single.space = d.space
            single.projectID = d.projectID
            if d.hasCounter {
                guard let total = d.counterTarget.parsedNumber, total > 0 else { toaster.error("累计总数要大于 0"); return false }
                single.counterTarget = total
                single.counterUnit = d.counterUnit.trimmedText.isEmpty ? "次" : d.counterUnit.trimmedText
            }
            if !d.hasMetric { return await data.createGoal(single) != nil }
        }

        var o = Noted_V1_Objective()
        o.title = title
        o.space = d.space
        o.projectID = d.projectID
        o.startTime = .init(date: Date.now.startOfDay)
        if d.hasMetric {
            o.metricName = d.metricName.trimmedText.isEmpty ? "指标" : d.metricName.trimmedText
            o.metricUnit = d.metricUnit.trimmedText
            o.metricStart = metricStart
            o.metricTarget = metricTarget
        }
        single.projectID = ""  // a dimension belongs to the project through its objective
        let dims: [Noted_V1_Goal] = d.dimensions.isEmpty ? [single] : d.dimensions.map { dim in
            var g = Noted_V1_Goal()
            g.title = dim.title
            g.period = dim.period
            g.target = dim.target
            g.unit = dim.unit
            g.timeZone = zone
            g.space = d.space
            return g
        }
        guard let made = await data.createObjective(o, dimensions: dims) else { return false }
        if d.hasMetric { await data.recordMeasurement(made.id, value: metricStart) }
        return true
    }
}
