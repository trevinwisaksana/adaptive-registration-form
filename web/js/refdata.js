// refdata.js — helpers for options_ref-backed select/multiselect fields:
// cascading (filter_by.parent) and typeahead for large lists (contract.md §2.5).

import { getRefdata } from "./api.js";

// Above this many items, swap the plain dropdown for a searchable typeahead
// (plan.md: "Renderer adapts to size: wheel picker for a dozen items,
// searchable typeahead for thousands"). The refdata endpoint doesn't report a
// total count, so we use "did the first page come back full" as the signal.
export const TYPEAHEAD_THRESHOLD = 20;

export function debounce(fn, ms) {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), ms);
  };
}

// Fetches the first page for a dataset (optionally parent-filtered) and
// reports whether the result looks "large" (i.e. should render as typeahead).
export async function loadOptions(dataset, { parent } = {}) {
  const res = await getRefdata(dataset, { parent });
  const items = res?.items ?? [];
  return { items, isLarge: items.length >= TYPEAHEAD_THRESHOLD, version: res?.version };
}

export async function searchOptions(dataset, q, { parent } = {}) {
  const res = await getRefdata(dataset, { parent, q });
  return res?.items ?? [];
}
