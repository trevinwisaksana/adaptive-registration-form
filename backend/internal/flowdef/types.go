// Package flowdef holds the flow-definition JSON schema (plan.md §2, contract
// §5) and pure logic over it: step ordering with branching, conditional
// expression evaluation, and field validation. No I/O in this package.
package flowdef

// Definition is a single versioned flow document, as stored in
// flow_versions.definition.
type Definition struct {
	Flow        string       `json:"flow"`
	Version     int          `json:"version"`
	Steps       []Step       `json:"steps"`
	Transitions []Transition `json:"transitions"`
	OnComplete  []OnComplete `json:"on_complete"`
}

// Step is one page/screen in the flow. Which fields are meaningful depends on
// Type: form uses Fields, camera uses Capture, document uses Doc, external
// uses Adapter. signature and pin use only ID/Type/TitleKey.
type Step struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"` // form|camera|signature|document|pin|external
	TitleKey string  `json:"title_key,omitempty"`
	Fields   []Field `json:"fields,omitempty"`
	Capture  string  `json:"capture,omitempty"` // camera: id_card|selfie
	Doc      string  `json:"doc,omitempty"`     // document: legal_docs.kind
	Adapter  string  `json:"adapter,omitempty"` // external
}

// Field is one form field definition (contract §3.1).
type Field struct {
	Key          string    `json:"key"`
	Kind         string    `json:"kind"` // text|date|select|multiselect|money|bool
	LabelKey     string    `json:"label_key"`
	Required     bool      `json:"required,omitempty"`
	RequiredWhen string    `json:"required_when,omitempty"`
	VisibleWhen  string    `json:"visible_when,omitempty"`
	OptionsRef   string    `json:"options_ref,omitempty"`
	FilterBy     *FilterBy `json:"filter_by,omitempty"`
	Rules        []string  `json:"rules,omitempty"`
	SinceVersion int       `json:"since_version,omitempty"`
	NoDraft      bool      `json:"no_draft,omitempty"`
}

type FilterBy struct {
	Parent string `json:"parent"`
}

// Transition inserts extra steps when a condition over earlier answers holds
// (contract §5, the FATCA branch is the worked example).
type Transition struct {
	From   string   `json:"from"`
	When   string   `json:"when"`
	Insert []string `json:"insert"`
	Before string   `json:"before,omitempty"`
	After  string   `json:"after,omitempty"`
}

type OnComplete struct {
	Adapter string `json:"adapter"`
}

// StepByID returns the step definition with the given id, if present.
func (d *Definition) StepByID(id string) (Step, bool) {
	for _, s := range d.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

// AllStepIDs returns every step id that could ever appear for this
// definition — base steps plus anything transitions might insert — used to
// validate that a submitted step_id belongs to the flow at all.
func (d *Definition) AllStepIDs() map[string]bool {
	ids := map[string]bool{}
	for _, s := range d.Steps {
		ids[s.ID] = true
	}
	for _, t := range d.Transitions {
		for _, id := range t.Insert {
			ids[id] = true
		}
	}
	return ids
}
