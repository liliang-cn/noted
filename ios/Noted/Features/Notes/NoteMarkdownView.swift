import SwiftUI

/// A note's Markdown as the preview shows it: headings, lists, to-dos you can tick in place,
/// quotes, code, and `noted://` references as tappable chips. Links open through the
/// environment's `openURL`, so the page decides where a reference goes.
struct NoteMarkdownView: View {
    let text: String
    var onToggleTodo: (Int) -> Void

    @Environment(Session.self) private var session

    private enum Block {
        case heading(Int, String)
        case todo(Int, Bool, String)   // line index, done, text
        case bullet(String)
        case numbered(String, String)
        case quote(String)
        case code(String)
        case paragraph(String)
        case gap
    }

    private var blocks: [Block] {
        var out: [Block] = []
        var code: [String]?
        for (i, line) in text.components(separatedBy: "\n").enumerated() {
            if line.hasPrefix("```") {
                if let c = code { out.append(.code(c.joined(separator: "\n"))); code = nil } else { code = [] }
                continue
            }
            if code != nil { code?.append(line); continue }
            if line.trimmingCharacters(in: .whitespaces).isEmpty {
                if case .gap = out.last {} else if !out.isEmpty { out.append(.gap) }
                continue
            }
            let t = line.drop { $0 == " " }
            if t.hasPrefix("### ") { out.append(.heading(3, String(t.dropFirst(4)))) }
            else if t.hasPrefix("## ") { out.append(.heading(2, String(t.dropFirst(3)))) }
            else if t.hasPrefix("# ") { out.append(.heading(1, String(t.dropFirst(2)))) }
            else if t.hasPrefix("- [ ] ") || t.hasPrefix("* [ ] ") { out.append(.todo(i, false, String(t.dropFirst(6)))) }
            else if t.hasPrefix("- [x] ") || t.hasPrefix("- [X] ") || t.hasPrefix("* [x] ") { out.append(.todo(i, true, String(t.dropFirst(6)))) }
            else if t.hasPrefix("- ") || t.hasPrefix("* ") { out.append(.bullet(String(t.dropFirst(2)))) }
            else if t.hasPrefix(">") { out.append(.quote(String(t.dropFirst()).trimmingCharacters(in: .whitespaces))) }
            else if let dot = t.firstIndex(of: "."), let n = Int(t[..<dot]), t[t.index(after: dot)...].hasPrefix(" ") {
                out.append(.numbered("\(n).", String(t[t.index(dot, offsetBy: 2)...])))
            } else { out.append(.paragraph(line)) }
        }
        if let c = code { out.append(.code(c.joined(separator: "\n"))) }
        if case .gap = out.last { out.removeLast() }
        return out
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, b in block(b) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private func block(_ b: Block) -> some View {
        switch b {
        case .heading(let level, let s):
            let font: Font = level == 1 ? .title2.weight(.bold) : level == 2 ? .title3.weight(.bold) : .headline
            Self.inline(s, accent: accent).font(font)
                .padding(.top, 8)
                .accessibilityAddTraits(.isHeader)
        case .todo(let line, let done, let s):
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Button { onToggleTodo(line) } label: {
                    CheckMark(isOn: done, size: 22)
                        .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 6 }
                        .frame(width: 28, alignment: .leading)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(done ? "标为未完成 \(NoteText.inline(s))" : "完成 \(NoteText.inline(s))")
                Self.inline(s, accent: accent)
                    .foregroundStyle(done ? AnyShapeStyle(.secondaryText) : AnyShapeStyle(.primary))
                    .strikethrough(done, color: .secondaryText)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.vertical, 2)
        case .bullet(let s):
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Circle().fill(.primary).frame(width: 5, height: 5)
                    .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
                    .frame(width: 18)
                Self.inline(s, accent: accent).frame(maxWidth: .infinity, alignment: .leading)
            }
        case .numbered(let n, let s):
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(n).monospacedDigit().foregroundStyle(.secondaryText).frame(minWidth: 22, alignment: .leading)
                Self.inline(s, accent: accent).frame(maxWidth: .infinity, alignment: .leading)
            }
        case .quote(let s):
            HStack(spacing: 12) {
                RoundedRectangle(cornerRadius: 1.5).fill(accent.opacity(0.35)).frame(width: 3)
                Self.inline(s, accent: accent).foregroundStyle(.secondaryText)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, 2)
            }
            .fixedSize(horizontal: false, vertical: true)
            .padding(.vertical, 4)
        case .code(let s):
            ScrollView(.horizontal) {
                Text(s).font(.system(.subheadline, design: .monospaced))
                    .padding(12)
            }
            .scrollIndicators(.hidden)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.fillQuiet, in: RoundedRectangle(cornerRadius: 10))
            .padding(.vertical, 4)
        case .paragraph(let s):
            Self.inline(s, accent: accent).lineSpacing(3)
        case .gap:
            Color.clear.frame(height: 8)
        }
    }

    private var accent: Color { session.accent }

    /// One line of inline Markdown. References become an icon and their title in the accent on a
    /// tinted background; other links are accent-colored.
    static func inline(_ s: String, accent: Color) -> Text {
        let a = (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
        var out = Text(verbatim: "")
        for (link, range) in a.runs[\.link] {
            if let url = link, let ref = MarkdownEdit.reference(url) {
                var title = String(a[range].characters)
                if let f = title.first, MarkdownEdit.isMentionMark(f) { title.removeFirst() }
                var chip = AttributedString("\u{2009}\(title)\u{2009}")
                chip.link = url
                chip.foregroundColor = accent
                chip.backgroundColor = accent.opacity(0.1)
                chip.font = .body.weight(.semibold)
                let icon = Text(Image(systemName: ref.symbol)).font(.footnote.weight(.semibold)).foregroundStyle(accent)
                out = Text("\(out)\(icon)\(chip)")
            } else {
                var piece = AttributedString(a[range])
                if link != nil { piece.foregroundColor = accent }
                out = Text("\(out)\(piece)")
            }
        }
        return out
    }
}
