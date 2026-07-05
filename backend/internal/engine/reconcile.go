package engine

import (
	"context"
	"strings"
	"time"

	"adaptive-registration-form/backend/internal/api"
	"adaptive-registration-form/backend/internal/flowdef"
	"adaptive-registration-form/backend/internal/store"
)

// Result is everything BuildEnvelope needs to hand back to an HTTP handler.
type Result struct {
	Envelope api.Envelope
	Session  store.Session
}

// BuildEnvelope is the single reconciliation entry point — used by session
// create/resume and every step submit response (contract §4). It may
// soft-upgrade the session's pinned flow_version and may trigger the
// on_complete adapter as side effects, matching plan.md §4's resume model.
func (e *Engine) BuildEnvelope(ctx context.Context, sess store.Session) (Result, error) {
	if sess.BlockedMinVersion != nil {
		return Result{Session: sess, Envelope: e.forceUpdateEnvelope(ctx, sess)}, nil
	}

	if err := e.maybeUpgradeFlowVersion(ctx, &sess); err != nil {
		return Result{}, err
	}

	def, err := e.Store.GetFlowVersion(ctx, sess.FlowKey, sess.FlowVersion)
	if err != nil {
		return Result{}, err
	}
	subs, err := e.Store.GetStepSubmissions(ctx, sess.ID)
	if err != nil {
		return Result{}, err
	}
	answers := buildAnswers(subs)
	order := flowdef.ResolveOrder(&def, answers)

	repairs, err := e.computeRepairs(ctx, sess, &def, order, subs, answers)
	if err != nil {
		return Result{}, err
	}

	idx, found := e.nextUnresolved(&def, order, subs, sess, repairs)

	progress := api.Progress{Total: len(order)}
	var nextView map[string]any
	if found {
		progress.Completed = idx
		nextView, err = e.resolveStepView(ctx, &def, order[idx], sess.Locale, answers)
		if err != nil {
			return Result{}, err
		}
	} else {
		progress.Completed = len(order)
		if err := e.ensureOnComplete(ctx, &sess, &def); err != nil {
			return Result{}, err
		}
	}

	sysScope := ""
	if nextView != nil {
		sysScope, _ = nextView["id"].(string)
	}
	sys, err := e.computeSystem(ctx, sess.Locale, sysScope)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Session: sess,
		Envelope: api.Envelope{
			System:   sys,
			Progress: progress,
			NextStep: nextView,
			Repairs:  repairs,
		},
	}, nil
}

func (e *Engine) forceUpdateEnvelope(ctx context.Context, sess store.Session) api.Envelope {
	sys, _ := e.computeSystem(ctx, sess.Locale, "force_update")
	return api.Envelope{
		System:   sys,
		Progress: api.Progress{Completed: 0, Total: 1},
		NextStep: map[string]any{
			"id": "force_update", "type": "external", "adapter": "force_update",
			"min_app_version": *sess.BlockedMinVersion,
		},
		Repairs: []api.Repair{},
	}
}

// maybeUpgradeFlowVersion soft-upgrades an in-progress session to the latest
// published flow version for its flow key. Sessions are "pinned" only in the
// sense that a completed/terminal session never moves and mid-flight
// structural changes don't retroactively break what's already rendered — the
// reconciliation loop (computeRepairs) is what makes catching up safe: any
// newly-required field becomes a targeted collect_fields repair rather than
// a silent behavior change (plan.md §4 worked example).
func (e *Engine) maybeUpgradeFlowVersion(ctx context.Context, sess *store.Session) error {
	if sess.Status != "in_progress" {
		return nil
	}
	latest, err := e.Store.GetLatestFlowVersion(ctx, sess.FlowKey)
	if err != nil {
		return err
	}
	if latest.Version <= sess.FlowVersion {
		return nil
	}
	if err := e.Store.UpdateSessionFlowVersion(ctx, sess.ID, latest.Version); err != nil {
		return err
	}
	sess.FlowVersion = latest.Version
	_ = e.Store.LogEvent(ctx, sess.ID, "flow_version_upgraded", map[string]any{"to_version": latest.Version})
	return nil
}

// computeRepairs is the one mechanism behind reaccept_document,
// collect_fields, and redo_step (contract §4) — it also drives cross-page
// edit cascades for free: re-resolving every form step's required fields
// against the *current* answers-so-far naturally surfaces a field that just
// became required (or stops flagging one that's no longer visible).
func (e *Engine) computeRepairs(ctx context.Context, sess store.Session, def *flowdef.Definition, order []string, subs map[string]store.StepSubmission, answers flowdef.Answers) ([]api.Repair, error) {
	repairs := []api.Repair{}
	now := time.Now().UTC()

	for _, stepID := range order {
		step, ok := def.StepByID(stepID)
		if !ok {
			continue
		}
		switch step.Type {
		case "form":
			sub, has := subs[stepID]
			if !has || sub.Status == "invalidated" {
				continue // not yet submitted — a fresh step, not a repair
			}
			var missing []string
			flowBumped := false
			for _, f := range step.Fields {
				if !flowdef.Visible(f, sub.Payload, answers) {
					continue
				}
				if !flowdef.RequiredNow(f, sub.Payload, answers) {
					continue
				}
				v, present := sub.Payload[f.Key]
				if present && !isEmptyAny(v) {
					continue
				}
				missing = append(missing, f.Key)
				if f.SinceVersion > sess.OriginalFlowVersion {
					flowBumped = true
				}
			}
			if len(missing) > 0 {
				reason := "cross_page_dependency_changed"
				if flowBumped {
					reason = "flow_version_bumped"
				}
				repairs = append(repairs, api.Repair{
					Kind: "collect_fields", StepID: stepID, Reason: reason,
					Detail: map[string]any{"fields": missing},
				})
			}
		case "document":
			consent, has, err := e.Store.GetLatestConsent(ctx, sess.ID, step.Doc)
			if err != nil {
				return nil, err
			}
			if !has {
				continue
			}
			active, ok, err := e.Store.GetActiveLegalDoc(ctx, step.Doc, consent.DocLocale, now)
			if err != nil {
				return nil, err
			}
			if ok && active.Version != consent.DocVersion && active.Reacceptance == "required" {
				repairs = append(repairs, api.Repair{
					Kind: "reaccept_document", StepID: stepID, Reason: "tnc_version_changed",
					Detail: map[string]any{"doc": e.docPointer(active.Kind, active.Version, active.Locale, active.SHA256)},
				})
			}
		case "camera":
			doc, has, err := e.Store.GetDocumentByKind(ctx, sess.ID, step.Capture)
			if err != nil {
				return nil, err
			}
			if !has || doc.UploadedAt == nil {
				continue
			}
			if now.Sub(*doc.UploadedAt) > e.Config.DocumentTTL {
				repairs = append(repairs, api.Repair{
					Kind: "redo_step", StepID: stepID, Reason: "document_expired",
					Detail: map[string]any{"ttl_days": int(e.Config.DocumentTTL.Hours() / 24)},
				})
			}
		}
	}

	if run, has, err := e.Store.GetOnCompleteRun(ctx, sess.ID, "vendor_kyc"); err != nil {
		return nil, err
	} else if has && run.Status == "rejected" {
		target := stepForRejectionReason(def, run.Reason)
		reason := "kyc_rejected"
		if run.Reason != nil {
			reason = *run.Reason
		}
		repairs = append(repairs, api.Repair{Kind: "redo_step", StepID: target, Reason: reason, Detail: map[string]any{}})
	}

	return repairs, nil
}

// stepForRejectionReason maps a mock-vendor rejection code to the camera
// step it targets — e.g. "blurry_id"/"blurry_selfie" (contract §2.7).
func stepForRejectionReason(def *flowdef.Definition, reason *string) string {
	r := ""
	if reason != nil {
		r = *reason
	}
	if strings.Contains(r, "selfie") {
		if _, ok := def.StepByID("selfie"); ok {
			return "selfie"
		}
	}
	if _, ok := def.StepByID("id_card"); ok {
		return "id_card"
	}
	return "id_card"
}

// nextUnresolved finds the first step (by order) that is either the target
// of an outstanding repair or hasn't been completed yet — contract §4:
// "next_step always points at the first unresolved item".
func (e *Engine) nextUnresolved(def *flowdef.Definition, order []string, subs map[string]store.StepSubmission, sess store.Session, repairs []api.Repair) (int, bool) {
	repaired := map[string]bool{}
	for _, r := range repairs {
		repaired[r.StepID] = true
	}
	for i, id := range order {
		if repaired[id] {
			return i, true
		}
		step, ok := def.StepByID(id)
		if !ok {
			continue
		}
		if step.Type == "pin" {
			if !sess.PinSet {
				return i, true
			}
			continue
		}
		sub, has := subs[id]
		if !has || sub.Status == "invalidated" {
			return i, true
		}
	}
	return len(order), false
}

func isEmptyAny(v any) bool {
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
