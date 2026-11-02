import Foundation

extension AppData {
    /// The compass of a space; the all-spaces mode reads as life.
    func compass(for space: Noted_V1_Space) -> Noted_V1_Compass? {
        compasses[space == .work ? .work : .life]
    }

    func loadCompass(_ space: Noted_V1_Space) async {
        let sp: Noted_V1_Space = space == .work ? .work : .life
        var r = Noted_V1_GetCompassRequest(); r.space = sp
        if let c = await perform({ try await $0.compass.getCompass(r) }) { compasses[sp] = c }
    }

    /// Saves all four fields as a new version. Returns whether it went through.
    func saveCompass(_ space: Noted_V1_Space, antiVision: String, vision: String, identity: String, constraints: [String]) async -> Bool {
        let sp: Noted_V1_Space = space == .work ? .work : .life
        var c = Noted_V1_Compass()
        c.space = sp; c.antiVision = antiVision; c.vision = vision; c.identity = identity; c.constraints = constraints
        var r = Noted_V1_SetCompassRequest(); r.compass = c
        guard let saved = await perform({ try await $0.compass.setCompass(r) }) else { return false }
        compasses[sp] = saved
        return true
    }
}
