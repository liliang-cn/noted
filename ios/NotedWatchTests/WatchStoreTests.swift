import XCTest
@testable import NotedWatch

@MainActor
final class WatchStoreTests: XCTestCase {
    private var url: URL!

    override func setUp() {
        url = FileManager.default.temporaryDirectory.appendingPathComponent("watch-\(UUID().uuidString).json")
        SnapshotStore.overrideURL = url
    }

    override func tearDown() {
        SnapshotStore.overrideURL = nil
        try? FileManager.default.removeItem(at: url)
    }

    func testApplyingASnapshotKeepsAndSharesIt() {
        let store = WatchStore(isolated: false)
        var s = Snapshot()
        s.pro = true
        s.items = [.init(id: "t1", title: "交房租", isTask: true)]
        store.apply(s)
        XCTAssertEqual(store.snapshot.items.map(\.title), ["交房租"])
        XCTAssertEqual(SnapshotStore.load()?.items.count, 1, "the complication reads the same copy")
    }

    func testAnActionWithNoPhoneNearbySaysSo() {
        let store = WatchStore(isolated: true)   // a paired phone may well be running next to the simulator
        store.perform("complete", id: "t1")
        XCTAssertEqual(store.message, "手机不在身边")
        XCTAssertFalse(store.busy)
    }

    func testStartsFromWhatWasSavedLast() {
        var s = Snapshot()
        s.items = [.init(id: "e1", title: "上次的", time: nil)]
        SnapshotStore.save(s)
        XCTAssertEqual(WatchStore(isolated: false).snapshot.items.first?.title, "上次的")
    }

    func testSampleModeIgnoresWhatThePhoneSends() {
        let store = WatchStore(isolated: true)
        var s = Snapshot()
        s.items = [.init(id: "t1", title: "来自手机", isTask: true)]
        store.apply(s)
        XCTAssertFalse(store.snapshot.items.contains { $0.title == "来自手机" })
    }
}
