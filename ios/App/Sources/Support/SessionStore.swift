import Foundation

/// Persists just enough to resume a session across app launches.
///
/// TODO(security): this is UserDefaults for POC simplicity. A real build must move
/// `sessionToken` (and the resume token) into the Keychain with
/// `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`, and exclude it from iCloud/device
/// backups — same discipline the plan calls for on the on-device draft store (plan.md §2.1
/// "Mid-page drop-off"). Nothing here is PII by itself (it's an opaque bearer token), but it
/// is a session credential and shouldn't be treated more casually than that.
final class SessionStore {
    static let shared = SessionStore()

    private enum Keys {
        static let sessionId = "session.id"
        static let token = "session.token"
        static let resumeToken = "session.resumeToken"
    }

    private let defaults = UserDefaults.standard

    var sessionId: String? {
        get { defaults.string(forKey: Keys.sessionId) }
        set { defaults.set(newValue, forKey: Keys.sessionId) }
    }

    var token: String? {
        get { defaults.string(forKey: Keys.token) }
        set { defaults.set(newValue, forKey: Keys.token) }
    }

    /// Not yet returned by the contract's `/sessions` response (only a bearer `token` is), but
    /// plumbed through so resume-by-token is a one-line change when the backend adds it.
    var resumeToken: String? {
        get { defaults.string(forKey: Keys.resumeToken) }
        set { defaults.set(newValue, forKey: Keys.resumeToken) }
    }

    func clear() {
        defaults.removeObject(forKey: Keys.sessionId)
        defaults.removeObject(forKey: Keys.token)
        defaults.removeObject(forKey: Keys.resumeToken)
    }
}
