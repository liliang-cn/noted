import SwiftUI

@main
struct NotedApp: App {
    @State private var session = Session()

    init() { WatchBridge.shared.start() }

    var body: some Scene {
        WindowGroup {
            Group {
                if session.api != nil { RootView() } else { ConnectView() }
            }
            .environment(session)
            .environment(\.palette, session.palette)
            .environment(\.mode, session.mode)
            .tint(session.mode.accent(session.palette))
            .preferredColorScheme(.light)
        }
    }
}
