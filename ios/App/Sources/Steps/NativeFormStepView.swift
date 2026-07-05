import SwiftUI

/// Minimal **native** form renderer — text/select/bool/date only — built purely as a
/// side-by-side comparison against the webview renderer (task scope), toggled in Settings.
/// The hybrid decision (plan.md §1) routes real `form` steps to `WebStepView`; this exists so
/// you can flip a switch and see the same step definition rendered natively instead.
///
/// Does NOT implement: `money`/`multiselect` kinds, `options_ref` cascading via `filter_by`
/// beyond a same-page parent read, or on-device drafts (plan.md §2.1) — those are real work for
/// the production native renderer and out of scope for a comparison stub.
struct NativeFormStepView: View {
    let step: StepDefinition
    @ObservedObject var session: SessionViewModel

    @State private var textValues: [String: String] = [:]
    @State private var boolValues: [String: Bool] = [:]
    @State private var dateValues: [String: Date] = [:]
    @State private var selectValues: [String: String] = [:]
    @State private var optionsByField: [String: [RefDataItem]] = [:]
    @State private var isSubmitting = false
    @State private var generalError: String?

    private var fieldErrors: [String: String] {
        Dictionary(uniqueKeysWithValues: session.lastValidationErrors.map { ($0.key, $0.message) })
    }

    private var fields: [FormField] { step.fields ?? [] }

    /// Live same-page answers, for `visible_when`/`required_when` evaluation (contract.md §3.1).
    private var currentAnswers: [String: AnyCodable] {
        var answers: [String: AnyCodable] = [:]
        for (k, v) in textValues { answers[k] = AnyCodable(v) }
        for (k, v) in boolValues { answers[k] = AnyCodable(v) }
        for (k, v) in selectValues { answers[k] = AnyCodable(v) }
        return answers
    }

    var body: some View {
        Form {
            if let title = step.title {
                Section {
                    Text(title).font(.title3.bold())
                }
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            }

            ForEach(visibleFields) { field in
                Section {
                    fieldEditor(field)
                    if let error = fieldErrors[field.key] {
                        Text(error).font(.caption).foregroundStyle(.red)
                    }
                } header: {
                    Text(field.label ?? field.key)
                }
            }

            if let generalError {
                Text(generalError).foregroundStyle(.red)
            }

            Button {
                Task { await submit() }
            } label: {
                if isSubmitting {
                    ProgressView()
                } else {
                    Text("Continue")
                }
            }
            .disabled(isSubmitting)
        }
        .task { await loadOptions() }
    }

    private var visibleFields: [FormField] {
        fields.filter { field in
            guard let expr = field.visibleWhen else { return true }
            return ConditionExpression.evaluate(expr, answers: currentAnswers)
        }
    }

    @ViewBuilder
    private func fieldEditor(_ field: FormField) -> some View {
        switch field.kind {
        case .text:
            TextField(field.label ?? field.key, text: binding(for: field.key))
        case .bool:
            Toggle(field.label ?? field.key, isOn: boolBinding(for: field.key))
        case .date:
            DatePicker(field.label ?? field.key, selection: dateBinding(for: field.key), displayedComponents: .date)
        case .select:
            Picker(field.label ?? field.key, selection: selectBinding(for: field.key)) {
                Text("Select…").tag("")
                ForEach(optionsByField[field.key] ?? []) { option in
                    Text(option.label).tag(option.code)
                }
            }
        case .money, .multiselect:
            // Not supported by this comparison renderer — falls through to a disabled note;
            // the webview renderer (the real path for `form` steps) handles these.
            Text("\(field.label ?? field.key): unsupported in native comparison renderer")
                .foregroundStyle(.secondary)
                .font(.footnote)
        }
    }

    private func binding(for key: String) -> Binding<String> {
        Binding(get: { textValues[key] ?? "" }, set: { textValues[key] = $0 })
    }

    private func boolBinding(for key: String) -> Binding<Bool> {
        Binding(get: { boolValues[key] ?? false }, set: { boolValues[key] = $0 })
    }

    private func dateBinding(for key: String) -> Binding<Date> {
        Binding(get: { dateValues[key] ?? Date() }, set: { dateValues[key] = $0 })
    }

    private func selectBinding(for key: String) -> Binding<String> {
        Binding(get: { selectValues[key] ?? "" }, set: { selectValues[key] = $0 })
    }

    private func loadOptions() async {
        for field in fields where field.kind == .select || field.kind == .multiselect {
            guard let dataset = field.optionsRef else { continue }
            let parentValue = field.filterBy.flatMap { selectValues[$0.parent] ?? textValues[$0.parent] }
            do {
                let response = try await session.refData(dataset: dataset, parent: parentValue, query: nil)
                optionsByField[field.key] = response.items
            } catch {
                // Reference data is best-effort in this comparison renderer; leave the picker
                // empty rather than blocking the whole page on a refdata hiccup.
            }
        }
    }

    private func submit() async {
        isSubmitting = true
        generalError = nil
        defer { isSubmitting = false }

        var answers: [String: AnyCodable] = [:]
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withFullDate]
        for field in visibleFields {
            switch field.kind {
            case .text:
                answers[field.key] = AnyCodable(textValues[field.key] ?? "")
            case .bool:
                answers[field.key] = AnyCodable(boolValues[field.key] ?? false)
            case .date:
                let date = dateValues[field.key] ?? Date()
                answers[field.key] = AnyCodable(formatter.string(from: date))
            case .select:
                answers[field.key] = AnyCodable(selectValues[field.key] ?? "")
            case .money, .multiselect:
                continue
            }
        }

        await session.submit(step: step, body: FormSubmitBody(answers: answers))

        if let message = session.errorMessage {
            generalError = message
        }
    }
}
