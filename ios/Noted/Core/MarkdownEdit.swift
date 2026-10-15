import Foundation

/// Text edits behind the Markdown toolbar, on character offsets so they are easy to test.
/// Each returns the new text and where the cursor or selection should go.
enum MarkdownEdit {
    struct Result: Equatable {
        var text: String
        var selection: Range<Int>
    }

    /// The line prefixes the toolbar toggles. A line carries at most one of them.
    static let linePrefixes = ["# ", "## ", "### ", "- [ ] ", "- [x] ", "- ", "> ", "1. "]

    private static func lineBounds(_ chars: [Character], at offset: Int) -> Range<Int> {
        var start = min(offset, chars.count)
        while start > 0, chars[start - 1] != "\n" { start -= 1 }
        var end = min(offset, chars.count)
        while end < chars.count, chars[end] != "\n" { end += 1 }
        return start..<end
    }

    /// Puts `prefix` at the start of every line the selection touches, or takes it off when every
    /// one of them already has it. Any other list or heading prefix on those lines is replaced.
    static func toggleLinePrefix(_ text: String, _ sel: Range<Int>, _ prefix: String) -> Result {
        var chars = Array(text)
        let first = lineBounds(chars, at: sel.lowerBound).lowerBound
        let last = lineBounds(chars, at: max(sel.lowerBound, sel.upperBound - (sel.isEmpty ? 0 : 1))).upperBound
        let lines = String(chars[first..<last]).components(separatedBy: "\n")
        let allHave = lines.allSatisfy { $0.hasPrefix(prefix) }
        var delta = 0, firstDelta = 0
        let changed = lines.enumerated().map { i, line -> String in
            var body = line
            if let old = linePrefixes.first(where: { line.hasPrefix($0) }) { body = String(line.dropFirst(old.count)) }
            let out = allHave ? body : prefix + body
            let d = out.count - line.count
            if i == 0 { firstDelta = d }
            delta += d
            return out
        }.joined(separator: "\n")
        chars.replaceSubrange(first..<last, with: Array(changed))
        let lo = max(first, sel.lowerBound + firstDelta)
        let hi = max(lo, sel.upperBound + delta)
        return Result(text: String(chars), selection: lo..<hi)
    }

    /// Wraps the selection in `marker` (bold, italic, code), or unwraps it when it already is.
    /// With nothing selected it inserts a pair and puts the cursor between them.
    static func wrap(_ text: String, _ sel: Range<Int>, _ marker: String) -> Result {
        var chars = Array(text)
        let m = Array(marker), n = m.count
        if sel.lowerBound >= n, sel.upperBound + n <= chars.count,
           Array(chars[(sel.lowerBound - n)..<sel.lowerBound]) == m, Array(chars[sel.upperBound..<(sel.upperBound + n)]) == m {
            chars.removeSubrange(sel.upperBound..<(sel.upperBound + n))
            chars.removeSubrange((sel.lowerBound - n)..<sel.lowerBound)
            return Result(text: String(chars), selection: (sel.lowerBound - n)..<(sel.upperBound - n))
        }
        chars.insert(contentsOf: m, at: sel.upperBound)
        chars.insert(contentsOf: m, at: sel.lowerBound)
        return Result(text: String(chars), selection: (sel.lowerBound + n)..<(sel.upperBound + n))
    }

    /// Inserts `snippet` at the cursor, replacing any selection. `cursorAt` is where the cursor
    /// lands inside the snippet; nil puts it at the end.
    static func insert(_ text: String, _ sel: Range<Int>, _ snippet: String, cursorAt: Int? = nil) -> Result {
        var chars = Array(text)
        chars.replaceSubrange(sel, with: Array(snippet))
        let c = sel.lowerBound + (cursorAt ?? snippet.count)
        return Result(text: String(chars), selection: c..<c)
    }

    /// After the person presses return on a list line, starts the next item; on an empty item it
    /// ends the list instead. Returns nil when the edit was anything else.
    static func continueList(old: String, new: String, cursor: Int) -> Result? {
        guard new.count == old.count + 1, cursor > 0 else { return nil }
        let chars = Array(new)
        guard cursor <= chars.count, chars[cursor - 1] == "\n" else { return nil }
        let prev = lineBounds(chars, at: cursor - 1)
        let line = String(chars[prev])
        var next: String?
        if line.hasPrefix("- [ ] ") || line.hasPrefix("- [x] ") { next = "- [ ] " }
        else if line.hasPrefix("- ") { next = "- " }
        else if line.hasPrefix("> ") { next = "> " }
        else if let dot = line.firstIndex(of: "."), let n = Int(line[..<dot]), line[line.index(after: dot)...].hasPrefix(" ") {
            next = "\(n + 1). "
        }
        guard let next else { return nil }
        let bare = linePrefixes.first { line.hasPrefix($0) }.map { line.count == $0.count } ?? (line.hasSuffix(". ") && line.count <= 4)
        var out = chars
        if bare {
            // An empty item: drop it and the new line, which ends the list.
            out.removeSubrange(prev.lowerBound..<cursor)
            return Result(text: String(out), selection: prev.lowerBound..<prev.lowerBound)
        }
        out.insert(contentsOf: Array(next), at: cursor)
        let c = cursor + next.count
        return Result(text: String(out), selection: c..<c)
    }

    /// Ticks or unticks the to-do on line `index` (0-based).
    static func toggleTodo(_ text: String, line index: Int) -> String {
        var lines = text.components(separatedBy: "\n")
        guard lines.indices.contains(index) else { return text }
        let l = lines[index]
        if l.hasPrefix("- [ ] ") { lines[index] = "- [x] " + l.dropFirst(6) }
        else if l.hasPrefix("- [x] ") || l.hasPrefix("- [X] ") { lines[index] = "- [ ] " + l.dropFirst(6) }
        return lines.joined(separator: "\n")
    }

    /// Where an edit ends in `new`: just after what was typed, or where text was taken out.
    static func changeEnd(old: String, new: String) -> Int {
        let a = Array(old), b = Array(new)
        var pre = 0
        while pre < a.count, pre < b.count, a[pre] == b[pre] { pre += 1 }
        var suf = 0
        while suf < min(a.count, b.count) - pre, a[a.count - 1 - suf] == b[b.count - 1 - suf] { suf += 1 }
        return b.count - suf
    }

    // MARK: references

    /// What a `noted://<kind>/<id>` link points at.
    struct Reference: Equatable {
        var kind: String
        var id: String
    }

    static let referenceKinds = ["project", "task", "event", "note", "goal", "objective"]

    /// An `@` being typed: where the `@` is and what follows it up to the cursor.
    struct Mention: Equatable {
        var at: Int
        var query: String
    }

    static func isMentionMark(_ c: Character) -> Bool { c == "@" || c == "＠" }

    /// The `@query` that ends at `cursor`, if any. It stays on one line and does not reach back
    /// into a link that is already there.
    static func mention(in text: String, cursor: Int) -> Mention? {
        let chars = Array(text)
        guard cursor <= chars.count else { return nil }
        var i = cursor
        while i > 0, cursor - i <= 40 {
            let c = chars[i - 1]
            if isMentionMark(c) { return Mention(at: i - 1, query: String(chars[i..<cursor])) }
            if c.isNewline || "[]()".contains(c) { return nil }
            i -= 1
        }
        return nil
    }

    /// Swaps the `@` at `at` and the `queryLength` characters after it for `link`, and puts the
    /// cursor after the link.
    static func replaceMention(_ text: String, atOffset at: Int, queryLength: Int, link: String) -> Result {
        var chars = Array(text)
        guard at < chars.count, isMentionMark(chars[at]) else { return Result(text: text, selection: chars.count..<chars.count) }
        chars.replaceSubrange(at..<min(at + 1 + queryLength, chars.count), with: Array(link))
        let c = at + link.count
        return Result(text: String(chars), selection: c..<c)
    }

    /// `[@title](noted://kind/id)`: a plain Markdown link, so it survives export and other clients.
    static func referenceLink(title: String, kind: String, id: String) -> String {
        var t = title.trimmingCharacters(in: .whitespacesAndNewlines).replacingOccurrences(of: "\n", with: " ")
        if t.isEmpty { t = "无标题" }
        for c in ["\\", "[", "]", "*", "_", "`"] { t = t.replacingOccurrences(of: c, with: "\\" + c) }
        let safe = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-_.~"))
        return "[@\(t)](noted://\(kind)/\(id.addingPercentEncoding(withAllowedCharacters: safe) ?? id))"
    }

    static func reference(_ url: URL) -> Reference? {
        guard url.scheme == "noted", let kind = url.host(), referenceKinds.contains(kind) else { return nil }
        let id = url.path(percentEncoded: false).trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        return id.isEmpty ? nil : Reference(kind: kind, id: id)
    }
}
