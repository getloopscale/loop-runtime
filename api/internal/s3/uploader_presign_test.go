// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package s3

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Sprint 6 / T1.2 — presigned URL contract tests.

func TestUploader_Presign_HappyPath(t *testing.T) {
	u := New("https://nyc3.digitaloceanspaces.com", "AKID", "SECRET")
	signed, err := u.Presign(context.Background(), "loopzero-static-assets",
		"sbom/tn-1/proj/abc/api.cdx.json", time.Hour)
	if err != nil {
		t.Fatalf("Presign: %v", err)
	}

	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("invalid URL: %v", err)
	}

	if parsed.Host != "nyc3.digitaloceanspaces.com" {
		t.Errorf("host = %q", parsed.Host)
	}
	if parsed.Path != "/loopzero-static-assets/sbom/tn-1/proj/abc/api.cdx.json" {
		t.Errorf("path = %q", parsed.Path)
	}

	q := parsed.Query()
	for _, key := range []string{
		"X-Amz-Algorithm", "X-Amz-Credential", "X-Amz-Date",
		"X-Amz-Expires", "X-Amz-SignedHeaders", "X-Amz-Signature",
	} {
		if q.Get(key) == "" {
			t.Errorf("missing required query parameter %q", key)
		}
	}
	if q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
		t.Errorf("Algorithm = %q", q.Get("X-Amz-Algorithm"))
	}
	if q.Get("X-Amz-Expires") != "3600" {
		t.Errorf("Expires = %q (1h = 3600s)", q.Get("X-Amz-Expires"))
	}
}

func TestUploader_Presign_RejectsEmptyInputs(t *testing.T) {
	u := New("https://example.com", "AKID", "SECRET")

	_, err := u.Presign(context.Background(), "", "key", time.Hour)
	if err == nil {
		t.Error("expected error for empty bucket")
	}
	_, err = u.Presign(context.Background(), "bucket", "", time.Hour)
	if err == nil {
		t.Error("expected error for empty key")
	}
}

// TTL clamping per S3 spec: 1s ≤ TTL ≤ 7d. Below 1s defaults to 1h.
func TestUploader_Presign_ClampsTTL(t *testing.T) {
	u := New("https://example.com", "AKID", "SECRET")

	// Below 1s defaults to 1h.
	signed, _ := u.Presign(context.Background(), "b", "k", time.Millisecond)
	q, _ := url.ParseQuery(strings.SplitN(signed, "?", 2)[1])
	if q.Get("X-Amz-Expires") != "3600" {
		t.Errorf("expected 3600 default, got %s", q.Get("X-Amz-Expires"))
	}

	// Above 7d clamps to 7d (604800s).
	signed, _ = u.Presign(context.Background(), "b", "k", 30*24*time.Hour)
	q, _ = url.ParseQuery(strings.SplitN(signed, "?", 2)[1])
	if q.Get("X-Amz-Expires") != "604800" {
		t.Errorf("expected clamp to 604800 (7d), got %s", q.Get("X-Amz-Expires"))
	}
}

// Different secrets must produce different signatures (no shared cache).
func TestUploader_Presign_DifferentSecretsDifferentSigs(t *testing.T) {
	u1 := New("https://example.com", "AKID", "SECRET-1")
	u2 := New("https://example.com", "AKID", "SECRET-2")

	s1, _ := u1.Presign(context.Background(), "b", "k", time.Hour)
	s2, _ := u2.Presign(context.Background(), "b", "k", time.Hour)

	q1, _ := url.ParseQuery(strings.SplitN(s1, "?", 2)[1])
	q2, _ := url.ParseQuery(strings.SplitN(s2, "?", 2)[1])

	if q1.Get("X-Amz-Signature") == q2.Get("X-Amz-Signature") {
		t.Error("different secrets must produce different signatures")
	}
}
