import Foundation

/// Thin REST client for the endpoints in docs/contract.md §2. No retry/backoff logic beyond
/// what's needed to demo the maintenance path — this is a POC shell, not a resilience layer.
final class APIClient {
    let baseURL: URL
    private let session: URLSession

    init(baseURL: URL) {
        self.baseURL = baseURL
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 20
        self.session = URLSession(configuration: config)
    }

    // MARK: - Sessions

    /// `POST /sessions` (contract.md §2.1) — start, or resume via `resumeToken`.
    func startSession(locale: String, resumeToken: String?) async throws -> StartSessionResponse {
        let body = StartSessionRequest(locale: locale, resumeToken: resumeToken)
        return try await send(path: "/sessions", method: "POST", token: nil, idempotencyKey: nil, body: body)
    }

    /// `GET /sessions/{id}` (contract.md §2.2) — resume: recomputes the envelope + repairs fresh.
    func resumeSession(id: String, token: String) async throws -> Envelope {
        try await send(path: "/sessions/\(id)", method: "GET", token: token, idempotencyKey: nil, body: Optional<NoBody>.none)
    }

    /// `POST /sessions/{id}/steps/{stepId}` (contract.md §2.3). `Idempotency-Key` is required by
    /// the contract on every submit so flaky-network retries can't double-submit.
    func submitStep<Body: Encodable>(
        sessionId: String, stepId: String, token: String, idempotencyKey: String, body: Body
    ) async throws -> Envelope {
        try await send(
            path: "/sessions/\(sessionId)/steps/\(stepId)", method: "POST",
            token: token, idempotencyKey: idempotencyKey, body: body)
    }

    // MARK: - Uploads

    /// `POST /sessions/{id}/uploads` (contract.md §2.4).
    func requestUploadSlot(sessionId: String, token: String, kind: String, contentType: String, sizeBytes: Int) async throws -> UploadSlotResponse {
        let body = UploadSlotRequest(kind: kind, contentType: contentType, sizeBytes: sizeBytes)
        return try await send(
            path: "/sessions/\(sessionId)/uploads", method: "POST",
            token: token, idempotencyKey: nil, body: body)
    }

    /// Raw `PUT` of the file bytes straight to the presigned URL — never through the API
    /// (contract.md §2.4: "Client PUTs the raw bytes directly to url").
    func putUpload(slot: UploadSlotResponse, data: Data) async throws {
        guard let url = URL(string: slot.url) else { throw APIError.invalidURL }
        var request = URLRequest(url: url)
        request.httpMethod = slot.method
        for (key, value) in slot.headers {
            request.setValue(value, forHTTPHeaderField: key)
        }
        request.httpBody = data
        do {
            let (_, response) = try await session.upload(for: request, from: data)
            try Self.checkOK(response)
        } catch let error as APIError {
            throw error
        } catch {
            throw APIError.network(error)
        }
    }

    // MARK: - System / refdata

    /// `GET /system` (contract.md §2.8) — no auth, global banners/maintenance only.
    func fetchSystem() async throws -> SystemStatus {
        let envelope: SystemEnvelope = try await send(
            path: "/system", method: "GET", token: nil, idempotencyKey: nil, body: Optional<NoBody>.none)
        return envelope.system
    }

    /// `GET /refdata/{dataset}` (contract.md §2.5).
    func fetchRefData(dataset: String, parent: String?, query: String?, token: String) async throws -> RefDataResponse {
        var components = URLComponents(url: baseURL.appendingPathComponent("/refdata/\(dataset)"), resolvingAgainstBaseURL: false)!
        var items: [URLQueryItem] = []
        if let parent { items.append(URLQueryItem(name: "parent", value: parent)) }
        if let query { items.append(URLQueryItem(name: "q", value: query)) }
        components.queryItems = items.isEmpty ? nil : items
        return try await send(url: components.url!, method: "GET", token: token, idempotencyKey: nil, body: Optional<NoBody>.none)
    }

    // MARK: - Core request plumbing

    private func send<Body: Encodable, Response: Decodable>(
        path: String, method: String, token: String?, idempotencyKey: String?, body: Body?
    ) async throws -> Response {
        try await send(url: baseURL.appendingPathComponent(path), method: method, token: token, idempotencyKey: idempotencyKey, body: body)
    }

    private func send<Body: Encodable, Response: Decodable>(
        url: URL, method: String, token: String?, idempotencyKey: String?, body: Body?
    ) async throws -> Response {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let idempotencyKey { request.setValue(idempotencyKey, forHTTPHeaderField: "Idempotency-Key") }
        if let body, !(body is NoBody) {
            do {
                request.httpBody = try JSONEncoder().encode(body)
            } catch {
                throw APIError.decoding(error)
            }
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw APIError.network(error)
        }

        guard let http = response as? HTTPURLResponse else {
            throw APIError.network(URLError(.badServerResponse))
        }

        if http.statusCode == 503 {
            let retryAfter = (http.value(forHTTPHeaderField: "Retry-After")).flatMap(Int.init)
            throw APIError.maintenance(retryAfter: retryAfter)
        }

        guard (200...299).contains(http.statusCode) else {
            let errorBody = try? JSONDecoder().decode(APIErrorBody.self, from: data)
            throw APIError.http(status: http.statusCode, body: errorBody)
        }

        if data.isEmpty, let empty = EmptyResponse() as? Response {
            return empty
        }

        do {
            return try JSONDecoder().decode(Response.self, from: data)
        } catch {
            throw APIError.decoding(error)
        }
    }

    private static func checkOK(_ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            let status = (response as? HTTPURLResponse)?.statusCode ?? -1
            throw APIError.http(status: status, body: nil)
        }
    }
}

/// Placeholder decode target for the rare 204/empty-body response.
struct EmptyResponse: Decodable {}

/// Sentinel meaning "no request body" for GET requests through the generic `send` helper.
private struct NoBody: Encodable {}
