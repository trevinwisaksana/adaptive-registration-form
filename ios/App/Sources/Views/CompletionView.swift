import SwiftUI

/// `next_step == null` (contract.md §1) — the state machine has moved to `on_complete`/terminal.
/// KYC verification (plan.md §2.1) is deliberately not a page; this is a "pending review"
/// status screen, not a final "approved" screen — the mock KYC webhook flips status later.
struct CompletionView: View {
    @EnvironmentObject var session: SessionViewModel

    var body: some View {
        VStack(spacing: 20) {
            Image(systemName: "checkmark.seal.fill")
                .font(.system(size: 56))
                .foregroundStyle(.green)
            Text("You're all set")
                .font(.title.bold())
            Text("We're verifying your details now. This usually takes a few minutes — you'll be notified when it's done.")
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 32)

            Button("Check status") {
                Task { await session.refresh() }
            }
            .buttonStyle(.bordered)

            Button("Start a new session (debug)") {
                Task { await session.resetAndStartNew() }
            }
            .font(.footnote)
            .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
