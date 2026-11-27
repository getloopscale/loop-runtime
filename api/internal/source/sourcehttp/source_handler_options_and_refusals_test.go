// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package sourcehttp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// =============================================================================
// SourceHandler — construction and options
// =============================================================================

func TestNewSourceHandler_DefaultBucket(t *testing.T) {
	h := NewSourceHandler(nil)
	if h.sourceBucket != "loop-runtime-builds" {
		t.Errorf("expected default bucket 'loop-runtime-builds', got %q", h.sourceBucket)
	}
	if h.objStore != nil {
		t.Error("expected nil objStore by default")
	}
}

func TestWithSourceBucket_OverridesDefault(t *testing.T) {
	h := NewSourceHandler(nil, WithSourceBucket("custom-bucket"))
	if h.sourceBucket != "custom-bucket" {
		t.Errorf("expected 'custom-bucket', got %q", h.sourceBucket)
	}
}

func TestWithSourceBucket_EmptyKeepsDefault(t *testing.T) {
	h := NewSourceHandler(nil, WithSourceBucket(""))
	if h.sourceBucket != "loop-runtime-builds" {
		t.Errorf("expected default bucket when empty string, got %q", h.sourceBucket)
	}
}

type fakeObjUploader struct {
	err error
}

func (f *fakeObjUploader) Upload(_ context.Context, _, _ string, _ io.Reader) error {
	return f.err
}

func TestWithObjectUploader_SetsStore(t *testing.T) {
	uploader := &fakeObjUploader{}
	h := NewSourceHandler(nil, WithObjectUploader(uploader))
	if h.objStore == nil {
		t.Error("expected non-nil objStore")
	}
}

func TestSourceHandler_Upload_Unauthenticated(t *testing.T) {
	h := NewSourceHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", nil)
	w := httptest.NewRecorder()

	h.Upload(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSourceHandler_Upload_ContentTooLarge(t *testing.T) {
	h := NewSourceHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", nil)
	req.ContentLength = maxBundleSize + 1
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Upload(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSourceHandler_Upload_InvalidMultipart(t *testing.T) {
	h := NewSourceHandler(nil)
	body := bytes.NewBufferString("not-valid-multipart")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=invalid")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Upload(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// Rollback — isLockTimeout
// =============================================================================

// =============================================================================
// Rollback — edge cases
// =============================================================================
