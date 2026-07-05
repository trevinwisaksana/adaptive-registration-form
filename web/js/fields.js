// fields.js — renders one form field per `kind` (contract.md §3.1) and
// exposes a small uniform controller interface to steps/form.js:
//
//   { key, el, value, setValue(v), setVisible(bool), setRequired(bool),
//     setError(msg), refreshOptions(parentValue), destroy() }
//
// Client-side "rules" checks here are a UX nicety only — contract.md is
// explicit that the server re-validates everything; we intentionally only
// handle the couple of rule shapes the plan calls out ("min:N", "age>=N")
// and silently skip anything else rather than guessing.

import { loadOptions, searchOptions, debounce } from "./refdata.js";

function el(tag, props = {}, children = []) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k === "class") node.className = v;
    else if (k.startsWith("on") && typeof v === "function") node.addEventListener(k.slice(2), v);
    else if (v !== undefined && v !== null) node.setAttribute(k, v);
  }
  for (const c of children) node.append(c);
  return node;
}

function parseRule(rule) {
  let m = /^min:(-?\d+(\.\d+)?)$/.exec(rule);
  if (m) return { type: "min", value: Number(m[1]) };
  m = /^max:(-?\d+(\.\d+)?)$/.exec(rule);
  if (m) return { type: "max", value: Number(m[1]) };
  m = /^age>=(\d+)$/.exec(rule);
  if (m) return { type: "age_gte", value: Number(m[1]) };
  return null; // unrecognized rule shapes are left to the server entirely
}

function ageFromISODate(iso) {
  if (!iso) return null;
  const dob = new Date(iso);
  if (Number.isNaN(dob.getTime())) return null;
  const now = new Date();
  let age = now.getFullYear() - dob.getFullYear();
  const monthDiff = now.getMonth() - dob.getMonth();
  if (monthDiff < 0 || (monthDiff === 0 && now.getDate() < dob.getDate())) age--;
  return age;
}

function checkClientRules(field, rawValue) {
  for (const rule of field.rules ?? []) {
    const parsed = parseRule(rule);
    if (!parsed) continue;
    if (parsed.type === "min" && rawValue !== "" && rawValue != null && Number(rawValue) < parsed.value) {
      return `Must be at least ${parsed.value}.`;
    }
    if (parsed.type === "max" && rawValue !== "" && rawValue != null && Number(rawValue) > parsed.value) {
      return `Must be at most ${parsed.value}.`;
    }
    if (parsed.type === "age_gte") {
      const age = ageFromISODate(rawValue);
      if (age != null && age < parsed.value) return `Must be at least ${parsed.value} years old.`;
    }
  }
  return null;
}

function baseWrapper(field, inputEl) {
  const label = el("label", { class: "field-label", for: `f-${field.key}` }, [
    field.label ?? field.key,
  ]);
  const requiredMark = el("span", { class: "field-required-mark", "aria-hidden": "true" }, ["*"]);
  const errorEl = el("div", { class: "field-error", role: "alert" });
  errorEl.hidden = true;
  const wrap = el("div", { class: "field", "data-field-key": field.key }, [label, inputEl, errorEl]);
  return { wrap, label, requiredMark, errorEl };
}

function makeControllerShell(field, wrap, label, requiredMark, errorEl) {
  return {
    key: field.key,
    el: wrap,
    setVisible(visible) {
      wrap.hidden = !visible;
    },
    setRequired(required) {
      if (required && !label.contains(requiredMark)) label.append(requiredMark);
      if (!required && label.contains(requiredMark)) label.removeChild(requiredMark);
    },
    setError(msg) {
      errorEl.textContent = msg || "";
      errorEl.hidden = !msg;
      wrap.classList.toggle("field--invalid", Boolean(msg));
    },
  };
}

function textField(field, initialValue, onChange) {
  const input = el("input", {
    id: `f-${field.key}`,
    type: "text",
    class: "field-input",
    value: initialValue ?? "",
    oninput: (e) => onChange(e.target.value),
  });
  const { wrap, label, requiredMark, errorEl } = baseWrapper(field, input);
  const ctl = makeControllerShell(field, wrap, label, requiredMark, errorEl);
  Object.defineProperty(ctl, "value", {
    get: () => input.value,
    set: (v) => { input.value = v ?? ""; },
  });
  ctl.checkRules = () => checkClientRules(field, input.value);
  return ctl;
}

function dateField(field, initialValue, onChange) {
  const input = el("input", {
    id: `f-${field.key}`,
    type: "date",
    class: "field-input",
    value: initialValue ?? "",
    onchange: (e) => onChange(e.target.value),
  });
  const { wrap, label, requiredMark, errorEl } = baseWrapper(field, input);
  const ctl = makeControllerShell(field, wrap, label, requiredMark, errorEl);
  Object.defineProperty(ctl, "value", {
    get: () => input.value,
    set: (v) => { input.value = v ?? ""; },
  });
  ctl.checkRules = () => checkClientRules(field, input.value);
  return ctl;
}

function moneyField(field, initialValue, onChange, locale) {
  const input = el("input", {
    id: `f-${field.key}`,
    type: "number",
    step: "0.01",
    inputmode: "decimal",
    class: "field-input",
    value: initialValue ?? "",
    oninput: (e) => onChange(e.target.value === "" ? null : Number(e.target.value)),
  });
  const hint = el("div", { class: "field-hint" });
  const updateHint = () => {
    const n = Number(input.value);
    hint.textContent = input.value !== "" && !Number.isNaN(n)
      ? new Intl.NumberFormat(locale || "en-US", { style: "decimal", maximumFractionDigits: 2 }).format(n)
      : "";
  };
  input.addEventListener("input", updateHint);
  updateHint();
  const { wrap, label, requiredMark, errorEl } = baseWrapper(field, input);
  wrap.append(hint);
  const ctl = makeControllerShell(field, wrap, label, requiredMark, errorEl);
  Object.defineProperty(ctl, "value", {
    get: () => (input.value === "" ? null : Number(input.value)),
    set: (v) => { input.value = v ?? ""; updateHint(); },
  });
  ctl.checkRules = () => checkClientRules(field, input.value);
  return ctl;
}

function boolField(field, initialValue, onChange) {
  // Rendered as an explicit Yes/No pair rather than a single checkbox: for a
  // `required` bool (e.g. `us_person`), "required" means the user must make
  // an explicit choice, not merely that the box is checked.
  const name = `f-${field.key}`;
  let value = initialValue ?? null;
  const yes = el("input", { type: "radio", name, id: `${name}-yes`, ...(value === true ? { checked: "checked" } : {}) });
  const no = el("input", { type: "radio", name, id: `${name}-no`, ...(value === false ? { checked: "checked" } : {}) });
  yes.addEventListener("change", () => { value = true; onChange(true); });
  no.addEventListener("change", () => { value = false; onChange(false); });
  const group = el("div", { class: "field-bool-group" }, [
    el("label", { class: "field-bool-option", for: `${name}-yes` }, [yes, " Yes"]),
    el("label", { class: "field-bool-option", for: `${name}-no` }, [no, " No"]),
  ]);
  const { wrap, label, requiredMark, errorEl } = baseWrapper(field, group);
  const ctl = makeControllerShell(field, wrap, label, requiredMark, errorEl);
  Object.defineProperty(ctl, "value", {
    get: () => value,
    set: (v) => {
      value = v ?? null;
      yes.checked = value === true;
      no.checked = value === false;
    },
  });
  return ctl;
}

function selectField(field, initialValue, onChange, locale) {
  let value = initialValue ?? "";
  let items = [];
  let mode = "select"; // "select" | "typeahead" — decided once options load

  const nativeSelect = el("select", {
    id: `f-${field.key}`,
    class: "field-input",
    onchange: (e) => { value = e.target.value; onChange(value); },
  });

  // Typeahead combobox (used for large datasets).
  const searchInput = el("input", {
    type: "text",
    class: "field-input field-typeahead-input",
    placeholder: "Type to search…",
    autocomplete: "off",
  });
  const listbox = el("ul", { class: "field-typeahead-list", role: "listbox" });
  listbox.hidden = true;
  const selectedLabel = el("div", { class: "field-typeahead-selected" });
  selectedLabel.hidden = true;
  const typeaheadWrap = el("div", { class: "field-typeahead" }, [selectedLabel, searchInput, listbox]);
  typeaheadWrap.hidden = true;

  function labelFor(code) {
    const found = items.find((i) => i.code === code);
    return found ? found.label : code;
  }

  function renderNativeOptions() {
    nativeSelect.innerHTML = "";
    nativeSelect.append(el("option", { value: "" }, ["—"]));
    for (const item of items) {
      const opt = el("option", { value: item.code }, [item.label]);
      if (item.code === value) opt.selected = true;
      nativeSelect.append(opt);
    }
  }

  function renderTypeaheadList(results) {
    listbox.innerHTML = "";
    if (results.length === 0) {
      listbox.append(el("li", { class: "field-typeahead-empty" }, ["No matches"]));
    }
    for (const item of results) {
      const li = el("li", { role: "option", tabindex: "0" }, [item.label]);
      li.addEventListener("click", () => selectTypeaheadItem(item));
      li.addEventListener("keydown", (e) => { if (e.key === "Enter") selectTypeaheadItem(item); });
      listbox.append(li);
    }
    listbox.hidden = false;
  }

  function selectTypeaheadItem(item) {
    value = item.code;
    onChange(value);
    selectedLabel.textContent = item.label;
    selectedLabel.hidden = false;
    searchInput.value = "";
    listbox.hidden = true;
  }

  const doSearch = debounce(async (q) => {
    const results = await searchOptions(field.options_ref, q, { parent: currentParentValue });
    renderTypeaheadList(results);
  }, 250);

  searchInput.addEventListener("input", (e) => {
    if (e.target.value.length === 0) { listbox.hidden = true; return; }
    doSearch(e.target.value);
  });
  searchInput.addEventListener("focus", () => {
    if (searchInput.value) doSearch(searchInput.value);
  });
  document.addEventListener("click", (e) => {
    if (!typeaheadWrap.contains(e.target)) listbox.hidden = true;
  });

  let currentParentValue;

  const { wrap, label, requiredMark, errorEl } = baseWrapper(field, el("div", { class: "field-select-container" }, [nativeSelect, typeaheadWrap]));
  const ctl = makeControllerShell(field, wrap, label, requiredMark, errorEl);

  Object.defineProperty(ctl, "value", {
    get: () => value || "",
    set: (v) => {
      value = v ?? "";
      renderNativeOptions();
      if (mode === "typeahead") {
        if (value) {
          selectedLabel.textContent = labelFor(value);
          selectedLabel.hidden = false;
        } else {
          selectedLabel.hidden = true;
        }
      }
    },
  });

  ctl.refreshOptions = async (parentValue) => {
    currentParentValue = parentValue;
    if (field.filter_by?.parent && !parentValue) {
      // Cascading field with no parent selected yet — nothing to show.
      items = [];
      value = "";
      onChange("");
      renderNativeOptions();
      selectedLabel.hidden = true;
      nativeSelect.disabled = true;
      searchInput.disabled = true;
      return;
    }
    nativeSelect.disabled = false;
    searchInput.disabled = false;
    const { items: loaded, isLarge } = await loadOptions(field.options_ref, { parent: parentValue });
    items = loaded;
    mode = isLarge ? "typeahead" : "select";
    nativeSelect.hidden = mode !== "select";
    typeaheadWrap.hidden = mode !== "typeahead";
    // Parent change invalidates a previously selected child value if it's no
    // longer in the option list.
    if (value && !items.some((i) => i.code === value)) {
      value = "";
      onChange("");
    }
    renderNativeOptions();
    if (mode === "typeahead" && value) {
      selectedLabel.textContent = labelFor(value);
      selectedLabel.hidden = false;
    }
  };

  return ctl;
}

function multiselectField(field, initialValue, onChange) {
  let selected = new Set(initialValue ?? []);
  let items = [];
  let mode = "checkbox";

  const checklist = el("div", { class: "field-checklist" });
  const searchInput = el("input", {
    type: "text",
    class: "field-input field-typeahead-input",
    placeholder: "Type to search…",
    autocomplete: "off",
  });
  searchInput.hidden = true;
  const chips = el("div", { class: "field-chips" });

  function emit() {
    onChange(Array.from(selected));
  }

  function renderChips() {
    chips.innerHTML = "";
    for (const code of selected) {
      const found = items.find((i) => i.code === code);
      const chip = el("span", { class: "field-chip" }, [found ? found.label : code]);
      const remove = el("button", { type: "button", class: "field-chip-remove", "aria-label": "Remove" }, ["×"]);
      remove.addEventListener("click", () => {
        selected.delete(code);
        emit();
        renderChips();
        if (mode === "checkbox") renderChecklist();
      });
      chip.append(remove);
      chips.append(chip);
    }
  }

  function renderChecklist() {
    checklist.innerHTML = "";
    for (const item of items) {
      const id = `f-${field.key}-${item.code}`;
      const cb = el("input", { type: "checkbox", id });
      cb.checked = selected.has(item.code);
      cb.addEventListener("change", () => {
        if (cb.checked) selected.add(item.code);
        else selected.delete(item.code);
        emit();
        renderChips();
      });
      checklist.append(el("label", { class: "field-checklist-option", for: id }, [cb, " ", item.label]));
    }
  }

  const doSearch = debounce(async (q) => {
    const results = await searchOptions(field.options_ref, q, { parent: currentParentValue });
    renderTypeaheadResults(results);
  }, 250);

  const resultsList = el("ul", { class: "field-typeahead-list", role: "listbox" });
  resultsList.hidden = true;

  function renderTypeaheadResults(results) {
    resultsList.innerHTML = "";
    for (const item of results) {
      const li = el("li", { role: "option", tabindex: "0" }, [
        item.label + (selected.has(item.code) ? " ✓" : ""),
      ]);
      li.addEventListener("click", () => {
        if (selected.has(item.code)) selected.delete(item.code);
        else selected.add(item.code);
        emit();
        renderChips();
        renderTypeaheadResults(results);
      });
      resultsList.append(li);
    }
    resultsList.hidden = false;
  }

  searchInput.addEventListener("input", (e) => {
    if (e.target.value.length === 0) { resultsList.hidden = true; return; }
    doSearch(e.target.value);
  });

  let currentParentValue;

  const container = el("div", { class: "field-select-container" }, [chips, checklist, searchInput, resultsList]);
  const { wrap, label, requiredMark, errorEl } = baseWrapper(field, container);
  const ctl = makeControllerShell(field, wrap, label, requiredMark, errorEl);

  Object.defineProperty(ctl, "value", {
    get: () => Array.from(selected),
    set: (v) => {
      selected = new Set(v ?? []);
      renderChips();
      if (mode === "checkbox") renderChecklist();
    },
  });

  ctl.refreshOptions = async (parentValue) => {
    currentParentValue = parentValue;
    if (field.filter_by?.parent && !parentValue) {
      items = [];
      checklist.innerHTML = "";
      return;
    }
    const { items: loaded, isLarge } = await loadOptions(field.options_ref, { parent: parentValue });
    items = loaded;
    mode = isLarge ? "typeahead" : "checkbox";
    checklist.hidden = mode !== "checkbox";
    searchInput.hidden = mode !== "typeahead";
    if (mode === "checkbox") renderChecklist();
    renderChips();
  };

  return ctl;
}

// Factory: builds the right controller for `field.kind`. `initialValue` seeds
// from a restored draft or server-provided value; `onChange(value)` is called
// on every user edit.
export function createFieldController(field, initialValue, onChange, opts = {}) {
  switch (field.kind) {
    case "text":
      return textField(field, initialValue, onChange);
    case "date":
      return dateField(field, initialValue, onChange);
    case "money":
      return moneyField(field, initialValue, onChange, opts.locale);
    case "bool":
      return boolField(field, initialValue, onChange);
    case "select":
      return selectField(field, initialValue, onChange, opts.locale);
    case "multiselect":
      return multiselectField(field, initialValue, onChange, opts.locale);
    default: {
      // Unknown field kind — render a disabled placeholder rather than crash;
      // the flow-level capability negotiation (force_update) is supposed to
      // prevent this, but fail soft just in case.
      const input = el("input", { type: "text", disabled: "disabled", value: "(unsupported field)" });
      const { wrap, label, requiredMark, errorEl } = baseWrapper(field, input);
      return makeControllerShell(field, wrap, label, requiredMark, errorEl);
    }
  }
}
