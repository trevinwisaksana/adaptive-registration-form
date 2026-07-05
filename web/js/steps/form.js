// steps/form.js — renders `type: "form"` steps (contract.md §3.1): field
// kinds, same-page visible_when/required_when, cascading options, per-field
// server validation errors, and on-device drafts (plan.md "Mid-page drop-off").

import { createFieldController } from "../fields.js";
import { evaluateExpr } from "../expr.js";
import { t } from "../i18n.js";
import { ApiError } from "../api.js";
import { repairNoticeFor } from "../repairs.js";

function draftKey(sessionId, stepId) {
  return `arf.draft.${sessionId}.${stepId}`;
}

function loadDraft(sessionId, stepId) {
  try {
    const raw = window.localStorage.getItem(draftKey(sessionId, stepId));
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

function saveDraft(sessionId, stepId, answers, fields) {
  const toSave = {};
  for (const f of fields) {
    if (f.no_draft) continue; // plan.md: per-field opt-out of local drafts
    if (Object.prototype.hasOwnProperty.call(answers, f.key)) toSave[f.key] = answers[f.key];
  }
  window.localStorage.setItem(draftKey(sessionId, stepId), JSON.stringify(toSave));
}

export function clearDraft(sessionId, stepId) {
  window.localStorage.removeItem(draftKey(sessionId, stepId));
}

// `onSubmit(body)` -> Promise; rejects with ApiError({code: "validation_failed", payload}) on 422.
export function renderFormStep(step, container, { sessionId, locale, onSubmit, repairs }) {
  container.innerHTML = "";
  const form = document.createElement("form");
  form.className = "step-form";
  form.noValidate = true;

  if (step.title) {
    form.append(Object.assign(document.createElement("h1"), { className: "step-title", textContent: step.title }));
  }

  const repairNotice = repairNoticeFor(repairs, step.id, locale);
  if (repairNotice) form.append(repairNotice);

  const draft = loadDraft(sessionId, step.id);
  const answers = { ...draft };
  const controllers = new Map();

  const fieldsHost = document.createElement("div");
  fieldsHost.className = "step-fields";
  form.append(fieldsHost);

  function recomputeConditions() {
    for (const field of step.fields) {
      const ctl = controllers.get(field.key);
      if (!ctl) continue;
      const visible = field.visible_when ? evaluateExpr(field.visible_when, answers, true) : true;
      const required = field.required || (field.required_when ? evaluateExpr(field.required_when, answers, false) : false);
      ctl.setVisible(visible);
      ctl.setRequired(required);
      ctl._visible = visible;
      ctl._required = required;
    }
  }

  function handleFieldChange(field, value) {
    if (value === "" || value === null || (Array.isArray(value) && value.length === 0)) {
      delete answers[field.key];
    } else {
      answers[field.key] = value;
    }
    controllers.get(field.key)?.setError(null);
    recomputeConditions();
    saveDraft(sessionId, step.id, answers, step.fields);

    // Cascading: any field whose filter_by.parent === this field's key needs
    // its options reloaded against the new parent value.
    for (const other of step.fields) {
      if (other.filter_by?.parent === field.key) {
        const childCtl = controllers.get(other.key);
        childCtl?.refreshOptions?.(value);
      }
    }
  }

  for (const field of step.fields) {
    const initial = draft[field.key];
    const ctl = createFieldController(field, initial, (v) => handleFieldChange(field, v), { locale });
    controllers.set(field.key, ctl);
    fieldsHost.append(ctl.el);
    if (initial !== undefined) answers[field.key] = initial;
  }

  // Initial load for select/multiselect (including cascading children, which
  // start disabled until their parent has a value).
  for (const field of step.fields) {
    if (field.kind === "select" || field.kind === "multiselect") {
      const parentKey = field.filter_by?.parent;
      const parentValue = parentKey ? answers[parentKey] : undefined;
      controllers.get(field.key)?.refreshOptions?.(parentValue);
    }
  }

  recomputeConditions();

  const errorSummary = document.createElement("div");
  errorSummary.className = "form-error-summary";
  errorSummary.hidden = true;
  form.append(errorSummary);

  const submitBtn = document.createElement("button");
  submitBtn.type = "submit";
  submitBtn.className = "btn btn-primary";
  submitBtn.textContent = t("next", locale);
  form.append(submitBtn);

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    errorSummary.hidden = true;
    for (const ctl of controllers.values()) ctl.setError(null);

    // Client-side pass: required + same-page visible/required + light rules.
    let firstInvalid = null;
    for (const field of step.fields) {
      const ctl = controllers.get(field.key);
      if (!ctl || !ctl._visible) continue;
      const value = answers[field.key];
      if (ctl._required && (value === undefined || value === null || value === "")) {
        ctl.setError(t("field_required", locale));
        firstInvalid = firstInvalid || ctl;
        continue;
      }
      const ruleError = ctl.checkRules?.();
      if (ruleError) {
        ctl.setError(ruleError);
        firstInvalid = firstInvalid || ctl;
      }
    }
    if (firstInvalid) {
      firstInvalid.el.scrollIntoView({ behavior: "smooth", block: "center" });
      return;
    }

    // Only send answers for currently-visible fields — a field hidden by
    // visible_when has a stale/irrelevant value (mirrors the server's
    // "orphaned answers dropped" reconciliation rule, contract.md §4).
    const payloadAnswers = {};
    for (const field of step.fields) {
      const ctl = controllers.get(field.key);
      if (ctl?._visible && Object.prototype.hasOwnProperty.call(answers, field.key)) {
        payloadAnswers[field.key] = answers[field.key];
      }
    }

    submitBtn.disabled = true;
    submitBtn.textContent = t("loading", locale);
    try {
      await onSubmit({ answers: payloadAnswers });
      clearDraft(sessionId, step.id);
    } catch (err) {
      if (err instanceof ApiError && err.code === "validation_failed") {
        const fields = err.payload?.error?.fields ?? [];
        let first = null;
        for (const fe of fields) {
          const ctl = controllers.get(fe.key);
          if (ctl) {
            ctl.setError(fe.message);
            first = first || ctl;
          }
        }
        if (fields.length && !first) {
          errorSummary.hidden = false;
          errorSummary.textContent = fields.map((f) => f.message).join(" ");
        }
        first?.el.scrollIntoView({ behavior: "smooth", block: "center" });
      } else {
        throw err; // let app.js show the generic error path
      }
    } finally {
      submitBtn.disabled = false;
      submitBtn.textContent = t("next", locale);
    }
  });

  container.append(form);
}
