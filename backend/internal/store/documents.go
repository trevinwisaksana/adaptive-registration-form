package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// UpsertDocumentSlot (re)issues the single object slot for a doc kind in a
// session — re-requesting overwrites the prior slot, per contract §2.4.
func (s *Store) UpsertDocumentSlot(ctx context.Context, sessionID, kind, objectKey string) (Document, error) {
	uploadRef := "up_" + randHex(6)
	var doc Document
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO documents (session_id, kind, upload_ref, object_key, review_status)
		VALUES ($1::uuid, $2, $3, $4, 'pending')
		ON CONFLICT (session_id, kind) DO UPDATE SET upload_ref=EXCLUDED.upload_ref, object_key=EXCLUDED.object_key,
			review_status='pending', content_type=NULL, size_bytes=NULL, sha256=NULL, uploaded_at=NULL
		RETURNING session_id::text, kind, upload_ref, object_key, coalesce(content_type,''), coalesce(size_bytes,0),
			coalesce(sha256,''), review_status, uploaded_at`,
		sessionID, kind, uploadRef, objectKey).Scan(
		&doc.SessionID, &doc.Kind, &doc.UploadRef, &doc.ObjectKey, &doc.ContentType, &doc.SizeBytes,
		&doc.SHA256, &doc.ReviewStatus, &doc.UploadedAt)
	if err != nil {
		return doc, fmt.Errorf("store: upsert document slot: %w", err)
	}
	return doc, nil
}

func (s *Store) GetDocumentByKind(ctx context.Context, sessionID, kind string) (Document, bool, error) {
	return s.scanDocument(ctx, `SELECT session_id::text, kind, upload_ref, object_key, coalesce(content_type,''),
		coalesce(size_bytes,0), coalesce(sha256,''), review_status, uploaded_at
		FROM documents WHERE session_id=$1::uuid AND kind=$2`, sessionID, kind)
}

func (s *Store) GetDocumentByUploadRef(ctx context.Context, sessionID, uploadRef string) (Document, bool, error) {
	return s.scanDocument(ctx, `SELECT session_id::text, kind, upload_ref, object_key, coalesce(content_type,''),
		coalesce(size_bytes,0), coalesce(sha256,''), review_status, uploaded_at
		FROM documents WHERE session_id=$1::uuid AND upload_ref=$2`, sessionID, uploadRef)
}

func (s *Store) scanDocument(ctx context.Context, query, a, b string) (Document, bool, error) {
	var doc Document
	err := s.Pool.QueryRow(ctx, query, a, b).Scan(
		&doc.SessionID, &doc.Kind, &doc.UploadRef, &doc.ObjectKey, &doc.ContentType, &doc.SizeBytes,
		&doc.SHA256, &doc.ReviewStatus, &doc.UploadedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return doc, false, nil
		}
		return doc, false, fmt.Errorf("store: get document: %w", err)
	}
	return doc, true, nil
}

func (s *Store) MarkDocumentChecked(ctx context.Context, sessionID, kind, contentType string, size int64, sha256 string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE documents SET content_type=$1, size_bytes=$2, sha256=$3, review_status='checked', uploaded_at=now()
		WHERE session_id=$4::uuid AND kind=$5`, contentType, size, sha256, sessionID, kind)
	if err != nil {
		return fmt.Errorf("store: mark document checked: %w", err)
	}
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
