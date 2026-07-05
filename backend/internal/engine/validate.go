package engine

import (
	"context"
	"strings"

	"adaptive-registration-form/backend/internal/api"
	"adaptive-registration-form/backend/internal/flowdef"
)

// validateForm re-checks every field server-side (contract §3.1: "always
// re-checked server-side") and returns localized field errors. pageAnswers
// may include keys for fields that are no longer visible — those are simply
// ignored, matching the edit-cascade "orphaned answers are dropped" behavior
// described in plan.md §2.
func (e *Engine) validateForm(ctx context.Context, step flowdef.Step, pageAnswers map[string]any, all flowdef.Answers, locale string) []api.FieldError {
	var errs []api.FieldError
	for _, f := range step.Fields {
		if !flowdef.Visible(f, pageAnswers, all) {
			continue
		}
		v, present := pageAnswers[f.Key]
		if viol := flowdef.ValidateField(f, v, present, pageAnswers, all); viol != nil {
			errs = append(errs, e.localizeViolation(f, *viol, locale))
			continue
		}
		if !present || isEmptyAny(v) {
			continue
		}
		if (f.Kind == "select" || f.Kind == "multiselect") && f.OptionsRef != "" {
			if fe := e.validateOptionCode(ctx, f, v, locale); fe != nil {
				errs = append(errs, *fe)
			}
		}
	}
	return errs
}

// validateOptionCode enforces contract §2.1: "the list is enforcement, not
// just UI" — a submitted select/multiselect code must be an active row in
// its ref dataset, so a stale device cache can never produce an invalid
// submission.
func (e *Engine) validateOptionCode(ctx context.Context, f flowdef.Field, v any, locale string) *api.FieldError {
	codes := codesOf(f.Kind, v)
	for _, code := range codes {
		_, ok, err := e.Store.GetRefItem(ctx, f.OptionsRef, code)
		if err != nil || !ok {
			msg := e.renderMessage("validation.invalid_option", locale, map[string]string{"label": e.tr(f.LabelKey, locale)})
			return &api.FieldError{Key: f.Key, Rule: "invalid_option", Message: msg}
		}
	}
	return nil
}

func codesOf(kind string, v any) []string {
	switch kind {
	case "multiselect":
		list, _ := v.([]any)
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		if s, ok := v.(string); ok {
			return []string{s}
		}
		return nil
	}
}

func (e *Engine) localizeViolation(f flowdef.Field, v flowdef.RuleViolation, locale string) api.FieldError {
	label := e.tr(f.LabelKey, locale)
	params := map[string]string{"label": label}
	for k, val := range v.Params {
		params[k] = val
	}
	msg := e.renderMessage("validation."+v.Rule, locale, params)
	return api.FieldError{Key: f.Key, Rule: v.Rule, Message: msg}
}

func (e *Engine) renderMessage(key, locale string, params map[string]string) string {
	msg := e.tr(key, locale)
	for k, v := range params {
		msg = strings.ReplaceAll(msg, "{"+k+"}", v)
	}
	return msg
}
