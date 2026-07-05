import Foundation

/// Drives the whole app: start/resume, hold the current envelope, submit steps, and react to
/// the system envelope (maintenance/banners). This is the "thin shell" from plan.md §1 — it
/// never knows the flow, only how to ask for `next_step` and hand it to the step registry.
@MainActor
final class SessionViewModel: ObservableObject {
    @Published private(set) var envelope: Envelope?
    @Published private(set) var sessionInfo: SessionInfo?
    @Published private(set) var token: String?
    @Published var isLoading = false
    @Published var errorMessage: String?
    @Published var forceUpdateMinVersion: String?
    /// Structured `422 validation_failed` field errors (contract.md §2.3) from the most recent
    /// submit, if any — cleared on every new submit attempt and on any successful response.
    @Published var lastValidationErrors: [APIErrorBody.FieldError] = []

    private var api: APIClient
    private let store = SessionStore.shared
    let settings: AppSettings

    init(settings: AppSettings) {
        self.settings = settings
        self.api = APIClient(baseURL: settings.baseURL)
    }

    /// Call after a Settings change to a different backend host.
    func rebuildClient() {
        api = APIClient(baseURL: settings.baseURL)
    }

    var isMaintenance: Bool { envelope?.system.status == .maintenance }

    // MARK: - Bootstrap

    /// Resumes the persisted session if one exists, otherwise starts a fresh one. This is the
    /// one call the root view makes on launch.
    func bootstrap() async {
        isLoading = true
        defer { isLoading = false }
        do {
            if let id = store.sessionId, let token = store.token {
                sessionInfo = SessionInfo(id: id, flow: "retail_onboarding", flowVersion: 0, expiresAt: "")
                self.token = token
                let env = try await api.resumeSession(id: id, token: token)
                apply(env)
                return
            }
            try await start()
        } catch APIError.maintenance(let retryAfter) {
            applyMaintenance(retryAfter: retryAfter)
        } catch {
            // Resume failed for a reason other than maintenance (expired/unknown session,
            // corrupted local state, …) — fall back to a fresh session rather than getting
            // stuck. A real build would distinguish 404/410 from a transient network error here.
            store.clear()
            do {
                try await start()
            } catch APIError.maintenance(let retryAfter) {
                applyMaintenance(retryAfter: retryAfter)
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    private func start() async throws {
        let response = try await api.startSession(locale: settings.locale, resumeToken: store.resumeToken)
        store.sessionId = response.session.id
        store.token = response.token
        sessionInfo = response.session
        token = response.token
        apply(response.envelope)
    }

    /// Re-fetches full state — used after resolving maintenance, after a webview step reports
    /// completion, and as a manual "refresh" affordance.
    func refresh() async {
        guard let id = sessionInfo?.id, let token else { return }
        do {
            let env = try await api.resumeSession(id: id, token: token)
            apply(env)
        } catch APIError.maintenance(let retryAfter) {
            applyMaintenance(retryAfter: retryAfter)
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    /// Long-dwell steps (camera) poll this on a slow timer per plan.md §3.1, without re-fetching
    /// the whole session.
    func pollSystemOnly() async {
        do {
            let system = try await api.fetchSystem()
            if var env = envelope {
                env = Envelope(system: system, progress: env.progress, nextStep: env.nextStep, repairs: env.repairs)
                envelope = env
            }
        } catch {
            // Best-effort background poll; swallow errors rather than interrupting the user.
        }
    }

    // MARK: - Submit

    func submit<Body: Encodable>(step: StepDefinition, body: Body) async {
        guard let sessionId = sessionInfo?.id, let token else { return }
        isLoading = true
        lastValidationErrors = []
        defer { isLoading = false }
        do {
            let env = try await api.submitStep(
                sessionId: sessionId, stepId: step.id, token: token,
                idempotencyKey: UUID().uuidString, body: body)
            apply(env)
        } catch APIError.maintenance(let retryAfter) {
            applyMaintenance(retryAfter: retryAfter)
        } catch APIError.http(let status, let errorBody) where status == 422 {
            lastValidationErrors = errorBody?.error.fields ?? []
            errorMessage = errorBody?.error.message
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    // MARK: - Uploads

    func uploadCapture(kind: String, data: Data, contentType: String) async throws -> String {
        guard let sessionId = sessionInfo?.id, let token else { throw APIError.invalidURL }
        let slot = try await api.requestUploadSlot(
            sessionId: sessionId, token: token, kind: kind, contentType: contentType, sizeBytes: data.count)
        try await api.putUpload(slot: slot, data: data)
        return slot.uploadRef
    }

    // MARK: - WebView renderer plumbing

    /// URL for the WKWebView renderer for `form`/`document` steps — the hybrid escape hatch
    /// (plan.md §1). The session token is also injected via a `WKUserScript` (see
    /// `WebStepView`), never left only in the URL, but it's included here too since the query
    /// string is how the web renderer would identify the session on first load in a real build.
    func webRendererURL(for step: StepDefinition) -> URL {
        var components = URLComponents(url: settings.baseURL, resolvingAgainstBaseURL: false)!
        components.path = "/web/"
        components.fragment = "/step/\(step.id)"
        components.queryItems = [URLQueryItem(name: "session", value: sessionInfo?.id)]
        return components.url ?? settings.baseURL
    }

    func refData(dataset: String, parent: String?, query: String?) async throws -> RefDataResponse {
        guard let token else { throw APIError.invalidURL }
        return try await api.fetchRefData(dataset: dataset, parent: parent, query: query, token: token)
    }

    // MARK: - Helpers

    private func apply(_ env: Envelope) {
        errorMessage = nil
        envelope = env
        if env.nextStep?.id == "force_update" {
            forceUpdateMinVersion = env.nextStep?.minAppVersion
        } else {
            forceUpdateMinVersion = nil
        }
    }

    /// Synthesizes a maintenance envelope for a plain gateway `503 + Retry-After` that carried
    /// no envelope body at all (plan.md §3.1).
    private func applyMaintenance(retryAfter: Int?) {
        let banners = envelope?.system.banners ?? []
        let progress = envelope?.progress ?? Progress(completed: 0, total: 0)
        envelope = Envelope(
            system: SystemStatus(status: .maintenance, retryAfter: retryAfter, banners: banners),
            progress: progress, nextStep: envelope?.nextStep, repairs: envelope?.repairs ?? [])
    }

    /// Starts over with a brand-new session, discarding the persisted one. Used from the
    /// completion screen and as a debug affordance in Settings.
    func resetAndStartNew() async {
        store.clear()
        token = nil
        sessionInfo = nil
        envelope = nil
        isLoading = true
        defer { isLoading = false }
        do {
            try await start()
        } catch APIError.maintenance(let retryAfter) {
            applyMaintenance(retryAfter: retryAfter)
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
