import AuthenticationServices
import CryptoKit
import SwiftUI

/// What Sign in with Apple hands back: the identity token and the raw nonce the server needs to check it.
struct AppleProof: Sendable {
    let identityToken: String
    let nonce: String
}

/// The system button. Apple gets the nonce's SHA-256; the server later gets the raw nonce, so a token
/// minted for another request is refused.
struct AppleButton: View {
    let type: SignInWithAppleButton.Label
    let onProof: (Result<AppleProof, Error>) -> Void
    @State private var nonce = ""

    var body: some View {
        SignInWithAppleButton(type) { request in
            nonce = Self.randomNonce()
            request.requestedScopes = []
            request.nonce = Self.sha256(nonce)
        } onCompletion: { result in
            switch result {
            case .failure(let e): onProof(.failure(e))
            case .success(let auth):
                guard let c = auth.credential as? ASAuthorizationAppleIDCredential,
                      let data = c.identityToken, let token = String(data: data, encoding: .utf8) else {
                    onProof(.failure(AppleError.noToken)); return
                }
                onProof(.success(AppleProof(identityToken: token, nonce: nonce)))
            }
        }
        .signInWithAppleButtonStyle(.black)
    }

    static func randomNonce() -> String {
        var bytes = [UInt8](repeating: 0, count: 32)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }

    static func sha256(_ s: String) -> String {
        SHA256.hash(data: Data(s.utf8)).map { String(format: "%02x", $0) }.joined()
    }
}

enum AppleError: LocalizedError {
    case noToken
    var errorDescription: String? { "Apple 没有返回身份令牌" }
}

extension Error {
    /// The person closing the Apple sheet is not a failure worth showing.
    var isAppleCancel: Bool { (self as? ASAuthorizationError)?.code == .canceled }
}
