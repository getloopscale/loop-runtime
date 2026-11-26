// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scalinghttp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

type fakeScaleAppLookup struct {
	names []string
	err   error
}

func (f fakeScaleAppLookup) DeployedAppNames(_ context.Context, _, _, _ string) ([]string, error) {
	return f.names, f.err
}

// scaleAppRequest posts a fixed-replica scale of app to project "hello" with an
// apply closure that records whether it ran.
func scaleAppRequest(t *testing.T, lookup ScaleAppLookup, app string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "hello", TeamID: "tn-test-123"},
	}
	applied := false
	apply := func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		applied = true
		return true, nil
	}
	h := NewScaleHandler(projects, apply, false)
	h.SetAppLookup(lookup)

	body := `{"environment":"production","app":"` + app + `","replicas":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/hello/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "hello")
	w := httptest.NewRecorder()

	h.Scale(w, req)
	return w, applied
}

// The console sent app "api" for a project whose only app is "web", and the API
// answered 200 changed:true while scaling nothing.
func TestScaleRefusesAnAppTheProjectDoesNotDeclare(t *testing.T) {
	w, applied := scaleAppRequest(t, fakeScaleAppLookup{names: []string{"web", "worker"}}, "api")

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "APP_NOT_FOUND") {
		t.Errorf("body lacks the APP_NOT_FOUND code: %s", body)
	}
	if !strings.Contains(body, "web") || !strings.Contains(body, "worker") {
		t.Errorf("body does not name the apps the project has: %s", body)
	}
	if applied {
		t.Error("the scaling config was written for an app the project does not have")
	}
}

func TestScaleAppliesAnAppTheProjectDeclares(t *testing.T) {
	w, applied := scaleAppRequest(t, fakeScaleAppLookup{names: []string{"web"}}, "web")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !applied {
		t.Error("the scaling config was not written")
	}
}

// Without an active release there is nothing to compare against, and a scaling
// config set before the first deploy is still read by that deploy.
func TestScaleAppliesWhenTheEnvironmentHasNoActiveRelease(t *testing.T) {
	w, applied := scaleAppRequest(t, fakeScaleAppLookup{}, "web")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !applied {
		t.Error("the scaling config was not written")
	}
}

// An app list that cannot be read refuses the request instead of writing a config
// for an app nobody checked.
func TestScaleRefusesWhenTheProjectAppsCannotBeRead(t *testing.T) {
	w, applied := scaleAppRequest(t, fakeScaleAppLookup{err: errors.New("database unavailable")}, "web")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
	if applied {
		t.Error("the scaling config was written without checking the app")
	}
}
