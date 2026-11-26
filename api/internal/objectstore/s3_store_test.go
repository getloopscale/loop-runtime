// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package objectstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T, endpoint, secret string) *S3Store {
	t.Helper()
	s, err := NewS3Store(endpoint, "AKID", secret)
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	return s
}

type capturedPut struct {
	method, escapedPath, auth, body string
}

func TestUploadSendsASignedPathStylePutForAKeyNeedingEncoding(t *testing.T) {
	// Arrange: a key with a space and a non-ASCII character — the case the
	// hand-rolled signer placed unencoded in the canonical request.
	var got capturedPut
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = capturedPut{method: r.Method, escapedPath: r.URL.EscapedPath(), auth: r.Header.Get("Authorization"), body: string(b)}
		w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	store := newStore(t, srv.URL, "SECRET")

	// Act
	err := store.Upload(context.Background(), "loopruntime-builds", "tn-1/source/my file é.bundle", strings.NewReader("payload")) // english-only: a non-ASCII key is the case under test

	// Assert
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if got.method != http.MethodPut {
		t.Errorf("method = %s, want PUT", got.method)
	}
	if want := "/loopruntime-builds/tn-1/source/my%20file%20%C3%A9.bundle"; got.escapedPath != want {
		t.Errorf("path = %s, want %s", got.escapedPath, want)
	}
	if !strings.HasPrefix(got.auth, "AWS4-HMAC-SHA256 Credential=AKID/") || !strings.Contains(got.auth, "/us-east-1/s3/aws4_request") {
		t.Errorf("Authorization = %q, want a SigV4 us-east-1 signature", got.auth)
	}
	if !strings.Contains(got.body, "payload") {
		t.Errorf("body = %q, does not carry the payload", got.body)
	}
}

func TestUploadReturnsTheServerRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`)
	}))
	defer srv.Close()

	err := newStore(t, srv.URL, "SECRET").Upload(context.Background(), "loopruntime-builds", "tn-1/k", strings.NewReader("x"))

	if err == nil || !strings.Contains(err.Error(), "loopruntime-builds/tn-1/k") {
		t.Fatalf("err = %v, want an error naming the object", err)
	}
}

func TestPresignProducesAPathStyleSigV4URL(t *testing.T) {
	signed, err := newStore(t, "https://nyc3.digitaloceanspaces.com", "SECRET").
		Presign(context.Background(), "loopruntime-static-assets", "sbom/tn-1/proj/abc/api.cdx.json", time.Hour)
	if err != nil {
		t.Fatalf("Presign: %v", err)
	}

	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("invalid URL: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "nyc3.digitaloceanspaces.com" {
		t.Errorf("origin = %s://%s", parsed.Scheme, parsed.Host)
	}
	if parsed.Path != "/loopruntime-static-assets/sbom/tn-1/proj/abc/api.cdx.json" {
		t.Errorf("path = %q", parsed.Path)
	}
	q := parsed.Query()
	for _, key := range []string{"X-Amz-Algorithm", "X-Amz-Credential", "X-Amz-Date", "X-Amz-Expires", "X-Amz-SignedHeaders", "X-Amz-Signature"} {
		if q.Get(key) == "" {
			t.Errorf("missing query parameter %q", key)
		}
	}
	if q.Get("X-Amz-Expires") != "3600" {
		t.Errorf("Expires = %q, want 3600", q.Get("X-Amz-Expires"))
	}
}

func TestPresignRejectsEmptyInputs(t *testing.T) {
	s := newStore(t, "https://example.com", "SECRET")
	if _, err := s.Presign(context.Background(), "", "k", time.Hour); err == nil {
		t.Error("expected an error for an empty bucket")
	}
	if _, err := s.Presign(context.Background(), "b", "", time.Hour); err == nil {
		t.Error("expected an error for an empty key")
	}
}

func TestPresignClampsTheTTL(t *testing.T) {
	s := newStore(t, "https://example.com", "SECRET")
	expires := func(ttl time.Duration) string {
		signed, err := s.Presign(context.Background(), "bucket", "key", ttl)
		if err != nil {
			t.Fatalf("Presign(%v): %v", ttl, err)
		}
		u, _ := url.Parse(signed)
		return u.Query().Get("X-Amz-Expires")
	}
	if got := expires(time.Millisecond); got != "3600" {
		t.Errorf("sub-second TTL: Expires = %s, want 3600", got)
	}
	if got := expires(30 * 24 * time.Hour); got != "604800" {
		t.Errorf("30d TTL: Expires = %s, want 604800", got)
	}
}

func TestPresignSignaturesDependOnTheSecret(t *testing.T) {
	sig := func(secret string) string {
		signed, err := newStore(t, "https://example.com", secret).Presign(context.Background(), "bucket", "key", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(signed)
		return u.Query().Get("X-Amz-Signature")
	}
	if sig("SECRET-1") == sig("SECRET-2") {
		t.Error("different secrets produced the same signature")
	}
}

func TestNewS3StoreEndpointForms(t *testing.T) {
	cases := []struct {
		endpoint string
		wantErr  bool
	}{
		{"minio.loopruntime-system.svc:9000", false},
		{"http://minio:9000/", false},
		{"https://nyc3.digitaloceanspaces.com", false},
		{"", true},
		{"ftp://host", true},
		{"https://host/prefix", true},
	}
	for _, tc := range cases {
		_, err := NewS3Store(tc.endpoint, "AKID", "SECRET")
		if (err != nil) != tc.wantErr {
			t.Errorf("NewS3Store(%q) err = %v, wantErr %v", tc.endpoint, err, tc.wantErr)
		}
	}
}

func TestUploadStreamsAReaderOfUnknownLength(t *testing.T) {
	// A reader with no known length goes through the multipart protocol; the
	// fake answers the three calls it makes.
	var parts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPost && q.Has("uploads"):
			_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><Bucket>loopruntime-builds</Bucket><Key>k</Key><UploadId>u1</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut && q.Get("uploadId") == "u1":
			b, _ := io.ReadAll(r.Body)
			parts = append(parts, string(b))
			w.Header().Set("ETag", `"e1"`)
		case r.Method == http.MethodPost && q.Get("uploadId") == "u1":
			_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><Bucket>loopruntime-builds</Bucket><Key>k</Key><ETag>"e"</ETag></CompleteMultipartUploadResult>`)
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	defer srv.Close()

	err := newStore(t, srv.URL, "SECRET").Upload(context.Background(), "loopruntime-builds", "tn-1/k", io.MultiReader(strings.NewReader("pay"), strings.NewReader("load")))

	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if len(parts) != 1 || !strings.Contains(parts[0], "payload") {
		t.Errorf("parts = %q, want one part carrying the payload", parts)
	}
}

// The SBOM download reads the object through the API, in place of handing the
// browser a presigned URL to the storage endpoint (getloopscale/loop-runtime#104).
func TestOpenReturnsTheObjectContent(t *testing.T) {
	const content = `{"bomFormat":"CycloneDX"}`
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Length", "25")
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("Last-Modified", "Wed, 25 Nov 2026 10:00:00 GMT")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(content))
		}
	}))
	defer srv.Close()
	s, err := NewS3Store(srv.URL, "ak", "sk")
	if err != nil {
		t.Fatal(err)
	}

	body, err := s.Open(context.Background(), "builds", "sbom/tn-1/api.cdx.json")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = body.Close() }()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("content = %q", got)
	}
	if gotPath != "/builds/sbom/tn-1/api.cdx.json" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestOpenFailsForAMissingObjectBeforeAnyByteIsRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`))
	}))
	defer srv.Close()
	s, err := NewS3Store(srv.URL, "ak", "sk")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Open(context.Background(), "builds", "missing.json"); err == nil {
		t.Error("expected an error for a missing object")
	}
}

func TestOpenRejectsEmptyInputs(t *testing.T) {
	s, err := NewS3Store("http://127.0.0.1:1", "ak", "sk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(context.Background(), "", "k"); err == nil {
		t.Error("expected an error for an empty bucket")
	}
	if _, err := s.Open(context.Background(), "b", ""); err == nil {
		t.Error("expected an error for an empty key")
	}
}
