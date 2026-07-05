// Package api holds the wire-format response/request shapes from
// docs/contract.md. Kept separate from internal/engine so the engine's
// internal logic isn't tangled with JSON tags.
package api

// SystemInfo is the envelope's "system" block (contract §1).
type SystemInfo struct {
	Status     string   `json:"status"` // ok|degraded|maintenance
	RetryAfter *int     `json:"retry_after"`
	Banners    []Banner `json:"banners"`
}

type Banner struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Scope    string `json:"scope"`
	Text     string `json:"text"`
}

type Progress struct {
	Completed int `json:"completed"`
	Total     int `json:"total"`
}

// Repair mirrors contract §4.
type Repair struct {
	Kind   string         `json:"kind"` // reaccept_document|collect_fields|redo_step
	StepID string         `json:"step_id"`
	Reason string         `json:"reason"`
	Detail map[string]any `json:"detail,omitempty"`
}

// Envelope is the shape wrapping every session-scoped response (contract §1).
// NextStep is left as a flexible map because its fields vary by step type.
type Envelope struct {
	System   SystemInfo     `json:"system"`
	Progress Progress       `json:"progress"`
	NextStep map[string]any `json:"next_step"`
	Repairs  []Repair       `json:"repairs"`
}

// SessionResponse is the POST /sessions response body (contract §2.1).
type SessionResponse struct {
	Session SessionInfo `json:"session"`
	Token   string      `json:"token"`
	Envelope
}

type SessionInfo struct {
	ID          string `json:"id"`
	Flow        string `json:"flow"`
	FlowVersion int    `json:"flow_version"`
	ExpiresAt   string `json:"expires_at"`
}

// SessionRequest is the POST /sessions request body.
type SessionRequest struct {
	Locale            string     `json:"locale"`
	Client            ClientInfo `json:"client"`
	DeviceAttestation string     `json:"device_attestation"`
	ResumeToken       *string    `json:"resume_token"`
}

type ClientInfo struct {
	Platform            string   `json:"platform"`
	AppVersion          string   `json:"app_version"`
	SupportedTypes      []string `json:"supported_types"`
	SupportedFieldKinds []string `json:"supported_field_kinds"`
}

// FieldError is one entry in a 422 validation_failed response (contract §2.3).
type FieldError struct {
	Key     string `json:"key"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

type ErrorDetail struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	StepID  string       `json:"step_id,omitempty"`
	Fields  []FieldError `json:"fields,omitempty"`
}

// ErrorResponse is the generic error envelope. CurrentDoc is only set for
// the stale_document 409 (contract §2.3).
type ErrorResponse struct {
	Error      ErrorDetail    `json:"error"`
	CurrentDoc map[string]any `json:"current_doc,omitempty"`
}
