import SwiftUI

@main
struct RegistrationApp: App {
    @StateObject private var settings: AppSettings
    @StateObject private var session: SessionViewModel

    init() {
        let settings = AppSettings()
        _settings = StateObject(wrappedValue: settings)
        _session = StateObject(wrappedValue: SessionViewModel(settings: settings))
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(session)
                .environmentObject(settings)
        }
    }
}
