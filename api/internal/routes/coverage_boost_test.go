// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// =============================================================================
// SourceHandler — construction and options
// =============================================================================

func TestNewSourceHandler_DefaultBucket(t *testing.T) {
	h := NewSourceHandler(nil)
	if h.sourceBucket != "loopzero-builds" {
		t.Errorf("expected default bucket 'loopzero-builds', got %q", h.sourceBucket)
	}
	if h.isProduction {
		t.Error("expected isProduction=false by default")
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

func TestWithProductionMode_SetsFlag(t *testing.T) {
	h := NewSourceHandler(nil, WithProductionMode())
	if !h.isProduction {
		t.Error("expected isProduction=true")
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
		&fakeRollbackProjectStore{project: &ProjectResult{ID: "p1", Name: "my-api"}},
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
		&fakeRollbackProjectStore{project: &ProjectResult{ID: "p1", Name: "my-api"}},
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

// =============================================================================
// EnvironmentHandler — additional error paths
// =============================================================================

func TestEnvs_List_ServiceError(t *testing.T) {
	mgr := &fakeEnvMgr{listErr: errors.New("list failed")}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestEnvs_Create_Unauthenticated(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	body := `{"name":"staging"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestEnvs_Create_InvalidJSON(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString("not-json"))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestEnvs_Create_ServiceError(t *testing.T) {
	mgr := &fakeEnvMgr{addErr: errors.New("add failed")}
	h := NewEnvironmentHandler(mgr)

	body := `{"name":"staging"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestEnvs_Get_Unauthenticated(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/prod", nil)
	req = withChiParam(req, "name", "prod")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestEnvs_Get_MissingName(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestEnvs_Delete_Unauthenticated(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging", nil)
	req = withChiParam(req, "name", "staging")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestEnvs_Delete_MissingName(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestEnvs_Delete_ServiceError(t *testing.T) {
	mgr := &fakeEnvMgr{removeErr: errors.New("remove failed")}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "staging")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

// =============================================================================
// StubEnvironmentManager
// =============================================================================

func TestStubEnvMgr_ListEnvironments(t *testing.T) {
	stub := &StubEnvironmentManager{}
	envs, err := stub.ListEnvironments(context.Background(), "tn-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(envs) != 1 || envs[0].Name != "production" {
		t.Errorf("expected [production], got %v", envs)
	}
}

func TestStubEnvMgr_AddEnvironment(t *testing.T) {
	stub := &StubEnvironmentManager{}
	if err := stub.AddEnvironment(context.Background(), "tn-test", "staging"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStubEnvMgr_RemoveEnvironment(t *testing.T) {
	stub := &StubEnvironmentManager{}
	if err := stub.RemoveEnvironment(context.Background(), "tn-test", "staging"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStubEnvMgr_GetEnvironmentDetail_Found(t *testing.T) {
	stub := &StubEnvironmentManager{}
	detail, err := stub.GetEnvironmentDetail(context.Background(), "tn-test", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail == nil {
		t.Fatal("expected non-nil detail for production")
	}
	if detail.Name != "production" {
		t.Errorf("expected 'production', got %q", detail.Name)
	}
}

func TestStubEnvMgr_GetEnvironmentDetail_NotFound(t *testing.T) {
	stub := &StubEnvironmentManager{}
	detail, err := stub.GetEnvironmentDetail(context.Background(), "tn-test", "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail != nil {
		t.Errorf("expected nil for non-production env, got %v", detail)
	}
}

// =============================================================================
// HTTPServiceChecker
// =============================================================================

func TestHTTPServiceChecker_HealthyService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPServiceChecker("test-svc", server.URL, "test-token", server.Client())
	if checker.Name() != "test-svc" {
		t.Errorf("expected name 'test-svc', got %q", checker.Name())
	}

	if err := checker.Check(context.Background()); err != nil {
		t.Fatalf("expected healthy, got error: %v", err)
	}
}

func TestHTTPServiceChecker_AuthFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	checker := NewHTTPServiceChecker("test-svc", server.URL, "bad-token", server.Client())
	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("auth_failed")) {
		t.Errorf("expected 'auth_failed' in error, got: %v", err)
	}
}

func TestHTTPServiceChecker_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := NewHTTPServiceChecker("test-svc", server.URL, "", server.Client())
	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestHTTPServiceChecker_NilClient(t *testing.T) {
	checker := NewHTTPServiceChecker("test-svc", "http://localhost:1/test", "", nil)
	if checker.client == nil {
		t.Error("expected default client when nil passed")
	}
}

func TestHTTPServiceChecker_ForbiddenIsAuthFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	checker := NewHTTPServiceChecker("test-svc", server.URL, "token", server.Client())
	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("auth_failed")) {
		t.Errorf("expected 'auth_failed' in error for 403, got: %v", err)
	}
}

// =============================================================================
// SecretHandler — construction and setters
// =============================================================================

type fakeSecretStore2 struct{}

func (f *fakeSecretStore2) ListNames(_ context.Context, _, _, _ string) ([]SecretMetadata, error) {
	return nil, nil
}
func (f *fakeSecretStore2) Set(_ context.Context, _, _, _, _ string, _ []byte) (bool, error) {
	return true, nil
}
func (f *fakeSecretStore2) Delete(_ context.Context, _, _, _, _, _ string) (bool, error) {
	return true, nil
}

func TestNewSecretHandler_CreatesHandler(t *testing.T) {
	h := NewSecretHandler(&fakeSecretStore2{})
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestSecretHandler_SetAuditLogger(t *testing.T) {
	h := NewSecretHandler(&fakeSecretStore2{})
	h.SetAuditLogger(nil)
}

func TestSecretHandler_SetK8sSync(t *testing.T) {
	h := NewSecretHandler(&fakeSecretStore2{})
	h.SetK8sSync(nil)
}

func TestSecretHandler_List_Unauthenticated(t *testing.T) {
	h := NewSecretHandler(&fakeSecretStore2{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSecretHandler_Set_Unauthenticated(t *testing.T) {
	h := NewSecretHandler(&fakeSecretStore2{})
	body := `{"name":"DB_URL","value":"postgres://..."}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Set(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSecretHandler_Delete_Unauthenticated(t *testing.T) {
	h := NewSecretHandler(&fakeSecretStore2{})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/DB_URL", nil)
	w := httptest.NewRecorder()

	h.DeleteSecret(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}
