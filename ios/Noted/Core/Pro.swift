import Foundation
import Observation
import StoreKit

/// Noted Pro through the App Store. The server does not know about it: it stores
/// whatever theme and layout the app writes, and the app decides what may be edited.
@MainActor @Observable
final class Pro {
    static let yearlyID = "cn.superleo.noted.pro.yearly"
    static let monthlyID = "cn.superleo.noted.pro.monthly"

    var isPro = false
    var products: [Product] = []
    var busy = false
    var message: String?
    private var updates: Task<Void, Never>?

    init() {
        #if DEBUG
        if ProcessInfo.processInfo.environment["NOTED_PRO"] == "1" { isPro = true }
        #endif
        updates = Task { [weak self] in
            for await result in Transaction.updates {
                if case .verified(let t) = result { await t.finish() }
                await self?.refresh()
            }
        }
    }

    func load() async {
        products = ((try? await Product.products(for: [Self.yearlyID, Self.monthlyID])) ?? [])
            .sorted { $0.price > $1.price }
        await refresh()
    }

    /// A failed renewal still being retried, an expiry and a refund all end access; a grace period does not.
    static func unlocks(_ state: Product.SubscriptionInfo.RenewalState) -> Bool {
        state == .subscribed || state == .inGracePeriod
    }

    /// Pro is on while a subscription is active or in its grace period. A renewal that failed,
    /// an expiry and a refund all end it.
    func refresh() async {
        var active = false
        if let group = products.first?.subscription?.subscriptionGroupID,
           let statuses = try? await Product.SubscriptionInfo.status(for: group) {
            active = statuses.contains { s in
                guard case .verified = s.transaction else { return false }
                return Self.unlocks(s.state)
            }
        } else {
            // Products not loaded yet (offline at launch): fall back to the entitlements on the device.
            for await result in Transaction.currentEntitlements {
                if case .verified(let t) = result, [Self.yearlyID, Self.monthlyID].contains(t.productID), t.revocationDate == nil,
                   t.expirationDate.map({ $0 > .now }) ?? true {
                    active = true
                }
            }
        }
        #if DEBUG
        if ProcessInfo.processInfo.environment["NOTED_PRO"] == "1" { active = true }
        #endif
        isPro = active
    }

    var yearly: Product? { products.first { $0.id == Self.yearlyID } }
    var monthly: Product? { products.first { $0.id == Self.monthlyID } }

    /// How much cheaper a year is than twelve months, as a whole percent. Nil when there is no saving.
    var yearlySaving: Int? {
        guard let y = yearly, let m = monthly else { return nil }
        let full = NSDecimalNumber(decimal: m.price).doubleValue * 12
        guard full > 0 else { return nil }
        let saved = Int(((1 - NSDecimalNumber(decimal: y.price).doubleValue / full) * 100).rounded())
        return saved > 0 ? saved : nil
    }

    func buy(_ product: Product) async {
        busy = true
        defer { busy = false }
        do {
            switch try await product.purchase() {
            case .success(let result):
                if case .verified(let t) = result { await t.finish() }
                await refresh()
                message = isPro ? nil : "购买没有通过验证"
            case .pending: message = "等待批准中"
            case .userCancelled: message = nil
            @unknown default: message = nil
            }
        } catch { message = error.localizedDescription }
    }

    func restore() async {
        busy = true
        defer { busy = false }
        do { try await AppStore.sync() } catch { message = error.localizedDescription }
        await refresh()
        if !isPro { message = "没有找到可恢复的购买" }
    }
}
