// Package engine is the state machine: it walks a flow definition against a
// session's stored answers, resolves the next step (localized, refdata- and
// cross-page-condition-resolved), computes progress, and reconciles on
// resume/edit to produce repairs (docs/contract.md §4, plan.md §4).
package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"adaptive-registration-form/backend/internal/flowdef"
	"adaptive-registration-form/backend/internal/media"
	"adaptive-registration-form/backend/internal/store"
)

const (
	DefaultLocale = "en-US"
	FlowKey       = "retail_onboarding"
)

// Config holds the tunables that would be "config, not code" in production
// (plan.md §5, rate-limits section).
type Config struct {
	SessionTTL    time.Duration
	DocumentTTL   time.Duration // e.g. selfie/id_card expire after this and must be redone
	KYCDelay      time.Duration // mock vendor "processing" delay before the webhook fires
	BaseURL       string        // this server's own base URL, used for legal doc URLs and the KYC self-webhook
	WebhookSecret string        // HMAC secret for the mock vendor webhook, TODO(prod): per-vendor key management
}

type Engine struct {
	Store        *store.Store
	Media        *media.Service
	Config       Config
	trCache      map[string]map[string]string // key -> locale -> text, loaded at startup
	knownLocales []string                      // BCP-47 tags seen in the translations table, e.g. "id-ID"
}

func New(s *store.Store, m *media.Service, cfg Config) *Engine {
	return &Engine{Store: s, Media: m, Config: cfg, trCache: map[string]map[string]string{}}
}

// LoadTranslations (re)loads the full translations table into memory. Called
// at startup after seeding; the POC's translation set is tiny.
func (e *Engine) LoadTranslations(ctx context.Context) error {
	all, err := e.Store.AllTranslations(ctx)
	if err != nil {
		return fmt.Errorf("engine: load translations: %w", err)
	}
	e.trCache = all

	seen := map[string]bool{}
	locales := []string{}
	for _, byLocale := range all {
		for locale := range byLocale {
			if !seen[locale] {
				seen[locale] = true
				locales = append(locales, locale)
			}
		}
	}
	e.knownLocales = locales
	return nil
}

// NormalizeLocale maps a client-supplied locale (e.g. bare language code
// "id", or a different case/region than we store) onto the closest known
// BCP-47 tag in the translations table (e.g. "id-ID"). Clients only ever
// need to send a language code; the server owns the canonical tag, same
// "server resolves, client stays dumb" pattern as everything else
// (plan.md §2.1 Localization). Unknown locales pass through unchanged and
// fall back to DefaultLocale at lookup time (tr, refdata labels, legal docs).
func (e *Engine) NormalizeLocale(locale string) string {
	if locale == "" {
		return DefaultLocale
	}
	for _, known := range e.knownLocales {
		if strings.EqualFold(known, locale) {
			return known
		}
	}
	lang := strings.SplitN(locale, "-", 2)[0]
	for _, known := range e.knownLocales {
		if strings.EqualFold(strings.SplitN(known, "-", 2)[0], lang) {
			return known
		}
	}
	return locale
}

// tr resolves a translation key for a locale, falling back to DefaultLocale
// and finally to the raw key (so a missing string is visible, not blank —
// plan.md §2.1 "runtime gaps fall back to the default locale and log").
func (e *Engine) tr(key, locale string) string {
	if key == "" {
		return ""
	}
	if byLocale, ok := e.trCache[key]; ok {
		if text, ok := byLocale[locale]; ok {
			return text
		}
		if text, ok := byLocale[DefaultLocale]; ok {
			return text
		}
	}
	return key
}

// buildAnswers assembles the cross-page "answers.<step>.<field>" view from
// every stored submission's payload (form pages, plus document/camera
// payloads for completeness — expressions in this POC only ever reference
// form fields, but nothing stops a future flow from checking e.g. whether a
// camera step was completed).
func buildAnswers(subs map[string]store.StepSubmission) flowdef.Answers {
	answers := flowdef.Answers{}
	for stepID, sub := range subs {
		if sub.Status == "invalidated" {
			continue
		}
		answers[stepID] = sub.Payload
	}
	return answers
}
