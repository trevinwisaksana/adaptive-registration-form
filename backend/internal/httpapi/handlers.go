package httpapi

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"adaptive-registration-form/backend/internal/api"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("httpapi: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, api.ErrorResponse{Error: api.ErrorDetail{Code: code, Message: message}})
}

func writeInternalError(w http.ResponseWriter, err error) {
	log.Printf("httpapi: internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again.")
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func rateLimited(w http.ResponseWriter, retryAfter int) {
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests. Please slow down and try again shortly.")
}

// --- POST /sessions ---

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req api.SessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed request body.")
		return
	}
	deviceID := req.DeviceAttestation
	if deviceID == "" {
		deviceID = "unknown-device"
	}
	limitKey := deviceID + "|" + clientIP(r)
	if ok, retryAfter := s.SessionLimiter.Allow(limitKey); !ok {
		rateLimited(w, retryAfter)
		return
	}

	resp, status, err := s.Engine.CreateOrResumeSession(r.Context(), req, deviceID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, status, resp)
}

// --- GET /sessions/{id} ---

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.Engine.Authenticate(r.Context(), id, r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid session token.")
		return
	}
	env, _, err := s.Engine.GetSession(r.Context(), sess.ID, r.URL.Query().Get("locale"))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// --- POST /sessions/{id}/steps/{stepId} ---

func (s *Server) handleSubmitStep(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	stepID := r.PathValue("stepId")

	sess, err := s.Engine.Authenticate(r.Context(), id, r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid session token.")
		return
	}

	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		writeError(w, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key header is required.")
		return
	}

	if ok, retryAfter := s.SubmitLimiter.Allow(sess.ID); !ok {
		rateLimited(w, retryAfter)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Could not read request body.")
		return
	}

	result, err := s.Engine.SubmitStep(r.Context(), sess, stepID, idemKey, body)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if result.Raw != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(result.StatusCode)
		_, _ = w.Write(result.Raw)
		return
	}
	writeJSON(w, result.StatusCode, result.Body)
}

// --- POST /sessions/{id}/uploads ---

func (s *Server) handleIssueUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.Engine.Authenticate(r.Context(), id, r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid session token.")
		return
	}

	var req struct {
		Kind        string `json:"kind"`
		ContentType string `json:"content_type"`
		SizeBytes   int64  `json:"size_bytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed request body.")
		return
	}
	const maxUploadBytes = 15 << 20
	if req.SizeBytes <= 0 || req.SizeBytes > maxUploadBytes {
		writeError(w, http.StatusBadRequest, "invalid_size", "size_bytes must be between 1 and 15MB.")
		return
	}

	slot, err := s.Engine.IssueUploadSlot(r.Context(), sess, req.Kind, req.ContentType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be one of id_card, selfie, signature.")
		return
	}
	writeJSON(w, http.StatusCreated, slot)
}

// --- GET /refdata/{dataset} ---

func (s *Server) handleRefData(w http.ResponseWriter, r *http.Request) {
	dataset := r.PathValue("dataset")
	parent := r.URL.Query().Get("parent")
	q := r.URL.Query().Get("q")
	locale := s.Engine.NormalizeLocale(r.URL.Query().Get("locale"))

	if ok, retryAfter := s.RefdataLimiter.Allow(clientIP(r)); !ok {
		rateLimited(w, retryAfter)
		return
	}

	resp, err := s.Engine.GetRefData(r.Context(), dataset, parent, q, locale)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown_dataset", "Unknown reference dataset.")
		return
	}

	etag := `"` + dataset + "-" + strconv.Itoa(resp.Version) + `"`
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- GET /legal/{kind}/{version} ---

func (s *Server) handleLegalDoc(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	version := r.PathValue("version")
	locale := r.URL.Query().Get("locale")

	doc, err := s.Engine.GetLegalDoc(r.Context(), kind, version, locale)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Legal document not found.")
		return
	}
	// Immutable per (kind, version, locale) — cacheable forever (contract §2.6).
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writeJSON(w, http.StatusOK, map[string]any{
		"kind": doc.Kind, "version": doc.Version, "locale": doc.Locale,
		"sha256": doc.SHA256, "content_type": doc.ContentType, "content": doc.Content,
		"effective_at": doc.EffectiveAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	})
}

// --- POST /webhooks/mock-kyc ---

func (s *Server) handleKYCWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Could not read request body.")
		return
	}
	sig := r.Header.Get("X-Vendor-Signature")
	if !s.Engine.VerifyWebhookSignature(body, sig) {
		writeError(w, http.StatusUnauthorized, "invalid_signature", "Webhook signature verification failed.")
		return
	}

	var payload struct {
		SessionID string  `json:"session_id"`
		Vendor    string  `json:"vendor"`
		Verdict   string  `json:"verdict"`
		Reason    *string `json:"reason"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed webhook payload.")
		return
	}

	if err := s.Engine.HandleKYCWebhook(r.Context(), payload.SessionID, payload.Verdict, payload.Reason); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"received": true})
}

// --- GET /system ---

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	sys, err := s.Engine.GetGlobalSystem(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"system": sys})
}
