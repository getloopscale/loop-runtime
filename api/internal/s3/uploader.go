// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package s3 provides an S3-compatible object uploader for DigitalOcean Spaces.
package s3

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Uploader implements routes.ObjectUploader for S3-compatible storage.
type Uploader struct {
	endpoint  string // e.g., "https://nyc3.digitaloceanspaces.com"
	accessKey string
	secretKey string
	client    *http.Client
}

// New creates a new S3 uploader.
//
// The endpoint may be supplied as a full URL ("http://host:port",
// "https://host") or as a bare host[:port]. Without a scheme the Go
// HTTP client rejects requests with an opaque "unsupported protocol
// scheme" error — defensively prepend `http://` so misconfigured
// dev environments fail with a clear upstream error instead of an
// unhelpful client-side parse error (CHG-2026-04-29-DOGFOOD iter 9).
func New(endpoint, accessKey, secretKey string) *Uploader {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint != "" && !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}
	return &Uploader{
		endpoint:  endpoint,
		accessKey: accessKey,
		secretKey: secretKey,
		client: &http.Client{
			Timeout:   60 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// Upload puts an object into the specified bucket.
func (u *Uploader) Upload(ctx context.Context, bucket, key string, data io.Reader) error {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.source.upload",
		trace.WithAttributes(
			attribute.String("loopzero.s3.bucket", bucket),
			attribute.String("loopzero.s3.key", key),
		))
	defer span.End()

	body, err := io.ReadAll(data)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "read body failed")
		return fmt.Errorf("s3: read body: %w", err)
	}
	span.SetAttributes(attribute.Int("loopzero.s3.size_bytes", len(body)))

	url := fmt.Sprintf("%s/%s/%s", u.endpoint, bucket, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("s3: create request: %w", err)
	}

	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	region := "us-east-1" // DO Spaces uses us-east-1 for SigV4
	service := "s3"

	// Simple S3 SigV4 signing
	payloadHash := sha256Hex(body)
	host := strings.TrimPrefix(u.endpoint, "https://")
	host = strings.TrimPrefix(host, "http://")

	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n", host, payloadHash, amzDate)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := fmt.Sprintf("PUT\n/%s/%s\n\n%s\n%s\n%s", bucket, key, canonicalHeaders, signedHeaders, payloadHash)

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s", amzDate, credentialScope, sha256Hex([]byte(canonicalRequest)))

	signingKey := deriveKey(u.secretKey, dateStamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		u.accessKey, credentialScope, signedHeaders, signature)

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(body))

	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("s3: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("s3: upload failed: status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Presign returns a presigned GET URL for the object (Sprint 6 / T1.2).
// TTL ranges 1s..7d (S3 max). Returns absolute URL with all query params.
//
// The URL is computed locally — no roundtrip to the origin. Callers can
// hand it to clients (CLI, dashboard) without exposing AWS credentials.
//
// Format: AWS Signature Version 4 (sigv4) with query-string parameters.
// Reference: https://docs.aws.amazon.com/AmazonS3/latest/API/sigv4-query-string-auth.html
func (u *Uploader) Presign(_ context.Context, bucket, key string, ttl time.Duration) (string, error) {
	if bucket == "" || key == "" {
		return "", fmt.Errorf("s3: bucket and key are required")
	}
	if ttl < time.Second {
		ttl = time.Hour
	}
	if ttl > 7*24*time.Hour {
		ttl = 7 * 24 * time.Hour
	}

	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	region := "us-east-1"
	service := "s3"

	host := strings.TrimPrefix(u.endpoint, "https://")
	host = strings.TrimPrefix(host, "http://")

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	credential := fmt.Sprintf("%s/%s", u.accessKey, credentialScope)
	signedHeaders := "host"

	// Query parameters in lexicographic order — required for canonical request.
	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", credential)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", int(ttl.Seconds())))
	q.Set("X-Amz-SignedHeaders", signedHeaders)

	canonicalQuery := q.Encode()
	canonicalHeaders := fmt.Sprintf("host:%s\n", host)
	canonicalRequest := fmt.Sprintf("GET\n/%s/%s\n%s\n%s\n%s\nUNSIGNED-PAYLOAD",
		bucket, key, canonicalQuery, canonicalHeaders, signedHeaders)

	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate, credentialScope, sha256Hex([]byte(canonicalRequest)))

	signingKey := deriveKey(u.secretKey, dateStamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))
	q.Set("X-Amz-Signature", signature)

	return fmt.Sprintf("%s/%s/%s?%s", u.endpoint, bucket, key, q.Encode()), nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func deriveKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}
