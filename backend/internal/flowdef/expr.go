package flowdef

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Answers is the full set of stored answers for a session, keyed by step id
// then field key. Used to resolve cross-page expressions
// ("answers.<step>.<field>").
type Answers map[string]map[string]any

// exprPattern splits "<path> <op> <rhs>" into three groups. Supported ops are
// the only ones used anywhere in plan.md/contract.md's worked examples:
// =='!='in. This is deliberately not a general expression language — a POC
// needs just enough to demonstrate cross-page and same-page conditionals.
var exprPattern = regexp.MustCompile(`^\s*(\S+)\s+(==|!=|in)\s+(.+?)\s*$`)

// Eval evaluates a visible_when/required_when/transition "when" expression.
// pagePath resolves bare (non "answers.") identifiers against the current
// page's own answers (same-page fields); "answers.<step>.<field>" always
// resolves against all (cross-page).
func Eval(expr string, pageAnswers map[string]any, all Answers) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true, nil
	}
	m := exprPattern.FindStringSubmatch(expr)
	if m == nil {
		return false, fmt.Errorf("flowdef: unsupported expression %q", expr)
	}
	path, op, rhs := m[1], m[2], m[3]

	left, _ := resolvePath(path, pageAnswers, all)

	switch op {
	case "in":
		list, err := parseList(rhs)
		if err != nil {
			return false, err
		}
		for _, v := range list {
			if looseEqual(left, v) {
				return true, nil
			}
		}
		return false, nil
	case "==", "!=":
		right, err := parseLiteral(rhs)
		if err != nil {
			return false, err
		}
		eq := looseEqual(left, right)
		if op == "!=" {
			return !eq, nil
		}
		return eq, nil
	}
	return false, fmt.Errorf("flowdef: unsupported operator %q", op)
}

// resolvePath resolves "answers.<step>.<field>" against all, otherwise a bare
// key against pageAnswers.
func resolvePath(path string, pageAnswers map[string]any, all Answers) (any, bool) {
	if strings.HasPrefix(path, "answers.") {
		parts := strings.SplitN(strings.TrimPrefix(path, "answers."), ".", 2)
		if len(parts) != 2 {
			return nil, false
		}
		step, field := parts[0], parts[1]
		if step == "" {
			return nil, false
		}
		v, ok := all[step][field]
		return v, ok
	}
	v, ok := pageAnswers[path]
	return v, ok
}

func parseLiteral(s string) (any, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'") && len(s) >= 2 {
		return s[1 : len(s)-1], nil
	}
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) && len(s) >= 2 {
		return s[1 : len(s)-1], nil
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return n, nil
	}
	return nil, fmt.Errorf("flowdef: cannot parse literal %q", s)
}

func parseList(s string) ([]any, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("flowdef: expected list literal, got %q", s)
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil, nil
	}
	parts := strings.Split(inner, ",")
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		lit, err := parseLiteral(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		out = append(out, lit)
	}
	return out, nil
}

// looseEqual compares JSON-decoded values (numbers arrive as float64, strings
// as string, bools as bool) loosely enough to make "17 == 17.0" and
// "'US' == 'US'" both work regardless of how the value was decoded.
func looseEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		return af == bf
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}
