import SwiftUI

/// Full-screen native view for `system.status == "maintenance"` — or a plain gateway
/// `503 + Retry-After` with no envelope at all (plan.md §3.1). Session state is entirely
/// server-side, so there's nothing to preserve here beyond "try again shortly".
struct MaintenanceView: View {
    let retryAfter: Int?
    let onRetry: () async -> Void

    @State private var isRetrying = false

    var body: some View {
        VStack(spacing: 20) {
            Image(systemName: "wrench.and.screwdriver.fill")
                .font(.system(size: 48))
                .foregroundStyle(.secondary)
            Text("Under maintenance")
                .font(.title2.bold())
            Text(retryAfter.map { "We'll be back in about \($0 / 60 < 1 ? "a minute" : "\($0 / 60) minutes"). Your progress is saved." }
                 ?? "We're doing a bit of upkeep. Your progress is saved — check back shortly.")
                .font(.body)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 32)

            Button {
                Task {
                    isRetrying = true
                    await onRetry()
                    isRetrying = false
                }
            } label: {
                if isRetrying {
                    ProgressView()
                } else {
                    Text("Try again")
                }
            }
            .buttonStyle(.borderedProminent)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground))
    }
}
