import XCTest

final class ExportUITests: NotedUITestCase {
    static let zipPath = "/tmp/noted-ui-export.zip"

    func testExportOffersTheFileToShareAndSave() throws {
        try? FileManager.default.removeItem(atPath: Self.zipPath)
        try launch(extra: ["NOTED_EXPORT_COPY": Self.zipPath])
        openSettings()
        let row = app.buttons["export"]
        exists(row)
        row.tap()
        // The system share sheet names the file it carries.
        let sheet = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@ OR identifier == %@", "noted-", "ActivityListView")).firstMatch
        exists(sheet, 15)
        XCTAssertTrue(FileManager.default.fileExists(atPath: Self.zipPath), "the app wrote the zip")
    }
}
