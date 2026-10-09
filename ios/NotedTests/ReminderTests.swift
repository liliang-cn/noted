import XCTest
@testable import Noted

final class ReminderTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func occ(_ title: String, in minutes: Double, remind: Int32?, space: Noted_V1_Space = .life, id: String = "e1") -> Noted_V1_Occurrence {
        var o = Noted_V1_Occurrence()
        o.event.id = id; o.event.title = title; o.event.space = space
        if let remind { o.event.remindBeforeMinutes = remind }
        o.startTime = .init(now.addingTimeInterval(minutes * 60))
        return o
    }

    private func task(_ title: String, remindIn minutes: Double?, done: Bool = false, id: String = "t1") -> Noted_V1_Task {
        var t = Noted_V1_Task()
        t.id = id; t.title = title; t.completed = done
        if let minutes { t.remindTime = .init(now.addingTimeInterval(minutes * 60)) }
        return t
    }

    func testEventReminderFiresLeadMinutesBeforeStart() {
        let r = ReminderPlanner.plan(occurrences: [occ("牙医", in: 60, remind: 10)], tasks: [], now: now, mode: .all, mute: MuteRules())
        XCTAssertEqual(r.count, 1)
        XCTAssertEqual(r[0].fire, now.addingTimeInterval(50 * 60))
        XCTAssertEqual(r[0].title, "牙医")
        XCTAssertTrue(r[0].body.contains("10 分钟后"))
    }

    func testNoReminderWhenUnset() {
        XCTAssertTrue(ReminderPlanner.plan(occurrences: [occ("无提醒", in: 60, remind: nil)], tasks: [], now: now, mode: .all, mute: MuteRules()).isEmpty)
    }

    func testZeroLeadMeansAtStart() {
        let r = ReminderPlanner.plan(occurrences: [occ("准点", in: 30, remind: 0)], tasks: [], now: now, mode: .all, mute: MuteRules())
        XCTAssertEqual(r.first?.body, "现在开始")
        XCTAssertEqual(r.first?.fire, now.addingTimeInterval(30 * 60))
    }

    func testPastReminderIsDropped() {
        // starts in 5 minutes but should have fired 10 minutes ahead
        XCTAssertTrue(ReminderPlanner.plan(occurrences: [occ("太晚了", in: 5, remind: 10)], tasks: [], now: now, mode: .all, mute: MuteRules()).isEmpty)
    }

    func testEachOccurrenceOfARecurringEventGetsItsOwnNotification() {
        let r = ReminderPlanner.plan(occurrences: [occ("站会", in: 60, remind: 5), occ("站会", in: 60 + 1440, remind: 5)], tasks: [], now: now, mode: .all, mute: MuteRules())
        XCTAssertEqual(r.count, 2)
        XCTAssertEqual(Set(r.map(\.id)).count, 2, "identifiers must differ or one replaces the other")
    }

    func testTaskReminders() {
        let r = ReminderPlanner.plan(occurrences: [], tasks: [task("交房租", remindIn: 120), task("做完了", remindIn: 120, done: true, id: "t2"), task("没提醒", remindIn: nil, id: "t3"), task("过期", remindIn: -5, id: "t4")], now: now, mode: .all, mute: MuteRules())
        XCTAssertEqual(r.map(\.title), ["交房租"])
        XCTAssertEqual(r[0].id, "noted.t.t1")
    }

    func testResultIsSortedAndCapped() {
        let many = (0..<80).map { occ("e\($0)", in: Double(1000 - $0), remind: 1, id: "id\($0)") }
        let r = ReminderPlanner.plan(occurrences: many, tasks: [], now: now, mode: .all, mute: MuteRules())
        XCTAssertEqual(r.count, ReminderPlanner.limit)
        XCTAssertEqual(r, r.sorted { $0.fire < $1.fire })
        XCTAssertLessThanOrEqual(ReminderPlanner.limit, 64)
        XCTAssertEqual(r.first?.title, "e79", "the soonest ones are the ones that are kept")
    }

    func testMuteRules() {
        let life = occ("生活的", in: 60, remind: 5, space: .life, id: "a")
        let work = occ("工作的", in: 60, remind: 5, space: .work, id: "b")
        var m = MuteRules()
        m.lifeInWork = true
        XCTAssertEqual(ReminderPlanner.plan(occurrences: [life, work], tasks: [], now: now, mode: .work, mute: m).map(\.title), ["工作的"])
        XCTAssertEqual(ReminderPlanner.plan(occurrences: [life, work], tasks: [], now: now, mode: .life, mute: m).count, 2, "muting applies only while in that mode")
        XCTAssertEqual(ReminderPlanner.plan(occurrences: [life, work], tasks: [], now: now, mode: .all, mute: m).count, 2)
        m = MuteRules(); m.workInLife = true
        XCTAssertEqual(ReminderPlanner.plan(occurrences: [life, work], tasks: [], now: now, mode: .life, mute: m).map(\.title), ["生活的"])
        XCTAssertEqual(ReminderPlanner.plan(occurrences: [], tasks: [task("工作待办", remindIn: 30)], now: now, mode: .life, mute: m).count, 1, "an untagged space is never muted")
    }

    func testMuteRulesDecodeWithMissingKeys() throws {
        XCTAssertEqual(try JSONDecoder().decode(MuteRules.self, from: Data("{}".utf8)), MuteRules())
        XCTAssertTrue(try JSONDecoder().decode(MuteRules.self, from: Data(#"{"workInLife":true}"#.utf8)).workInLife)
    }
}
