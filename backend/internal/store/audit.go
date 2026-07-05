package store

import (
	"context"
	"fmt"
)

// LogEvent writes one structured audit_log row. sessionID may be empty for
// pre-session events (there are none currently, but kept flexible).
func (s *Store) LogEvent(ctx context.Context, sessionID, event string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	var err error
	if sessionID == "" {
		_, err = s.Pool.Exec(ctx, `INSERT INTO audit_log (session_id, event, detail) VALUES (NULL, $1, $2)`, event, detail)
	} else {
		_, err = s.Pool.Exec(ctx, `INSERT INTO audit_log (session_id, event, detail) VALUES ($1::uuid, $2, $3)`, sessionID, event, detail)
	}
	if err != nil {
		return fmt.Errorf("store: log event: %w", err)
	}
	return nil
}
