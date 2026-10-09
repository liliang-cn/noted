import SwiftUI
import XCTest
@testable import Noted

@MainActor
final class NoteImageTests: XCTestCase {
    private func note(_ title: String, _ content: String, tags: [String] = []) -> Noted_V1_Note {
        var n = Noted_V1_Note()
        n.title = title; n.content = content; n.tags = tags; n.space = .life
        n.updateTime = .init(Date(timeIntervalSince1970: 1_800_000_000))
        return n
    }

    func testMarkdownBlocks() {
        let b = MarkdownBlock.parse("# 标题\n\n- 一\n- 二\n> 引用\n正文 **粗体**\n\n\n结尾")
        XCTAssertEqual(b, [.heading(1, "标题"), .blank, .bullet("一"), .bullet("二"), .quote("引用"), .paragraph("正文 **粗体**"), .blank, .paragraph("结尾")],
                       "runs of blank lines collapse into one")
    }

    func testMarkdownIgnoresLeadingAndTrailingBlanks() {
        XCTAssertEqual(MarkdownBlock.parse("\n\n正文\n\n"), [.paragraph("正文")])
        XCTAssertEqual(MarkdownBlock.parse(""), [])
        XCTAssertEqual(MarkdownBlock.parse("## 二级"), [.heading(2, "二级")])
        XCTAssertEqual(MarkdownBlock.parse("#没有空格"), [.paragraph("#没有空格")], "a heading needs a space after the hashes")
        XCTAssertEqual(MarkdownBlock.parse("a\r\nb"), [.paragraph("a"), .paragraph("b")])
    }

    func testPictureIsAFixedWidthAndGrowsWithTheNote() throws {
        let short = try XCTUnwrap(NoteImage.render(note("短", "一行"), palette: .daylight, scale: 1))
        let long = try XCTUnwrap(NoteImage.render(note("长", String(repeating: "很长的一段文字。\n\n", count: 60)), palette: .daylight, scale: 1))
        XCTAssertEqual(short.size.width, NoteImageView.width)
        XCTAssertEqual(long.size.width, NoteImageView.width)
        XCTAssertGreaterThan(long.size.height, short.size.height * 3)
    }

    func testPictureUsesTheTheme() throws {
        func corner(_ p: Palette) throws -> [UInt8] {
            let img = try XCTUnwrap(NoteImage.render(note("t", "x"), palette: p, scale: 1)?.cgImage)
            let data = try XCTUnwrap(img.dataProvider?.data) as Data
            return Array(data.prefix(3))   // top-left pixel is the background
        }
        XCTAssertNotEqual(try corner(.daylight), try corner(.paper), "paper is warmer than daylight")
    }

    func testAnEmptyNoteStillRenders() {
        XCTAssertNotNil(NoteImage.render(note("", ""), palette: .moss, scale: 1))
    }

    func testAnEnormousNoteIsCutInsteadOfFailing() throws {
        let huge = note("巨大", String(repeating: "一段文字,重复很多遍。\n\n", count: 1500))
        let img = try XCTUnwrap(NoteImage.render(huge, palette: .daylight, scale: 1))
        XCTAssertLessThanOrEqual(img.size.height, NoteImage.maxHeight + 400)
    }

    func testPlainTextKeepsTheTitleAsAHeading() {
        XCTAssertEqual(NoteImage.text(note("周会", "要点")), "# 周会\n\n要点")
        XCTAssertEqual(NoteImage.text(note("", "只有正文")), "只有正文")
    }
}
