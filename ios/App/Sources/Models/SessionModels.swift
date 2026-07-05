import Foundation

struct ClientCapabilities: Codable {
    let platform = "ios"
    let appVersion = "1.0.0-poc"
    let supportedTypes = ["form", "camera", "signature", "document", "pin", "external"]
    let supportedFieldKinds = ["text", "date", "select", "multiselect", "money", "bool"]

    enum CodingKeys: String, CodingKey {
        case platform
        case appVersion = "app_version"
        case supportedTypes = "supported_types"
        case supportedFieldKinds = "supported_field_kinds"
    }
}

struct StartSessionRequest: Codable {
    let locale: String
    let client = ClientCapabilities()
    /// TODO: real App Attest token (plan.md §5 rate limits — "device attestation is what makes
    /// the device key real"). POC stub only; the backend is expected to accept any non-empty
    /// string here.
    let deviceAttestation: String? = "poc-stub-attestation-token"
    let resumeToken: String?

    enum CodingKeys: String, CodingKey {
        case locale, client
        case deviceAttestation = "device_attestation"
        case resumeToken = "resume_token"
    }
}

struct SessionInfo: Codable {
    let id: String
    let flow: String
    let flowVersion: Int
    let expiresAt: String

    enum CodingKeys: String, CodingKey {
        case id, flow
        case flowVersion = "flow_version"
        case expiresAt = "expires_at"
    }
}

struct StartSessionResponse: Codable {
    let session: SessionInfo
    let token: String
    let system: SystemStatus
    let progress: Progress
    let nextStep: StepDefinition?
    let repairs: [Repair]

    enum CodingKeys: String, CodingKey {
        case session, token, system, progress, repairs
        case nextStep = "next_step"
    }

    var envelope: Envelope { Envelope(system: system, progress: progress, nextStep: nextStep, repairs: repairs) }
}

struct UploadSlotRequest: Codable {
    let kind: String // id_card | selfie | signature
    let contentType: String
    let sizeBytes: Int

    enum CodingKeys: String, CodingKey {
        case kind
        case contentType = "content_type"
        case sizeBytes = "size_bytes"
    }
}

struct UploadSlotResponse: Codable {
    let uploadRef: String
    let url: String
    let method: String
    let headers: [String: String]
    let expiresAt: String

    enum CodingKeys: String, CodingKey {
        case uploadRef = "upload_ref"
        case url, method, headers
        case expiresAt = "expires_at"
    }
}

// MARK: - Step submit bodies (contract.md §2.3)

struct FormSubmitBody: Encodable {
    let answers: [String: AnyCodable]
}

struct CameraSubmitBody: Encodable {
    struct ClientChecks: Encodable {
        let facePresent: Bool
        let blurry: Bool
        enum CodingKeys: String, CodingKey {
            case facePresent = "face_present"
            case blurry
        }
    }

    let uploadRef: String
    let clientChecks: ClientChecks

    enum CodingKeys: String, CodingKey {
        case uploadRef = "upload_ref"
        case clientChecks = "client_checks"
    }
}

struct SignatureSubmitBody: Encodable {
    let uploadRef: String
    enum CodingKeys: String, CodingKey { case uploadRef = "upload_ref" }
}

struct DocumentAcceptBody: Encodable {
    let accept: Bool
    let doc: DocPointer
}

struct PinSubmitBody: Encodable {
    let pin: String
}

struct ExternalSubmitBody: Encodable {
    let adapter: String
    let result: [String: AnyCodable]
}

// MARK: - Error envelope (contract.md §2.3)

struct APIErrorBody: Codable {
    struct FieldError: Codable {
        let key: String
        let rule: String
        let message: String
    }

    struct ErrorDetail: Codable {
        let code: String
        let message: String
        let stepId: String?
        let fields: [FieldError]?

        enum CodingKeys: String, CodingKey {
            case code, message, fields
            case stepId = "step_id"
        }
    }

    let error: ErrorDetail
    let currentDoc: DocPointer?

    enum CodingKeys: String, CodingKey {
        case error
        case currentDoc = "current_doc"
    }
}

// MARK: - Reference data (contract.md §2.5)

struct RefDataItem: Codable, Identifiable, Hashable {
    let code: String
    let label: String
    var id: String { code }
}

struct RefDataResponse: Codable {
    let dataset: String
    let version: Int
    let items: [RefDataItem]
}
