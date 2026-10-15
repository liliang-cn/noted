import Foundation
import SwiftProtobuf

/// Unsaved edits kept on the device until the server has them, so nothing typed is lost to a
/// crash, a dropped connection or leaving the page before the save landed.
enum NoteDrafts {
    static func key(_ id: String) -> String { id.isEmpty ? "draft.note.new" : "draft.note.\(id)" }

    static func load(_ id: String) -> Noted_V1_Note? {
        guard let s = UserDefaults.standard.string(forKey: key(id)) else { return nil }
        return try? Noted_V1_Note(jsonString: s)
    }

    static func store(_ n: Noted_V1_Note) {
        if let s = try? n.jsonString() { UserDefaults.standard.set(s, forKey: key(n.id)) }
    }

    static func clear(_ id: String) { UserDefaults.standard.removeObject(forKey: key(id)) }

    /// Where a new note starts (e.g. filed in a project): the new note page picks it up once.
    static func seed(_ n: Noted_V1_Note) {
        if let s = try? n.jsonString() { UserDefaults.standard.set(s, forKey: "draft.note.seed") }
    }

    static func takeSeed() -> Noted_V1_Note? {
        defer { UserDefaults.standard.removeObject(forKey: "draft.note.seed") }
        guard let s = UserDefaults.standard.string(forKey: "draft.note.seed") else { return nil }
        return try? Noted_V1_Note(jsonString: s)
    }
}

enum NoteText {
    /// The body as one line of plain text for list previews: no Markdown marks, references as
    /// their titles.
    static func preview(_ markdown: String, limit: Int = 160) -> String {
        var parts: [String] = []
        var inCode = false
        for raw in markdown.split(separator: "\n", omittingEmptySubsequences: true) {
            var line = raw.trimmingCharacters(in: .whitespaces)
            if line.hasPrefix("```") { inCode.toggle(); continue }
            if !inCode {
                for p in ["### ", "## ", "# ", "- [ ] ", "- [x] ", "- [X] ", "* [ ] ", "* [x] ", "- ", "* ", "> "] where line.hasPrefix(p) {
                    line = String(line.dropFirst(p.count)); break
                }
                line = inline(line)
            }
            if !line.isEmpty { parts.append(line) }
            if parts.joined().count > limit { break }
        }
        return parts.joined(separator: " ")
    }

    /// Strips inline Markdown: links keep their text (without a reference's "@"), emphasis and
    /// code marks go.
    static func inline(_ s: String) -> String {
        var t = s.replacing(/\[@?((?:\\.|[^\]])*)\]\([^)]*\)/) { m in String(m.1) }
        t = t.replacing(/\\([\\\[\]*_`])/) { m in String(m.1) }
        for mark in ["**", "__", "`", "*"] { t = t.replacingOccurrences(of: mark, with: "") }
        return t
    }

    /// The title a note shows: its own, or "无标题".
    static func title(_ n: Noted_V1_Note) -> String {
        let t = n.title.trimmingCharacters(in: .whitespacesAndNewlines)
        return t.isEmpty ? "无标题" : t
    }

    static func isBlank(_ n: Noted_V1_Note) -> Bool {
        n.title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && n.content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// Whether two copies differ in anything the editor saves.
    static func sameContent(_ a: Noted_V1_Note, _ b: Noted_V1_Note) -> Bool {
        a.id == b.id && a.title == b.title && a.content == b.content && a.pinned == b.pinned
            && a.tags == b.tags && a.space == b.space && a.projectID == b.projectID
    }
}

extension MarkdownEdit.Reference {
    /// The SF Symbol of a reference chip.
    var symbol: String {
        switch kind {
        case "project": "folder"
        case "task": "checklist"
        case "event": "calendar"
        case "note": "note.text"
        case "goal", "objective": "target"
        default: "link"
        }
    }

    /// Where tapping it goes.
    var route: Route? {
        switch kind {
        case "project": .project(id)
        case "task": .todo(id)
        case "event": .event(id)
        case "note": .note(id)
        case "goal": .goal(id)
        case "objective": .objective(id)
        default: nil
        }
    }
}
