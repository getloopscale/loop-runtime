// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package s3 provides an S3-compatible object uploader for DigitalOcean Spaces.
package s3

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
func New(endpoint, accessKey, secretKey string) *Uploader {
	return &Uploader{
		endpoint:  strings.TrimRight(endpoint, "/"),
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
