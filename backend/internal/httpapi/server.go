// Package httpapi wires docs/contract.md's REST surface onto stdlib
// net/http — no framework, using Go 1.22+'s method+wildcard ServeMux
// patterns for routing.
package httpapi

import (
	"log"
	"net/http"
	"time"

	"adaptive-registration-form/backend/internal/engine"
	"adaptive-registration-form/backend/internal/ratelimit"
)

type Server struct {
	Engine         *engine.Engine
	SessionLimiter *ratelimit.Limiter // ~5 sessions/day/device (contract §2.1)
	SubmitLimiter  *ratelimit.Limiter // ~30 writes/min/session (plan.md §5)
	RefdataLimiter *ratelimit.Limiter // ~60/min/session (plan.md §5)
	WebDir         string             // static web/ renderer, served at /web/
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /sessions", s.handleCreateSession)
	mux.HandleFunc("GET /sessions/{id}", s.handleGetSession)
	mux.HandleFunc("POST /sessions/{id}/steps/{stepId}", s.handleSubmitStep)
	mux.HandleFunc("POST /sessions/{id}/uploads", s.handleIssueUpload)

	mux.HandleFunc("GET /refdata/{dataset}", s.handleRefData)
	mux.HandleFunc("GET /legal/{kind}/{version}", s.handleLegalDoc)
	mux.HandleFunc("POST /webhooks/mock-kyc", s.handleKYCWebhook)
	mux.HandleFunc("GET /system", s.handleSystem)

	mux.HandleFunc("PUT /internal/local-uploads/", s.Engine.Media.LocalUploadHandler)

	if s.WebDir != "" {
		fs := http.FileServer(http.Dir(s.WebDir))
		mux.Handle("GET /web/", http.StripPrefix("/web/", fs))
	}

	return withCORS(withLogging(mux))
}

func withCORS(next http.Handler) http.Handler {
	// Permissive by design for this POC (contract has no vendor scripts on
	// these endpoints); the web renderer is built by a separate agent and
	// may be served from a different origin during development.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, If-None-Match")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}
