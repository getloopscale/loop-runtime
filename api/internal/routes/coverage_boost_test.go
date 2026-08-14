// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/getloopscale/loopzero/api/internal/domain"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// =============================================================================
// SourceHandler — construction and options
// =============================================================================

func TestNewSourceHandler_DefaultBucket(t *testing.T) {
	h := NewSourceHandler(nil)
	if h.sourceBucket != "loopzero-builds" {
		t.Errorf("expected default bucket 'loopzero-builds', got %q", h.sourceBucket)
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
	if h.sourceBucket != "loopzero-builds" {
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
	req = withAuth(req, defaultUserContext())
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
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Upload(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// Rollback — isLockTimeout
// =============================================================================

func TestIsLockTimeout_NilError(t *testing.T) {
	if isLockTimeout(nil) {
		t.Error("nil error should not be lock timeout")
	}
}

func TestIsLockTimeout_LockTimeoutError(t *testing.T) {
	if !isLockTimeout(loopzeroerrors.ErrLockTimeout) {
		t.Error("ErrLockTimeout should be detected as lock timeout")
	}
}

func TestIsLockTimeout_StringContains(t *testing.T) {
	if !isLockTimeout(fmt.Errorf("some lock timeout occurred")) {
		t.Error("error containing 'lock timeout' should be detected")
	}
}

func TestIsLockTimeout_OtherError(t *testing.T) {
	if isLockTimeout(errors.New("something else")) {
		t.Error("unrelated error should not be lock timeout")
	}
}

// =============================================================================
// Rollback — edge cases
// =============================================================================

func TestRollback_MissingProjectName(t *testing.T) {
	h := NewRollbackHandler(&fakeRollbackProjectStore{}, &fakeRollbackService{}, "dev.loopzero.example")

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects//rollback", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRollback_InvalidBody(t *testing.T) {
	h := NewRollbackHandler(
		&fakeRollbackProjectStore{project: &domain.ProjectResult{ID: "p1", Name: "my-api"}},
		&fakeRollbackService{},
		"dev.loopzero.example",
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString("not-json"))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRollback_InternalError(t *testing.T) {
	h := NewRollbackHandler(
		&fakeRollbackProjectStore{project: &domain.ProjectResult{ID: "p1", Name: "my-api"}},
		&fakeRollbackService{err: errors.New("database exploded")},
		"dev.loopzero.example",
	)

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
}
