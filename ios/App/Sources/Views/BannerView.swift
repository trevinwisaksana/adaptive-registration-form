import SwiftUI

/// Renders `system.banners` scoped to either "global" or the current step id
/// (contract.md §1 / plan.md §3.1). The component is built once; every "when/what" decision
/// is server data (`scope`, `severity`, `text`).
struct BannerListView: View {
    let banners: [Banner]
    let currentStepId: String?

    private var visible: [Banner] {
        banners.filter { $0.scope == "global" || $0.scope == currentStepId }
    }

    var body: some View {
        if !visible.isEmpty {
            VStack(spacing: 8) {
                ForEach(visible) { banner in
                    BannerRow(banner: banner)
                }
            }
        }
    }
}

private struct BannerRow: View {
    let banner: Banner

    private var color: Color {
        switch banner.severity {
        case "warning": return .orange
        case "error": return .red
        default: return .blue
        }
    }

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "info.circle.fill")
                .foregroundStyle(color)
            Text(banner.text)
                .font(.footnote)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
        }
        .padding(10)
        .background(color.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
    }
}

/// Lists resume/reconciliation repairs (contract.md §4) so the user understands why they're
/// being asked to redo or add something. `next_step` already points at the first unresolved
/// repair — this is purely explanatory context above it.
struct RepairsListView: View {
    let repairs: [Repair]

    var body: some View {
        if !repairs.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Label("Welcome back — a few things changed", systemImage: "arrow.triangle.2.circlepath")
                    .font(.subheadline.bold())
                ForEach(repairs) { repair in
                    Text("• \(repair.displayText)")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            .padding(10)
            .background(Color.secondary.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
        }
    }
}
