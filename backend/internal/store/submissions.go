package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *Store) UpsertStepSubmission(ctx context.Context, sessionID, stepID string, payload map[string]any, status string) error {
	if payload == nil {
		payload = map[string]any{}
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO step_submissions (session_id, step_id, payload, status)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (session_id, step_id) DO UPDATE SET payload=EXCLUDED.payload, status=EXCLUDED.status, updated_at=now()`,
		sessionID, stepID, payload, status)
	if err != nil {
		return fmt.Errorf("store: upsert step submission: %w", err)
	}
	return nil
}

// GetStepSubmissions returns every submission for a session keyed by step id.
func (s *Store) GetStepSubmissions(ctx context.Context, sessionID string) (map[string]StepSubmission, error) {
	rows, err := s.Pool.Query(ctx, `SELECT step_id, payload, status, created_at, updated_at FROM step_submissions WHERE session_id=$1::uuid`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: list step submissions: %w", err)
	}
	defer rows.Close()

	out := map[string]StepSubmission{}
	for rows.Next() {
		var sub StepSubmission
		sub.SessionID = sessionID
		if err := rows.Scan(&sub.StepID, &sub.Payload, &sub.Status, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan step submission: %w", err)
		}
		out[sub.StepID] = sub
	}
	return out, rows.Err()
}

// --- Idempotency keys ---

type IdempotencyRecord struct {
	RequestHash  string
	StatusCode   int
	ResponseBody []byte
}

func (s *Store) GetIdempotencyRecord(ctx context.Context, sessionID, stepID, key string) (*IdempotencyRecord, error) {
	var rec IdempotencyRecord
	err := s.Pool.QueryRow(ctx, `SELECT request_hash, status_code, response_body FROM idempotency_keys
		WHERE session_id=$1::uuid AND step_id=$2 AND idempotency_key=$3`, sessionID, stepID, key).
		Scan(&rec.RequestHash, &rec.StatusCode, &rec.ResponseBody)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: get idempotency record: %w", err)
	}
	return &rec, nil
}

// SaveIdempotencyRecord stores body as plain text, verbatim — see the
// response_body column comment in migrations/0001_init.sql for why this
// isn't jsonb.
func (s *Store) SaveIdempotencyRecord(ctx context.Context, sessionID, stepID, key, requestHash string, statusCode int, body []byte) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO idempotency_keys (session_id, step_id, idempotency_key, request_hash, status_code, response_body)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)
		ON CONFLICT (session_id, step_id, idempotency_key) DO NOTHING`,
		sessionID, stepID, key, requestHash, statusCode, string(body))
	if err != nil {
		return fmt.Errorf("store: save idempotency record: %w", err)
	}
	return nil
}
