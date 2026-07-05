package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *Store) GetOnCompleteRun(ctx context.Context, sessionID, adapter string) (OnCompleteRun, bool, error) {
	var r OnCompleteRun
	err := s.Pool.QueryRow(ctx, `SELECT session_id::text, adapter, attempt, status, reason, started_at, finished_at
		FROM on_complete_runs WHERE session_id=$1::uuid AND adapter=$2`, sessionID, adapter).
		Scan(&r.SessionID, &r.Adapter, &r.Attempt, &r.Status, &r.Reason, &r.StartedAt, &r.FinishedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return r, false, nil
		}
		return r, false, fmt.Errorf("store: get on_complete_run: %w", err)
	}
	return r, true, nil
}

// StartOnCompleteRun records a fresh attempt (insert, or bump attempt if a
// prior run exists e.g. after a rejection + redo). Idempotent: only ever one
// row per (session, adapter); "exactly once, idempotently" per plan.md §2.1
// is enforced by the caller checking GetOnCompleteRun first.
func (s *Store) StartOnCompleteRun(ctx context.Context, sessionID, adapter string) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO on_complete_runs (session_id, adapter, attempt, status, started_at)
		VALUES ($1::uuid, $2, 1, 'pending', now())
		ON CONFLICT (session_id, adapter) DO UPDATE SET attempt = on_complete_runs.attempt + 1,
			status='pending', reason=NULL, started_at=now(), finished_at=NULL`,
		sessionID, adapter)
	if err != nil {
		return fmt.Errorf("store: start on_complete_run: %w", err)
	}
	return nil
}

// DeleteOnCompleteRun clears a run record — used when a targeted redo_step
// repair is fulfilled, so the next full completion re-triggers the adapter.
func (s *Store) DeleteOnCompleteRun(ctx context.Context, sessionID, adapter string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM on_complete_runs WHERE session_id=$1::uuid AND adapter=$2`, sessionID, adapter)
	if err != nil {
		return fmt.Errorf("store: delete on_complete_run: %w", err)
	}
	return nil
}

func (s *Store) FinishOnCompleteRun(ctx context.Context, sessionID, adapter, status string, reason *string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE on_complete_runs SET status=$1, reason=$2, finished_at=now()
		WHERE session_id=$3::uuid AND adapter=$4`, status, reason, sessionID, adapter)
	if err != nil {
		return fmt.Errorf("store: finish on_complete_run: %w", err)
	}
	return nil
}
