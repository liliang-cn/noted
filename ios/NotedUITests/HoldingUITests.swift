import XCTest

final class HoldingUITests: NotedUITestCase {
    func testTrackAHoldingWithAMonthlyPlanOnThe15th() throws {
        try launch(mode: "life")
        tab("项目")
        button("标的").tap()
        let new = app.buttons["new-holding"]
        exists(new)
        new.tap()

        type("qqqm", into: app.textFields["holding-symbol"])
        type("10", into: app.textFields["owned-shares"])
        type("150", into: app.textFields["owned-cost"])
        type("500", into: app.textFields["plan-amount"])
        app.buttons["save-holding"].tap()

        let card = app.otherElements["holding-QQQM"]
        exists(card, 12)
        exists(card.staticTexts["10 股 · 均价 150.00"])
        exists(card.staticTexts["本月待投"])
        XCTAssertTrue(card.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "每月 15 号 · 500")).firstMatch.exists, "the plan day and amount show")

        // A buy this month: 3 more at 160 -> 13 shares at (1500 + 480) / 13.
        card.buttons["买入 QQQM"].tap()
        type("3", into: app.textFields["trade-shares"])
        type("160", into: app.textFields["trade-price"])
        app.buttons["save-trade"].tap()
        exists(card.staticTexts["13 股 · 均价 152.31"])
        exists(card.staticTexts["本月已投"])

        // A price the person types gives market value and the gain: 13 * 170 - 1980 = 230.
        card.staticTexts["QQQM"].tap()
        let price = app.textFields["last-price"]
        exists(price)
        type("170", into: price)
        app.buttons["save-price"].tap()
        exists(text("+230.00"))
        exists(text("2210.00 USD"))
    }
}
