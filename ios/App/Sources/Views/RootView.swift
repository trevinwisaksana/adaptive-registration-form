import SwiftUI

/// App root: bootstraps the session, then renders — in priority order — force-update,
/// maintenance, completion, or the current step via the registry. Progress bar, banners, and
/// repairs are shown around whatever the registry renders, never inside it (plan.md §3.1 /
/// contract.md §4).
struct RootView: View {
    @EnvironmentObject var session: SessionViewModel
    @State private var showSettings = false

    var body: some View {
        NavigationStack {
            Group {
                if session.envelope == nil && session.isLoading {
                    ProgressView("Starting session…")
                } else if session.isMaintenance {
                    MaintenanceView(retryAfter: session.envelope?.system.retryAfter) {
                        await session.refresh()
                    }
                } else if session.forceUpdateMinVersion != nil {
                    ForceUpdateView(minVersion: session.forceUpdateMinVersion ?? "unknown")
                } else if let envelope = session.envelope {
                    if let step = envelope.nextStep {
                        stepScreen(envelope: envelope, step: step)
                    } else {
                        CompletionView()
                    }
                } else if let errorMessage = session.errorMessage {
                    errorScreen(message: errorMessage)
                } else {
                    ProgressView()
                }
            }
            .navigationTitle("Onboarding")
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        showSettings = true
                    } label: {
                        Image(systemName: "gearshape")
                    }
                }
            }
            .sheet(isPresented: $showSettings) {
                SettingsView()
            }
        }
        .task {
            if session.envelope == nil {
                await session.bootstrap()
            }
        }
    }

    @ViewBuilder
    private func stepScreen(envelope: Envelope, step: StepDefinition) -> some View {
        VStack(spacing: 16) {
            ProgressBarView(progress: envelope.progress)
                .padding(.horizontal)

            if !envelope.repairs.isEmpty || !envelope.system.banners.isEmpty {
                VStack(spacing: 12) {
                    RepairsListView(repairs: envelope.repairs)
                    BannerListView(banners: envelope.system.banners, currentStepId: step.id)
                }
                .padding(.horizontal)
            }

            StepRendererView(step: step, session: session)
        }
    }

    private func errorScreen(message: String) -> some View {
        VStack(spacing: 16) {
            Image(systemName: "exclamationmark.triangle")
                .font(.system(size: 40))
                .foregroundStyle(.orange)
            Text(message)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 32)
            Button("Retry") {
                Task { await session.bootstrap() }
            }
            .buttonStyle(.borderedProminent)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// `next_step.id == "force_update"` (contract.md §2.1) — a capability gate, not an outage.
/// `system.status` stays `"ok"`; the client just can't render this flow version.
struct ForceUpdateView: View {
    let minVersion: String

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "arrow.up.circle.fill")
                .font(.system(size: 48))
                .foregroundStyle(.blue)
            Text("Update required")
                .font(.title2.bold())
            Text("This registration flow needs app version \(minVersion) or later. Please update from the App Store.")
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 32)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
