import Foundation

enum APIError: Error, LocalizedError {
    case invalidURL
    case network(Error)
    case decoding(Error)
    case http(status: Int, body: APIErrorBody?)
    /// Synthesized from either `system.status == "maintenance"` in a decoded envelope, or a
    /// plain gateway `503 + Retry-After` with no body at all (plan.md §3.1 — "the app also
    /// treats a plain gateway 503+Retry-After the same way").
    case maintenance(retryAfter: Int?)

    var errorDescription: String? {
        switch self {
        case .invalidURL: return "Invalid URL."
        case .network(let e): return "Network error: \(e.localizedDescription)"
        case .decoding(let e): return "Couldn't parse the server response: \(e.localizedDescription)"
        case .http(let status, let body):
            return body?.error.message ?? "Server returned status \(status)."
        case .maintenance:
            return "The service is temporarily under maintenance."
        }
    }
}
