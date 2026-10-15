import Foundation
import GRPCCore

/// What 设置 needs beyond the shared writes: the server's AI switches, linking an Apple ID, and
/// approving a client's pairing code.
extension AppData {
    /// The server's AI layer and this account's switches for it.
    struct AISettings: Sendable {
        var status: Noted_V1_GetStatusResponse
        var features: Noted_V1_AIFeatures
        var access: Noted_V1_AIAccess
    }

    /// Reads the AI status, and the feature and space switches when AI is on. Silent on failure
    /// (a server without AI is normal): nil means the status could not be read.
    func loadAISettings() async -> AISettings? {
        guard let api = session.api,
              let st = try? await api.ai.getStatus(Noted_V1_GetStatusRequest()) else { return nil }
        guard st.enabled else { return AISettings(status: st, features: .init(), access: .init()) }
        async let f = try? api.ai.getAIFeatures(Noted_V1_GetAIFeaturesRequest())
        async let a = try? api.ai.getAIAccess(Noted_V1_GetAIAccessRequest())
        let features = await f ?? Noted_V1_AIFeatures()
        let access = await a ?? Noted_V1_AIAccess()
        session.aiChat = st.chat
        session.aiNoteTools = st.chat && features.noteTools
        return AISettings(status: st, features: features, access: access)
    }

    @discardableResult
    func setAIFeatures(_ f: Noted_V1_AIFeatures) async -> Noted_V1_AIFeatures? {
        var r = Noted_V1_SetAIFeaturesRequest()
        r.dailyBriefing = f.dailyBriefing; r.suggestions = f.suggestions
        r.weeklyReview = f.weeklyReview; r.noteTools = f.noteTools
        guard let got = await perform({ try await $0.ai.setAIFeatures(r) }) else { return nil }
        session.aiNoteTools = session.aiChat && got.noteTools
        return got
    }

    @discardableResult
    func setAIAccess(_ a: Noted_V1_AIAccess) async -> Noted_V1_AIAccess? {
        var r = Noted_V1_SetAIAccessRequest()
        r.allowWork = a.allowWork; r.allowLife = a.allowLife
        return await perform { try await $0.ai.setAIAccess(r) }
    }

    /// Links the Apple ID behind `proof` to this account, so it can sign in on new devices.
    @discardableResult
    func linkApple(_ proof: AppleProof) async -> Bool {
        var r = Noted_V1_LinkAppleRequest()
        r.identityToken = proof.identityToken; r.nonce = proof.nonce
        guard let a = await perform({ try await $0.account.linkApple(r) }) else { return false }
        account = a
        return true
    }

    @discardableResult
    func unlinkApple() async -> Bool {
        guard let a = await perform({ try await $0.account.unlinkApple(Noted_V1_UnlinkAppleRequest()) }) else { return false }
        account = a
        return true
    }

    // MARK: pairing

    /// Why a pairing call failed, in words for the pairing page (it shows them in place, not as a toast).
    struct PairingError: Error {
        var message: String
        /// The code is unknown, expired or already used: another code is needed.
        var gone: Bool
    }

    /// A pairing waiting for this account's answer.
    func getPairing(_ code: String) async -> Result<Noted_V1_Pairing, PairingError> {
        var r = Noted_V1_GetPairingRequest(); r.code = code
        return await pairingCall { try await $0.account.getPairing(r) }
    }

    /// Lets the client have a token of this account, labelled with its name.
    func approvePairing(_ code: String) async -> Result<Noted_V1_Pairing, PairingError> {
        var r = Noted_V1_ApprovePairingRequest(); r.code = code
        let got = await pairingCall { try await $0.account.approvePairing(r) }
        if case .success = got {
            // The token is minted when the client trades the approval for it (its browser notices
            // within a second or two), so the list is read again a few times.
            Task { for s in [2, 3, 5] { try? await Task.sleep(for: .seconds(s)); await loadTokens() } }
        }
        return got
    }

    /// Turns the client away. A code that is already gone counts as done.
    func denyPairing(_ code: String) async -> PairingError? {
        var r = Noted_V1_DenyPairingRequest(); r.code = code
        if case .failure(let e) = await pairingCall({ try await $0.account.denyPairing(r) }), !e.gone { return e }
        return nil
    }

    private func pairingCall<T: Sendable>(_ op: (API) async throws -> T) async -> Result<T, PairingError> {
        guard let api = session.api else { return .failure(PairingError(message: "先登录 noted", gone: false)) }
        do {
            return .success(try await op(api))
        } catch let e as RPCError where e.code == .notFound {
            return .failure(PairingError(message: "配对码无效或已过期", gone: true))
        } catch {
            return .failure(PairingError(message: describe(error), gone: false))
        }
    }
}
