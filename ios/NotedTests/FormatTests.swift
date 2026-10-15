import XCTest
@testable import Noted

final class FormatTests: XCTestCase {
    /// Saturday 2026-10-10 11:00 local.
    private let now: Date = {
        var c = DateComponents(); c.year = 2026; c.month = 10; c.day = 10; c.hour = 11
        return Fmt.calendar.date(from: c)!
    }()

    func testRelativeDays() {
        XCTAssertEqual(Fmt.day(now, now: now), "今天")
        XCTAssertEqual(Fmt.day(now.adding(days: 1), now: now), "明天")
        XCTAssertEqual(Fmt.day(now.adding(days: -1), now: now), "昨天")
        XCTAssertEqual(Fmt.day(now.adding(days: 2), now: now), "后天")
        XCTAssertEqual(Fmt.day(now.adding(days: 8), now: now), "10月18日")
        XCTAssertEqual(Fmt.mdw(now), "10月10日 周六")
    }

    func testLaterThisWeekIsAWeekday() {
        let monday = now.adding(days: -5)   // 10月5日 周一
        XCTAssertEqual(Fmt.day(monday.adding(days: 3), now: monday), "周四")
    }

    func testStamps() {
        XCTAssertEqual(Fmt.stamp(now.at(hour: 9, minute: 5), now: now), "09:05")
        XCTAssertEqual(Fmt.stamp(now.adding(days: -1), now: now), "昨天")
        XCTAssertEqual(Fmt.stamp(now.adding(days: -3), now: now), "周三")
        XCTAssertEqual(Fmt.stamp(now.adding(days: -9), now: now), "10月1日")
    }

    func testDurationsAndMoney() {
        XCTAssertEqual(Fmt.duration(minutes: 30), "30 分钟")
        XCTAssertEqual(Fmt.duration(minutes: 60), "1 小时")
        XCTAssertEqual(Fmt.duration(minutes: 90), "1 小时 30 分")
        XCTAssertEqual(Fmt.money(2877.5), "$2,877.50")
        XCTAssertEqual(Fmt.money(-12, signed: true), "−$12.00")
        XCTAssertEqual(Fmt.money(12, signed: true), "+$12.00")
        XCTAssertEqual(Fmt.percent(7.8, signed: true), "+7.80%")
        XCTAssertEqual(Fmt.daysLeft(41), "还剩 41 天")
        XCTAssertEqual(Fmt.daysLeft(-3), "已过期 3 天")
    }

    func testRepeatRuleRoundTrip() {
        let r = RepeatRule(rrule: "FREQ=WEEKLY;BYDAY=MO,WE")
        XCTAssertEqual(r.frequency, .weekly)
        XCTAssertEqual(r.weekdays, [2, 4])
        XCTAssertEqual(r.label, "每周一、三")
        XCTAssertEqual(r.rrule, "FREQ=WEEKLY;BYDAY=MO,WE")
        XCTAssertEqual(RepeatRule(rrule: "").label, "永不")
        XCTAssertEqual(RepeatRule(rrule: "FREQ=MONTHLY").rrule, "FREQ=MONTHLY")
    }

    func testReminderLeadOfATask() {
        var t = Noted_V1_Task()
        t.dueTime = .init(date: now)
        XCTAssertEqual(t.reminderLead, .none)
        t.setReminder(.min30)
        XCTAssertEqual(t.reminderLead, .min30)
        XCTAssertTrue(t.hasClockTime)
        t.dueTime = .init(date: now.startOfDay)
        XCTAssertFalse(t.hasClockTime, "midnight means a date-only task")
    }

    func testOverdue() {
        var t = Noted_V1_Task()
        t.dueTime = .init(date: now.adding(days: -1).startOfDay)
        XCTAssertTrue(t.isOverdue(now: now))
        t.dueTime = .init(date: now.startOfDay)
        XCTAssertFalse(t.isOverdue(now: now), "a date-only task due today is not late yet")
        t.dueTime = .init(date: now.at(hour: 10))
        XCTAssertTrue(t.isOverdue(now: now))
        t.completed = true
        XCTAssertFalse(t.isOverdue(now: now))
    }

    func testInitials() {
        XCTAssertEqual(Avatar.initials("Liang Li"), "LL")
        XCTAssertEqual(Avatar.initials("李亮"), "李")
        XCTAssertEqual(Avatar.initials(""), "N")
    }
}
