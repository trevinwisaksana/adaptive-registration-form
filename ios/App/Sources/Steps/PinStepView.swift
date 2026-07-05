import SwiftUI

/// Native secure PIN pad (`type: "pin"`, contract.md §3.2) — native on both platforms per
/// plan.md §1 ("PIN pad must be native — Keychain / Android Keystore"), and last page of the
/// flow. Two entry passes (enter, then confirm) with a custom 0-9 keypad; never a system text
/// field, so there's no keyboard/autofill surface and nothing to leave in an undo buffer.
///
/// Per plan.md §5, the PIN is a *credential*, not a form answer: it is submitted through the
/// same `POST /sessions/{id}/steps/{stepId}` endpoint as everything else (contract.md §3.2), but
/// the **server** routes it straight to the auth service and never writes it to
/// `step_submissions`. Nothing on this screen persists the digits anywhere — they live only in
/// `@State` for the duration of entry and are discarded the moment submit succeeds or fails.
///
/// TODO(security): a real build stores a local biometric-unlock copy in the Keychain /
/// Secure Enclave (`kSecAttrAccessibleWhenUnlockedThisDeviceOnly` + biometry protection) —
/// out of scope for this POC, which only demonstrates the entry UI and the submit call.
struct PinStepView: View {
    let step: StepDefinition
    @ObservedObject var session: SessionViewModel

    private enum Phase {
        case enter, confirm
    }

    @State private var phase: Phase = .enter
    @State private var firstEntry = ""
    @State private var digits = ""
    @State private var isSubmitting = false
    @State private var errorMessage: String?

    private let pinLength = 6

    var body: some View {
        VStack(spacing: 28) {
            Text(step.title ?? "Set up your PIN")
                .font(.title2.bold())

            Text(promptText)
                .font(.subheadline)
                .foregroundStyle(.secondary)

            HStack(spacing: 14) {
                ForEach(0..<pinLength, id: \.self) { index in
                    Circle()
                        .fill(index < digits.count ? Color.primary : Color.secondary.opacity(0.25))
                        .frame(width: 16, height: 16)
                }
            }
            .padding(.vertical, 8)

            if let errorMessage {
                Text(errorMessage).font(.footnote).foregroundStyle(.red)
            }

            Spacer()

            NumericKeypad(
                onDigit: { digit in appendDigit(digit) },
                onDelete: { if !digits.isEmpty { digits.removeLast() } },
                disabled: isSubmitting
            )
        }
        .padding()
    }

    private var promptText: String {
        switch phase {
        case .enter: return "Enter a 6-digit PIN"
        case .confirm: return "Re-enter your PIN to confirm"
        }
    }

    private func appendDigit(_ digit: String) {
        guard digits.count < pinLength, !isSubmitting else { return }
        errorMessage = nil
        digits.append(digit)
        guard digits.count == pinLength else { return }

        switch phase {
        case .enter:
            firstEntry = digits
            digits = ""
            phase = .confirm
        case .confirm:
            if digits == firstEntry {
                Task { await submit() }
            } else {
                // Restart the two-pass flow from scratch on mismatch.
                errorMessage = "PINs didn't match — try again."
                firstEntry = ""
                digits = ""
                phase = .enter
            }
        }
    }

    private func submit() async {
        isSubmitting = true
        errorMessage = nil
        let pin = firstEntry
        // Clear local copies before the network call returns — the value only needs to exist
        // long enough to be handed to the transport layer.
        firstEntry = ""
        digits = ""
        defer { isSubmitting = false }
        let body = PinSubmitBody(pin: pin)
        await session.submit(step: step, body: body)
        if session.errorMessage != nil {
            errorMessage = "Couldn't set your PIN. Please try again."
            phase = .enter
        }
    }
}

private struct NumericKeypad: View {
    let onDigit: (String) -> Void
    let onDelete: () -> Void
    let disabled: Bool

    private let rows: [[String]] = [
        ["1", "2", "3"],
        ["4", "5", "6"],
        ["7", "8", "9"],
        ["", "0", "⌫"],
    ]

    var body: some View {
        VStack(spacing: 16) {
            ForEach(rows, id: \.self) { row in
                HStack(spacing: 16) {
                    ForEach(row, id: \.self) { key in
                        keyButton(key)
                    }
                }
            }
        }
        .disabled(disabled)
    }

    @ViewBuilder
    private func keyButton(_ key: String) -> some View {
        if key.isEmpty {
            Color.clear.frame(width: 72, height: 60)
        } else if key == "⌫" {
            Button(action: onDelete) {
                Image(systemName: "delete.left")
                    .font(.title3)
                    .frame(width: 72, height: 60)
            }
        } else {
            Button {
                onDigit(key)
            } label: {
                Text(key)
                    .font(.title2)
                    .frame(width: 72, height: 60)
            }
            .background(Color.secondary.opacity(0.08), in: RoundedRectangle(cornerRadius: 12))
        }
    }
}
