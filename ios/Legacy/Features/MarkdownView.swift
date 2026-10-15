import SwiftUI

/// Renders a note's Markdown the way the design shows it: headings, lists, quotes, code, and
/// to-dos you can tick right in the note.
struct MarkdownView: View {
    @Environment(\.palette) private var p
    @Environment(\.mode) private var mode
    let text: String
    var onToggleTodo: ((Int) -> Void)?

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
        for (i, raw) in text.components(separatedBy: "\n").enumerated() {
            if raw.hasPrefix("```") {
                if let c = code { out.append(.code(c.joined(separator: "\n"))); code = nil } else { code = [] }
                continue
            }
            if code != nil { code?.append(raw); continue }
            let line = raw
            if line.trimmingCharacters(in: .whitespaces).isEmpty { out.append(.gap); continue }
            if line.hasPrefix("### ") { out.append(.heading(3, String(line.dropFirst(4)))) }
            else if line.hasPrefix("## ") { out.append(.heading(2, String(line.dropFirst(3)))) }
            else if line.hasPrefix("# ") { out.append(.heading(1, String(line.dropFirst(2)))) }
            else if line.hasPrefix("- [ ] ") { out.append(.todo(i, false, String(line.dropFirst(6)))) }
            else if line.hasPrefix("- [x] ") || line.hasPrefix("- [X] ") { out.append(.todo(i, true, String(line.dropFirst(6)))) }
            else if line.hasPrefix("- ") || line.hasPrefix("* ") { out.append(.bullet(String(line.dropFirst(2)))) }
            else if line.hasPrefix("> ") { out.append(.quote(String(line.dropFirst(2)))) }
            else if let dot = line.firstIndex(of: "."), let n = Int(line[..<dot]), line[line.index(after: dot)...].hasPrefix(" ") {
                out.append(.numbered("\(n).", String(line[line.index(dot, offsetBy: 2)...])))
            } else { out.append(.paragraph(line)) }
        }
        if let c = code { out.append(.code(c.joined(separator: "\n"))) }
        return out
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, b in block(b) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// `size` is the line's font size, so `@` references (noted:// links) keep it when drawn as chips.
    private func inline(_ s: String, size: CGFloat = 15) -> Text {
        var a = (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
        for r in a.runs.filter({ $0.link?.scheme == "noted" }).map(\.range) {
            a[r].foregroundColor = mode.accent(p)
            a[r].backgroundColor = mode.accent(p).opacity(0.1)
            a[r].font = .ui(size, .semibold)
        }
        return Text(a)
    }

    @ViewBuilder private func block(_ b: Block) -> some View {
        switch b {
        case .heading(let level, let s):
            let size: CGFloat = level == 1 ? 22 : level == 2 ? 19 : 17
            inline(s, size: size).font(.ui(size, .semibold)).foregroundStyle(p.ink)
                .padding(.top, 4)
        case .todo(let line, let done, let s):
            Button { onToggleTodo?(line) } label: {
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    ZStack {
                        RoundedRectangle(cornerRadius: 6).fill(done ? p.ok : Color.clear)
                        RoundedRectangle(cornerRadius: 6).stroke(done ? p.ok : p.ink3, lineWidth: 1.75)
                        if done { Image(systemName: "checkmark").font(.system(size: 11, weight: .heavy)).foregroundStyle(.white) }
                    }
                    .frame(width: 20, height: 20)
                    .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 5 }
                    inline(s).font(.ui(15)).foregroundStyle(done ? p.ink3 : p.ink)
                        .strikethrough(done, color: p.ink3)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(done ? "已完成 \(s)" : "待办 \(s)")
        case .bullet(let s):
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Circle().fill(p.ink2).frame(width: 5, height: 5).alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
                inline(s).font(.ui(15)).foregroundStyle(p.ink).lineSpacing(4)
            }
        case .numbered(let n, let s):
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(n).font(.mono(14)).foregroundStyle(p.ink2)
                inline(s).font(.ui(15)).foregroundStyle(p.ink).lineSpacing(4)
            }
        case .quote(let s):
            HStack(spacing: 10) {
                RoundedRectangle(cornerRadius: 1).fill(mode.accent(p)).frame(width: 3)
                inline(s).font(.ui(15)).foregroundStyle(p.ink2).lineSpacing(4)
            }
            .fixedSize(horizontal: false, vertical: true)
        case .code(let s):
            Text(s).font(.mono(13)).foregroundStyle(p.ink)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(12)
                .background(p.surface2, in: RoundedRectangle(cornerRadius: p.radiusSm))
        case .paragraph(let s):
            inline(s).font(.ui(15)).foregroundStyle(p.ink).lineSpacing(6)
        case .gap:
            Color.clear.frame(height: 2)
        }
    }
}
