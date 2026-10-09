import StoreKit
import StoreKitTest
import XCTest
@testable import Noted

/// Buys, refunds and lets a subscription lapse, against StoreKit's local test store.
@MainActor
final class PurchaseTests: XCTestCase {
    private var store: SKTestSession!

    /// StoreKit reports a refund or an expiry a moment after it happens.
    private func settle(_ pro: Pro, isPro expected: Bool) async {
        for _ in 0..<30 {
            await pro.refresh()
            if pro.isPro == expected { return }
            try? await Task.sleep(nanoseconds: 100_000_000)
        }
    }

    override func setUp() async throws {
        store = try SKTestSession(configurationFileNamed: "Noted")
        store.resetToDefaultState()
        store.disableDialogs = true
        store.askToBuyEnabled = false
        store.clearTransactions()
    }

    override func tearDown() async throws {
        store.clearTransactions()
    }

    func testTheTwoPlansAreOffered() async {
        let pro = Pro()
        await pro.load()
        XCTAssertEqual(Set(pro.products.map(\.id)), [Pro.yearlyID, Pro.monthlyID])
        XCTAssertFalse(pro.isPro)
        let prices = pro.products.map { "\($0.id)=\($0.price)" }.joined(separator: ", ")
        XCTAssertNotNil(pro.yearlySaving, "twelve months cost more than a year (\(prices))")
        XCTAssertEqual(pro.yearlySaving, 32, prices)   // 98 against 12 × 12
    }

    func testBuyingTheYearlyPlanTurnsProOn() async throws {
        let pro = Pro()
        await pro.load()
        await pro.buy(try XCTUnwrap(pro.yearly))
        XCTAssertTrue(pro.isPro)
        XCTAssertNil(pro.message)
    }

    func testBuyingTheMonthlyPlanTurnsProOn() async throws {
        let pro = Pro()
        await pro.load()
        await pro.buy(try XCTUnwrap(pro.monthly))
        XCTAssertTrue(pro.isPro)
    }

    func testARefundTurnsProOff() async throws {
        let pro = Pro()
        await pro.load()
        await pro.buy(try XCTUnwrap(pro.yearly))
        XCTAssertTrue(pro.isPro)
        let t = try XCTUnwrap(store.allTransactions().first)
        try store.refundTransaction(identifier: t.identifier)
        await settle(pro, isPro: false)
        XCTAssertFalse(pro.isPro, "a refunded purchase no longer unlocks anything")
    }

    /// StoreKit's test store renews on `expireSubscription` whatever the billing-retry setting, so the
    /// states that must end access are checked directly.
    func testOnlyAnActiveOrGracePeriodSubscriptionUnlocksPro() {
        XCTAssertTrue(Pro.unlocks(.subscribed))
        XCTAssertTrue(Pro.unlocks(.inGracePeriod), "a payment problem inside the grace period keeps access")
        XCTAssertFalse(Pro.unlocks(.inBillingRetryPeriod), "a declined renewal with no grace period ends access")
        XCTAssertFalse(Pro.unlocks(.expired))
        XCTAssertFalse(Pro.unlocks(.revoked))
    }

    /// With renewals working, the subscription simply carries on.
    func testASubscriptionThatRenewsStaysPro() async throws {
        let pro = Pro()
        await pro.load()
        await pro.buy(try XCTUnwrap(pro.monthly))
        try store.expireSubscription(productIdentifier: Pro.monthlyID)
        await pro.refresh()
        XCTAssertTrue(pro.isPro)
    }

    func testRestoringFindsAnEarlierPurchase() async throws {
        let first = Pro()
        await first.load()
        await first.buy(try XCTUnwrap(first.yearly))
        let second = Pro()      // a fresh install on the same Apple ID
        await second.refresh()
        XCTAssertTrue(second.isPro)
        await second.restore()
        XCTAssertTrue(second.isPro)
    }

    func testRestoringWithNothingBoughtSaysSo() async {
        let pro = Pro()
        await pro.restore()
        XCTAssertFalse(pro.isPro)
        XCTAssertEqual(pro.message, "没有找到可恢复的购买")
    }

    func testACancelledPurchaseLeavesThingsAlone() async throws {
        try await store.setSimulatedError(.generic(.unknown), forAPI: .purchase)
        let pro = Pro()
        await pro.load()
        await pro.buy(try XCTUnwrap(pro.yearly))
        XCTAssertFalse(pro.isPro)
        XCTAssertNotNil(pro.message, "the failure is shown")
    }
}
