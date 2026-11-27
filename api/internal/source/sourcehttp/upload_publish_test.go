// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package sourcehttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/source"
)

// Basis paths of the publish half of Upload: what reaches object storage,
// and how each failure after validation is answered.

// pushingStore returns a fixed result from PushBundle.
type pushingStore struct {
	result source.PushResult
}

func (p *pushingStore) PushBundle(_ context.Context, _, _ string, bundle io.Reader) (source.PushResult, error) {
	_, _ = io.Copy(io.Discard, bundle)
	return p.result, nil
}

type recordingUploader struct {
	bucket, objectName, body string
	err                      error
}

func (u *recordingUploader) Upload(_ context.Context, bucket, objectName string, data io.Reader) error {
	b, _ := io.ReadAll(data)
	u.bucket, u.objectName, u.body = bucket, objectName, string(b)
	return u.err
}

func uploadCode(t *testing.T, h *SourceHandler) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Upload(rec, buildValidUploadRequest(t))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestUpload_UntypedPushFailureAnswersSourceUploadFailed(t *testing.T) {
	h := NewSourceHandler(&fakeFixedErrSourceStore{err: errors.New("disk full")})
	code, body := uploadCode(t, h)
	if code != http.StatusInternalServerError || body["code"] != "SOURCE_UPLOAD_FAILED" {
		t.Fatalf("want 500 SOURCE_UPLOAD_FAILED, got %d %v", code, body)
	}
}

func TestUpload_WithoutObjectStoreAnswersTheCommitAndApps(t *testing.T) {
	h := NewSourceHandler(&pushingStore{result: source.PushResult{SourceID: "sha-1"}})
	code, body := uploadCode(t, h)
	if code != http.StatusOK || body["commit_sha"] != "sha-1" {
		t.Fatalf("want 200 with commit sha-1, got %d %v", code, body)
	}
}

func TestUpload_GitBundleUploadsTheOriginalBundleUnderTheTenantPath(t *testing.T) {
	up := &recordingUploader{}
	h := NewSourceHandler(&pushingStore{result: source.PushResult{SourceID: "sha-1"}},
		WithObjectUploader(up), WithSourceBucket("bkt"))
	code, _ := uploadCode(t, h)
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
	if up.bucket != "bkt" || up.objectName != "tn-test-123/source/sha-1.bundle" || up.body == "" {
		t.Fatalf("unexpected object store write: bucket=%q object=%q bytes=%d", up.bucket, up.objectName, len(up.body))
	}
}

func TestUpload_TarGzUploadsTheGitBundleThePushProduced(t *testing.T) {
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "converted.bundle")
	if err := os.WriteFile(bundlePath, []byte("GIT-BUNDLE"), 0o600); err != nil {
		t.Fatal(err)
	}
	up := &recordingUploader{}
	h := NewSourceHandler(&pushingStore{result: source.PushResult{SourceID: "ch-9", GitBundlePath: bundlePath}},
		WithObjectUploader(up))
	code, _ := uploadCode(t, h)
	if code != http.StatusOK || up.body != "GIT-BUNDLE" {
		t.Fatalf("want 200 and the converted bundle uploaded, got %d body=%q", code, up.body)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the converted bundle's directory must be cleaned up, stat err=%v", err)
	}
}

func TestUpload_MissingConvertedBundleAnswersInternalError(t *testing.T) {
	h := NewSourceHandler(&pushingStore{result: source.PushResult{SourceID: "ch-9",
		GitBundlePath: filepath.Join(t.TempDir(), "gone", "x.bundle")}}, WithObjectUploader(&recordingUploader{}))
	code, body := uploadCode(t, h)
	if code != http.StatusInternalServerError || body["code"] != "INTERNAL_ERROR" {
		t.Fatalf("want 500 INTERNAL_ERROR, got %d %v", code, body)
	}
}

func TestUpload_ObjectStoreFailureAnswersSourceUploadFailed(t *testing.T) {
	h := NewSourceHandler(&pushingStore{result: source.PushResult{SourceID: "sha-1"}},
		WithObjectUploader(&recordingUploader{err: errors.New("503")}))
	code, body := uploadCode(t, h)
	if code != http.StatusInternalServerError || body["code"] != "SOURCE_UPLOAD_FAILED" {
		t.Fatalf("want 500 SOURCE_UPLOAD_FAILED, got %d %v", code, body)
	}
}
