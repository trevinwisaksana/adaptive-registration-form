// steps/document.js — renders `type: "document"` (T&C) steps (contract.md
// §3.3): fetches content from /legal/{kind}/{version}, gates the accept
// button behind scroll-to-bottom, and echoes version+sha256 on accept
// (§2.3). Handles the 409 "stale_document" race explicitly.

import { getLegalDoc } from "../api.js";
import { t } from "../i18n.js";
import { ApiError } from "../api.js";
import { repairNoticeFor } from "../repairs.js";

export async function renderDocumentStep(step, container, { locale, onSubmit, repairs }) {
  container.innerHTML = "";

  const wrap = document.createElement("div");
  wrap.className = "step-document";
  if (step.title) {
    wrap.append(Object.assign(document.createElement("h1"), { className: "step-title", textContent: step.title }));
  }

  const repairNotice = repairNoticeFor(repairs, step.id, locale);
  if (repairNotice) wrap.append(repairNotice);

  const staleNotice = document.createElement("div");
  staleNotice.className = "banner banner-warning";
  staleNotice.hidden = true;
  staleNotice.textContent = t("doc_stale", locale);
  wrap.append(staleNotice);

  const scrollArea = document.createElement("div");
  scrollArea.className = "document-scroll";
  const contentEl = document.createElement("div");
  contentEl.className = "document-content";
  contentEl.textContent = t("loading", locale);
  scrollArea.append(contentEl);
  wrap.append(scrollArea);

  const hint = document.createElement("div");
  hint.className = "document-hint";
  hint.textContent = t("scroll_to_continue", locale);
  wrap.append(hint);

  const acceptBtn = document.createElement("button");
  acceptBtn.type = "button";
  acceptBtn.className = "btn btn-primary";
  acceptBtn.textContent = t("accept", locale);
  acceptBtn.disabled = true;
  wrap.append(acceptBtn);

  container.append(wrap);

  let currentDoc = step.doc;

  async function loadDoc(doc) {
    contentEl.textContent = t("loading", locale);
    acceptBtn.disabled = true;
    hint.hidden = false;
    const legal = await getLegalDoc(doc.kind, doc.version, doc.locale);
    // The doc's own content_type governs rendering. This content comes from
    // our own backend (not third-party), so setting innerHTML for text/html
    // doesn't violate the "no third-party scripts" CSP rule (plan.md §1) —
    // but it IS user-facing legal text from an ops-editable table, so a
    // production build should still run it through a sanitizer allow-list
    // before injecting. TODO: add HTML sanitization before go-live.
    if (legal.content_type === "text/html") {
      contentEl.innerHTML = legal.content;
    } else {
      contentEl.textContent = legal.content;
    }
    currentDoc = { kind: legal.kind, version: legal.version, locale: legal.locale, sha256: legal.sha256 };
    scrollArea.scrollTop = 0;
    checkScrolled();
  }

  function checkScrolled() {
    const atBottom = scrollArea.scrollTop + scrollArea.clientHeight >= scrollArea.scrollHeight - 8;
    acceptBtn.disabled = !atBottom;
    if (atBottom) hint.hidden = true;
  }
  scrollArea.addEventListener("scroll", checkScrolled);

  await loadDoc(currentDoc);
  // Short documents might not need scrolling at all — re-check once laid out.
  requestAnimationFrame(checkScrolled);

  acceptBtn.addEventListener("click", async () => {
    acceptBtn.disabled = true;
    acceptBtn.textContent = t("loading", locale);
    staleNotice.hidden = true;
    try {
      await onSubmit({ accept: true, doc: currentDoc });
    } catch (err) {
      if (err instanceof ApiError && err.code === "stale_document") {
        staleNotice.hidden = false;
        const fresh = err.payload?.current_doc;
        if (fresh) await loadDoc(fresh);
        acceptBtn.textContent = t("accept", locale);
      } else {
        throw err;
      }
    }
  });
}
