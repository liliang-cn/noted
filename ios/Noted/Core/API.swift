import Foundation
import GRPCCore
import GRPCNIOTransportHTTP2
import GRPCProtobuf
import SwiftProtobuf

/// Adds the bearer token to every call.
struct AuthInterceptor: ClientInterceptor {
    let token: String
    func intercept<Input: Sendable, Output: Sendable>(
        request: StreamingClientRequest<Input>,
        context: ClientContext,
        next: (StreamingClientRequest<Input>, ClientContext) async throws -> StreamingClientResponse<Output>
    ) async throws -> StreamingClientResponse<Output> {
        var request = request
        request.metadata.addString("Bearer \(token)", forKey: "authorization")
        return try await next(request, context)
    }
}

typealias Transport = HTTP2ClientTransport.Posix

/// One connection to a noted server.
final class API: Sendable {
    let client: GRPCClient<Transport>
    let notes: Noted_V1_NoteService.Client<Transport>
    let calendar: Noted_V1_CalendarService.Client<Transport>
    let goals: Noted_V1_GoalService.Client<Transport>
    let projects: Noted_V1_ProjectService.Client<Transport>
    let focus: Noted_V1_FocusService.Client<Transport>
    let suggest: Noted_V1_SuggestionService.Client<Transport>
    let ai: Noted_V1_AIService.Client<Transport>
    let prefs: Noted_V1_PreferenceService.Client<Transport>
    let export: Noted_V1_ExportService.Client<Transport>
    let objectives: Noted_V1_ObjectiveService.Client<Transport>
    let holdings: Noted_V1_HoldingService.Client<Transport>
    private let runner: Task<Void, Never>

    init(host: String, port: Int, tls: Bool, token: String) throws {
        let transport = try HTTP2ClientTransport.Posix(
            target: .dns(host: host, port: port),
            transportSecurity: tls ? .tls : .plaintext)
        let client = GRPCClient(
            transport: transport,
            interceptors: [AuthInterceptor(token: token)])
        self.client = client
        notes = .init(wrapping: client)
        calendar = .init(wrapping: client)
        goals = .init(wrapping: client)
        projects = .init(wrapping: client)
        focus = .init(wrapping: client)
        suggest = .init(wrapping: client)
        ai = .init(wrapping: client)
        prefs = .init(wrapping: client)
        export = .init(wrapping: client)
        objectives = .init(wrapping: client)
        holdings = .init(wrapping: client)
        runner = Task { try? await client.runConnections() }
    }

    func close() {
        client.beginGracefulShutdown()
        runner.cancel()
    }
}

extension Google_Protobuf_Timestamp {
    init(_ d: Date) { self.init(date: d) }
}

func fieldMask(_ paths: String...) -> Google_Protobuf_FieldMask {
    var m = Google_Protobuf_FieldMask()
    m.paths = paths
    return m
}

/// What to tell the person. The server's own messages are English and written for developers.
func describe(_ error: Error) -> String {
    guard let e = error as? RPCError else { return error.localizedDescription }
    switch e.code {
    case .unauthenticated: return "令牌无效或已被撤销,请重新连接"
    case .unavailable: return "连不上服务器"
    case .deadlineExceeded: return "服务器没有及时回应"
    case .failedPrecondition: return "这台服务器没有开启智能功能"
    case .permissionDenied: return "这部分内容没有允许智能功能读取"
    case .internalError where e.message.hasPrefix("AI request failed"): return "智能功能这次没有答上来,稍后再试"
    default: return e.message.isEmpty ? "出错了(\(e.code))" : e.message
    }
}
