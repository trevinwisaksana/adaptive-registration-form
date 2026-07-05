// repairs.js — shared helper for surfacing `repairs[]` (contract.md §4) inline
// on whichever step renderer owns the affected `step_id`. One tiny module so
// form/document/camera don't each reinvent the same lookup + DOM snippet.

import { t } from "./i18n.js";

// Returns a ready-to-append <div class="repair-notice"> for `stepId`, or
// `null` if no repair targets this step. Falls back to the raw `reason` code
// if we don't have UI copy for the specific repair `kind`.
export function repairNoticeFor(repairs, stepId, locale) {
  const repair = repairs?.find((r) => r.step_id === stepId);
  if (!repair) return null;
  const box = document.createElement("div");
  box.className = "repair-notice";
  box.textContent = t(`repair_${repair.kind}`, locale) || repair.reason;
  return box;
}
