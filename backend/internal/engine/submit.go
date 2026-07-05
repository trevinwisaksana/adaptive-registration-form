package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"adaptive-registration-form/backend/internal/api"
	"adaptive-registration-form/backend/internal/store"
)

// SubmitResult is the outcome of a step submit — always has a status code and
// a JSON-serializable body, success or error alike, so the HTTP layer and the
// idempotency cache can treat them uniformly. Raw is set only on an
// idempotency-key replay: it holds the exact bytes persisted for the
// original request, so a replay returns that response verbatim (contract
// §2.3) instead of round-tripping through interface{} — encoding/json sorts
// map keys on marshal, which would silently reorder an unmarshal+remarshal
// of the cached body relative to what the client saw the first time.
type SubmitResult struct {
	StatusCode int
	Body       any
	Raw        json.RawMessage
}

// SubmitStep implements POST /sessions/{id}/steps/{stepId} (contract §2.3),
// including idempotency-key replay/conflict handling.
func (e *Engine) SubmitStep(ctx context.Context, sess store.Session, stepID, idempotencyKey string, rawBody []byte) (SubmitResult, error) {
	hash := sha256.Sum256(rawBody)
	requestHash := hex.EncodeToString(hash[:])

	rec, err := e.Store.GetIdempotencyRecord(ctx, sess.ID, stepID, idempotencyKey)
	if err != nil {
		return SubmitResult{}, err
	}
	if rec != nil {
		if rec.RequestHash != requestHash {
			return SubmitResult{StatusCode: http.StatusConflict, Body: api.ErrorResponse{Error: api.ErrorDetail{
				Code: "idempotency_key_reused", Message: "This request key was already used with a different payload.",
			}}}, nil
		}
		return SubmitResult{StatusCode: rec.StatusCode, Raw: json.RawMessage(rec.ResponseBody)}, nil
	}

	result, err := e.processStep(ctx, sess, stepID, rawBody)
	if err != nil {
		return SubmitResult{}, err
	}

	body, err := json.Marshal(result.Body)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("engine: marshal submit response: %w", err)
	}
	if err := e.Store.SaveIdempotencyRecord(ctx, sess.ID, stepID, idempotencyKey, requestHash, result.StatusCode, body); err != nil {
		return SubmitResult{}, err
	}
	return result, nil
}

// processStep validates and stores one step submission against the flow
// version the session is *currently* pinned to — deliberately not
// soft-upgraded here. The client fetched this step's definition (and
// whatever fields it requires) under that version; upgrading first would
// let a flow publish that lands mid-keystroke reject an honest submission
// for a field the user was never shown. The version upgrade and its
// consequences (a newly-required field surfacing as a collect_fields
// repair) happen once, below, via BuildEnvelope — which computes the
// *next* step, after this submission is safely recorded.
func (e *Engine) processStep(ctx context.Context, sess store.Session, stepID string, rawBody []byte) (SubmitResult, error) {
	def, err := e.Store.GetFlowVersion(ctx, sess.FlowKey, sess.FlowVersion)
	if err != nil {
		return SubmitResult{}, err
	}
	step, ok := def.StepByID(stepID)
	if !ok {
		return SubmitResult{StatusCode: http.StatusNotFound, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "unknown_step", Message: "This step does not belong to the current flow.",
		}}}, nil
	}

	subs, err := e.Store.GetStepSubmissions(ctx, sess.ID)
	if err != nil {
		return SubmitResult{}, err
	}
	answers := buildAnswers(subs)

	var result *SubmitResult
	switch step.Type {
	case "form":
		result, err = e.submitForm(ctx, sess, step, rawBody, answers)
	case "camera":
		result, err = e.submitCapture(ctx, sess, step, step.Capture, rawBody)
	case "signature":
		result, err = e.submitCapture(ctx, sess, step, "signature", rawBody)
	case "document":
		result, err = e.submitDocument(ctx, sess, step, rawBody)
	case "pin":
		result, err = e.submitPin(ctx, sess, step, rawBody)
	case "external":
		result, err = e.submitExternal(ctx, sess, step, rawBody)
	default:
		return SubmitResult{StatusCode: http.StatusBadRequest, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "unsupported_step_type", Message: "Unsupported step type.",
		}}}, nil
	}
	if err != nil {
		return SubmitResult{}, err
	}
	if result != nil {
		return *result, nil // validation/staleness error — no mutation happened
	}

	_ = e.Store.LogEvent(ctx, sess.ID, "step_submitted", map[string]any{"step_id": stepID})

	fresh, err := e.Store.GetSession(ctx, sess.ID)
	if err != nil {
		return SubmitResult{}, err
	}
	env, err := e.BuildEnvelope(ctx, fresh)
	if err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{StatusCode: http.StatusOK, Body: env.Envelope}, nil
}
