import SwiftUI
import WebKit

/// The hybrid escape hatch (plan.md §1): `form` and `document` steps — and third-party
/// `external` steps — render inside a `WKWebView` instead of native SwiftUI. Two flavors:
///
/// - `.hybridStep`: loads *our own* web renderer (`{baseURL}/web/#/step/{id}?session=…`), which
///   talks to the same session API directly using the token the shell injects. When it finishes
///   the step, it posts `{"type":"stepCompleted"}` back to the shell, which just re-fetches
///   `GET /sessions/{id}` to pick up whatever `next_step` the server decided.
/// - `.external`: loads a third-party `webview_url` (contract.md §3.4). The shell doesn't trust
///   this content with the session token at all — it only listens for a `stepCompleted` message
///   carrying a `result` payload, then submits that itself via the normal `external` step body.
///
/// Security notes (plan.md §1 "web security tax"): strict CSP + zero third-party scripts on our
/// own registration pages is a *server-side* discipline (the web renderer's responsibility, out
/// of scope here) — what the shell owns is (a) never handing the session token to third-party
/// content, and (b) injecting it as a short-lived value via `WKUserScript`, never through
/// `localStorage`/cookies the page could persist.
enum WebStepMode: Equatable {
    case hybridStep
    case external
}

struct WebStepView: View {
    let url: URL
    let sessionToken: String?
    let mode: WebStepMode
    let onHybridStepCompleted: () -> Void
    let onExternalResult: ([String: AnyCodable]) -> Void

    @State private var isLoading = true
    @State private var loadFailed = false
    @State private var reloadToken = UUID()

    var body: some View {
        ZStack {
            WebViewRepresentable(
                url: url, sessionToken: sessionToken, mode: mode,
                reloadToken: reloadToken,
                onLoadStart: { isLoading = true; loadFailed = false },
                onLoadFinish: { isLoading = false },
                onLoadFail: { isLoading = false; loadFailed = true },
                onHybridStepCompleted: onHybridStepCompleted,
                onExternalResult: onExternalResult
            )

            if isLoading {
                ProgressView("Loading…")
            }

            if loadFailed {
                VStack(spacing: 12) {
                    Image(systemName: "wifi.exclamationmark")
                        .font(.system(size: 36))
                        .foregroundStyle(.secondary)
                    Text("Couldn't load the web renderer at\n\(url.absoluteString)")
                        .font(.footnote)
                        .multilineTextAlignment(.center)
                        .foregroundStyle(.secondary)
                    Button("Retry") { reloadToken = UUID() }
                        .buttonStyle(.bordered)

                    // POC-only convenience: if the web renderer isn't running locally, this
                    // still lets you exercise the rest of the flow. A real build has no such
                    // bypass — completion only ever comes from the JS bridge message.
                    if mode == .hybridStep {
                        Button("Skip (dev only — no local web server)") {
                            onHybridStepCompleted()
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                }
                .padding()
            }
        }
    }
}

private struct WebViewRepresentable: UIViewRepresentable {
    let url: URL
    let sessionToken: String?
    let mode: WebStepMode
    let reloadToken: UUID
    let onLoadStart: () -> Void
    let onLoadFinish: () -> Void
    let onLoadFail: () -> Void
    let onHybridStepCompleted: () -> Void
    let onExternalResult: ([String: AnyCodable]) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIView(context: Context) -> WKWebView {
        let config = WebViewConfigurationFactory.make()

        // Native -> web token handoff (plan.md §1): injected as a JS global by a user script at
        // document start, never written to localStorage/cookies the page could persist beyond
        // the session. Only relevant for our own hybrid pages — external vendor content never
        // gets this script at all (see `updateUIView`).
        if mode == .hybridStep, let sessionToken,
           let tokenJSON = try? JSONEncoder().encode(sessionToken),
           let tokenLiteral = String(data: tokenJSON, encoding: .utf8) {
            let js = "window.__SESSION_TOKEN__ = \(tokenLiteral);"
            let script = WKUserScript(source: js, injectionTime: .atDocumentStart, forMainFrameOnly: true)
            config.userContentController.addUserScript(script)
        }
        config.userContentController.add(context.coordinator, name: "bridge")

        let webView = WKWebView(frame: .zero, configuration: config)
        webView.navigationDelegate = context.coordinator
        webView.load(URLRequest(url: url))
        return webView
    }

    func updateUIView(_ webView: WKWebView, context: Context) {
        if context.coordinator.loadedReloadToken != reloadToken {
            context.coordinator.loadedReloadToken = reloadToken
            webView.load(URLRequest(url: url))
        }
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKScriptMessageHandler {
        let parent: WebViewRepresentable
        var loadedReloadToken: UUID

        init(_ parent: WebViewRepresentable) {
            self.parent = parent
            self.loadedReloadToken = parent.reloadToken
        }

        func webView(_ webView: WKWebView, didStartProvisionalNavigation navigation: WKNavigation!) {
            parent.onLoadStart()
        }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            parent.onLoadFinish()
        }

        func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
            parent.onLoadFail()
        }

        func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
            parent.onLoadFail()
        }

        /// JS bridge: the page calls
        /// `window.webkit.messageHandlers.bridge.postMessage({type: "stepCompleted", result: {...}})`.
        /// For hybrid form/document pages `result` is ignored (the page already submitted to the
        /// API itself); for external steps it's exactly the `result` object the contract expects
        /// in the `external` submit body.
        func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
            guard let dict = message.body as? [String: Any], dict["type"] as? String == "stepCompleted" else { return }
            switch parent.mode {
            case .hybridStep:
                parent.onHybridStepCompleted()
            case .external:
                let result = (dict["result"] as? [String: Any]) ?? [:]
                parent.onExternalResult(result.mapValues(AnyCodable.init))
            }
        }
    }
}

/// Small shim so the CSP/config choice is in one obvious place. POC: default config, no
/// exotic process-pool sharing needed for a single-webview-at-a-time renderer.
private enum WebViewConfigurationFactory {
    static func make() -> WKWebViewConfiguration {
        let config = WKWebViewConfiguration()
        config.defaultWebpagePreferences.allowsContentJavaScript = true
        return config
    }
}
