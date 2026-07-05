import SwiftUI

/// Server-computed progress (contract.md §1) — the client never hardcodes `total`, so a
/// branch (e.g. FATCA) or a flow-version change just makes the bar longer or shorter.
struct ProgressBarView: View {
    let progress: Progress

    private var fraction: Double {
        guard progress.total > 0 else { return 0 }
        return min(1, Double(progress.completed) / Double(progress.total))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.secondary.opacity(0.2))
                    Capsule()
                        .fill(Color.accentColor)
                        .frame(width: geo.size.width * fraction)
                        .animation(.easeInOut(duration: 0.25), value: fraction)
                }
            }
            .frame(height: 6)

            Text("Step \(progress.completed) of \(progress.total)")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}
