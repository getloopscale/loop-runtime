// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Coverage moved out of the parent's coverage_boost_test.go when the family was
// extracted (B-080, slice-38): the lock-timeout predicate and the rollback edge
// cases. Both were reached only from the parent's shared coverage file, which is
// the import direction the routes fence forbids once the family is its own
// package.
package rollback

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"

	"github.com/getloopscale/loopzero/api/internal/routes/routestest"
)

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

func TestRollback_MissingProjectName(t *testing.T) {
	h := NewRollbackHandler(&fakeRollbackProjectStore{}, &fakeRollbackService{}, "dev.loopzero.example")

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects//rollback", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRollback_InvalidBody(t *testing.T) {
	h := NewRollbackHandler(
		&fakeRollbackProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "my-api"}},
		&fakeRollbackService{},
		"dev.loopzero.example",
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString("not-json"))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRollback_InternalError(t *testing.T) {
	h := NewRollbackHandler(
		&fakeRollbackProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "my-api"}},
		&fakeRollbackService{err: errors.New("database exploded")},
		"dev.loopzero.example",
	)

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
}
