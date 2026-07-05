package engine

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"adaptive-registration-form/backend/internal/api"
	"adaptive-registration-form/backend/internal/flowdef"
)

// resolveStepView builds the client-facing JSON for one step: localized
// title/labels, cross-page visible_when/required_when resolved away (fields
// dropped or annotated with a plain boolean), same-page conditions shipped
// raw for live client UX (contract §3.1).
func (e *Engine) resolveStepView(ctx context.Context, def *flowdef.Definition, stepID string, locale string, answers flowdef.Answers) (map[string]any, error) {
	step, ok := def.StepByID(stepID)
	if !ok {
		return nil, fmt.Errorf("engine: unknown step %q", stepID)
	}
	view := map[string]any{
		"id":   step.ID,
		"type": step.Type,
	}
	if title := e.tr(step.TitleKey, locale); title != "" {
		view["title"] = title
	}

	switch step.Type {
	case "form":
		pageAnswers := answers[step.ID]
		if pageAnswers == nil {
			pageAnswers = map[string]any{}
		}
		fields := make([]map[string]any, 0, len(step.Fields))
		for _, f := range step.Fields {
			fv, visible := e.resolveField(f, pageAnswers, answers, locale)
			if !visible {
				continue
			}
			fields = append(fields, fv)
		}
		view["fields"] = fields
	case "camera":
		view["capture"] = step.Capture
	case "signature":
		// no extra fields
	case "pin":
		// no extra fields
	case "document":
		doc, ok, err := e.Store.GetActiveLegalDoc(ctx, step.Doc, locale, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		if !ok {
			// fall back to default locale if nothing published for this one
			doc, ok, err = e.Store.GetActiveLegalDoc(ctx, step.Doc, DefaultLocale, time.Now().UTC())
			if err != nil {
				return nil, err
			}
		}
		if ok {
			view["doc"] = e.docPointer(doc.Kind, doc.Version, doc.Locale, doc.SHA256)
		}
	case "external":
		view["adapter"] = step.Adapter
	}
	return view, nil
}

// resolveField decides visibility and shape for one field given the current
// page's own answers and the full cross-page answer set.
func (e *Engine) resolveField(f flowdef.Field, pageAnswers map[string]any, all flowdef.Answers, locale string) (map[string]any, bool) {
	visibleWhenCrossPage := strings.Contains(f.VisibleWhen, "answers.")
	requiredWhenCrossPage := strings.Contains(f.RequiredWhen, "answers.")

	if f.VisibleWhen != "" && visibleWhenCrossPage {
		if !flowdef.Visible(f, pageAnswers, all) {
			return nil, false
		}
	}

	out := map[string]any{
		"key":   f.Key,
		"kind":  f.Kind,
		"label": e.tr(f.LabelKey, locale),
	}
	if f.OptionsRef != "" {
		out["options_ref"] = f.OptionsRef
	}
	if f.FilterBy != nil {
		out["filter_by"] = map[string]any{"parent": f.FilterBy.Parent}
	}
	if len(f.Rules) > 0 {
		out["rules"] = f.Rules
	}
	if f.VisibleWhen != "" && !visibleWhenCrossPage {
		out["visible_when"] = f.VisibleWhen
	}

	switch {
	case f.Required:
		out["required"] = true
	case f.RequiredWhen != "" && requiredWhenCrossPage:
		if flowdef.RequiredNow(f, pageAnswers, all) {
			out["required"] = true
		}
	case f.RequiredWhen != "":
		out["required_when"] = f.RequiredWhen
	}
	return out, true
}

// docPointer builds the {kind, version, locale, sha256, url} shape used both
// in a document step's `doc` and in repair details (contract §3.3, §4.1).
func (e *Engine) docPointer(kind, version, locale, sha256 string) map[string]any {
	return map[string]any{
		"kind":    kind,
		"version": version,
		"locale":  locale,
		"sha256":  sha256,
		"url":     fmt.Sprintf("%s/legal/%s/%s?locale=%s", e.Config.BaseURL, url.PathEscape(kind), url.PathEscape(version), url.QueryEscape(locale)),
	}
}

// GetGlobalSystem implements GET /system (contract §2.8): global banners and
// maintenance status only, no session/auth required.
func (e *Engine) GetGlobalSystem(ctx context.Context) (api.SystemInfo, error) {
	return e.computeSystem(ctx, DefaultLocale, "")
}

// computeSystem builds the envelope's "system" block: maintenance/degraded
// status plus banners scoped to global or the given step id (contract §1,
// plan.md §3.1). scope == "" means "no session step in view" (e.g. GET
// /system) — only global banners apply.
func (e *Engine) computeSystem(ctx context.Context, locale, scopeStepID string) (api.SystemInfo, error) {
	anns, err := e.Store.ListActiveAnnouncements(ctx, time.Now().UTC())
	if err != nil {
		return api.SystemInfo{}, err
	}
	info := api.SystemInfo{Status: "ok", Banners: []api.Banner{}}
	rank := map[string]int{"ok": 0, "degraded": 1, "maintenance": 2}
	for _, a := range anns {
		if a.Scope != "global" && a.Scope != scopeStepID {
			continue
		}
		text := a.TextByLocale[locale]
		if text == "" {
			text = a.TextByLocale[DefaultLocale]
		}
		info.Banners = append(info.Banners, api.Banner{ID: a.ID, Severity: a.Severity, Scope: a.Scope, Text: text})
		if rank[a.StatusOverride] > rank[info.Status] {
			info.Status = a.StatusOverride
			info.RetryAfter = a.RetryAfter
		}
	}
	return info, nil
}
