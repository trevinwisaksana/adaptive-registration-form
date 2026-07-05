package engine

import (
	"context"
	"fmt"
)

// RefDataResponse mirrors contract §2.5.
type RefDataResponse struct {
	Dataset string        `json:"dataset"`
	Version int           `json:"version"`
	Items   []RefDataItem `json:"items"`
}

type RefDataItem struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// GetRefData implements GET /refdata/{dataset}?parent=&q= (contract §2.5).
// Locale-aware labels, codes stored/returned unchanged (contract §2.1: "codes
// not labels are stored").
func (e *Engine) GetRefData(ctx context.Context, dataset, parent, q, locale string) (RefDataResponse, error) {
	version, err := e.Store.GetDatasetVersion(ctx, dataset)
	if err != nil {
		return RefDataResponse{}, fmt.Errorf("engine: unknown dataset %q: %w", dataset, err)
	}
	items, err := e.Store.ListRefItems(ctx, dataset, parent, q)
	if err != nil {
		return RefDataResponse{}, err
	}
	out := RefDataResponse{Dataset: dataset, Version: version, Items: make([]RefDataItem, 0, len(items))}
	for _, it := range items {
		label := it.Labels[locale]
		if label == "" {
			label = it.Labels[DefaultLocale]
		}
		if label == "" {
			label = it.Code
		}
		out.Items = append(out.Items, RefDataItem{Code: it.Code, Label: label})
	}
	return out, nil
}
