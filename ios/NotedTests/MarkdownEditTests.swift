import XCTest
@testable import Noted

final class MarkdownEditTests: XCTestCase {
    func testLinePrefixTogglesAndReplacesOtherPrefixes() {
        var r = MarkdownEdit.toggleLinePrefix("买菜", 1..<1, "- [ ] ")
        XCTAssertEqual(r.text, "- [ ] 买菜")
        XCTAssertEqual(r.selection, 7..<7)
        r = MarkdownEdit.toggleLinePrefix(r.text, r.selection, "- [ ] ")
        XCTAssertEqual(r.text, "买菜")
        r = MarkdownEdit.toggleLinePrefix("- 买菜", 3..<3, "## ")
        XCTAssertEqual(r.text, "## 买菜", "a list item becomes a heading instead of both")
    }

    func testLinePrefixCoversEverySelectedLine() {
        let r = MarkdownEdit.toggleLinePrefix("a\nb\nc", 0..<3, "- ")
        XCTAssertEqual(r.text, "- a\n- b\nc")
    }

    func testWrapAndUnwrap() {
        var r = MarkdownEdit.wrap("hello world", 6..<11, "**")
        XCTAssertEqual(r.text, "hello **world**")
        XCTAssertEqual(r.selection, 8..<13)
        r = MarkdownEdit.wrap(r.text, r.selection, "**")
        XCTAssertEqual(r.text, "hello world")
        r = MarkdownEdit.wrap("ab", 1..<1, "`")
        XCTAssertEqual(r.text, "a``b")
        XCTAssertEqual(r.selection, 2..<2, "the cursor lands between the pair")
    }

    func testReturnContinuesAndEndsLists() {
        var r = MarkdownEdit.continueList(old: "- [ ] 机票", new: "- [ ] 机票\n", cursor: 9)
        XCTAssertEqual(r?.text, "- [ ] 机票\n- [ ] ")
        r = MarkdownEdit.continueList(old: "1. a", new: "1. a\n", cursor: 5)
        XCTAssertEqual(r?.text, "1. a\n2. ")
        r = MarkdownEdit.continueList(old: "- a\n- ", new: "- a\n- \n", cursor: 7)
        XCTAssertEqual(r?.text, "- a\n", "return on an empty item ends the list")
        XCTAssertNil(MarkdownEdit.continueList(old: "plain", new: "plain\n", cursor: 6))
    }

    func testTickingATodoInPlace() {
        XCTAssertEqual(MarkdownEdit.toggleTodo("# 行程\n- [ ] 机票\n- [x] 酒店", line: 1), "# 行程\n- [x] 机票\n- [x] 酒店")
        XCTAssertEqual(MarkdownEdit.toggleTodo("- [x] 酒店", line: 0), "- [ ] 酒店")
    }

    func testChangeEndFollowsTheEdit() {
        XCTAssertEqual(MarkdownEdit.changeEnd(old: "ab", new: "a@b"), 2)
        XCTAssertEqual(MarkdownEdit.changeEnd(old: "a@", new: "a@@"), 3, "typing at the end of a run")
        XCTAssertEqual(MarkdownEdit.changeEnd(old: "a@b", new: "ab"), 1)
        XCTAssertEqual(MarkdownEdit.changeEnd(old: "@r", new: "@日"), 2, "the input method swapping its marked text")
    }

    func testFindsTheMentionBeingTyped() {
        XCTAssertEqual(MarkdownEdit.mention(in: "见 @日本", cursor: 5), MarkdownEdit.Mention(at: 2, query: "日本"))
        XCTAssertEqual(MarkdownEdit.mention(in: "见@", cursor: 2), MarkdownEdit.Mention(at: 1, query: ""))
        XCTAssertEqual(MarkdownEdit.mention(in: "@Japan trip", cursor: 11)?.query, "Japan trip")
        XCTAssertEqual(MarkdownEdit.mention(in: "见＠日", cursor: 3)?.at, 1, "the full-width @ of Chinese keyboards")
        XCTAssertNil(MarkdownEdit.mention(in: "@a\nb", cursor: 4), "a mention stays on its line")
        XCTAssertNil(MarkdownEdit.mention(in: "[@日本](noted://project/p1) x", cursor: 27), "an existing link is not a mention")
        XCTAssertNil(MarkdownEdit.mention(in: "plain", cursor: 5))
    }

    func testChoosingAMentionInsertsTheLink() {
        let link = MarkdownEdit.referenceLink(title: "日本旅行", kind: "project", id: "p1")
        XCTAssertEqual(link, "[@日本旅行](noted://project/p1)")
        let r = MarkdownEdit.replaceMention("见 @日本 吧", atOffset: 2, queryLength: 2, link: link)
        XCTAssertEqual(r.text, "见 [@日本旅行](noted://project/p1) 吧")
        XCTAssertEqual(r.selection, 29..<29, "the cursor lands after the link")
        let missing = MarkdownEdit.replaceMention("abc", atOffset: 1, queryLength: 0, link: link)
        XCTAssertEqual(missing.text, "abc", "nothing happens when the @ is gone")
    }

    func testReferenceLinksEscapeTitlesAndParseBack() {
        XCTAssertEqual(MarkdownEdit.referenceLink(title: "a [b]", kind: "task", id: "t 1"), "[@a \\[b\\]](noted://task/t%201)")
        XCTAssertEqual(MarkdownEdit.referenceLink(title: " ", kind: "note", id: "n1"), "[@无标题](noted://note/n1)")
        XCTAssertEqual(MarkdownEdit.reference(URL(string: "noted://task/t%201")!), MarkdownEdit.Reference(kind: "task", id: "t 1"))
        XCTAssertEqual(MarkdownEdit.reference(URL(string: "noted://goal/g1")!), MarkdownEdit.Reference(kind: "goal", id: "g1"))
        XCTAssertNil(MarkdownEdit.reference(URL(string: "noted://unknown/x")!))
        XCTAssertNil(MarkdownEdit.reference(URL(string: "https://example.com/project/x")!))
        let parsed = try? AttributedString(markdown: "见 [@日本旅行](noted://project/p1)", options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))
        let url = parsed?.runs.compactMap(\.link).first
        XCTAssertEqual(url.flatMap(MarkdownEdit.reference), MarkdownEdit.Reference(kind: "project", id: "p1"))
    }
}
