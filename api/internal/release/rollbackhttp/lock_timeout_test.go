// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Coverage moved out of the parent's coverage_boost_test.go when the family was
// extracted (B-080, slice-38): the lock-timeout predicate and the rollback edge
// cases. Both were reached only from the parent's shared coverage file, which is
// the import direction the routes fence forbids once the family is its own
// package.
package rollbackhttp

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// The lock returns the typed ErrLockTimeout, and services wrap it; the handler
// must still answer the typed 409 through errors.As, with no string matching.
func TestRollback_WrappedLockTimeoutIs409LockTimeout(t *testing.T) {
	projects := &fakeRollbackProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "my-api"}}
	svc := &fakeRollbackService{err: fmt.Errorf("rollback: %w", loopzeroerrors.ErrLockTimeout)}
	h := NewRollbackHandler(projects, svc, "dev.loopzero.example")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString(`{"environment":"production"}`))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "LOCK_TIMEOUT") {
		t.Fatalf("want 409 LOCK_TIMEOUT, got %d body=%s", w.Code, w.Body.String())
	}
}

// An error that merely mentions "lock timeout" is not one: it is unexpected,
// and answers 500 rather than being guessed into a retryable 409.
func TestRollback_ErrorMentioningLockTimeoutIsNotOne(t *testing.T) {
	projects := &fakeRollbackProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "my-api"}}
	svc := &fakeRollbackService{err: errors.New("upstream said: lock timeout")}
	h := NewRollbackHandler(projects, svc, "dev.loopzero.example")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString(`{"environment":"production"}`))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 for an untyped error, got %d body=%s", w.Code, w.Body.String())
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
