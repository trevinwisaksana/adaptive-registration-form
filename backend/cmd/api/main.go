// Command api is the adaptive registration form backend (docs/contract.md,
// plan.md). It runs migrations, seeds demo data, and serves the REST API
// plus the static web/ renderer.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"adaptive-registration-form/backend/internal/dbx"
	"adaptive-registration-form/backend/internal/engine"
	"adaptive-registration-form/backend/internal/httpapi"
	"adaptive-registration-form/backend/internal/media"
	"adaptive-registration-form/backend/internal/ratelimit"
	"adaptive-registration-form/backend/internal/seed"
	"adaptive-registration-form/backend/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()

	dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/registration?sslmode=disable")
	pool, err := connectWithRetry(ctx, dsn, 20, 1*time.Second)
	if err != nil {
		log.Fatalf("main: connect to postgres: %v", err)
	}
	defer pool.Close()

	migrationsDir := getenv("MIGRATIONS_DIR", "./migrations")
	if err := dbx.Migrate(ctx, pool, migrationsDir); err != nil {
		log.Fatalf("main: migrate: %v", err)
	}

	st := store.New(pool)

	seedDir := getenv("SEED_DIR", "../seed")
	if err := seed.All(ctx, st, seedDir); err != nil {
		log.Fatalf("main: seed: %v", err)
	}
	log.Printf("main: seed loaded from %s", seedDir)

	mediaCfg := media.Config{
		Endpoint:       getenv("MINIO_ENDPOINT", "localhost:9000"),
		PublicEndpoint: getenv("MINIO_PUBLIC_ENDPOINT", ""),
		AccessKey:      getenv("MINIO_ACCESS_KEY", "minioadmin"),
		SecretKey:      getenv("MINIO_SECRET_KEY", "minioadmin"),
		Bucket:         getenv("MINIO_BUCKET", "registration"),
		UseSSL:         getenv("MINIO_USE_SSL", "false") == "true",
		LocalDir:       getenv("UPLOADS_DIR", "./uploads"),
		BaseURL:        getenv("BASE_URL", "http://localhost:8080"),
	}
	mediaSvc := media.New(mediaCfg)
	if mediaSvc.UsingMinIO() {
		log.Printf("main: uploads backed by MinIO at %s", mediaCfg.Endpoint)
	} else {
		log.Printf("main: MinIO unreachable at %s — falling back to local-disk uploads at %s", mediaCfg.Endpoint, mediaCfg.LocalDir)
	}

	eng := engine.New(st, mediaSvc, engine.Config{
		SessionTTL:    parseDuration(getenv("SESSION_TTL", "720h")), // 30 days
		DocumentTTL:   parseDuration(getenv("DOCUMENT_TTL", "720h")),
		KYCDelay:      parseDuration(getenv("KYC_DELAY", "10s")),
		BaseURL:       getenv("BASE_URL", "http://localhost:8080"),
		WebhookSecret: getenv("WEBHOOK_SECRET", "dev-webhook-secret"), // TODO(prod): per-vendor signing keys, real secret management (KMS)
	})
	if err := eng.LoadTranslations(ctx); err != nil {
		log.Fatalf("main: load translations: %v", err)
	}

	server := &httpapi.Server{
		Engine: eng,
		// Rate limits are config, not code (plan.md §5) — POC values below.
		SessionLimiter: ratelimit.New(5, 24*time.Hour),   // ~5 sessions/day/device
		SubmitLimiter:  ratelimit.New(30, 1*time.Minute), // ~30 writes/min/session
		RefdataLimiter: ratelimit.New(60, 1*time.Minute), // ~60/min/session
		WebDir:         getenv("WEB_DIR", "../web"),
	}

	addr := ":" + getenv("PORT", "8080")
	log.Printf("main: listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		log.Fatalf("main: serve: %v", err)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Fatalf("main: bad duration %q: %v", s, err)
	}
	return d
}

// connectWithRetry waits for Postgres to accept connections — docker-compose
// starts the API and the database together, so the DB may not be ready yet
// on the first attempt.
func connectWithRetry(ctx context.Context, dsn string, attempts int, delay time.Duration) (*pgxpool.Pool, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		pool, err := dbx.Connect(ctx, dsn)
		if err == nil {
			return pool, nil
		}
		lastErr = err
		log.Printf("main: postgres not ready yet (%d/%d): %v", i+1, attempts, err)
		time.Sleep(delay)
	}
	return nil, lastErr
}
