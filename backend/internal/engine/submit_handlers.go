package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"adaptive-registration-form/backend/internal/api"
	"adaptive-registration-form/backend/internal/flowdef"
	"adaptive-registration-form/backend/internal/media"
	"adaptive-registration-form/backend/internal/store"
)

// submitForm validates and stores a form page (contract §3.1). A non-nil
// result means "stop here, nothing was mutated" (a 422).
func (e *Engine) submitForm(ctx context.Context, sess store.Session, step flowdef.Step, rawBody []byte, all flowdef.Answers) (*SubmitResult, error) {
	var body struct {
		Answers map[string]any `json:"answers"`
	}
	if err := json.Unmarshal(rawBody, &body); err != nil || body.Answers == nil {
		return &SubmitResult{StatusCode: http.StatusBadRequest, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "bad_request", Message: "Expected a JSON object with an \"answers\" field.",
		}}}, nil
	}

	if errs := e.validateForm(ctx, step, body.Answers, all, sess.Locale); len(errs) > 0 {
		return &SubmitResult{StatusCode: http.StatusUnprocessableEntity, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "validation_failed", StepID: step.ID, Message: "One or more fields are invalid.", Fields: errs,
		}}}, nil
	}

	// Keep only keys the current field set actually recognizes — orphaned
	// answers from a since-changed visible_when are dropped here, per the
	// edit-cascade rule in plan.md §2.
	kept := map[string]any{}
	for _, f := range step.Fields {
		if v, ok := body.Answers[f.Key]; ok {
			kept[f.Key] = v
		}
	}
	if err := e.Store.UpsertStepSubmission(ctx, sess.ID, step.ID, kept, "submitted"); err != nil {
		return nil, err
	}
	return nil, nil
}

// submitCapture handles camera and signature steps: both submit an
// upload_ref pointing at a previously-issued upload slot (contract §2.4,
// §3.2). The cheap checks (size/type/decodes-as-image) happen here, once,
// at submit time — not at raw upload time, since MinIO uploads bypass our
// API entirely.
func (e *Engine) submitCapture(ctx context.Context, sess store.Session, step flowdef.Step, kind string, rawBody []byte) (*SubmitResult, error) {
	var body struct {
		UploadRef string `json:"upload_ref"`
	}
	if err := json.Unmarshal(rawBody, &body); err != nil || body.UploadRef == "" {
		return &SubmitResult{StatusCode: http.StatusBadRequest, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "bad_request", Message: "Expected an \"upload_ref\".",
		}}}, nil
	}

	doc, ok, err := e.Store.GetDocumentByUploadRef(ctx, sess.ID, body.UploadRef)
	if err != nil {
		return nil, err
	}
	if !ok || doc.Kind != kind {
		return &SubmitResult{StatusCode: http.StatusUnprocessableEntity, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "upload_missing", StepID: step.ID, Message: "No upload was found for this step. Request a new upload URL and try again.",
		}}}, nil
	}

	data, contentType, err := e.Media.Fetch(ctx, doc.ObjectKey)
	if err != nil {
		return &SubmitResult{StatusCode: http.StatusUnprocessableEntity, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "upload_missing", StepID: step.ID, Message: "The file hasn't finished uploading yet. Try again in a moment.",
		}}}, nil
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if err := media.ValidateImage(data); err != nil {
		return &SubmitResult{StatusCode: http.StatusUnprocessableEntity, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "invalid_upload", StepID: step.ID, Message: "The uploaded file isn't a valid image. Please retake and try again.",
		}}}, nil
	}

	sha := media.SHA256Hex(data)
	if err := e.Store.MarkDocumentChecked(ctx, sess.ID, kind, contentType, int64(len(data)), sha); err != nil {
		return nil, err
	}
	if err := e.Store.UpsertStepSubmission(ctx, sess.ID, step.ID, map[string]any{"upload_ref": body.UploadRef}, "submitted"); err != nil {
		return nil, err
	}
	// A redo of a previously KYC-rejected capture clears the rejection so
	// the flow can re-trigger verification once everything else is done.
	if run, has, err := e.Store.GetOnCompleteRun(ctx, sess.ID, "vendor_kyc"); err == nil && has && run.Status == "rejected" {
		_ = e.Store.DeleteOnCompleteRun(ctx, sess.ID, "vendor_kyc")
	}
	return nil, nil
}

// submitDocument handles T&C acceptance (contract §2.3, §4.1). The client
// echoes the version/hash it displayed; if that doesn't match the version
// active right now, consent is rejected as stale and the fresh pointer is
// returned so the client can re-render.
func (e *Engine) submitDocument(ctx context.Context, sess store.Session, step flowdef.Step, rawBody []byte) (*SubmitResult, error) {
	var body struct {
		Accept bool `json:"accept"`
		Doc    struct {
			Kind    string `json:"kind"`
			Version string `json:"version"`
			Locale  string `json:"locale"`
			SHA256  string `json:"sha256"`
		} `json:"doc"`
	}
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return &SubmitResult{StatusCode: http.StatusBadRequest, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "bad_request", Message: "Malformed request body.",
		}}}, nil
	}

	active, ok, err := e.Store.GetActiveLegalDoc(ctx, step.Doc, sess.Locale, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !ok {
		active, ok, err = e.Store.GetActiveLegalDoc(ctx, step.Doc, DefaultLocale, time.Now().UTC())
		if err != nil {
			return nil, err
		}
	}
	if !ok {
		return nil, fmt.Errorf("engine: no active legal doc for kind %q", step.Doc)
	}

	if !body.Accept || body.Doc.Version != active.Version || body.Doc.SHA256 != active.SHA256 {
		return &SubmitResult{StatusCode: http.StatusConflict, Body: api.ErrorResponse{
			Error:      api.ErrorDetail{Code: "stale_document", Message: "This document was updated. Please review the new version."},
			CurrentDoc: e.docPointer(active.Kind, active.Version, active.Locale, active.SHA256),
		}}, nil
	}

	if err := e.Store.InsertConsent(ctx, sess.ID, active.Kind, active.Version, active.Locale, active.SHA256); err != nil {
		return nil, err
	}
	if err := e.Store.UpsertStepSubmission(ctx, sess.ID, step.ID, map[string]any{
		"accepted": true, "version": active.Version, "locale": active.Locale,
	}, "submitted"); err != nil {
		return nil, err
	}
	_ = e.Store.LogEvent(ctx, sess.ID, "consent_recorded", map[string]any{"doc_kind": active.Kind, "doc_version": active.Version})
	return nil, nil
}

var pinPattern = regexp.MustCompile(`^\d{6}$`)

// submitPin handles PIN setup. Per plan.md §5, the PIN is a credential, not
// an answer: it is never written to step_submissions. TODO(prod): forward
// over TLS to a real auth service and store only an Argon2 hash there; this
// POC has no auth service, so it just records that a PIN was set.
func (e *Engine) submitPin(ctx context.Context, sess store.Session, step flowdef.Step, rawBody []byte) (*SubmitResult, error) {
	var body struct {
		Pin string `json:"pin"`
	}
	if err := json.Unmarshal(rawBody, &body); err != nil || !pinPattern.MatchString(body.Pin) {
		return &SubmitResult{StatusCode: http.StatusUnprocessableEntity, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "validation_failed", StepID: step.ID, Message: "Invalid PIN.",
			Fields: []api.FieldError{{Key: "pin", Rule: "format", Message: e.renderMessage("validation.pin_format", sess.Locale, nil)}},
		}}}, nil
	}
	if err := e.Store.SetPinSet(ctx, sess.ID); err != nil {
		return nil, err
	}
	_ = e.Store.LogEvent(ctx, sess.ID, "pin_set", map[string]any{}) // no payload — never the PIN itself
	return nil, nil
}

// submitExternal handles the third-party/webview escape hatch (contract
// §3.4): the client reports back whatever the vendor adapter returned.
func (e *Engine) submitExternal(ctx context.Context, sess store.Session, step flowdef.Step, rawBody []byte) (*SubmitResult, error) {
	var body struct {
		Adapter string         `json:"adapter"`
		Result  map[string]any `json:"result"`
	}
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return &SubmitResult{StatusCode: http.StatusBadRequest, Body: api.ErrorResponse{Error: api.ErrorDetail{
			Code: "bad_request", Message: "Malformed request body.",
		}}}, nil
	}
	if err := e.Store.UpsertStepSubmission(ctx, sess.ID, step.ID, body.Result, "submitted"); err != nil {
		return nil, err
	}
	return nil, nil
}
