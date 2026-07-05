package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *Store) UpsertRefDataset(ctx context.Context, key string, version int) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO ref_datasets (key, version) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET version=EXCLUDED.version`, key, version)
	if err != nil {
		return fmt.Errorf("store: upsert ref dataset: %w", err)
	}
	return nil
}

func (s *Store) UpsertRefItem(ctx context.Context, datasetKey, code string, parentCode *string, labels map[string]string, active bool, sort int) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO ref_items (dataset_key, code, parent_code, labels, active, sort)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (dataset_key, code) DO UPDATE SET parent_code=EXCLUDED.parent_code, labels=EXCLUDED.labels,
			active=EXCLUDED.active, sort=EXCLUDED.sort`,
		datasetKey, code, parentCode, labels, active, sort)
	if err != nil {
		return fmt.Errorf("store: upsert ref item: %w", err)
	}
	return nil
}

func (s *Store) GetDatasetVersion(ctx context.Context, key string) (int, error) {
	var v int
	err := s.Pool.QueryRow(ctx, `SELECT version FROM ref_datasets WHERE key=$1`, key).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("store: get dataset version: %w", err)
	}
	return v, nil
}

// ListRefItems returns active items for a dataset, optionally filtered by
// parent code and a case-insensitive label substring search (any locale).
func (s *Store) ListRefItems(ctx context.Context, datasetKey, parent, q string) ([]RefItem, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT code, parent_code, labels, active, sort FROM ref_items
		WHERE dataset_key=$1 AND active=true
		  AND ($2 = '' OR parent_code=$2)
		  AND ($3 = '' OR labels::text ILIKE '%' || $3 || '%')
		ORDER BY sort, code`, datasetKey, parent, q)
	if err != nil {
		return nil, fmt.Errorf("store: list ref items: %w", err)
	}
	defer rows.Close()

	var out []RefItem
	for rows.Next() {
		var it RefItem
		if err := rows.Scan(&it.Code, &it.ParentCode, &it.Labels, &it.Active, &it.Sort); err != nil {
			return nil, fmt.Errorf("store: scan ref item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetRefItem looks up one active item by code — used to validate a submitted
// select/multiselect code against the enforcement dataset (contract §2.1
// "Codes, not labels, are stored").
func (s *Store) GetRefItem(ctx context.Context, datasetKey, code string) (RefItem, bool, error) {
	var it RefItem
	err := s.Pool.QueryRow(ctx, `SELECT code, parent_code, labels, active, sort FROM ref_items
		WHERE dataset_key=$1 AND code=$2 AND active=true`, datasetKey, code).
		Scan(&it.Code, &it.ParentCode, &it.Labels, &it.Active, &it.Sort)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return it, false, nil
		}
		return it, false, fmt.Errorf("store: get ref item: %w", err)
	}
	return it, true, nil
}

// --- translations ---

func (s *Store) UpsertTranslation(ctx context.Context, key, locale, text string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO translations (key, locale, text) VALUES ($1, $2, $3)
		ON CONFLICT (key, locale) DO UPDATE SET text=EXCLUDED.text`, key, locale, text)
	if err != nil {
		return fmt.Errorf("store: upsert translation: %w", err)
	}
	return nil
}

func (s *Store) GetTranslation(ctx context.Context, key, locale string) (string, bool, error) {
	var text string
	err := s.Pool.QueryRow(ctx, `SELECT text FROM translations WHERE key=$1 AND locale=$2`, key, locale).Scan(&text)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("store: get translation: %w", err)
	}
	return text, true, nil
}

// AllTranslations loads the whole table into memory — the POC's translation
// set is tiny, so one map beats a query per label on every page render.
func (s *Store) AllTranslations(ctx context.Context) (map[string]map[string]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT key, locale, text FROM translations`)
	if err != nil {
		return nil, fmt.Errorf("store: list translations: %w", err)
	}
	defer rows.Close()
	out := map[string]map[string]string{}
	for rows.Next() {
		var key, locale, text string
		if err := rows.Scan(&key, &locale, &text); err != nil {
			return nil, fmt.Errorf("store: scan translation: %w", err)
		}
		if out[key] == nil {
			out[key] = map[string]string{}
		}
		out[key][locale] = text
	}
	return out, rows.Err()
}
