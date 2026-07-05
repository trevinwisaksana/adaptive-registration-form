// steps/signature.js — `type: "signature"` (contract.md §3.2). Native
// signature pads feel better in production (plan.md §1); this canvas-based
// pad is a working stand-in so the web demo can walk the full flow.

import { t } from "../i18n.js";
import { requestUploadSlot, putUpload } from "../api.js";

export function renderSignatureStep(step, container, { sessionId, locale, onSubmit }) {
  container.innerHTML = "";
  const wrap = document.createElement("div");
  wrap.className = "step-signature";
  if (step.title) {
    wrap.append(Object.assign(document.createElement("h1"), { className: "step-title", textContent: step.title }));
  }

  const hint = document.createElement("p");
  hint.className = "step-note";
  hint.textContent = t("signature_pad_hint", locale);
  wrap.append(hint);

  const canvas = document.createElement("canvas");
  canvas.className = "signature-canvas";
  canvas.width = 600;
  canvas.height = 240;
  wrap.append(canvas);

  const ctx = canvas.getContext("2d");
  ctx.lineWidth = 2.5;
  ctx.lineCap = "round";
  ctx.strokeStyle = "#1b1b1f";

  let drawing = false;
  let hasStroke = false;

  function pos(e) {
    const rect = canvas.getBoundingClientRect();
    const point = e.touches ? e.touches[0] : e;
    return {
      x: ((point.clientX - rect.left) / rect.width) * canvas.width,
      y: ((point.clientY - rect.top) / rect.height) * canvas.height,
    };
  }

  function start(e) {
    e.preventDefault();
    drawing = true;
    const { x, y } = pos(e);
    ctx.beginPath();
    ctx.moveTo(x, y);
  }
  function move(e) {
    if (!drawing) return;
    e.preventDefault();
    const { x, y } = pos(e);
    ctx.lineTo(x, y);
    ctx.stroke();
    hasStroke = true;
    submitBtn.disabled = !hasStroke;
  }
  function end() { drawing = false; }

  canvas.addEventListener("mousedown", start);
  canvas.addEventListener("mousemove", move);
  window.addEventListener("mouseup", end);
  canvas.addEventListener("touchstart", start, { passive: false });
  canvas.addEventListener("touchmove", move, { passive: false });
  canvas.addEventListener("touchend", end);

  const controls = document.createElement("div");
  controls.className = "signature-controls";
  const clearBtn = document.createElement("button");
  clearBtn.type = "button";
  clearBtn.className = "btn btn-secondary";
  clearBtn.textContent = t("signature_clear", locale);
  clearBtn.addEventListener("click", () => {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    hasStroke = false;
    submitBtn.disabled = true;
  });
  controls.append(clearBtn);
  wrap.append(controls);

  const submitBtn = document.createElement("button");
  submitBtn.type = "button";
  submitBtn.className = "btn btn-primary";
  submitBtn.textContent = t("next", locale);
  submitBtn.disabled = true;
  wrap.append(submitBtn);

  submitBtn.addEventListener("click", async () => {
    submitBtn.disabled = true;
    try {
      const blob = await new Promise((resolve) => canvas.toBlob(resolve, "image/png"));
      const slot = await requestUploadSlot(sessionId, {
        kind: "signature",
        contentType: "image/png",
        sizeBytes: blob.size,
      });
      await putUpload(slot.url, slot.headers, blob);
      await onSubmit({ upload_ref: slot.upload_ref });
    } finally {
      submitBtn.disabled = !hasStroke;
    }
  });

  container.append(wrap);
}
