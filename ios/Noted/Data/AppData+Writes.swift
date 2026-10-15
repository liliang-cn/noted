import Foundation
import SwiftProtobuf

// Every write the app makes. Update masks name the proto fields being changed, e.g.
// `await data.updateTask(t, fields: ["title", "due_time"])`. Valid fields per kind:
//   task:      title, notes, due_time, priority, completed, remind_time, tags, project_id, space, rrule
//   event:     title, description, location, start_time, end_time, all_day, time_zone, rrule,
//              remind_before_minutes, project_id, space
//   note:      title, content, tags, pinned, archived, project_id, space
//   project:   title, notes, pinned, archived, start_time, due_time, space
//   goal:      title, notes, period, target, unit, time_zone, archived, space, counter_unit,
//              counter_target, objective_id, project_id, milestones
//   objective: title, notes, space, start_time, due_time, metric_name, metric_unit, metric_start,
//              metric_target, archived, project_id, milestones (replaces the list; ids keep done state)
//   holding:   name, currency, notes, last_price, dca_amount, dca_day, dca_time, time_zone, archived
// A field in the mask whose value is unset clears it (e.g. "due_time" with no due time).

extension AppData {
    // MARK: tasks

    /// Creates a task. Space unspecified means: the project's, else 个人.
    @discardableResult
    func createTask(_ task: Noted_V1_Task) async -> Noted_V1_Task? {
        var r = Noted_V1_CreateTaskRequest(); r.task = task
        guard let t = await perform({ try await $0.calendar.createTask(r) }) else { return nil }
        upsert(t)
        projectChanged(t.projectID)
        didWrite()
        return t
    }

    /// Saves the listed fields of `task`. The change shows at once and is undone if the server refuses it.
    @discardableResult
    func updateTask(_ task: Noted_V1_Task, fields: [String]) async -> Noted_V1_Task? {
        let old = self.task(task.id)
        upsert(task)
        var r = Noted_V1_UpdateTaskRequest(); r.task = task; r.updateMask = mask(fields)
        guard let t = await perform({ try await $0.calendar.updateTask(r) }) else {
            if let old { upsert(old) }
            return nil
        }
        upsert(t)
        projectChanged(t.projectID, old?.projectID ?? "")
        didWrite()
        return t
    }

    /// Ticks a task off, or reopens it.
    @discardableResult
    func setCompleted(_ taskID: String, _ done: Bool) async -> Bool {
        guard var t = task(taskID) else { return false }
        t.completed = done
        if done { t.completeTime = Google_Protobuf_Timestamp(date: .now) } else { t.clearCompleteTime() }
        guard await updateTask(t, fields: ["completed"]) != nil else { return false }
        // A repeating to-do stays done; the server has made the next one.
        if done && t.repeats { await refreshTasks() }
        return true
    }

    /// Moves a task `days` later, counting from today when it is overdue or undated; keeps its
    /// time of day and moves its reminder along. Returns the new due date.
    @discardableResult
    func postpone(_ taskID: String, days: Int = 1) async -> Date? {
        guard var t = task(taskID) else { return nil }
        let today = Date.now.startOfDay
        let base = t.due.map { $0.startOfDay < today ? today.at(hour: Fmt.calendar.component(.hour, from: $0), minute: Fmt.calendar.component(.minute, from: $0)) : $0 } ?? today
        let newDue = base.adding(days: days)
        let lead = t.reminderLead
        t.dueTime = Google_Protobuf_Timestamp(date: newDue)
        var fields = ["due_time"]
        if t.hasRemindTime { t.setReminder(lead); fields.append("remind_time") }
        return await updateTask(t, fields: fields) != nil ? newDue : nil
    }

    @discardableResult
    func deleteTask(_ taskID: String) async -> Bool {
        let old = task(taskID)
        tasks.removeAll { $0.id == taskID }
        for k in projectDetails.keys { projectDetails[k]?.tasks.removeAll { $0.id == taskID } }
        var r = Noted_V1_DeleteTaskRequest(); r.id = taskID
        guard await perform({ _ = try await $0.calendar.deleteTask(r) }) != nil else {
            if let old { upsert(old) }
            return false
        }
        projectChanged(old?.projectID ?? "")
        didWrite()
        return true
    }

    /// Logs a sitting on a task: minutes spent, optionally how far along it now is (100 completes it)
    /// and a note. Returns the task with its new totals.
    @discardableResult
    func logTaskWork(_ taskID: String, minutes: Int, progress: Int? = nil, note: String = "", at time: Date = .now) async -> Noted_V1_Task? {
        var r = Noted_V1_LogTaskWorkRequest()
        r.taskID = taskID; r.minutes = Int32(minutes); r.note = note
        r.time = Google_Protobuf_Timestamp(date: time)
        if let progress { r.progress = Int32(progress) }
        guard let t = await perform({ try await $0.calendar.logTaskWork(r) }) else { return nil }
        upsert(t)
        if t.completed && t.repeats { await refreshTasks() }
        await loadTaskWork(taskID)
        projectChanged(t.projectID)
        didWrite()
        return t
    }

    @discardableResult
    func deleteTaskWork(_ entryID: String, taskID: String) async -> Bool {
        var r = Noted_V1_DeleteTaskWorkRequest(); r.id = entryID
        guard let t = await perform({ try await $0.calendar.deleteTaskWork(r) }) else { return false }
        upsert(t)
        taskWork[taskID]?.removeAll { $0.id == entryID }
        didWrite()
        return true
    }

    // MARK: events

    @discardableResult
    func createEvent(_ event: Noted_V1_Event) async -> Noted_V1_Event? {
        var e = event
        if e.timeZone.isEmpty { e.timeZone = TimeZone.current.identifier }
        var r = Noted_V1_CreateEventRequest(); r.event = e
        guard let made = await perform({ try await $0.calendar.createEvent(r) }) else { return nil }
        looseEvents[made.id] = made
        await loadMonth(made.start)
        await reloadEvents()
        projectChanged(made.projectID)
        didWrite()
        return made
    }

    @discardableResult
    func updateEvent(_ event: Noted_V1_Event, fields: [String]) async -> Noted_V1_Event? {
        let old = self.event(event.id)
        replaceEvent(event)
        var r = Noted_V1_UpdateEventRequest(); r.event = event; r.updateMask = mask(fields)
        guard let e = await perform({ try await $0.calendar.updateEvent(r) }) else {
            if let old { replaceEvent(old) }
            return nil
        }
        replaceEvent(e)
        if fields.contains(where: { ["start_time", "end_time", "all_day", "rrule", "time_zone", "space"].contains($0) }) {
            await reloadEvents()
        }
        projectChanged(e.projectID, old?.projectID ?? "")
        didWrite()
        return e
    }

    /// Deletes an event and every occurrence of it.
    @discardableResult
    func deleteEvent(_ eventID: String) async -> Bool {
        let old = event(eventID)
        let kept = occurrences
        occurrences.removeAll { $0.event.id == eventID }
        var r = Noted_V1_DeleteEventRequest(); r.id = eventID
        guard await perform({ _ = try await $0.calendar.deleteEvent(r) }) != nil else {
            occurrences = kept
            return false
        }
        looseEvents[eventID] = nil
        for k in projectDetails.keys { projectDetails[k]?.events.removeAll { $0.id == eventID } }
        projectChanged(old?.projectID ?? "")
        didWrite()
        return true
    }

    // MARK: notes

    @discardableResult
    func createNote(_ note: Noted_V1_Note) async -> Noted_V1_Note? {
        var r = Noted_V1_CreateNoteRequest(); r.note = note
        guard let n = await perform({ try await $0.notes.createNote(r) }) else { return nil }
        upsert(n)
        projectChanged(n.projectID)
        didWrite()
        return n
    }

    /// Saves the listed fields. Safe to call often (autosave): the latest local text stays on screen.
    @discardableResult
    func updateNote(_ note: Noted_V1_Note, fields: [String]) async -> Noted_V1_Note? {
        let old = self.note(note.id)
        upsert(note)
        var r = Noted_V1_UpdateNoteRequest(); r.note = note; r.updateMask = mask(fields)
        guard let n = await perform({ try await $0.notes.updateNote(r) }) else {
            if let old { upsert(old) }
            return nil
        }
        // Keep newer local typing if the person kept editing while this was in flight.
        if let cur = self.note(note.id), cur.title == note.title, cur.content == note.content { upsert(n) }
        if fields.contains("project_id") { projectChanged(n.projectID, old?.projectID ?? "") }
        didWrite()
        return n
    }

    @discardableResult
    func setPinned(note noteID: String, _ pinned: Bool) async -> Bool {
        guard var n = note(noteID) else { return false }
        n.pinned = pinned
        return await updateNote(n, fields: ["pinned"]) != nil
    }

    @discardableResult
    func deleteNote(_ noteID: String) async -> Bool {
        let old = note(noteID)
        notes.removeAll { $0.id == noteID }
        for k in projectDetails.keys { projectDetails[k]?.notes.removeAll { $0.id == noteID } }
        var r = Noted_V1_DeleteNoteRequest(); r.id = noteID
        guard await perform({ _ = try await $0.notes.deleteNote(r) }) != nil else {
            if let old { upsert(old) }
            return false
        }
        projectChanged(old?.projectID ?? "")
        didWrite()
        return true
    }

    // MARK: projects

    @discardableResult
    func createProject(_ project: Noted_V1_Project) async -> Noted_V1_Project? {
        var r = Noted_V1_CreateProjectRequest(); r.project = project
        guard let p = await perform({ try await $0.projects.createProject(r) }) else { return nil }
        upsert(p)
        await refreshProjects()
        didWrite()
        return p
    }

    @discardableResult
    func updateProject(_ project: Noted_V1_Project, fields: [String]) async -> Noted_V1_Project? {
        let old = self.project(project.id)
        upsert(project)
        var r = Noted_V1_UpdateProjectRequest(); r.project = project; r.updateMask = mask(fields)
        guard let p = await perform({ try await $0.projects.updateProject(r) }) else {
            if let old { upsert(old) }
            return nil
        }
        upsert(p)
        if fields.contains(where: { ["pinned", "start_time", "due_time"].contains($0) }) { await refreshProjects() }
        didWrite()
        return p
    }

    @discardableResult
    func setPinned(project projectID: String, _ pinned: Bool) async -> Bool {
        guard var p = project(projectID) else { return false }
        p.pinned = pinned
        return await updateProject(p, fields: ["pinned"]) != nil
    }

    @discardableResult
    func setArchived(project projectID: String, _ archived: Bool) async -> Bool {
        guard var p = project(projectID) else { return false }
        p.archived = archived
        return await updateProject(p, fields: ["archived"]) != nil
    }

    /// Deletes a project. Its tasks, events and notes are kept (and leave it) unless `deleteItems`.
    @discardableResult
    func deleteProject(_ projectID: String, deleteItems: Bool) async -> Bool {
        var r = Noted_V1_DeleteProjectRequest(); r.id = projectID; r.deleteItems = deleteItems
        guard await perform({ _ = try await $0.projects.deleteProject(r) }) != nil else { return false }
        projects.removeAll { $0.id == projectID }
        projectDetails[projectID] = nil
        await refreshTasks()
        await refreshNotes()
        await refreshGoals()
        await reloadEvents()
        didWrite()
        return true
    }

    /// Puts items into a project, or takes them out with `projectID` "".
    @discardableResult
    func setProject(_ items: [ItemRef], projectID: String) async -> Int {
        var moved = 0
        var touched: Set<String> = [projectID]
        for item in items {
            switch item {
            case .task(let id):
                guard var t = task(id) else { continue }
                touched.insert(t.projectID); t.projectID = projectID
                if await updateTask(t, fields: ["project_id"]) != nil { moved += 1 }
            case .event(let id):
                guard var e = event(id) else { continue }
                touched.insert(e.projectID); e.projectID = projectID
                if await updateEvent(e, fields: ["project_id"]) != nil { moved += 1 }
            case .note(let id):
                guard var n = note(id) else { continue }
                touched.insert(n.projectID); n.projectID = projectID
                if await updateNote(n, fields: ["project_id"]) != nil { moved += 1 }
            case .goal(let id):
                guard var g = goal(id) else { continue }
                touched.insert(g.projectID); g.projectID = projectID
                if await updateGoal(g, fields: ["project_id"]) != nil { moved += 1 }
            case .objective(let id):
                guard var o = objective(id) else { continue }
                touched.insert(o.projectID); o.projectID = projectID
                if await updateObjective(o, fields: ["project_id"]) != nil { moved += 1 }
            }
        }
        await refreshProjects()
        for id in touched where !id.isEmpty && projectDetails[id] != nil { await loadProject(id) }
        return moved
    }

    // MARK: goals

    @discardableResult
    func createGoal(_ goal: Noted_V1_Goal) async -> Noted_V1_Goal? {
        var g = goal
        if g.timeZone.isEmpty { g.timeZone = TimeZone.current.identifier }
        var r = Noted_V1_CreateGoalRequest(); r.goal = g
        guard let made = await perform({ try await $0.goals.createGoal(r) }) else { return nil }
        upsert(made)
        if !made.objectiveID.isEmpty { await refreshGoals() }
        projectChanged(made.projectID)
        didWrite()
        return made
    }

    @discardableResult
    func updateGoal(_ goal: Noted_V1_Goal, fields: [String]) async -> Noted_V1_Goal? {
        let old = self.goal(goal.id)
        var r = Noted_V1_UpdateGoalRequest(); r.goal = goal; r.updateMask = mask(fields)
        guard let g = await perform({ try await $0.goals.updateGoal(r) }) else { return nil }
        upsert(g)
        if !g.objectiveID.isEmpty || !(old?.objectiveID.isEmpty ?? true) { await refreshGoals() }
        if fields.contains("project_id") { projectChanged(g.projectID, old?.projectID ?? "") }
        didWrite()
        return g
    }

    @discardableResult
    func deleteGoal(_ goalID: String) async -> Bool {
        let old = goal(goalID)
        var r = Noted_V1_DeleteGoalRequest(); r.id = goalID
        guard await perform({ _ = try await $0.goals.deleteGoal(r) }) != nil else { return false }
        goals.removeAll { $0.id == goalID }
        if !(old?.objectiveID.isEmpty ?? true) { await refreshGoals() }
        projectChanged(old?.projectID ?? "")
        didWrite()
        return true
    }

    /// Records a check-in: `amount` toward this period's target (1 time, or minutes), and
    /// `count` toward the running total (课时) when the goal has one. Returns the goal with fresh progress.
    @discardableResult
    func checkIn(_ goalID: String, amount: Double = 1, count: Double = 0, note: String = "", at time: Date = .now) async -> Noted_V1_Goal? {
        var r = Noted_V1_RecordCheckInRequest()
        r.goalID = goalID; r.amount = amount; r.count = count; r.note = note
        r.time = Google_Protobuf_Timestamp(date: time)
        guard let resp = await perform({ try await $0.goals.recordCheckIn(r) }) else { return nil }
        upsert(resp.goal)
        if !resp.goal.objectiveID.isEmpty { await refreshGoals() }
        if checkIns[goalID] != nil { checkIns[goalID]?.insert(resp.checkIn, at: 0) }
        didWrite()
        return resp.goal
    }

    @discardableResult
    func deleteCheckIn(_ checkInID: String, goalID: String) async -> Bool {
        var r = Noted_V1_DeleteCheckInRequest(); r.id = checkInID
        guard let g = await perform({ try await $0.goals.deleteCheckIn(r) }) else { return false }
        upsert(g)
        checkIns[goalID]?.removeAll { $0.id == checkInID }
        if !g.objectiveID.isEmpty { await refreshGoals() }
        didWrite()
        return true
    }

    // MARK: objectives

    /// Creates an objective, then its dimensions (goals) under it.
    @discardableResult
    func createObjective(_ objective: Noted_V1_Objective, dimensions: [Noted_V1_Goal] = []) async -> Noted_V1_Objective? {
        var r = Noted_V1_CreateObjectiveRequest(); r.objective = objective
        guard let o = await perform({ try await $0.objectives.createObjective(r) }) else { return nil }
        for var g in dimensions {
            g.objectiveID = o.id
            if g.space == .unspecified { g.space = o.space }
            _ = await createGoal(g)
        }
        await refreshGoals()
        projectChanged(o.projectID)
        didWrite()
        return self.objective(o.id) ?? o
    }

    @discardableResult
    func updateObjective(_ objective: Noted_V1_Objective, fields: [String]) async -> Noted_V1_Objective? {
        let old = self.objective(objective.id)
        var r = Noted_V1_UpdateObjectiveRequest(); r.objective = objective; r.updateMask = mask(fields)
        guard let o = await perform({ try await $0.objectives.updateObjective(r) }) else { return nil }
        upsert(o)
        if fields.contains("project_id") { projectChanged(o.projectID, old?.projectID ?? "") }
        didWrite()
        return o
    }

    /// Ticks (or unticks) one of an objective's milestones.
    @discardableResult
    func setObjectiveMilestoneDone(_ objectiveID: String, milestoneID: String, done: Bool) async -> Bool {
        var r = Noted_V1_SetObjectiveMilestoneDoneRequest()
        r.objectiveID = objectiveID; r.milestoneID = milestoneID; r.done = done
        guard let o = await perform({ try await $0.objectives.setObjectiveMilestoneDone(r) }) else { return false }
        upsert(o)
        didWrite()
        return true
    }

    /// Deletes an objective; its dimensions stay as stand-alone goals.
    @discardableResult
    func deleteObjective(_ objectiveID: String) async -> Bool {
        let old = objective(objectiveID)
        var r = Noted_V1_DeleteObjectiveRequest(); r.id = objectiveID
        guard await perform({ _ = try await $0.objectives.deleteObjective(r) }) != nil else { return false }
        objectives.removeAll { $0.id == objectiveID }
        await refreshGoals()
        projectChanged(old?.projectID ?? "")
        didWrite()
        return true
    }

    /// Records a reading of the objective's metric (体重 78.6). Returns the objective with new progress.
    @discardableResult
    func recordMeasurement(_ objectiveID: String, value: Double, note: String = "", at time: Date = .now) async -> Noted_V1_Objective? {
        var r = Noted_V1_RecordMeasurementRequest()
        r.objectiveID = objectiveID; r.value = value; r.note = note
        r.time = Google_Protobuf_Timestamp(date: time)
        guard let o = await perform({ try await $0.objectives.recordMeasurement(r) }) else { return nil }
        upsert(o)
        await loadMeasurements(objectiveID)
        didWrite()
        return o
    }

    @discardableResult
    func deleteMeasurement(_ measurementID: String, objectiveID: String) async -> Bool {
        var r = Noted_V1_DeleteMeasurementRequest(); r.id = measurementID
        guard await perform({ _ = try await $0.objectives.deleteMeasurement(r) }) != nil else { return false }
        measurements[objectiveID]?.removeAll { $0.id == measurementID }
        await refreshGoals()
        didWrite()
        return true
    }

    // MARK: holdings

    @discardableResult
    func createHolding(_ holding: Noted_V1_Holding) async -> Noted_V1_Holding? {
        var h = holding
        if h.timeZone.isEmpty { h.timeZone = TimeZone.current.identifier }
        var r = Noted_V1_CreateHoldingRequest(); r.holding = h
        guard let made = await perform({ try await $0.holdings.createHolding(r) }) else { return nil }
        upsert(made)
        didWrite()
        return made
    }

    @discardableResult
    func updateHolding(_ holding: Noted_V1_Holding, fields: [String]) async -> Noted_V1_Holding? {
        var r = Noted_V1_UpdateHoldingRequest(); r.holding = holding; r.updateMask = mask(fields)
        guard let h = await perform({ try await $0.holdings.updateHolding(r) }) else { return nil }
        upsert(h)
        if fields.contains(where: { $0.hasPrefix("dca") }) { await reloadEvents() }
        didWrite()
        return h
    }

    /// Deletes a holding with its trades and its monthly plan event.
    @discardableResult
    func deleteHolding(_ holdingID: String) async -> Bool {
        var r = Noted_V1_DeleteHoldingRequest(); r.id = holdingID
        guard await perform({ _ = try await $0.holdings.deleteHolding(r) }) != nil else { return false }
        holdings.removeAll { $0.id == holdingID }
        trades[holdingID] = nil
        await reloadEvents()
        didWrite()
        return true
    }

    /// Records a buy or sell. Selling more than is held is refused by the server (shown as a toast).
    @discardableResult
    func recordTrade(_ holdingID: String, side: String, shares: Double, price: Double, fee: Double = 0, note: String = "", at time: Date = .now) async -> Noted_V1_Holding? {
        var r = Noted_V1_RecordTradeRequest()
        r.holdingID = holdingID
        r.trade.side = side; r.trade.shares = shares; r.trade.price = price; r.trade.fee = fee; r.trade.note = note
        r.trade.time = Google_Protobuf_Timestamp(date: time)
        guard let h = await perform({ try await $0.holdings.recordTrade(r) }) else { return nil }
        upsert(h)
        await loadTrades(holdingID)
        didWrite()
        return h
    }

    @discardableResult
    func deleteTrade(_ tradeID: String, holdingID: String) async -> Bool {
        var r = Noted_V1_DeleteTradeRequest(); r.id = tradeID
        guard await perform({ _ = try await $0.holdings.deleteTrade(r) }) != nil else { return false }
        trades[holdingID]?.removeAll { $0.id == tradeID }
        await refreshHoldings()
        didWrite()
        return true
    }

    // MARK: account

    /// Mints an API token (for an AI assistant). Returns the secret, shown only once.
    func createToken(label: String) async -> (secret: String, info: Noted_V1_TokenInfo)? {
        var r = Noted_V1_CreateTokenRequest(); r.label = label
        guard let resp = await perform({ try await $0.account.createToken(r) }) else { return nil }
        tokens.insert(resp.info, at: 0)
        return (resp.token, resp.info)
    }

    @discardableResult
    func revokeToken(_ tokenID: String) async -> Bool {
        var r = Noted_V1_RevokeTokenRequest(); r.id = tokenID
        guard await perform({ _ = try await $0.account.revokeToken(r) }) != nil else { return false }
        tokens.removeAll { $0.id == tokenID }
        return true
    }

    /// Adds or changes the account's email + password.
    @discardableResult
    func setEmailPassword(email: String, password: String, currentPassword: String = "") async -> Bool {
        var r = Noted_V1_SetEmailPasswordRequest()
        r.email = email; r.password = password; r.currentPassword = currentPassword
        guard let a = await perform({ try await $0.account.setEmailPassword(r) }) else { return false }
        account = a
        return true
    }

    /// Everything as one zip (noted.json, notes/*.md, calendar.ics), ready for a share sheet.
    func exportAll() async -> URL? {
        await perform { try await Exporter.run(api: $0) }
    }

    /// Signs out of this server and forgets everything loaded.
    func signOut() {
        session.disconnect()
        reset()
    }

    // MARK: helpers

    func mask(_ fields: [String]) -> Google_Protobuf_FieldMask {
        var m = Google_Protobuf_FieldMask()
        m.paths = fields
        return m
    }
}
