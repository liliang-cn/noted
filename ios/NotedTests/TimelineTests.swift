import XCTest
@testable import Noted

final class TimelineTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    func testEntriesCoverEachUpcomingItemAndAHalfHourRefresh() {
        var s = Snapshot()
        s.items = [
            .init(id: "1", title: "过去", time: now.addingTimeInterval(-600)),
            .init(id: "2", title: "10 分钟后", time: now.addingTimeInterval(600)),
            .init(id: "3", title: "2 小时后", time: now.addingTimeInterval(7200)),
        ]
        let dates = Provider.entries(for: s, now: now).map(\.date)
        XCTAssertEqual(dates.first, now)
        XCTAssertTrue(dates.contains(now.addingTimeInterval(600)))
        XCTAssertTrue(dates.contains(now.addingTimeInterval(7200)))
        XCTAssertFalse(dates.contains(now.addingTimeInterval(-600)), "the past is not a future entry")
        XCTAssertTrue(dates.contains(now.addingTimeInterval(1800)))
        XCTAssertEqual(dates, dates.sorted())
        XCTAssertEqual(Set(dates).count, dates.count)
    }

    func testEmptySnapshotStillRefreshes() {
        let e = Provider.entries(for: Snapshot(), now: now)
        XCTAssertEqual(e.count, 2)
        XCTAssertEqual(e.last?.date, now.addingTimeInterval(1800))
    }

    func testEntryCountIsBounded() {
        var s = Snapshot()
        s.items = (1...50).map { .init(id: "\($0)", title: "x", time: now.addingTimeInterval(Double($0) * 60)) }
        XCTAssertLessThanOrEqual(Provider.entries(for: s, now: now).count, 10)
    }

    func testSampleIsProSoThePreviewShowsContent() {
        XCTAssertTrue(Sample.today.pro)
        XCTAssertFalse(Sample.today.items.isEmpty)
    }
}
