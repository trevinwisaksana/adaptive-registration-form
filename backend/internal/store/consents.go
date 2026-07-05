package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *Store) InsertConsent(ctx context.Context, sessionID, docKind, docVersion, docLocale, docSHA256 string) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO consents (session_id, doc_kind, doc_version, doc_locale, doc_sha256)
		VALUES ($1::uuid, $2, $3, $4, $5)`,
		sessionID, docKind, docVersion, docLocale, docSHA256)
	if err != nil {
		return fmt.Errorf("store: insert consent: %w", err)
	}
	return nil
}

// GetLatestConsent returns the most recent consent for a doc kind, if any.
func (s *Store) GetLatestConsent(ctx context.Context, sessionID, docKind string) (Consent, bool, error) {
	var c Consent
	err := s.Pool.QueryRow(ctx, `SELECT id::text, session_id::text, doc_kind, doc_version, doc_locale, doc_sha256, accepted_at
		FROM consents WHERE session_id=$1::uuid AND doc_kind=$2 ORDER BY accepted_at DESC LIMIT 1`,
		sessionID, docKind).Scan(&c.ID, &c.SessionID, &c.DocKind, &c.DocVersion, &c.DocLocale, &c.DocSHA256, &c.AcceptedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c, false, nil
		}
		return c, false, fmt.Errorf("store: get latest consent: %w", err)
	}
	return c, true, nil
}
