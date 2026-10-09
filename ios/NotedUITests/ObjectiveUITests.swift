import XCTest

final class ObjectiveUITests: NotedUITestCase {
    func testWeightLossObjectiveWithDimensionsAndReadings() throws {
        try launch(mode: "life")
        tab("项目")
        button("目标").tap()
        let new = app.buttons["new-objective"]
        exists(new)
        new.tap()

        app.buttons["template-weightloss"].tap()
        type("75", into: app.textFields["metric-start"])
        type("68", into: app.textFields["metric-target"])
        app.buttons["save-objective"].tap()

        // The objective shows with its measured result and every dimension.
        let card = app.otherElements["objective-减肥"]
        exists(card, 12)
        for dim in ["游泳", "跑步", "健身", "轻食", "控糖"] { exists(card.staticTexts[dim]) }
        exists(card.staticTexts["75 → 68"])

        // One tap on a dimension counts a check-in.
        card.buttons["打卡 游泳"].tap()
        exists(card.staticTexts["1/2 次"])

        // Open it and record a reading: 75 -> 73.5 is 1.5 of 7 kg.
        card.staticTexts["减肥"].tap()
        let field = app.textFields["reading-value"]
        exists(field)
        type("73.5", into: field)
        app.buttons["reading-save"].tap()
        exists(app.staticTexts["73.5"])
        exists(text("21%"))
        exists(text("维度 · 本期 0/5 项达标"))
    }
}
