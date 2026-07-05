// steps/pin.js — `type: "pin"` (contract.md §3.2). The PIN pad must be
// native in production (Keychain / Android Keystore, plan.md §5) — this is a
// plain stand-in for walking the demo flow in a browser. Note per the
// contract this never lands in `step_submissions`; it's just relayed to
// whatever `/steps/{stepId}` does server-side with it.

import { t } from "../i18n.js";

export function renderPinStep(step, container, { locale, onSubmit }) {
  container.innerHTML = "";
  const wrap = document.createElement("div");
  wrap.className = "step-pin";
  if (step.title) {
    wrap.append(Object.assign(document.createElement("h1"), { className: "step-title", textContent: step.title }));
  }

  const note = document.createElement("p");
  note.className = "step-note";
  note.textContent = "(Web demo stand-in — the shipped app uses a native secure PIN pad.)";
  wrap.append(note);

  function pinInput(labelText) {
    const label = document.createElement("label");
    label.className = "field-label";
    label.textContent = labelText;
    const input = document.createElement("input");
    input.type = "password";
    input.inputMode = "numeric";
    input.pattern = "[0-9]{6}";
    input.maxLength = 6;
    input.className = "field-input pin-input";
    return { label, input };
  }

  const pin = pinInput(t("pin_label", locale));
  const confirm = pinInput(t("pin_confirm_label", locale));
  const error = document.createElement("div");
  error.className = "field-error";
  error.hidden = true;

  wrap.append(pin.label, pin.input, confirm.label, confirm.input, error);

  const submitBtn = document.createElement("button");
  submitBtn.type = "button";
  submitBtn.className = "btn btn-primary";
  submitBtn.textContent = t("submit", locale);
  wrap.append(submitBtn);

  submitBtn.addEventListener("click", async () => {
    error.hidden = true;
    const a = pin.input.value.trim();
    const b = confirm.input.value.trim();
    if (!/^\d{6}$/.test(a)) {
      error.textContent = "Enter a 6-digit PIN.";
      error.hidden = false;
      return;
    }
    if (a !== b) {
      error.textContent = t("pin_mismatch", locale);
      error.hidden = false;
      return;
    }
    submitBtn.disabled = true;
    try {
      await onSubmit({ pin: a });
    } finally {
      submitBtn.disabled = false;
    }
  });

  container.append(wrap);
}
