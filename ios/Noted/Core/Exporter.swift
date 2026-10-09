import Foundation

/// Asks the server for everything the person has and keeps it as one zip file:
/// noted.json, notes/*.md and calendar.ics.
enum Exporter {
    static func run(api: API, into dir: URL = FileManager.default.temporaryDirectory) async throws -> URL {
        let (name, data): (String, Data) = try await api.export.export(Noted_V1_ExportRequest()) { response in
            var name = "noted.zip"
            var data = Data()
            for try await part in response.messages {
                if !part.filename.isEmpty { name = part.filename }
                data.append(part.data)
            }
            return (name, data)
        }
        guard !data.isEmpty else { throw ExportError.empty }
        let url = dir.appendingPathComponent(name)
        try? FileManager.default.removeItem(at: url)
        try data.write(to: url, options: .atomic)
        return url
    }
}

enum ExportError: Error { case empty }

struct ExportedFile: Identifiable {
    let id = UUID()
    let url: URL
}
