import Foundation

struct Banner: Codable, Identifiable {
    let id: String
    let severity: String
    let scope: String
    let text: String
}

struct SystemStatus: Codable {
    enum Status: String, Codable {
        case ok, degraded, maintenance
    }

    let status: Status
    let retryAfter: Int?
    let banners: [Banner]

    enum CodingKeys: String, CodingKey {
        case status
        case retryAfter = "retry_after"
        case banners
    }

    static let ok = SystemStatus(status: .ok, retryAfter: nil, banners: [])
}

struct Progress: Codable {
    let completed: Int
    let total: Int
}

/// contract.md §4. `detail` is intentionally untyped (`AnyCodable`) — its shape depends on
/// `kind`, and the client only needs it to render a human-readable reason; `next_step` (not
/// this struct) is what actually drives navigation.
struct Repair: Codable, Identifiable {
    let kind: String
    let stepId: String
    let reason: String
    let detail: [String: AnyCodable]?

    var id: String { stepId + "." + kind }

    enum CodingKeys: String, CodingKey {
        case kind
        case stepId = "step_id"
        case reason
        case detail
    }

    var displayText: String {
        switch kind {
        case "reaccept_document":
            return "The Terms & Conditions changed — please review and re-accept."
        case "collect_fields":
            let fields = detail?["fields"]?.arrayValue?.compactMap { $0 as? String }.joined(separator: ", ")
            return "We need a couple more details" + (fields.map { ": \($0)" } ?? ".")
        case "redo_step":
            return "One of your uploads needs to be retaken."
        default:
            return reason
        }
    }
}

/// `GET /system` (contract.md §2.8) — global-only, no session required.
struct SystemEnvelope: Codable {
    let system: SystemStatus
}

/// The envelope wrapping every session-scoped response (contract.md §1).
struct Envelope: Codable {
    let system: SystemStatus
    let progress: Progress
    let nextStep: StepDefinition?
    let repairs: [Repair]

    enum CodingKeys: String, CodingKey {
        case system, progress
        case nextStep = "next_step"
        case repairs
    }
}
