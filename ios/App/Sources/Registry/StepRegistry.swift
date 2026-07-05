import SwiftUI

/// The step renderer registry (plan.md §1 diagram: "Step Renderer Registry"). This is the one
/// place that maps a server-declared `type` to a concrete SwiftUI renderer — the thin-shell
/// promise is that this switch never grows a case for a *new step*, only (rarely) for a new
/// step *type*.
///
/// Routing, per the task's hybrid split:
///   - `camera`, `signature`, `pin` → always native.
///   - `document` → always the webview renderer.
///   - `form` → webview renderer, unless Settings' native-form comparison toggle is on AND the
///     step only uses field kinds that comparison renderer supports.
///   - `external` → webview renderer pointed at the vendor's own `webview_url` (the escape
///     hatch for third-party steps, and for any step type this build doesn't recognize).
struct StepRendererView: View {
    let step: StepDefinition
    @ObservedObject var session: SessionViewModel

    var body: some View {
        switch step.type {
        case .camera:
            CameraStepView(step: step, session: session)
        case .signature:
            SignatureStepView(step: step, session: session)
        case .pin:
            PinStepView(step: step, session: session)
        case .form:
            if session.settings.useNativeFormRenderer && step.isNativeFormRenderable {
                NativeFormStepView(step: step, session: session)
            } else {
                WebStepView(
                    url: session.webRendererURL(for: step),
                    sessionToken: session.token,
                    mode: .hybridStep,
                    onHybridStepCompleted: { Task { await session.refresh() } },
                    onExternalResult: { _ in }
                )
            }
        case .document:
            WebStepView(
                url: session.webRendererURL(for: step),
                sessionToken: session.token,
                mode: .hybridStep,
                onHybridStepCompleted: { Task { await session.refresh() } },
                onExternalResult: { _ in }
            )
        case .external:
            let externalURL = step.webviewURL.flatMap(URL.init) ?? session.webRendererURL(for: step)
            WebStepView(
                url: externalURL,
                sessionToken: nil, // never hand the session token to third-party content
                mode: .external,
                onHybridStepCompleted: {},
                onExternalResult: { result in
                    Task {
                        await session.submit(
                            step: step,
                            body: ExternalSubmitBody(adapter: step.adapter ?? "unknown", result: result))
                    }
                }
            )
        }
    }
}
