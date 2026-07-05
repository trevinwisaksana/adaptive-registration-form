// Package seed loads the POC's demo data (flow versions, reference data,
// translations, legal docs, announcements) from the top-level seed/
// directory into Postgres at startup. Every Load* call is an upsert, so
// re-running the seeder (e.g. on every container restart) is a no-op once
// the data matches.
package seed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"adaptive-registration-form/backend/internal/flowdef"
	"adaptive-registration-form/backend/internal/store"
)

// All loads every seed file under dir into st. dir is expected to look like:
//
//	flows/*.json         one flow_versions definition per file
//	refdata.json         { "datasets": [ {key, version, items:[{code,parent_code,labels,active,sort}]} ] }
//	translations.json    { "<key>": { "<locale>": "<text>" } }
//	legal_docs.json      [ {kind, version, locale, content_type, content, effective_at, reacceptance} ]
//	announcements.json   [ {id, severity, scope, status_override, retry_after, active, starts_at, ends_at, text_by_locale} ]
func All(ctx context.Context, st *store.Store, dir string) error {
	if err := Flows(ctx, st, filepath.Join(dir, "flows")); err != nil {
		return err
	}
	if err := RefData(ctx, st, filepath.Join(dir, "refdata.json")); err != nil {
		return err
	}
	if err := Translations(ctx, st, filepath.Join(dir, "translations.json")); err != nil {
		return err
	}
	if err := LegalDocs(ctx, st, filepath.Join(dir, "legal_docs.json")); err != nil {
		return err
	}
	if err := Announcements(ctx, st, filepath.Join(dir, "announcements.json")); err != nil {
		return err
	}
	return nil
}

// Flows loads every *.json flow definition file in dir into flow_versions.
func Flows(ctx context.Context, st *store.Store, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("seed: read flows dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("seed: read %s: %w", e.Name(), err)
		}
		var def flowdef.Definition
		if err := json.Unmarshal(data, &def); err != nil {
			return fmt.Errorf("seed: parse %s: %w", e.Name(), err)
		}
		if err := st.UpsertFlowVersion(ctx, def); err != nil {
			return fmt.Errorf("seed: load %s: %w", e.Name(), err)
		}
	}
	return nil
}

type refDataFile struct {
	Datasets []struct {
		Key     string `json:"key"`
		Version int    `json:"version"`
		Items   []struct {
			Code       string            `json:"code"`
			ParentCode *string           `json:"parent_code"`
			Labels     map[string]string `json:"labels"`
			Active     *bool             `json:"active"`
			Sort       int               `json:"sort"`
		} `json:"items"`
	} `json:"datasets"`
}

// RefData loads ref_datasets + ref_items (plan.md §2.1: regions, cities,
// occupations, plus the small enum-backed datasets form fields point at).
func RefData(ctx context.Context, st *store.Store, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("seed: read %s: %w", path, err)
	}
	var f refDataFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("seed: parse %s: %w", path, err)
	}
	for _, ds := range f.Datasets {
		if err := st.UpsertRefDataset(ctx, ds.Key, ds.Version); err != nil {
			return err
		}
		for _, it := range ds.Items {
			active := true
			if it.Active != nil {
				active = *it.Active
			}
			if err := st.UpsertRefItem(ctx, ds.Key, it.Code, it.ParentCode, it.Labels, active, it.Sort); err != nil {
				return err
			}
		}
	}
	return nil
}

// Translations loads the translations table from a { key: { locale: text } } map.
func Translations(ctx context.Context, st *store.Store, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("seed: read %s: %w", path, err)
	}
	var all map[string]map[string]string
	if err := json.Unmarshal(data, &all); err != nil {
		return fmt.Errorf("seed: parse %s: %w", path, err)
	}
	for key, byLocale := range all {
		for locale, text := range byLocale {
			if err := st.UpsertTranslation(ctx, key, locale, text); err != nil {
				return err
			}
		}
	}
	return nil
}

type legalDocFile struct {
	Kind         string `json:"kind"`
	Version      string `json:"version"`
	Locale       string `json:"locale"`
	ContentType  string `json:"content_type"`
	Content      string `json:"content"`
	EffectiveAt  string `json:"effective_at"`
	Reacceptance string `json:"reacceptance"`
}

// LegalDocs loads legal_docs, computing each row's sha256 from its content —
// the seed file is the source text, never a pre-computed (and potentially
// stale) hash.
func LegalDocs(ctx context.Context, st *store.Store, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("seed: read %s: %w", path, err)
	}
	var docs []legalDocFile
	if err := json.Unmarshal(data, &docs); err != nil {
		return fmt.Errorf("seed: parse %s: %w", path, err)
	}
	for _, d := range docs {
		effectiveAt, err := time.Parse(time.RFC3339, d.EffectiveAt)
		if err != nil {
			return fmt.Errorf("seed: legal doc %s/%s/%s: bad effective_at: %w", d.Kind, d.Version, d.Locale, err)
		}
		sum := sha256.Sum256([]byte(d.Content))
		doc := store.LegalDoc{
			Kind: d.Kind, Version: d.Version, Locale: d.Locale,
			SHA256: hex.EncodeToString(sum[:]), ContentType: d.ContentType, Content: d.Content,
			EffectiveAt: effectiveAt, Reacceptance: d.Reacceptance,
		}
		if err := st.UpsertLegalDoc(ctx, doc); err != nil {
			return err
		}
	}
	return nil
}

type announcementFile struct {
	ID             string            `json:"id"`
	Severity       string            `json:"severity"`
	Scope          string            `json:"scope"`
	StatusOverride string            `json:"status_override"`
	RetryAfter     *int              `json:"retry_after"`
	Active         bool              `json:"active"`
	StartsAt       *string           `json:"starts_at"`
	EndsAt         *string           `json:"ends_at"`
	TextByLocale   map[string]string `json:"text_by_locale"`
}

// Announcements loads the announcements table (plan.md §3.1 banners +
// maintenance toggle — ops flips `active`/window, no app release needed).
func Announcements(ctx context.Context, st *store.Store, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("seed: read %s: %w", path, err)
	}
	var anns []announcementFile
	if err := json.Unmarshal(data, &anns); err != nil {
		return fmt.Errorf("seed: parse %s: %w", path, err)
	}
	for _, a := range anns {
		var starts, ends *time.Time
		if a.StartsAt != nil {
			t, err := time.Parse(time.RFC3339, *a.StartsAt)
			if err != nil {
				return fmt.Errorf("seed: announcement %s: bad starts_at: %w", a.ID, err)
			}
			starts = &t
		}
		if a.EndsAt != nil {
			t, err := time.Parse(time.RFC3339, *a.EndsAt)
			if err != nil {
				return fmt.Errorf("seed: announcement %s: bad ends_at: %w", a.ID, err)
			}
			ends = &t
		}
		ann := store.Announcement{
			ID: a.ID, Severity: a.Severity, Scope: a.Scope, StatusOverride: a.StatusOverride,
			RetryAfter: a.RetryAfter, TextByLocale: a.TextByLocale,
		}
		if err := st.UpsertAnnouncement(ctx, ann, a.Active, starts, ends); err != nil {
			return err
		}
	}
	return nil
}
