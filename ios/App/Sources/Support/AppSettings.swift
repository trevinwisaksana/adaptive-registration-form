import Foundation
import Combine

/// App-wide POC settings, backed by UserDefaults. Nothing here is sensitive —
/// session token/id live in `SessionStore` instead (see its TODO on Keychain).
final class AppSettings: ObservableObject {
    @Published var baseURLString: String {
        didSet { UserDefaults.standard.set(baseURLString, forKey: Keys.baseURL) }
    }

    /// Settings-toggle comparison renderer for `type: "form"` steps (text/select/bool/date
    /// only). Everything else — and `document` always — goes through the WKWebView renderer.
    /// This exists purely to demo the hybrid trade-off from plan.md §1, side by side.
    @Published var useNativeFormRenderer: Bool {
        didSet { UserDefaults.standard.set(useNativeFormRenderer, forKey: Keys.nativeForm) }
    }

    @Published var locale: String {
        didSet { UserDefaults.standard.set(locale, forKey: Keys.locale) }
    }

    private enum Keys {
        static let baseURL = "settings.baseURL"
        static let nativeForm = "settings.useNativeFormRenderer"
        static let locale = "settings.locale"
    }

    init() {
        let defaults = UserDefaults.standard
        self.baseURLString = defaults.string(forKey: Keys.baseURL) ?? "http://localhost:8080"
        self.useNativeFormRenderer = defaults.object(forKey: Keys.nativeForm) as? Bool ?? false
        self.locale = defaults.string(forKey: Keys.locale) ?? "en-US"
    }

    var baseURL: URL {
        URL(string: baseURLString) ?? URL(string: "http://localhost:8080")!
    }
}
