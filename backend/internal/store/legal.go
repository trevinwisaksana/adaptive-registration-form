package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) UpsertLegalDoc(ctx context.Context, d LegalDoc) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO legal_docs (kind, version, locale, sha256, content_type, content, effective_at, reacceptance)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (kind, version, locale) DO UPDATE SET sha256=EXCLUDED.sha256, content_type=EXCLUDED.content_type,
			content=EXCLUDED.content, effective_at=EXCLUDED.effective_at, reacceptance=EXCLUDED.reacceptance`,
		d.Kind, d.Version, d.Locale, d.SHA256, d.ContentType, d.Content, d.EffectiveAt, d.Reacceptance)
	if err != nil {
		return fmt.Errorf("store: upsert legal doc: %w", err)
	}
	return nil
}

// GetActiveLegalDoc returns the highest effective_at <= now for (kind,
// locale) — the resolve-at-serve-time pattern from plan.md §4.1.
func (s *Store) GetActiveLegalDoc(ctx context.Context, kind, locale string, now time.Time) (LegalDoc, bool, error) {
	var d LegalDoc
	err := s.Pool.QueryRow(ctx, `SELECT kind, version, locale, sha256, content_type, content, effective_at, reacceptance
		FROM legal_docs WHERE kind=$1 AND locale=$2 AND effective_at <= $3
		ORDER BY effective_at DESC LIMIT 1`, kind, locale, now).
		Scan(&d.Kind, &d.Version, &d.Locale, &d.SHA256, &d.ContentType, &d.Content, &d.EffectiveAt, &d.Reacceptance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return d, false, nil
		}
		return d, false, fmt.Errorf("store: get active legal doc: %w", err)
	}
	return d, true, nil
}

func (s *Store) GetLegalDoc(ctx context.Context, kind, version, locale string) (LegalDoc, bool, error) {
	var d LegalDoc
	err := s.Pool.QueryRow(ctx, `SELECT kind, version, locale, sha256, content_type, content, effective_at, reacceptance
		FROM legal_docs WHERE kind=$1 AND version=$2 AND locale=$3`, kind, version, locale).
		Scan(&d.Kind, &d.Version, &d.Locale, &d.SHA256, &d.ContentType, &d.Content, &d.EffectiveAt, &d.Reacceptance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return d, false, nil
		}
		return d, false, fmt.Errorf("store: get legal doc: %w", err)
	}
	return d, true, nil
}
