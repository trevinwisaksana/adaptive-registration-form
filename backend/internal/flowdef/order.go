package flowdef

// ResolveOrder returns the ordered list of step ids for this definition given
// the answers collected so far — base order from Steps, with Transitions
// splicing in extra steps when their "when" expression holds (contract §5,
// the FATCA branch). Evaluated fresh every time answers change, so branches
// can appear or disappear as the user edits earlier pages.
//
// Steps is the full catalog of step *definitions* — it includes
// branch-only steps (e.g. fatca_form) so StepByID can resolve their fields —
// but the *default* order excludes any step id that's exclusively reachable
// via a transition's insert list; it only appears once its transition
// matches. That's what makes "no match = default order from steps" and
// "progress.total becomes 11 instead of 10" (contract §5) both true.
func ResolveOrder(d *Definition, all Answers) []string {
	conditional := map[string]bool{}
	for _, t := range d.Transitions {
		for _, id := range t.Insert {
			conditional[id] = true
		}
	}

	order := make([]string, 0, len(d.Steps))
	for _, s := range d.Steps {
		if !conditional[s.ID] {
			order = append(order, s.ID)
		}
	}

	for _, t := range d.Transitions {
		ok, err := Eval(t.When, all[t.From], all)
		if err != nil || !ok {
			continue
		}
		order = spliceIn(order, t.Insert, t.Before, t.After)
	}
	return order
}

func spliceIn(order []string, insert []string, before, after string) []string {
	already := map[string]bool{}
	for _, id := range order {
		already[id] = true
	}
	var fresh []string
	for _, id := range insert {
		if !already[id] {
			fresh = append(fresh, id)
		}
	}
	if len(fresh) == 0 {
		return order
	}

	idx := -1
	if before != "" {
		for i, id := range order {
			if id == before {
				idx = i
				break
			}
		}
	} else if after != "" {
		for i, id := range order {
			if id == after {
				idx = i + 1
				break
			}
		}
	}
	if idx < 0 {
		return append(order, fresh...)
	}
	out := make([]string, 0, len(order)+len(fresh))
	out = append(out, order[:idx]...)
	out = append(out, fresh...)
	out = append(out, order[idx:]...)
	return out
}
