import SwiftUI

/// POC settings — not part of the flow itself. Lets you point at a different backend host and
/// flip the native-vs-webview form renderer comparison (task scope) without an app release,
/// which is a nice, small demonstration of the same "server/data, not code" philosophy the rest
/// of the app is built around, even though this particular toggle is local-only.
struct SettingsView: View {
    @EnvironmentObject var session: SessionViewModel
    @EnvironmentObject var settings: AppSettings
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                Section("Backend") {
                    TextField("Base URL", text: $settings.baseURLString)
                        .keyboardType(.URL)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                    Text("e.g. http://localhost:8080 — also where the WKWebView renderer is served from (/web/…).")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Section("Renderer comparison") {
                    Toggle("Use native form renderer", isOn: $settings.useNativeFormRenderer)
                    Text("When on, `form` steps limited to text/select/bool/date render natively instead of via WKWebView. `document` steps and any form using money/multiselect always use the webview renderer.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Section("Session") {
                    Button("Start a new session", role: .destructive) {
                        Task { await session.resetAndStartNew() }
                        dismiss()
                    }
                }
            }
            .navigationTitle("Settings")
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") {
                        session.rebuildClient()
                        dismiss()
                    }
                }
            }
        }
    }
}
