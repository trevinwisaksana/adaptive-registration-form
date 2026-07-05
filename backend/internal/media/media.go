// Package media issues upload slots for camera/signature captures. It
// presigns direct-to-MinIO PUT URLs (hand-rolled AWS SigV4, no SDK — the
// task scope is stdlib + pgx only) when MinIO is reachable, and falls back to
// a local-disk PUT endpoint served by this same process otherwise. Either
// way, exactly one object slot exists per (session, doc kind); re-issuing
// overwrites it (contract §2.4).
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Endpoint string // host:port, no scheme, as reachable from THIS process
	// PublicEndpoint is the host:port clients (apps, browsers) can reach MinIO
	// at — inside docker compose, Endpoint is "minio:9000" but clients need the
	// host-mapped "localhost:9000". SigV4 signs the Host header, so presigned
	// URLs must be signed for the host the client will actually connect to.
	// Empty means same as Endpoint.
	PublicEndpoint string
	AccessKey      string
	SecretKey      string
	Bucket         string
	UseSSL         bool
	LocalDir       string // fallback disk root
	BaseURL        string // this server's own base URL, for local-fallback URLs
}

func (s *Service) publicEndpoint() string {
	if s.cfg.PublicEndpoint != "" {
		return s.cfg.PublicEndpoint
	}
	return s.cfg.Endpoint
}

type Service struct {
	cfg Config

	minioOK bool // reachability probed once at startup

	mu           sync.Mutex
	localContent map[string]string // object key -> content-type, local-mode bookkeeping
}

func New(cfg Config) *Service {
	s := &Service{cfg: cfg, localContent: map[string]string{}}
	s.minioOK = probe(cfg.Endpoint)
	if err := os.MkdirAll(cfg.LocalDir, 0o755); err != nil {
		// Local fallback dir must exist for uploads to work at all.
		panic(fmt.Sprintf("media: cannot create local upload dir %s: %v", cfg.LocalDir, err))
	}
	return s
}

func probe(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", endpoint, 800*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// UsingMinIO reports which backend is active — surfaced in logs/audit only.
func (s *Service) UsingMinIO() bool { return s.minioOK }

// UploadSlot is what POST /sessions/{id}/uploads returns (contract §2.4).
type UploadSlot struct {
	URL       string
	Method    string
	Headers   map[string]string
	ExpiresAt time.Time
}

// ObjectKey is deterministic per (session, kind) — one slot per doc kind per
// session, re-issuing overwrites (contract §2.4, §2.1 upload-abuse bound).
func ObjectKey(sessionID, kind string) string {
	return sessionID + "/" + kind
}

// Presign returns a direct-PUT upload slot for objectKey, valid for ttl.
func (s *Service) Presign(objectKey, contentType string, ttl time.Duration) (UploadSlot, error) {
	if s.minioOK {
		return s.presignMinIO(objectKey, contentType, ttl)
	}
	return s.presignLocal(objectKey, contentType, ttl), nil
}

func (s *Service) presignLocal(objectKey, contentType string, ttl time.Duration) UploadSlot {
	return UploadSlot{
		URL:       s.cfg.BaseURL + "/internal/local-uploads/" + urlEscape(objectKey),
		Method:    http.MethodPut,
		Headers:   map[string]string{"Content-Type": contentType},
		ExpiresAt: time.Now().Add(ttl),
	}
}

func urlEscape(s string) string {
	return strings.ReplaceAll(s, "/", "%2F")
}
func urlUnescape(s string) string {
	return strings.ReplaceAll(s, "%2F", "/")
}

// LocalUploadHandler serves the local-disk fallback PUT endpoint. Only
// reachable if MinIO wasn't detected at startup — mounted unconditionally
// since the smoke script targets whichever backend is active.
func (s *Service) LocalUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ref := strings.TrimPrefix(r.URL.Path, "/internal/local-uploads/")
	objectKey := urlUnescape(ref)
	if objectKey == "" || strings.Contains(objectKey, "..") {
		http.Error(w, "bad object key", http.StatusBadRequest)
		return
	}
	const maxSize = 15 << 20 // 15MB cap, POC-level size guard
	body, err := io.ReadAll(io.LimitReader(r.Body, maxSize+1))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if len(body) > maxSize {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	full := filepath.Join(s.cfg.LocalDir, filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(full, body, 0o644); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	ct := r.Header.Get("Content-Type")
	s.mu.Lock()
	s.localContent[objectKey] = ct
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// Fetch retrieves the object's bytes and content-type for the cheap
// server-side checks at step-submit time (size/type/decodes-as-image;
// contract §2.4 — "nothing more").
func (s *Service) Fetch(ctx context.Context, objectKey string) ([]byte, string, error) {
	if s.minioOK {
		return s.fetchMinIO(ctx, objectKey)
	}
	full := filepath.Join(s.cfg.LocalDir, filepath.FromSlash(objectKey))
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, "", fmt.Errorf("media: object not found: %w", err)
	}
	s.mu.Lock()
	ct := s.localContent[objectKey]
	s.mu.Unlock()
	return data, ct, nil
}

// ValidateImage does the "cheap checks" from plan.md §2.1: decodes and
// returns dimensions error if not a real image. Signature uploads are PNG
// pad captures and go through the same check.
func ValidateImage(data []byte) error {
	_, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("media: does not decode as an image: %w", err)
	}
	return nil
}

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
