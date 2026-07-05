// steps/camera.js — `type: "camera"` (contract.md §3.2). Camera capture is
// native-only in production (plan.md §1: AVFoundation/CameraX, on-device
// liveness) — this is a simple file-picker stand-in so the flow can still be
// walked end-to-end from a plain browser during the POC demo.

import { t } from "../i18n.js";
import { requestUploadSlot, putUpload } from "../api.js";
import { repairNoticeFor } from "../repairs.js";

export function renderCameraStep(step, container, { sessionId, locale, onSubmit, repairs }) {
  container.innerHTML = "";
  const wrap = document.createElement("div");
  wrap.className = "step-camera";
  if (step.title) {
    wrap.append(Object.assign(document.createElement("h1"), { className: "step-title", textContent: step.title }));
  }

  const repairNotice = repairNoticeFor(repairs, step.id, locale);
  if (repairNotice) wrap.append(repairNotice);

  const note = document.createElement("p");
  note.className = "step-note";
  note.textContent = "(Web demo stand-in — the shipped app captures this natively.)";
  wrap.append(note);

  const preview = document.createElement("img");
  preview.className = "camera-preview";
  preview.hidden = true;
  wrap.append(preview);

  const input = document.createElement("input");
  input.type = "file";
  input.accept = "image/*";
  input.capture = "environment";
  wrap.append(input);

  const status = document.createElement("div");
  status.className = "field-hint";
  wrap.append(status);

  const submitBtn = document.createElement("button");
  submitBtn.type = "button";
  submitBtn.className = "btn btn-primary";
  submitBtn.textContent = t("next", locale);
  submitBtn.disabled = true;
  wrap.append(submitBtn);

  let selectedFile = null;

  input.addEventListener("change", () => {
    selectedFile = input.files?.[0] ?? null;
    if (selectedFile) {
      preview.src = URL.createObjectURL(selectedFile);
      preview.hidden = false;
      submitBtn.disabled = false;
      // Stand-in for the on-device face/blur checks the plan describes
      // (plan.md §2.1 "cheap checks early") — a real client runs Vision
      // framework checks here before ever contacting the server.
      status.textContent = "";
    } else {
      submitBtn.disabled = true;
    }
  });

  submitBtn.addEventListener("click", async () => {
    if (!selectedFile) return;
    submitBtn.disabled = true;
    status.textContent = t("upload_uploading", locale);
    try {
      const slot = await requestUploadSlot(sessionId, {
        kind: step.capture,
        contentType: selectedFile.type || "image/jpeg",
        sizeBytes: selectedFile.size,
      });
      await putUpload(slot.url, slot.headers, selectedFile);
      await onSubmit({
        upload_ref: slot.upload_ref,
        client_checks: { face_present: true, blurry: false },
      });
    } finally {
      submitBtn.disabled = false;
      status.textContent = "";
    }
  });

  container.append(wrap);
}
