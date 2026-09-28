// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildshttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/builds"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Sprint 6 plan qa-sprint6-cleanup-2026-06-25 T3.1 (closes loopzero-cloud#34):
// mapBuildError ladder regression + discrimination tests. The ladder order
// MUST stay: (1) typed *LoopZeroError dispatch including DEPLOY_IN_PROGRESS
// short-circuit; (2) NEW build.AsSubmitBuildError dispatch with the
// reason-specific message; (3) fallback to ErrInternalError for genuinely
// unexpected errors only.

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v (raw=%s)", err, w.Body.String())
	}
	return body
}

// T3.1 — discriminated SubmitBuildError surfaces with BUILD_SUBMIT_FAILED
// code + reason-specific message + verbatim cause composed in.
func TestMapBuildError_DispatchesViaAsSubmitBuildError(t *testing.T) {
	cases := []struct {
		name          string
		reason        builds.SubmitBuildErrorReason
		stage         builds.SubmitBuildStage
		cause         error
		wantCode      string
		wantMsgHas    []string
		wantMsgNotHas []string
	}{
		{
			name:     "WorkflowFailed surfaces verbatim cause",
			reason:   builds.ReasonBuildSubmitWorkflowFailed,
			stage:    builds.StageSubmitWorkflow,
			cause:    errors.New("argo client: 503 Service Unavailable"),
			wantCode: "BUILD_SUBMIT_FAILED",
			wantMsgHas: []string{
				"Build trigger failed at workflow submit",
				"503",
			},
			wantMsgNotHas: []string{
				"Something went wrong", // #34 regression guard
			},
		},
		{
			name:     "ProjectEnsureFailed surfaces tenant lookup detail",
			reason:   builds.ReasonBuildSubmitProjectEnsureFailed,
			stage:    builds.StageEnsureProject,
			cause:    errors.New("project: tenant lookup failed"),
			wantCode: "BUILD_SUBMIT_FAILED",
			wantMsgHas: []string{
				"ensuring the project exists",
				"tenant lookup failed",
			},
			wantMsgNotHas: []string{"Something went wrong"},
		},
		{
			name:     "DBWriteFailed surfaces pq driver detail",
			reason:   builds.ReasonBuildSubmitDBWriteFailed,
			stage:    builds.StageCreateBuild,
			cause:    errors.New("pq: duplicate key value"),
			wantCode: "BUILD_SUBMIT_FAILED",
			wantMsgHas: []string{
				"database write step",
				"duplicate key",
			},
			wantMsgNotHas: []string{"Something went wrong"},
		},
		{
			name:     "Unknown surfaces unclassified cause verbatim",
			reason:   builds.ReasonBuildSubmitUnknown,
			stage:    builds.SubmitBuildStage("genuinely-novel-stage"),
			cause:    errors.New("some new failure we did not anticipate"),
			wantCode: "BUILD_SUBMIT_FAILED",
			wantMsgHas: []string{
				"cause not classified",
				"some new failure",
			},
			wantMsgNotHas: []string{"Something went wrong"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			se := builds.NewSubmitBuildError(tc.reason, tc.stage, tc.cause, tc.cause.Error())
			w := httptest.NewRecorder()
			mapBuildError(w, httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil), se)
			if w.Code != http.StatusInternalServerError {
				t.Errorf("HTTP status = %d, want 500", w.Code)
			}
			body := decodeBody(t, w)
			code, _ := body["code"].(string)
			if code != tc.wantCode {
				t.Errorf("code = %q, want %q", code, tc.wantCode)
			}
			msg, _ := body["message"].(string)
			for _, want := range tc.wantMsgHas {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q must contain %q", msg, want)
				}
			}
			for _, notWant := range tc.wantMsgNotHas {
				if strings.Contains(msg, notWant) {
					t.Errorf("message %q must NOT contain %q (#34 regression guard)", msg, notWant)
				}
			}
		})
	}
}

// T3.1 — DEPLOY_IN_PROGRESS still wins (regression-protector for the
// existing ladder order at routes/builds.go:549-557).
func TestMapBuildError_PreservesDeployInProgressShortCircuit(t *testing.T) {
	w := httptest.NewRecorder()
	mapBuildError(w, httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil), loopzeroerrors.ErrDeployInProgress)
	if w.Code != http.StatusConflict {
		t.Errorf("HTTP status = %d, want 409 (DEPLOY_IN_PROGRESS)", w.Code)
	}
	body := decodeBody(t, w)
	if code, _ := body["code"].(string); code != "DEPLOY_IN_PROGRESS" {
		t.Errorf("code = %q, want DEPLOY_IN_PROGRESS", code)
	}
}

// T3.1 — typed *LoopZeroError that is NOT DEPLOY_IN_PROGRESS dispatches with its
// own code (e.g., ScalingAmbiguous) — regression-protector for the existing
// ladder order at routes/builds.go:547-559.
func TestMapBuildError_PreservesTypedLoopZeroErrorDispatch(t *testing.T) {
	w := httptest.NewRecorder()
	mapBuildError(w, httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil), loopzeroerrors.ErrScalingAmbiguous)
	body := decodeBody(t, w)
	if code, _ := body["code"].(string); code != loopzeroerrors.ErrScalingAmbiguous.Code {
		t.Errorf("code = %q, want %q", code, loopzeroerrors.ErrScalingAmbiguous.Code)
	}
}

// M5 (#106) — a deploy-path PROJECTS_EXCEED_PLAN (returned by EnsureProject when
// a new project would exceed the per-Account cap) surfaces as HTTP 422 with its
// own code, NOT a generic 500. Mirrors the scale-cap #101 surfacing and is the
// mechanical oracle for the M5 DoD "typed 422 surfaced through the deploy path".
func TestMapBuildError_ProjectsExceedPlan_SurfacesAs422(t *testing.T) {
	capErr := loopzeroerrors.NewProjectsExceedPlan("starter", 1, 1, "acct-m5")
	w := httptest.NewRecorder()
	mapBuildError(w, httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil), capErr)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("HTTP status = %d, want 422", w.Code)
	}
	body := decodeBody(t, w)
	if code, _ := body["code"].(string); code != "PROJECTS_EXCEED_PLAN" {
		t.Errorf("code = %q, want PROJECTS_EXCEED_PLAN", code)
	}
	if msg, _ := body["message"].(string); strings.Contains(msg, "Something went wrong") {
		t.Errorf("deploy cap rejection must not collapse to the generic 500 message; got %q", msg)
	}
}

// T3.1 — genuinely-unknown errors (NOT from the orchestrator) still get the
// generic ErrInternalError catchall, but Sprint 6 narrows this surface so
// only request-decode-class errors reach here in practice.
func TestMapBuildError_GenuineUnknownStillFallsBack(t *testing.T) {
	w := httptest.NewRecorder()
	mapBuildError(w, httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil), errors.New("some error from outside the orchestrator"))
	body := decodeBody(t, w)
	if code, _ := body["code"].(string); code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR (genuine catchall)", code)
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "Something went wrong") {
		t.Errorf("message must contain canonical generic for genuinely-unknown; got %q", msg)
	}
}
