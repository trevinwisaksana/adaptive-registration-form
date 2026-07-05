package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewToken mints a stub bearer token. POC only — no signing, no expiry
// verification beyond the session row's own expires_at. TODO(prod): replace
// with a short-lived JWT bound to session + device (plan.md §5).
func NewToken(sessionID string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "stub." + sessionID + "." + hex.EncodeToString(b)
}

func (s *Store) CreateSession(ctx context.Context, flowKey string, version int, locale, deviceID, platform, appVersion string, ttl time.Duration) (Session, error) {
	var sess Session
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO sessions (id, token, flow_key, flow_version, original_flow_version, locale, device_id, platform, app_version, expires_at)
		VALUES (gen_random_uuid(), '', $1, $2, $2, $3, $4, $5, $6, $7)
		RETURNING id::text`,
		flowKey, version, locale, deviceID, platform, appVersion, time.Now().Add(ttl))
	var id string
	if err := row.Scan(&id); err != nil {
		return sess, fmt.Errorf("store: create session: %w", err)
	}
	token := NewToken(id)
	if _, err := s.Pool.Exec(ctx, `UPDATE sessions SET token=$1 WHERE id=$2::uuid`, token, id); err != nil {
		return sess, fmt.Errorf("store: set session token: %w", err)
	}
	return s.GetSession(ctx, id)
}

func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	return s.scanSession(ctx, `SELECT id::text, token, flow_key, flow_version, original_flow_version, locale, status, pin_set,
		blocked_min_version, coalesce(device_id,''), coalesce(platform,''), coalesce(app_version,''), created_at, updated_at, expires_at
		FROM sessions WHERE id=$1::uuid`, id)
}

func (s *Store) GetSessionByToken(ctx context.Context, token string) (Session, error) {
	return s.scanSession(ctx, `SELECT id::text, token, flow_key, flow_version, original_flow_version, locale, status, pin_set,
		blocked_min_version, coalesce(device_id,''), coalesce(platform,''), coalesce(app_version,''), created_at, updated_at, expires_at
		FROM sessions WHERE token=$1`, token)
}

func (s *Store) scanSession(ctx context.Context, query string, arg string) (Session, error) {
	var sess Session
	err := s.Pool.QueryRow(ctx, query, arg).Scan(
		&sess.ID, &sess.Token, &sess.FlowKey, &sess.FlowVersion, &sess.OriginalFlowVersion, &sess.Locale, &sess.Status, &sess.PinSet,
		&sess.BlockedMinVersion, &sess.DeviceID, &sess.Platform, &sess.AppVersion, &sess.CreatedAt, &sess.UpdatedAt, &sess.ExpiresAt)
	if err != nil {
		return sess, fmt.Errorf("store: get session: %w", err)
	}
	return sess, nil
}

func (s *Store) SetBlockedMinVersion(ctx context.Context, id, minVersion string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET blocked_min_version=$1, updated_at=now() WHERE id=$2::uuid`, minVersion, id)
	if err != nil {
		return fmt.Errorf("store: set blocked min version: %w", err)
	}
	return nil
}

func (s *Store) UpdateSessionFlowVersion(ctx context.Context, id string, version int) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET flow_version=$1, updated_at=now() WHERE id=$2::uuid`, version, id)
	if err != nil {
		return fmt.Errorf("store: update session flow version: %w", err)
	}
	return nil
}

// UpdateSessionLocale changes a session's locale mid-flow (plan.md §2.1:
// "Locale is session state — sent at session open, switchable mid-flow").
func (s *Store) UpdateSessionLocale(ctx context.Context, id, locale string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET locale=$1, updated_at=now() WHERE id=$2::uuid`, locale, id)
	if err != nil {
		return fmt.Errorf("store: update session locale: %w", err)
	}
	return nil
}

func (s *Store) UpdateSessionStatus(ctx context.Context, id, status string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET status=$1, updated_at=now() WHERE id=$2::uuid`, status, id)
	if err != nil {
		return fmt.Errorf("store: update session status: %w", err)
	}
	return nil
}

func (s *Store) SetPinSet(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET pin_set=true, updated_at=now() WHERE id=$1::uuid`, id)
	if err != nil {
		return fmt.Errorf("store: set pin: %w", err)
	}
	return nil
}
