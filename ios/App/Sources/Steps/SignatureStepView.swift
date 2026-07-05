import SwiftUI

/// Native signature pad (`type: "signature"`, contract.md §3.2) — native on both platforms per
/// plan.md §1 ("native signature pad feels better"). `Canvas` + a drag gesture collects points,
/// exported to a PNG and uploaded through the same presigned-upload flow as camera captures.
struct SignatureStepView: View {
    let step: StepDefinition
    @ObservedObject var session: SessionViewModel

    @State private var strokes: [[CGPoint]] = []
    @State private var currentStroke: [CGPoint] = []
    @State private var isUploading = false
    @State private var errorMessage: String?

    /// Fixed size for both the on-screen pad and the exported PNG, so stroke coordinates need
    /// no rescaling between drawing and export.
    private static let canvasSize = CGSize(width: 340, height: 220)

    private var isEmpty: Bool { strokes.isEmpty && currentStroke.isEmpty }

    var body: some View {
        VStack(spacing: 20) {
            Text(step.title ?? "Sign to continue")
                .font(.title2.bold())

            Text("Draw your signature below using your finger.")
                .font(.footnote)
                .foregroundStyle(.secondary)

            Canvas { context, size in
                var path = Path()
                for stroke in strokes + [currentStroke] {
                    guard let first = stroke.first else { continue }
                    path.move(to: first)
                    for point in stroke.dropFirst() {
                        path.addLine(to: point)
                    }
                }
                context.stroke(path, with: .color(.primary), style: StrokeStyle(lineWidth: 3, lineCap: .round, lineJoin: .round))
            }
            .frame(width: Self.canvasSize.width, height: Self.canvasSize.height)
            .background(Color.secondary.opacity(0.08))
            .clipShape(RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12).stroke(Color.secondary.opacity(0.3)))
            .gesture(
                DragGesture(minimumDistance: 0)
                    .onChanged { value in
                        currentStroke.append(value.location)
                    }
                    .onEnded { _ in
                        strokes.append(currentStroke)
                        currentStroke = []
                    }
            )

            HStack(spacing: 12) {
                Button("Clear") {
                    strokes = []
                    currentStroke = []
                }
                .buttonStyle(.bordered)
                .disabled(isEmpty)

                Button {
                    Task { await exportAndSubmit() }
                } label: {
                    if isUploading {
                        ProgressView()
                    } else {
                        Text("Submit Signature")
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(isEmpty || isUploading)
            }

            if let errorMessage {
                Text(errorMessage).font(.footnote).foregroundStyle(.red)
            }
        }
        .padding()
    }

    private func exportAndSubmit() async {
        isUploading = true
        errorMessage = nil
        defer { isUploading = false }

        let renderer = ImageRenderer(content: signatureImage(size: Self.canvasSize))
        renderer.scale = 2

        guard let uiImage = renderer.uiImage, let pngData = uiImage.pngData() else {
            errorMessage = "Couldn't export the signature image."
            return
        }

        do {
            let uploadRef = try await session.uploadCapture(kind: "signature", data: pngData, contentType: "image/png")
            let body = SignatureSubmitBody(uploadRef: uploadRef)
            await session.submit(step: step, body: body)
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    /// Re-renders the strokes into a fixed-size white-background canvas for export, independent
    /// of whatever on-screen frame the drawing canvas happened to have.
    private func signatureImage(size: CGSize) -> some View {
        let allStrokes = strokes
        return Canvas { context, canvasSize in
            context.fill(Path(CGRect(origin: .zero, size: canvasSize)), with: .color(.white))
            var path = Path()
            for stroke in allStrokes {
                guard let first = stroke.first else { continue }
                path.move(to: first)
                for point in stroke.dropFirst() {
                    path.addLine(to: point)
                }
            }
            context.stroke(path, with: .color(.black), style: StrokeStyle(lineWidth: 4, lineCap: .round, lineJoin: .round))
        }
        .frame(width: size.width, height: size.height)
    }
}
