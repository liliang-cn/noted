import SwiftUI

@main
struct NotedApp: App {
    @State private var session: Session
    @State private var toaster: Toaster
    @State private var data: AppData
    @State private var navigation = AppNavigation()

    init() {
        let s = Session()
        let t = Toaster()
        _session = State(initialValue: s)
        _toaster = State(initialValue: t)
        _data = State(initialValue: AppData(session: s, toaster: t))
        WatchBridge.shared.start()
    }

    var body: some Scene {
        WindowGroup {
            Group {
                if session.api != nil {
                    AppShell()
                } else {
                    SignInView().toastHost()
                }
            }
            .onOpenURL { open($0) }
            .environment(session)
            .environment(toaster)
            .environment(data)
            .environment(navigation)
            .environment(\.palette, session.palette)
            .environment(\.mode, session.mode)
            .tint(session.accent)
            .preferredColorScheme(.light)
        }
    }

    /// `noted://pair?code=…&server=…`, e.g. the authorize page's QR code read by the system camera.
    /// Other `noted://` links (note references) are handled inside the notes and never get here.
    private func open(_ url: URL) {
        guard let link = PairLink(url) else { return }
        guard session.api != nil else {
            toaster.error("先登录 noted")
            return
        }
        if let why = session.pairingMismatch(link.server) {
            toaster.error(why)
            return
        }
        navigation.openPairing(link.code)
    }
}
