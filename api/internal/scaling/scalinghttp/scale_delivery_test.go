// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scalinghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"
	"github.com/getloopscale/loop-runtime/api/internal/scaling"
)

// fakeScaleDeliverer answers Deliver with delivered and err, and counts calls.
type fakeScaleDeliverer struct {
	delivered bool
	err       error
	calls     []string
}

func (f *fakeScaleDeliverer) Deliver(_ context.Context, tenantID, project, env string) (bool, error) {
	f.calls = append(f.calls, tenantID+"/"+project+"/"+env)
	return f.delivered, f.err
}

// scaleWith posts a fixed scale of app "api" in project "my-api" to a handler
// whose store reports changed and whose deliverer is d (nil: none wired).
func scaleWith(t *testing.T, changed bool, d ScaleDeliverer) (*httptest.ResponseRecorder, scaleResponse) {
	t.Helper()
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	apply := func(context.Context, ScaleConfigInput) (bool, error) { return changed, nil }
	h := NewScaleHandler(projects, apply, true)
	if d != nil {
		h.SetDeliverer(d)
	}

	body := `{"environment":"production","app":"api","replicas":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	var resp scaleResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

// The handler stored the config and answered "applied" while the running
// Rollout kept its replica count. "applied" now means the config was delivered
// to the running workloads.
func TestScaleAnswersAppliedWhenTheConfigWasDelivered(t *testing.T) {
	d := &fakeScaleDeliverer{delivered: true}

	w, resp := scaleWith(t, true, d)

	if w.Code != http.StatusOK || resp.Status != "applied" {
		t.Fatalf("got %d status=%q, want 200 applied; body=%s", w.Code, resp.Status, w.Body.String())
	}
	if len(d.calls) != 1 || !strings.HasSuffix(d.calls[0], "/my-api/production") {
		t.Errorf("delivered to %v, want one delivery to my-api/production", d.calls)
	}
}

func TestScaleAnswersSavedWhenNothingIsRunning(t *testing.T) {
	w, resp := scaleWith(t, true, &fakeScaleDeliverer{delivered: false})

	if w.Code != http.StatusOK || resp.Status != "saved" {
		t.Fatalf("got %d status=%q, want 200 saved: no workload received the config; body=%s", w.Code, resp.Status, w.Body.String())
	}
}

func TestScaleAnswersSavedWhenNoDelivererIsWired(t *testing.T) {
	w, resp := scaleWith(t, true, nil)

	if w.Code != http.StatusOK || resp.Status != "saved" {
		t.Fatalf("got %d status=%q, want 200 saved: nothing applied the config; body=%s", w.Code, resp.Status, w.Body.String())
	}
}

// The stored config can already equal the request while the cluster does not,
// after a delivery that failed: sending the request again must deliver.
func TestScaleDeliversAgainWhenTheStoredConfigIsUnchanged(t *testing.T) {
	d := &fakeScaleDeliverer{delivered: true}

	w, resp := scaleWith(t, false, d)

	if w.Code != http.StatusOK || resp.Status != "no_change" {
		t.Fatalf("got %d status=%q, want 200 no_change; body=%s", w.Code, resp.Status, w.Body.String())
	}
	if len(d.calls) != 1 {
		t.Errorf("delivered %d times, want 1: a retry is how a failed delivery is repaired", len(d.calls))
	}
}

func TestScaleFailsWhenTheConfigCannotBeDelivered(t *testing.T) {
	w, resp := scaleWith(t, true, &fakeScaleDeliverer{err: errors.New("gitea unreachable")})

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503; body=%s", w.Code, w.Body.String())
	}
	if resp.Status == "applied" || strings.Contains(w.Body.String(), `"applied"`) {
		t.Errorf("the response says applied for a config that reached no workload: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "saved") {
		t.Errorf("the response does not say the config was saved: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "gitea") {
		t.Errorf("the response leaks the internal cause: %s", w.Body.String())
	}
}

func TestScaleAnswersConflictWhileADeployIsInFlight(t *testing.T) {
	w, _ := scaleWith(t, true, &fakeScaleDeliverer{err: scaling.ErrDeployInFlight})

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "DEPLOY_IN_PROGRESS") {
		t.Fatalf("got %d, want 409 DEPLOY_IN_PROGRESS; body=%s", w.Code, w.Body.String())
	}
}
