package media

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Hand-rolled AWS Signature Version 4 (query-string / presigned-URL variant)
// against a MinIO endpoint. MinIO speaks the S3 API, so this is the standard
// SigV4 presign algorithm — no vendor SDK needed for a POC. Region is fixed
// to "us-east-1", which MinIO accepts regardless of its own configured
// region for presigned URLs in default setups.
const sigV4Region = "us-east-1"
const sigV4Service = "s3"

func (s *Service) scheme() string {
	if s.cfg.UseSSL {
		return "https"
	}
	return "http"
}

// presignedURL builds a SigV4 presigned URL for method against objectKey,
// valid for ttl. host is the endpoint the *caller of the URL* will connect
// to — it is baked into the signature via the Host header, so it must match
// exactly (public endpoint for client-facing slots, internal for own fetches).
func (s *Service) presignedURL(method, objectKey string, ttl time.Duration, host string) (string, error) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, sigV4Region, sigV4Service)
	credential := s.cfg.AccessKey + "/" + credentialScope

	canonicalURI := "/" + s.cfg.Bucket + "/" + encodePath(objectKey)

	query := url.Values{}
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", credential)
	query.Set("X-Amz-Date", amzDate)
	query.Set("X-Amz-Expires", strconv.Itoa(int(ttl.Seconds())))
	query.Set("X-Amz-SignedHeaders", "host")
	canonicalQuery := canonicalQueryString(query)

	canonicalHeaders := "host:" + host + "\n"
	signedHeaders := "host"
	payloadHash := "UNSIGNED-PAYLOAD"

	canonicalRequest := strings.Join([]string{
		method, canonicalURI, canonicalQuery, canonicalHeaders, signedHeaders, payloadHash,
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, credentialScope, hashHex(canonicalRequest),
	}, "\n")

	signingKey := deriveSigningKey(s.cfg.SecretKey, dateStamp, sigV4Region, sigV4Service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	return fmt.Sprintf("%s://%s%s?%s&X-Amz-Signature=%s", s.scheme(), host, canonicalURI, canonicalQuery, signature), nil
}

func (s *Service) presignMinIO(objectKey, contentType string, ttl time.Duration) (UploadSlot, error) {
	u, err := s.presignedURL(http.MethodPut, objectKey, ttl, s.publicEndpoint())
	if err != nil {
		return UploadSlot{}, err
	}
	return UploadSlot{
		URL:       u,
		Method:    http.MethodPut,
		Headers:   map[string]string{"Content-Type": contentType},
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

func (s *Service) fetchMinIO(ctx context.Context, objectKey string) ([]byte, string, error) {
	u, err := s.presignedURL(http.MethodGet, objectKey, 30*time.Second, s.cfg.Endpoint)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("media: fetch from minio: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("media: minio fetch %s: %d %s", objectKey, resp.StatusCode, string(body))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func canonicalQueryString(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, awsEscape(k)+"="+awsEscape(v.Get(k)))
	}
	return strings.Join(parts, "&")
}

// awsEscape is RFC 3986 escaping as SigV4 requires (Go's url.QueryEscape
// encodes space as "+", which SigV4 rejects — it wants %20).
func awsEscape(s string) string {
	escaped := url.QueryEscape(s)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	return escaped
}

// encodePath percent-encodes each path segment but keeps the "/" separators,
// as S3's canonical URI requires.
func encodePath(objectKey string) string {
	segments := strings.Split(objectKey, "/")
	for i, seg := range segments {
		segments[i] = awsEscape(seg)
	}
	return strings.Join(segments, "/")
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func deriveSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}
