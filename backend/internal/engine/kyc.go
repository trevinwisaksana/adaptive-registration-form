package engine

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"adaptive-registration-form/backend/internal/flowdef"
	"adaptive-registration-form/backend/internal/store"
)

// ensureOnComplete triggers every on_complete adapter exactly once per
// session, the first time the flow has nothing left to resolve (plan.md
// §2.1: "expensive verification runs once, after the flow is done").
func (e *Engine) ensureOnComplete(ctx context.Context, sess *store.Session, def *flowdef.Definition) error {
	if len(def.OnComplete) == 0 {
		return nil
	}
	for _, oc := range def.OnComplete {
		run, has, err := e.Store.GetOnCompleteRun(ctx, sess.ID, oc.Adapter)
		if err != nil {
			return err
		}
		if has && (run.Status == "pending" || run.Status == "approved") {
			continue // already running or already finished successfully
		}
		if has && run.Status == "rejected" {
			continue // waiting on the user to redo the targeted step (a repair), not a fresh trigger
		}
		if err := e.Store.StartOnCompleteRun(ctx, sess.ID, oc.Adapter); err != nil {
			return err
		}
		if sess.Status == "in_progress" {
			if err := e.Store.UpdateSessionStatus(ctx, sess.ID, "verifying"); err != nil {
				return err
			}
			sess.Status = "verifying"
		}
		_ = e.Store.LogEvent(ctx, sess.ID, "kyc_triggered", map[string]any{"adapter": oc.Adapter})
		if oc.Adapter == "vendor_kyc" {
			go e.runMockKYC(sess.ID)
		}
	}
	return nil
}

// runMockKYC simulates a third-party verification vendor: waits
// Config.KYCDelay (standing in for real processing time) then calls back
// into this same process's webhook endpoint, exactly like a real vendor
// would — over HTTP, signed, asynchronously (plan.md §2.1, §6).
func (e *Engine) runMockKYC(sessionID string) {
	time.Sleep(e.Config.KYCDelay)

	body, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"vendor":     "mock_kyc",
		"verdict":    "approved",
		"reason":     nil,
	})
	sig := hex.EncodeToString(hmacSHA256(e.Config.WebhookSecret, body))

	req, err := http.NewRequest(http.MethodPost, e.Config.BaseURL+"/webhooks/mock-kyc", bytes.NewReader(body))
	if err != nil {
		log.Printf("engine: mock kyc webhook build failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Vendor-Signature", sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("engine: mock kyc webhook call failed: %v", err)
		return
	}
	defer resp.Body.Close()
}

func hmacSHA256(secret string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return mac.Sum(nil)
}

// VerifyWebhookSignature checks the HMAC on an inbound vendor webhook.
// TODO(prod): per-vendor signing keys and replay protection; this POC uses
// one shared secret (Config.WebhookSecret) purely to demonstrate the check.
func (e *Engine) VerifyWebhookSignature(body []byte, sigHex string) bool {
	expected := hmacSHA256(e.Config.WebhookSecret, body)
	got, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	return hmac.Equal(expected, got)
}

// HandleKYCWebhook applies a vendor verdict. Idempotent per session — a
// duplicate verdict on an already-terminal session is a no-op (contract §2.7).
func (e *Engine) HandleKYCWebhook(ctx context.Context, sessionID, verdict string, reason *string) error {
	sess, err := e.Store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("engine: kyc webhook: unknown session: %w", err)
	}
	run, has, err := e.Store.GetOnCompleteRun(ctx, sessionID, "vendor_kyc")
	if err != nil {
		return err
	}
	if !has || run.Status != "pending" {
		return nil // already resolved (or never started) — idempotent no-op
	}

	status := "approved"
	sessionStatus := "approved"
	if verdict == "rejected" {
		status = "rejected"
		sessionStatus = "in_progress" // back to the user for a targeted repair, not terminally rejected
	}
	if err := e.Store.FinishOnCompleteRun(ctx, sessionID, "vendor_kyc", status, reason); err != nil {
		return err
	}
	if err := e.Store.UpdateSessionStatus(ctx, sessionID, sessionStatus); err != nil {
		return err
	}
	_ = e.Store.LogEvent(ctx, sess.ID, "kyc_verdict", map[string]any{"verdict": verdict, "reason": reason})
	return nil
}
