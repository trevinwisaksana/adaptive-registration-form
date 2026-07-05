package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"adaptive-registration-form/backend/internal/api"
	"adaptive-registration-form/backend/internal/flowdef"
	"adaptive-registration-form/backend/internal/store"
)

var ErrUnauthorized = errors.New("engine: unauthorized")

// CreateOrResumeSession implements POST /sessions (contract §2.1).
func (e *Engine) CreateOrResumeSession(ctx context.Context, req api.SessionRequest, deviceID string) (api.SessionResponse, int, error) {
	if req.ResumeToken != nil && *req.ResumeToken != "" {
		sess, err := e.Store.GetSessionByToken(ctx, *req.ResumeToken)
		if err == nil {
			result, err := e.BuildEnvelope(ctx, sess)
			if err != nil {
				return api.SessionResponse{}, 0, err
			}
			return e.toSessionResponse(result), 200, nil
		}
		// fall through to creating a fresh session if the resume token is unknown/expired
	}

	locale := e.NormalizeLocale(req.Locale)

	latest, err := e.Store.GetLatestFlowVersion(ctx, FlowKey)
	if err != nil {
		return api.SessionResponse{}, 0, fmt.Errorf("engine: no published flow version: %w", err)
	}

	sess, err := e.Store.CreateSession(ctx, FlowKey, latest.Version, locale, deviceID, req.Client.Platform, req.Client.AppVersion, e.Config.SessionTTL)
	if err != nil {
		return api.SessionResponse{}, 0, err
	}

	if minVersion, ok := capabilityGap(&latest, req.Client); ok {
		if err := e.Store.SetBlockedMinVersion(ctx, sess.ID, minVersion); err != nil {
			return api.SessionResponse{}, 0, err
		}
		sess.BlockedMinVersion = &minVersion
	}

	_ = e.Store.LogEvent(ctx, sess.ID, "session_created", map[string]any{"flow_version": sess.FlowVersion})

	result, err := e.BuildEnvelope(ctx, sess)
	if err != nil {
		return api.SessionResponse{}, 0, err
	}
	return e.toSessionResponse(result), 201, nil
}

// GetSession implements GET /sessions/{id} (contract §2.2). An optional
// newLocale switches the session's locale mid-flow before re-resolving the
// envelope — plan.md §2.1: "Locale is session state ... switchable mid-flow
// ... a language switch just re-serves the current step re-resolved." This
// is a contract extension (§2.2 doesn't document a query param) needed by
// the web renderer's locale switcher; harmless no-op when newLocale is "".
func (e *Engine) GetSession(ctx context.Context, sessionID, newLocale string) (api.Envelope, store.Session, error) {
	sess, err := e.Store.GetSession(ctx, sessionID)
	if err != nil {
		return api.Envelope{}, store.Session{}, err
	}
	if newLocale != "" {
		locale := e.NormalizeLocale(newLocale)
		if locale != sess.Locale {
			if err := e.Store.UpdateSessionLocale(ctx, sess.ID, locale); err != nil {
				return api.Envelope{}, store.Session{}, err
			}
			sess.Locale = locale
		}
	}
	result, err := e.BuildEnvelope(ctx, sess)
	if err != nil {
		return api.Envelope{}, store.Session{}, err
	}
	return result.Envelope, result.Session, nil
}

// Authenticate validates the stub bearer token against the session id in the
// URL path. TODO(prod): short-lived JWT bound to session + device (plan.md
// §5) — this POC only checks the token was the one minted for this session.
func (e *Engine) Authenticate(ctx context.Context, sessionID, authHeader string) (store.Session, error) {
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return store.Session{}, ErrUnauthorized
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		return store.Session{}, ErrUnauthorized
	}
	sess, err := e.Store.GetSession(ctx, sessionID)
	if err != nil {
		return store.Session{}, ErrUnauthorized
	}
	if sess.Token != token {
		return store.Session{}, ErrUnauthorized
	}
	return sess, nil
}

// capabilityGap reports whether the flow needs a step type or field kind the
// client didn't declare support for (contract §2.1: "the server only serves
// flow versions the client can render"). An empty capability list means the
// client didn't declare (e.g. a curl/smoke-test client) — treated as
// capable of everything rather than blocking every non-iOS caller.
func capabilityGap(def *flowdef.Definition, client api.ClientInfo) (string, bool) {
	if len(client.SupportedTypes) == 0 && len(client.SupportedFieldKinds) == 0 {
		return "", false
	}
	types := toSet(client.SupportedTypes)
	kinds := toSet(client.SupportedFieldKinds)
	for _, step := range def.Steps {
		if !types[step.Type] {
			return "1.6.0", true
		}
		for _, f := range step.Fields {
			if !kinds[f.Kind] {
				return "1.6.0", true
			}
		}
	}
	return "", false
}

func toSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

func (e *Engine) toSessionResponse(r Result) api.SessionResponse {
	return api.SessionResponse{
		Session: api.SessionInfo{
			ID: r.Session.ID, Flow: r.Session.FlowKey, FlowVersion: r.Session.FlowVersion,
			ExpiresAt: r.Session.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		},
		Token:    r.Session.Token,
		Envelope: r.Envelope,
	}
}
