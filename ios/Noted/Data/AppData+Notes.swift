import Foundation

// Calls the 笔记 screens need that AppData+Writes does not wrap: server search (literal or by
// meaning) and the AI's "提取待办" proposal.

extension AppData {
    /// Searches notes on the server. `semantic` ranks by meaning when the server has embeddings
    /// (it falls back to full text otherwise). nil on failure.
    func searchNotes(_ query: String, semantic: Bool, space: Noted_V1_Space = .unspecified, limit: Int32 = 50) async -> [Noted_V1_NoteHit]? {
        var r = Noted_V1_SearchNotesRequest()
        r.query = query; r.semantic = semantic; r.space = space; r.limit = limit
        return await perform { try await $0.notes.searchNotes(r).hits }
    }

    /// Asks the assistant for the to-dos written in a note. Nothing is created until accepted.
    func extractTasks(noteID: String) async -> Noted_V1_Proposal? {
        var r = Noted_V1_ExtractTasksRequest(); r.noteID = noteID
        return await perform { try await $0.ai.extractTasks(r) }
    }

    /// Carries out the chosen operations of a proposal, then reloads what they touch.
    @discardableResult
    func acceptProposal(_ id: String, indexes: [Int], inputs: [String: String] = [:]) async -> Noted_V1_AcceptProposalResponse? {
        var r = Noted_V1_AcceptProposalRequest()
        r.id = id
        r.inputs = inputs
        var sel = Noted_V1_OperationSelection(); sel.indexes = indexes.map { Int32($0) }
        r.selection = sel
        guard let resp = await perform({ try await $0.suggest.acceptProposal(r) }) else { return nil }
        await refreshTasks()
        await refreshProjects()
        await reloadEvents()
        didWrite()
        return resp
    }

    /// Drops a proposal the person did not want, so it does not linger in suggestions.
    func dismissProposal(_ id: String) async {
        var r = Noted_V1_DismissProposalRequest(); r.id = id
        _ = try? await session.api?.suggest.dismissProposal(r)
    }
}
