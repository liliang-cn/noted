import XCTest
@testable import Noted

@MainActor
final class NotificationTests: XCTestCase {
    func testBannerShowsAndDismisses() {
        let rc = ReminderCenter()
        var r = Noted_V1_Reminder(); r.title = "牙医"
        rc.show(r)
        XCTAssertEqual(rc.banner?.title, "牙医")
        rc.dismissBanner()
        XCTAssertNil(rc.banner)
    }
}
