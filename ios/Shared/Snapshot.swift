import Foundation

/// What the widgets and the watch show: a copy of today, written by the phone app.
/// Widgets and the watch never talk to the server and never see the token.
struct Snapshot: Codable, Equatable, Sendable {
    struct Item: Codable, Equatable, Sendable, Identifiable {
        var id: String
        var title: String
        var time: Date?
        var allDay = false
        var isTask = false
        var overdue = false
        var work = false
    }
    struct Goal: Codable, Equatable, Sendable, Identifiable {
        var id: String
        var title: String
        var done: Double
        var target: Double
        var unit: String
        var behind = false
        var work = false
        var percent: Double { target > 0 ? min(done / target, 1) : 0 }
    }
    /// Resolved colors, so a widget looks like the app's theme without knowing about themes.
    struct Colors: Codable, Equatable, Sendable {
        var bg: UInt32 = 0xF4F6F8
        var surface: UInt32 = 0xFFFFFF
        var ink: UInt32 = 0x0E1A22
        var ink2: UInt32 = 0x55626C
        var line: UInt32 = 0xDDE2E7
        var accent: UInt32 = 0x1F5BFF
        var work: UInt32 = 0x1F5BFF
        var life: UInt32 = 0xB94A14
        var bad: UInt32 = 0xC62F26
    }

    var generated = Date()
    var pro = false
    var items: [Item] = []
    var goals: [Goal] = []
    var colors = Colors()

    /// The next thing that has not happened yet, falling back to the first open task.
    func next(now: Date = .now) -> Item? {
        items.first { ($0.time ?? .distantFuture) >= now && !$0.allDay }
            ?? items.first { $0.isTask }
            ?? items.first
    }

    init() {}
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        generated = try c.decodeIfPresent(Date.self, forKey: .generated) ?? Date()
        pro = try c.decodeIfPresent(Bool.self, forKey: .pro) ?? false
        items = try c.decodeIfPresent([Item].self, forKey: .items) ?? []
        goals = try c.decodeIfPresent([Goal].self, forKey: .goals) ?? []
        colors = try c.decodeIfPresent(Colors.self, forKey: .colors) ?? Colors()
    }
}

enum SnapshotStore {
    static let group = "group.cn.superleo.noted"
    private static let key = "snapshot"

    /// Tests point this at a temporary file; otherwise the shared container is used.
    nonisolated(unsafe) static var overrideURL: URL?

    private static var url: URL? {
        if let overrideURL { return overrideURL }
        return FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)?
            .appendingPathComponent("snapshot.json")
    }

    static func save(_ s: Snapshot) {
        guard let url, let data = try? encoder.encode(s) else { return }
        try? data.write(to: url, options: .atomic)
    }

    static func load() -> Snapshot? {
        guard let url, let data = try? Data(contentsOf: url) else { return nil }
        return try? decoder.decode(Snapshot.self, from: data)
    }

    private static var encoder: JSONEncoder { let e = JSONEncoder(); e.dateEncodingStrategy = .secondsSince1970; return e }
    private static var decoder: JSONDecoder { let d = JSONDecoder(); d.dateDecodingStrategy = .secondsSince1970; return d }

    static func encode(_ s: Snapshot) -> Data? { try? encoder.encode(s) }
    static func decode(_ d: Data) -> Snapshot? { try? decoder.decode(Snapshot.self, from: d) }
}
