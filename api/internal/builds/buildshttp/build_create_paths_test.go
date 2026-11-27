// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// Basis paths of BuildHandler.Create that nothing covered: environment
// provisioning, secret resync, app name/path validation and the admission
// gate.

type recordingEnsurer struct {
	calls int
	err   error
}

func (e *recordingEnsurer) EnsureEnvironment(context.Context, string, string, string) error {
	e.calls++
	return e.err
}

type recordingResyncer struct {
	calls int
	err   error
}

func (r *recordingResyncer) ResyncEnv(context.Context, string, string, string) error {
	r.calls++
	return r.err
}

type scriptedAdmission struct {
	admitted   bool
	retryAfter time.Duration
	err        error
	cellsSeen  []string
}

func (a *scriptedAdmission) Check(_ context.Context, cellID, _ string) (bool, time.Duration, string, string, error) {
	a.cellsSeen = append(a.cellsSeen, cellID)
	return a.admitted, a.retryAfter, "CELL_AT_CAPACITY", "cell full", a.err
}

type scriptedCells struct {
	cell string
	err  error
}

func (c scriptedCells) GetTenantCellID(context.Context, string) (string, error) { return c.cell, c.err }

const validBuildBody = `{"project_id":"shop","environment":"staging","apps":[{"name":"web","path":"apps/web"}]}`

func submittingOrch() *fakeBuildOrch {
	return &fakeBuildOrch{submitResult: &SubmitBuildResult{BuildID: "bld-1"}}
}

func postBuild(h *BuildHandler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Create(w, req)
	return w
}

func codeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	code, _ := body["code"].(string)
	return code
}

func TestCreate_ProvisionFailureWithTypedErrorAnswersThatError(t *testing.T) {
	h := NewBuildHandler(submittingOrch(), slog.Default()).
		WithEnvironmentEnsurer(&recordingEnsurer{err: loopruntimeerrors.ErrCellAtCapacity})
	w := postBuild(h, validBuildBody)
	if codeOf(t, w) != loopruntimeerrors.ErrCellAtCapacity.Code {
		t.Fatalf("want the ensurer's typed error, got %d %s", w.Code, w.Body.String())
	}
}

func TestCreate_ProvisionFailureWithUntypedErrorAnswersEnvProvisionFailed(t *testing.T) {
	h := NewBuildHandler(submittingOrch(), slog.Default()).
		WithEnvironmentEnsurer(&recordingEnsurer{err: errors.New("operator timeout")})
	w := postBuild(h, validBuildBody)
	if w.Code != http.StatusInternalServerError || codeOf(t, w) != "ENV_PROVISION_FAILED" {
		t.Fatalf("want 500 ENV_PROVISION_FAILED, got %d %s", w.Code, w.Body.String())
	}
}

func TestCreate_SecretResyncFailureDoesNotBlockTheBuild(t *testing.T) {
	resync := &recordingResyncer{err: errors.New("namespace not ready")}
	h := NewBuildHandler(submittingOrch(), slog.Default()).WithSecretResyncer(resync)
	w := postBuild(h, validBuildBody)
	if w.Code != http.StatusAccepted || resync.calls != 1 {
		t.Fatalf("want 202 after one resync attempt, got %d (resyncs=%d)", w.Code, resync.calls)
	}
}

func TestCreate_InvalidAppNameIsRefusedBeforeTheEnvironmentIsProvisioned(t *testing.T) {
	ensurer := &recordingEnsurer{}
	h := NewBuildHandler(submittingOrch(), slog.Default()).WithEnvironmentEnsurer(ensurer)
	w := postBuild(h, `{"project_id":"shop","environment":"staging","apps":[{"name":"web; curl x|sh"}]}`)
	if w.Code != http.StatusBadRequest || codeOf(t, w) != "INVALID_APP_NAME" {
		t.Fatalf("want 400 INVALID_APP_NAME, got %d %s", w.Code, w.Body.String())
	}
	if ensurer.calls != 0 {
		t.Fatalf("an invalid request provisioned an environment (%d calls)", ensurer.calls)
	}
}

func TestCreate_InvalidAppPathIsRefused(t *testing.T) {
	w := postBuild(NewBuildHandler(submittingOrch(), slog.Default()),
		`{"project_id":"shop","environment":"staging","apps":[{"name":"web","path":"../../etc"}]}`)
	if w.Code != http.StatusBadRequest || codeOf(t, w) != "INVALID_APP_PATH" {
		t.Fatalf("want 400 INVALID_APP_PATH, got %d %s", w.Code, w.Body.String())
	}
}

func TestCreate_AdmissionRefusalAnswers429WithRetryAfter(t *testing.T) {
	adm := &scriptedAdmission{admitted: false, retryAfter: 200 * time.Millisecond}
	h := NewBuildHandler(submittingOrch(), slog.Default()).WithAdmission(adm, scriptedCells{cell: "cell-7"})
	w := postBuild(h, validBuildBody)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("want 429 with Retry-After 1 (never 0), got %d Retry-After=%q", w.Code, w.Header().Get("Retry-After"))
	}
	if len(adm.cellsSeen) != 1 || adm.cellsSeen[0] != "cell-7" {
		t.Fatalf("admission must be checked against the tenant's cell, saw %v", adm.cellsSeen)
	}
}

func TestCreate_AdmissionQueryFailureFailsOpen(t *testing.T) {
	adm := &scriptedAdmission{err: errors.New("db down")}
	h := NewBuildHandler(submittingOrch(), slog.Default()).WithAdmission(adm, scriptedCells{cell: "cell-7"})
	if w := postBuild(h, validBuildBody); w.Code != http.StatusAccepted {
		t.Fatalf("the advisory gate failing must not block builds: got %d", w.Code)
	}
}

// When the tenant's cell cannot be resolved, admission cannot be evaluated
// for the right cell. Checking a hard-coded "cell-0" instead judges the build
// against another cell's capacity; the gate is skipped like any other
// failure of the advisory check.
func TestCreate_UnresolvableCellSkipsAdmissionInsteadOfCheckingAnotherCell(t *testing.T) {
	adm := &scriptedAdmission{admitted: false}
	h := NewBuildHandler(submittingOrch(), slog.Default()).WithAdmission(adm, scriptedCells{err: errors.New("no placement")})
	w := postBuild(h, validBuildBody)
	if len(adm.cellsSeen) != 0 {
		t.Fatalf("admission was checked against %v for a tenant whose cell is unknown", adm.cellsSeen)
	}
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", w.Code)
	}
}

func TestCreate_SubmitsWithEnsureAndResyncOnTheHappyPath(t *testing.T) {
	ensurer, resync := &recordingEnsurer{}, &recordingResyncer{}
	h := NewBuildHandler(submittingOrch(), slog.Default()).WithEnvironmentEnsurer(ensurer).WithSecretResyncer(resync)
	if w := postBuild(h, validBuildBody); w.Code != http.StatusAccepted || ensurer.calls != 1 || resync.calls != 1 {
		t.Fatalf("want 202 with one ensure and one resync, got %d ensure=%d resync=%d", w.Code, ensurer.calls, resync.calls)
	}
}
