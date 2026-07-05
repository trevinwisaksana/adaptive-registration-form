import Foundation

enum StepType: String, Codable {
    case form, camera, signature, document, pin, external
}

enum FieldKind: String, Codable {
    case text, date, select, multiselect, money, bool
}

struct FilterBy: Codable {
    let parent: String
}

struct FormField: Codable, Identifiable, Hashable {
    let key: String
    let kind: FieldKind
    let label: String?
    let required: Bool?
    let requiredWhen: String?
    let visibleWhen: String?
    let optionsRef: String?
    let filterBy: FilterBy?
    let rules: [String]?

    var id: String { key }

    enum CodingKeys: String, CodingKey {
        case key, kind, label, required, rules
        case requiredWhen = "required_when"
        case visibleWhen = "visible_when"
        case optionsRef = "options_ref"
        case filterBy = "filter_by"
    }

    static func == (lhs: FormField, rhs: FormField) -> Bool { lhs.key == rhs.key }
    func hash(into hasher: inout Hasher) { hasher.combine(key) }
}

extension FilterBy: Hashable {}

struct DocPointer: Codable, Equatable {
    let kind: String
    let version: String
    let locale: String
    let sha256: String
    let url: String?
}

/// `next_step` (contract.md §3): one shape, fields present depend on `type`. Modeling every
/// step type in a single struct (rather than an enum with associated values) matches how the
/// wire format actually looks and keeps decoding a single, boring `Codable`.
struct StepDefinition: Codable, Identifiable {
    let id: String
    let type: StepType
    let title: String?

    // form
    let fields: [FormField]?

    // camera
    let capture: String?

    // document
    let doc: DocPointer?

    // external
    let adapter: String?
    let webviewURL: String?
    let minAppVersion: String?

    enum CodingKeys: String, CodingKey {
        case id, type, title, fields, capture, doc, adapter
        case webviewURL = "webview_url"
        case minAppVersion = "min_app_version"
    }

    /// Whether the comparison native form renderer (Settings toggle) can render this step at
    /// all: text/select/bool/date only, per the task scope. `money`/`multiselect` form steps
    /// always use the standard web renderer.
    var isNativeFormRenderable: Bool {
        guard type == .form, let fields else { return false }
        let supported: Set<FieldKind> = [.text, .select, .bool, .date]
        return fields.allSatisfy { supported.contains($0.kind) }
    }
}
