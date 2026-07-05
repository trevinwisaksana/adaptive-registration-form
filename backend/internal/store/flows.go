package store

import (
	"context"
	"fmt"

	"adaptive-registration-form/backend/internal/flowdef"
)

// UpsertFlowVersion inserts or replaces a flow version's definition — used by
// the seed loader so re-running it is idempotent.
func (s *Store) UpsertFlowVersion(ctx context.Context, def flowdef.Definition) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO flow_versions (flow_key, version, definition)
		VALUES ($1, $2, $3)
		ON CONFLICT (flow_key, version) DO UPDATE SET definition = EXCLUDED.definition`,
		def.Flow, def.Version, def)
	if err != nil {
		return fmt.Errorf("store: upsert flow version: %w", err)
	}
	return nil
}

func (s *Store) GetFlowVersion(ctx context.Context, flowKey string, version int) (flowdef.Definition, error) {
	var def flowdef.Definition
	err := s.Pool.QueryRow(ctx, `SELECT definition FROM flow_versions WHERE flow_key=$1 AND version=$2`,
		flowKey, version).Scan(&def)
	if err != nil {
		return def, fmt.Errorf("store: get flow version %s/%d: %w", flowKey, version, err)
	}
	return def, nil
}

func (s *Store) GetLatestFlowVersion(ctx context.Context, flowKey string) (flowdef.Definition, error) {
	var def flowdef.Definition
	err := s.Pool.QueryRow(ctx, `SELECT definition FROM flow_versions WHERE flow_key=$1 ORDER BY version DESC LIMIT 1`,
		flowKey).Scan(&def)
	if err != nil {
		return def, fmt.Errorf("store: get latest flow version %s: %w", flowKey, err)
	}
	return def, nil
}
