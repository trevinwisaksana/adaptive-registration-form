package store

import (
	"context"
	"fmt"
	"time"
)

func (s *Store) UpsertAnnouncement(ctx context.Context, a Announcement, active bool, startsAt, endsAt *time.Time) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO announcements (id, severity, scope, status_override, retry_after, active, starts_at, ends_at, text_by_locale)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET severity=EXCLUDED.severity, scope=EXCLUDED.scope, status_override=EXCLUDED.status_override,
			retry_after=EXCLUDED.retry_after, active=EXCLUDED.active, starts_at=EXCLUDED.starts_at, ends_at=EXCLUDED.ends_at,
			text_by_locale=EXCLUDED.text_by_locale`,
		a.ID, a.Severity, a.Scope, a.StatusOverride, a.RetryAfter, active, startsAt, endsAt, a.TextByLocale)
	if err != nil {
		return fmt.Errorf("store: upsert announcement: %w", err)
	}
	return nil
}

// ListActiveAnnouncements returns announcements currently in their active
// window — ops flips `active` (or the window) to push a banner or flip
// maintenance mode with zero app release, per plan.md §3.1.
func (s *Store) ListActiveAnnouncements(ctx context.Context, now time.Time) ([]Announcement, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, severity, scope, status_override, retry_after, text_by_locale FROM announcements
		WHERE active=true AND (starts_at IS NULL OR starts_at <= $1) AND (ends_at IS NULL OR ends_at >= $1)
		ORDER BY id`, now)
	if err != nil {
		return nil, fmt.Errorf("store: list announcements: %w", err)
	}
	defer rows.Close()

	var out []Announcement
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(&a.ID, &a.Severity, &a.Scope, &a.StatusOverride, &a.RetryAfter, &a.TextByLocale); err != nil {
			return nil, fmt.Errorf("store: scan announcement: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
