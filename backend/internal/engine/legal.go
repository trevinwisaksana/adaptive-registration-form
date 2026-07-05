package engine

import (
	"context"
	"fmt"

	"adaptive-registration-form/backend/internal/store"
)

// GetLegalDoc implements GET /legal/{kind}/{version}?locale= (contract §2.6):
// immutable per (kind, version, locale), no auth required.
func (e *Engine) GetLegalDoc(ctx context.Context, kind, version, locale string) (store.LegalDoc, error) {
	locale = e.NormalizeLocale(locale)
	doc, ok, err := e.Store.GetLegalDoc(ctx, kind, version, locale)
	if err != nil {
		return store.LegalDoc{}, err
	}
	if !ok {
		return store.LegalDoc{}, fmt.Errorf("engine: no legal doc %s/%s/%s", kind, version, locale)
	}
	return doc, nil
}
