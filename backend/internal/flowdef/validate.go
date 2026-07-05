package flowdef

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Visible reports whether f should be shown/collected given the answers on
// its own page and all stored answers (for cross-page visible_when).
func Visible(f Field, pageAnswers map[string]any, all Answers) bool {
	if f.VisibleWhen == "" {
		return true
	}
	ok, err := Eval(f.VisibleWhen, pageAnswers, all)
	if err != nil {
		// Fail open (visible) on a bad expression — a POC-grade flow-JSON
		// bug shouldn't hide a field silently; server logs elsewhere.
		return true
	}
	return ok
}

// RequiredNow reports whether f is required given current answers.
func RequiredNow(f Field, pageAnswers map[string]any, all Answers) bool {
	if f.Required {
		return true
	}
	if f.RequiredWhen == "" {
		return false
	}
	ok, err := Eval(f.RequiredWhen, pageAnswers, all)
	if err != nil {
		return false
	}
	return ok
}

// RuleViolation is a single failed validation rule, ready to be localized by
// the caller (message key + params, not display text — flowdef doesn't know
// locales).
type RuleViolation struct {
	Key    string
	Rule   string
	Params map[string]string
}

// ValidateField checks one field's value against its kind and rules. present
// tells whether the key existed at all in the submitted answers map (vs.
// existing but empty).
func ValidateField(f Field, value any, present bool, pageAnswers map[string]any, all Answers) *RuleViolation {
	required := RequiredNow(f, pageAnswers, all)
	empty := !present || isEmptyValue(value)

	if required && empty {
		rule := "required"
		if f.RequiredWhen != "" && !f.Required {
			rule = "required_when"
		}
		return &RuleViolation{Key: f.Key, Rule: rule}
	}
	if empty {
		return nil // not required and not provided — nothing else to check
	}

	for _, rule := range f.Rules {
		if v := checkRule(f, rule, value); v != nil {
			return v
		}
	}
	if f.Kind == "select" || f.Kind == "multiselect" {
		// Code validity against the active ref dataset is checked by the
		// engine (it has DB access); flowdef only handles shape here.
	}
	return nil
}

func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	}
	return false
}

func checkRule(f Field, rule string, value any) *RuleViolation {
	switch {
	case strings.HasPrefix(rule, "min:"):
		n, err := numberOf(value)
		threshold, perr := strconv.ParseFloat(strings.TrimPrefix(rule, "min:"), 64)
		if err == nil && perr == nil && n < threshold {
			return &RuleViolation{Key: f.Key, Rule: "min", Params: map[string]string{"min": trimNum(threshold)}}
		}
	case strings.HasPrefix(rule, "max:"):
		n, err := numberOf(value)
		threshold, perr := strconv.ParseFloat(strings.TrimPrefix(rule, "max:"), 64)
		if err == nil && perr == nil && n > threshold {
			return &RuleViolation{Key: f.Key, Rule: "max", Params: map[string]string{"max": trimNum(threshold)}}
		}
	case strings.HasPrefix(rule, "age>="):
		minAge, perr := strconv.Atoi(strings.TrimPrefix(rule, "age>="))
		dob, ok := dateOf(value)
		if perr == nil && ok {
			age := ageInYears(dob, time.Now().UTC())
			if age < minAge {
				return &RuleViolation{Key: f.Key, Rule: "age_min", Params: map[string]string{"age": strconv.Itoa(minAge)}}
			}
		}
	}
	return nil
}

func numberOf(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case string:
		return strconv.ParseFloat(n, 64)
	}
	return 0, fmt.Errorf("not a number")
}

func dateOf(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func ageInYears(dob, now time.Time) int {
	age := now.Year() - dob.Year()
	if now.Month() < dob.Month() || (now.Month() == dob.Month() && now.Day() < dob.Day()) {
		age--
	}
	return age
}

func trimNum(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
