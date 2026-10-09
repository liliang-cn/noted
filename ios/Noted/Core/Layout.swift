import Foundation

struct ModuleState: Codable, Equatable, Identifiable, Sendable {
    var id: String
    var on: Bool
}

/// Which blocks the overview shows and in what order, as stored in "ui.layout".
struct Layout: Codable, Equatable, Sendable {
    static let catalog: [(id: String, title: String, detail: String)] = [
        ("pinned", "置顶项目", "横向卡片"),
        ("items", "日程与待办", "按时间排列"),
        ("goals", "目标", "进度条 · 紧凑"),
        ("briefing", "今日简报", "一段自动生成的文字"),
        ("notes", "最近笔记", "最近编辑的 3 条"),
        ("heat", "近 14 天热力", "每个目标一行"),
    ]
    static let standard: [ModuleState] = catalog.map { ModuleState(id: $0.id, on: ["pinned", "items", "goals"].contains($0.id)) }

    var perMode = false
    var all = Layout.standard
    var life = Layout.standard
    var work = Layout.standard

    init() {}

    /// Any field may be missing, and modules added since it was saved are appended switched off.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        perMode = try c.decodeIfPresent(Bool.self, forKey: .perMode) ?? false
        all = Layout.reconcile(try c.decodeIfPresent([ModuleState].self, forKey: .all))
        life = Layout.reconcile(try c.decodeIfPresent([ModuleState].self, forKey: .life))
        work = Layout.reconcile(try c.decodeIfPresent([ModuleState].self, forKey: .work))
    }

    static func reconcile(_ saved: [ModuleState]?) -> [ModuleState] {
        guard let saved, !saved.isEmpty else { return standard }
        let known = Set(catalog.map(\.id))
        var out = saved.filter { known.contains($0.id) }
        for m in standard where !out.contains(where: { $0.id == m.id }) { out.append(ModuleState(id: m.id, on: false)) }
        return out
    }

    func modules(for mode: Mode) -> [ModuleState] {
        guard perMode else { return all }
        switch mode {
        case .all: return all
        case .life: return life
        case .work: return work
        }
    }

    mutating func set(_ modules: [ModuleState], for mode: Mode) {
        guard perMode else { all = modules; return }
        switch mode {
        case .all: all = modules
        case .life: life = modules
        case .work: work = modules
        }
    }
}
