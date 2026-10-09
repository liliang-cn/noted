import SwiftUI
import UIKit

/// Block-level Markdown, enough for notes: headings, bullets, quotes, paragraphs.
/// Inline styling (bold, italic, links) is left to AttributedString.
enum MarkdownBlock: Equatable {
    case heading(Int, String)
    case bullet(String)
    case quote(String)
    case paragraph(String)
    case blank

    static func parse(_ text: String) -> [MarkdownBlock] {
        var out: [MarkdownBlock] = []
        for raw in text.replacingOccurrences(of: "\r\n", with: "\n").split(separator: "\n", omittingEmptySubsequences: false) {
            let line = String(raw)
            let t = line.trimmingCharacters(in: .whitespaces)
            if t.isEmpty { if out.last != .blank { out.append(.blank) }; continue }
            if let h = t.firstMatch(of: /^(#{1,3})\s+(.*)$/) {
                out.append(.heading(h.1.count, String(h.2)))
            } else if let b = t.firstMatch(of: /^[-*+]\s+(.*)$/) {
                out.append(.bullet(String(b.1)))
            } else if let q = t.firstMatch(of: /^>\s?(.*)$/) {
                out.append(.quote(String(q.1)))
            } else {
                out.append(.paragraph(t))
            }
        }
        while out.first == .blank { out.removeFirst() }
        while out.last == .blank { out.removeLast() }
        return out
    }
}

/// A note as a picture, in the current theme. A fixed width, as tall as the note needs.
struct NoteImageView: View {
    static let width: CGFloat = 390
    let note: Noted_V1_Note
    let palette: Palette
    var truncated = false

    var body: some View {
        let p = palette
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 8) {
                if note.space == .work || note.space == .life {
                    Circle().fill(note.space.color(p)).frame(width: 8, height: 8)
                    Text(note.space.label).font(.system(size: 12, weight: .semibold)).foregroundStyle(note.space.color(p))
                }
                Spacer()
                Text(Fmt.md(note.updateTime.date)).font(.system(size: 12, design: .monospaced)).foregroundStyle(p.ink2)
            }
            Text(note.title.isEmpty ? "无标题" : note.title)
                .font(.system(size: 28, weight: .bold)).foregroundStyle(p.ink).fixedSize(horizontal: false, vertical: true)
            VStack(alignment: .leading, spacing: 8) {
                ForEach(Array(MarkdownBlock.parse(note.content).enumerated()), id: \.offset) { _, b in block(b, p) }
                if truncated { Text("…").font(.system(size: 16)).foregroundStyle(p.ink2) }
            }
            if !note.tags.isEmpty {
                HStack(spacing: 6) {
                    ForEach(note.tags.prefix(6), id: \.self) { t in
                        Text(t).font(.system(size: 12, weight: .semibold)).padding(.horizontal, 8).frame(height: 24)
                            .background(p.surface2, in: RoundedRectangle(cornerRadius: 6)).foregroundStyle(p.ink2)
                    }
                }
            }
        }
        .padding(28)
        .frame(width: Self.width, alignment: .leading)
        .background(p.bg)
    }

    @ViewBuilder private func block(_ b: MarkdownBlock, _ p: Palette) -> some View {
        switch b {
        case .heading(let level, let s):
            inline(s).font(.system(size: level == 1 ? 22 : level == 2 ? 19 : 17, weight: .bold)).foregroundStyle(p.ink).padding(.top, 4)
        case .bullet(let s):
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text("•").foregroundStyle(p.ink2)
                inline(s).foregroundStyle(p.ink)
            }.font(.system(size: 16))
        case .quote(let s):
            HStack(spacing: 10) {
                Rectangle().fill(p.line).frame(width: 3)
                inline(s).font(.system(size: 16)).foregroundStyle(p.ink2)
            }.fixedSize(horizontal: false, vertical: true)
        case .paragraph(let s):
            inline(s).font(.system(size: 16)).foregroundStyle(p.ink).lineSpacing(3)
        case .blank:
            Color.clear.frame(height: 4)
        }
    }

    private func inline(_ s: String) -> Text {
        if let a = try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)) { return Text(a) }
        return Text(s)
    }
}

enum NoteImage {
    /// iOS renders at most 16384 pixels in one direction.
    static let maxHeight: CGFloat = 7000

    @MainActor static func render(_ note: Noted_V1_Note, palette: Palette, scale: CGFloat = 3) -> UIImage? {
        var n = note
        var cut = false
        // A very long note is cut off rather than failing to render.
        while NoteImageView(note: n, palette: palette).intrinsicHeight() > maxHeight, n.content.count > 200 {
            n.content = String(n.content.prefix(Int(Double(n.content.count) * 0.8)))
            cut = true
        }
        let r = ImageRenderer(content: NoteImageView(note: n, palette: palette, truncated: cut))
        r.scale = scale
        return r.uiImage
    }

    /// The note as plain Markdown text, the way it would be pasted elsewhere.
    static func text(_ note: Noted_V1_Note) -> String {
        let title = note.title.isEmpty ? "" : "# \(note.title)\n\n"
        return title + note.content
    }
}

private extension View {
    /// How tall this view wants to be at the note picture's width.
    @MainActor func intrinsicHeight() -> CGFloat {
        let host = UIHostingController(rootView: self)
        return host.sizeThatFits(in: CGSize(width: NoteImageView.width, height: .infinity)).height
    }
}

/// Saves a picture to the photo library (add-only access) and reports how it went.
final class PhotoSaver: NSObject, @unchecked Sendable {
    private var done: ((Error?) -> Void)?

    @MainActor func save(_ image: UIImage, completion: @escaping (Error?) -> Void) {
        done = completion
        UIImageWriteToSavedPhotosAlbum(image, self, #selector(finished(_:error:context:)), nil)
    }

    @objc private func finished(_ image: UIImage, error: Error?, context: UnsafeRawPointer) {
        done?(error)
        done = nil
    }
}
