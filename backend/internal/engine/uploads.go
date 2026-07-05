package engine

import (
	"context"
	"fmt"
	"time"

	"adaptive-registration-form/backend/internal/media"
	"adaptive-registration-form/backend/internal/store"
)

var validUploadKinds = map[string]bool{"id_card": true, "selfie": true, "signature": true}

const uploadURLTTL = 5 * time.Minute

// UploadSlotResponse mirrors contract §2.4's response body.
type UploadSlotResponse struct {
	UploadRef string            `json:"upload_ref"`
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt string            `json:"expires_at"`
}

// IssueUploadSlot implements POST /sessions/{id}/uploads. One object slot per
// doc kind per session — re-requesting overwrites the prior slot (contract
// §2.4, and the upload-abuse bound in plan.md §2.1).
func (e *Engine) IssueUploadSlot(ctx context.Context, sess store.Session, kind, contentType string) (UploadSlotResponse, error) {
	if !validUploadKinds[kind] {
		return UploadSlotResponse{}, fmt.Errorf("engine: invalid upload kind %q", kind)
	}
	objectKey := media.ObjectKey(sess.ID, kind)
	doc, err := e.Store.UpsertDocumentSlot(ctx, sess.ID, kind, objectKey)
	if err != nil {
		return UploadSlotResponse{}, err
	}
	slot, err := e.Media.Presign(objectKey, contentType, uploadURLTTL)
	if err != nil {
		return UploadSlotResponse{}, err
	}
	_ = e.Store.LogEvent(ctx, sess.ID, "upload_issued", map[string]any{"kind": kind, "upload_ref": doc.UploadRef})
	return UploadSlotResponse{
		UploadRef: doc.UploadRef,
		URL:       slot.URL,
		Method:    slot.Method,
		Headers:   slot.Headers,
		ExpiresAt: slot.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil
}
