import Foundation

/// Tiny evaluator for the same-page `visible_when` / `required_when` expression strings
/// (contract.md §3.1). Only same-page conditions ever reach the client — cross-page
/// (`answers.<step>.<field>`) ones are resolved server-side before the step is served, so this
/// intentionally does not implement that syntax at all.
///
/// Supports exactly the two shapes used in the contract:
///   `key in ['a','b','c']`
///   `key == 'value'`  /  `key == true`  /  `key == false`
///
/// This is UX-only — the server re-verifies at submit (contract.md §3.1), so a gap here degrades
/// the live show/hide, it never lets an invalid answer through.
enum ConditionExpression {
    static func evaluate(_ expression: String, answers: [String: AnyCodable]) -> Bool {
        let trimmed = expression.trimmingCharacters(in: .whitespaces)

        if let range = trimmed.range(of: " in [") {
            let key = String(trimmed[trimmed.startIndex..<range.lowerBound]).trimmingCharacters(in: .whitespaces)
            let listPart = trimmed[range.upperBound...].replacingOccurrences(of: "]", with: "")
            let options = listPart
                .split(separator: ",")
                .map { $0.trimmingCharacters(in: CharacterSet(charactersIn: " '\"")) }
            guard let current = answers[key]?.stringValue else { return false }
            return options.contains(current)
        }

        if let range = trimmed.range(of: "==") {
            let key = String(trimmed[trimmed.startIndex..<range.lowerBound]).trimmingCharacters(in: .whitespaces)
            let rawValue = String(trimmed[range.upperBound...])
                .trimmingCharacters(in: CharacterSet(charactersIn: " '\""))
            if rawValue == "true" { return answers[key]?.boolValue == true }
            if rawValue == "false" { return answers[key]?.boolValue == false }
            return answers[key]?.stringValue == rawValue
        }

        // Unknown shape: fail open (show the field) rather than silently hiding required data —
        // the server is the enforcement point regardless.
        return true
    }
}
