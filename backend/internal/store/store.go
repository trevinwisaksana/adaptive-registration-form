// Package store is the only place that talks SQL. Plain queries via pgx, no
// ORM. Every exported method takes a context and does one round trip (or a
// small transaction) — callers (internal/engine) compose these into flows.
package store

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{Pool: pool}
}

// Session mirrors the sessions table.
type Session struct {
	ID                  string
	Token               string
	FlowKey             string
	FlowVersion         int
	OriginalFlowVersion int
	Locale              string
	Status              string
	PinSet              bool
	BlockedMinVersion   *string
	DeviceID            string
	Platform            string
	AppVersion          string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ExpiresAt           time.Time
}

// StepSubmission mirrors step_submissions.
type StepSubmission struct {
	SessionID string
	StepID    string
	Payload   map[string]any
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Document mirrors documents (one row per doc kind per session).
type Document struct {
	SessionID    string
	Kind         string
	UploadRef    string
	ObjectKey    string
	ContentType  string
	SizeBytes    int64
	SHA256       string
	ReviewStatus string
	UploadedAt   *time.Time
}

// Consent mirrors consents.
type Consent struct {
	ID         string
	SessionID  string
	DocKind    string
	DocVersion string
	DocLocale  string
	DocSHA256  string
	AcceptedAt time.Time
}

// OnCompleteRun mirrors on_complete_runs.
type OnCompleteRun struct {
	SessionID  string
	Adapter    string
	Attempt    int
	Status     string
	Reason     *string
	StartedAt  time.Time
	FinishedAt *time.Time
}

// RefItem mirrors ref_items.
type RefItem struct {
	Code       string
	ParentCode *string
	Labels     map[string]string
	Active     bool
	Sort       int
}

// LegalDoc mirrors legal_docs.
type LegalDoc struct {
	Kind         string
	Version      string
	Locale       string
	SHA256       string
	ContentType  string
	Content      string
	EffectiveAt  time.Time
	Reacceptance string
}

// Announcement mirrors announcements.
type Announcement struct {
	ID             string
	Severity       string
	Scope          string
	StatusOverride string
	RetryAfter     *int
	TextByLocale   map[string]string
}
