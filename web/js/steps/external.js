// steps/external.js — `type: "external"` (contract.md §3.4): the escape
// hatch for third-party vendor steps and unsupported step types. Also
// handles the special `force_update` capability-gate step (contract.md §2.1)
// as a dedicated full-page state rather than a generic webview.

import { t } from "../i18n.js";

export function renderExternalStep(step, container, { locale, onSubmit }) {
  container.innerHTML = "";

  if (step.id === "force_update" || step.adapter === "force_update") {
    const wrap = document.createElement("div");
    wrap.className = "step-force-update";
    wrap.append(
      Object.assign(document.createElement("h1"), { className: "step-title", textContent: t("force_update_title", locale) }),
      Object.assign(document.createElement("p"), { textContent: t("force_update_body", locale, step.min_app_version ?? "") })
    );
    container.append(wrap);
    return;
  }

  const wrap = document.createElement("div");
  wrap.className = "step-external";
  if (step.title) {
    wrap.append(Object.assign(document.createElement("h1"), { className: "step-title", textContent: step.title }));
  }

  const note = document.createElement("p");
  note.className = "step-note";
  note.textContent = "This step happens with one of our partners.";
  wrap.append(note);

  const openBtn = document.createElement("a");
  openBtn.className = "btn btn-secondary";
  openBtn.href = step.webview_url ?? "#";
  openBtn.target = "_blank";
  openBtn.rel = "noopener noreferrer";
  openBtn.textContent = t("external_open", locale);
  wrap.append(openBtn);

  const doneBtn = document.createElement("button");
  doneBtn.type = "button";
  doneBtn.className = "btn btn-primary";
  doneBtn.textContent = t("external_done", locale);
  wrap.append(doneBtn);

  doneBtn.addEventListener("click", async () => {
    doneBtn.disabled = true;
    try {
      await onSubmit({ adapter: step.adapter, result: { status: "completed" } });
    } finally {
      doneBtn.disabled = false;
    }
  });

  container.append(wrap);
}
