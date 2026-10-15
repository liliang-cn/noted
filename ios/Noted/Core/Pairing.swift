import Foundation

/// Pairing a client that signs in through the browser (Claude Code's /mcp → 登录): the server's
/// page shows a code ("K7QM-29XD") and a QR code of `noted://pair?code=K7QM29XD&server=https://…`,
/// and this app approves it.
enum PairCode {
    /// "k7qm-29xd", "K7QM 29XD" → "K7QM29XD"; nil when it cannot be a code.
    static func normalize(_ s: String) -> String? {
        let kept = s.uppercased().filter { $0 != "-" && !$0.isWhitespace }
        guard kept.count >= 4, kept.count <= 32, kept.allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber) }) else { return nil }
        return kept
    }

    /// "K7QM29XD" → "K7QM-29XD": groups of four.
    static func display(_ code: String) -> String {
        var out = ""
        for (i, c) in code.enumerated() {
            if i > 0, i % 4 == 0 { out.append("-") }
            out.append(c)
        }
        return out
    }
}

/// A `noted://pair?code=…&server=…` link (scanned, or opened by the system camera).
struct PairLink: Equatable {
    var code: String
    /// The server that minted the code; nil when the link does not say.
    var server: URL?

    init?(_ url: URL) {
        guard url.scheme?.lowercased() == "noted", url.host()?.lowercased() == "pair",
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems,
              let code = items.first(where: { $0.name == "code" })?.value.flatMap(PairCode.normalize) else { return nil }
        self.code = code
        server = items.first(where: { $0.name == "server" })?.value.flatMap { URL(string: $0) }
    }

    init?(_ text: String) {
        guard let url = URL(string: text.trimmingCharacters(in: .whitespacesAndNewlines)) else { return nil }
        self.init(url)
    }
}

extension Session {
    /// Why a pairing link from `server` cannot be approved here, or nil when it is this server.
    /// The host must match. Scheme and port are compared when the app talks to the server on a web
    /// port (the hosted service behind its proxy); a server reached on its own gRPC port serves the
    /// pairing page on another port, so there only the host can be compared.
    func pairingMismatch(_ server: URL?) -> String? {
        guard let server, let host = server.host(), !host.isEmpty else { return nil }
        let scheme = server.scheme?.lowercased() ?? "https"
        let theirPort = server.port ?? (scheme == "http" ? 80 : 443)
        let ours = self.host.trimmingCharacters(in: CharacterSet(charactersIn: " ."))
        let ourPort = Int(port) ?? 0
        var same = Self.sameHost(host, ours)
        if same, ourPort == 443 || ourPort == 80 {
            same = theirPort == ourPort && (scheme == "https") == tls
        }
        guard !same else { return nil }
        let theirs = server.port == nil ? host : "\(host):\(theirPort)"
        let mine = (tls && ourPort == 443) || (!tls && ourPort == 80) ? ours : "\(ours):\(port)"
        return "这个配对码来自 \(theirs)，你当前登录的是 \(mine)"
    }

    private static func sameHost(_ a: String, _ b: String) -> Bool {
        func clean(_ s: String) -> String {
            s.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: ".[]"))
        }
        return clean(a) == clean(b)
    }
}
