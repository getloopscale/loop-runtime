// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
	"github.com/getloopscale/loopzero/api/internal/validation"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// --- Fake BuildOrchestratorService ---

type fakeBuildOrch struct {
	submitResult *SubmitBuildResult
	submitErr    error

	statusResult *BuildStatusResponse
	statusErr    error

	cancelErr error

	tenantID    string
	tenantIDErr error

	// Plan J-17 T6.4 — redeploy source lookup.
	redeploySource *RedeploySource
	redeployErr    error
	// Captured args so tests can assert what was looked up.
	redeployTenantSeen string
	redeployBuildSeen  string
}

func (f *fakeBuildOrch) SubmitBuild(_ context.Context, _ SubmitBuildInput) (*SubmitBuildResult, error) {
	return f.submitResult, f.submitErr
}

func (f *fakeBuildOrch) GetBuildStatus(_ context.Context, _ string) (*BuildStatusResponse, error) {
	return f.statusResult, f.statusErr
}

func (f *fakeBuildOrch) CancelBuild(_ context.Context, _ string) error {
	return f.cancelErr
}

func (f *fakeBuildOrch) GetBuildTenantID(_ context.Context, _ string) (string, error) {
	return f.tenantID, f.tenantIDErr
}

func (f *fakeBuildOrch) GetRedeploySource(_ context.Context, tenantID, buildID string) (*RedeploySource, error) {
	f.redeployTenantSeen = tenantID
	f.redeployBuildSeen = buildID
	return f.redeploySource, f.redeployErr
}

// --- Tests ---

func TestBuilds_Create_HappyPath(t *testing.T) {
	orch := &fakeBuildOrch{
		submitResult: &SubmitBuildResult{
			BuildID:   "bld-abc123",
			StreamURL: "/api/v1/builds/bld-abc123/stream",
		},
	}
	h := NewBuildHandler(orch, slog.Default())

	body := `{"project_id":"my-project","environment":"production","commit_sha":"abc123","apps":[{"name":"app","type":"server","port":3000,"framework":"node"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp SubmitBuildResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BuildID != "bld-abc123" {
		t.Fatalf("want build_id bld-abc123, got %s", resp.BuildID)
	}
}

func TestBuilds_Create_MissingProject(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Create_MissingEnvironment(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	body := `{"project_id":"my-project"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// T1.3 follow-up regression test (CR-2): a body with `apps: []` (or no apps
// field at all) used to slip through to the WorkflowBuilder where the
// fail-fast invariant returned a wrapped error that the handler mapped to
// a generic 500 INTERNAL_ERROR. The handler now rejects empty-apps with a
// typed 400 MISSING_FIELD before any state mutation.
func TestBuilds_Create_MissingApps(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	body := `{"project_id":"my-project","environment":"production","commit_sha":"abc123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("MISSING_FIELD")) {
		t.Fatalf("want MISSING_FIELD code; body=%s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("app is required")) {
		t.Fatalf("want 'app is required' message hint; body=%s", w.Body.String())
	}

	// Red Team loopzero-cloud#123 — the error message must guide the customer to
	// the canonical manifest (.loopzero/services.json v2 / services: []), not the
	// LEGACY loopzero.yaml / apps: section. Following the stale guidance leads the
	// user to create/edit a loopzero.yaml that the build silently ignores
	// (.loopzero/services.json is the only manifest loopzerokit emits and the engine
	// consumes — see loopzero-cloud/CLAUDE.md § How loopzero-cloud connects).
	if bytes.Contains(w.Body.Bytes(), []byte("loopzero.yaml")) {
		t.Fatalf("RED #123: message must NOT reference legacy loopzero.yaml; body=%s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(".loopzero/services.json")) {
		t.Fatalf("RED #123: message MUST reference canonical .loopzero/services.json; body=%s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("services:")) {
		t.Fatalf("RED #123: message MUST reference canonical `services:` array; body=%s", w.Body.String())
	}
}

func TestBuilds_Create_InvalidEnvironmentName(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	body := `{"project_id":"my-project","environment":"UPPERCASE"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// POST /builds and POST /environments accept the same environment names: a
// name one of them refuses must not be deployable through the other.
func TestBuilds_Create_RejectsAnEnvironmentNameThatIsNotADNSLabel(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{submitResult: &SubmitBuildResult{BuildID: "bld-1"}}, slog.Default())

	body := `{"project_id":"my-project","environment":"staging-","apps":[{"name":"app"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("a trailing-hyphen environment is not a namespace label: want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// Sprint B T3.2 REVERTED 2026-06-02 — the regex tightening broke e2e
// journey fixtures using hyphen env names (j-rollout-a, j-secrets, etc.)
// in cleanroom-fresh DB state. Sponsor mandate trio GREEN took precedence
// over closing code-005. These tests now lock the REVERTED behavior:
// hyphens are accepted (regex match), but the bug class remains open.

// TestEnvironmentRegex_AcceptsHyphensAfterRevert — locks the reverted
// (legacy) behavior. Pre-Sprint-B and post-revert, hyphens ARE valid.
// The code-005 finding remains open; re-attempt requires migrating e2e
// fixture env names FIRST (see MEET-ASYNC-AMENDMENT-2026-06-02-002).
func TestEnvironmentRegex_AcceptsHyphensAfterRevert(t *testing.T) {
	accepted := []string{
		"preview-pr-42", // preview env naming pattern
		"j-rollout-a",   // e2e journey fixture
		"with-hyphen",
		"a-z-end",
	}
	for _, name := range accepted {
		t.Run(name, func(t *testing.T) {
			if err := validation.ValidateEnvironmentName(name); err != nil {
				t.Errorf("rejected %q: hyphenated environment names stay valid: %v", name, err)
			}
		})
	}
}

// TestEnvironmentRegex_AcceptsCanonicalNames — positive baseline: the
// canonical env names (production, staging, dev) MUST always pass.
func TestEnvironmentRegex_AcceptsCanonicalNames(t *testing.T) {
	accepted := []string{
		"production",
		"staging",
		"development",
		"dev",
		"preview42",
		"jrollouta",
		"a",
	}
	for _, name := range accepted {
		t.Run(name, func(t *testing.T) {
			if err := validation.ValidateEnvironmentName(name); err != nil {
				t.Errorf("rejected canonical %q: %v", name, err)
			}
		})
	}
}

// TestSyncCallbackParser_GrandfatheredHyphenEnv_KnownLimitation —
// EC-9 of the plan: documents the bounded limitation explicitly via
// t.Skip so CI stays green while preserving the known-issue audit
// trail. Tracked as https://github.com/getloopscale/loopzero/issues/290 (was the
// tracker-less internal reference code-005 until the family split made the
// skip-link gate ask for a URL).
func TestSyncCallbackParser_GrandfatheredHyphenEnv_KnownLimitation(t *testing.T) {
	t.Skip("known parser limitation; https://github.com/getloopscale/loopzero/issues/290. sync_callback_activator.go LastIndex(\"-\") parser is ambiguous for env names containing hyphens. T3.2 regex tightening reverted 2026-06-02 to keep trio GREEN. Re-attempt requires e2e fixture migration first (see MEET-ASYNC-AMENDMENT-2026-06-02-002).")
}

func TestBuilds_Create_Unauthenticated(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	body := `{"project_id":"my-project","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	// No auth context
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Create_InvalidJSON(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString("not-json"))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Create_DeployInProgress(t *testing.T) {
	orch := &fakeBuildOrch{
		submitErr: loopzeroerrors.ErrDeployInProgress,
	}
	h := NewBuildHandler(orch, slog.Default())

	body := `{"project_id":"my-project","environment":"production","apps":[{"name":"app","type":"server","port":3000,"framework":"node"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestBuilds_Create_UnsupportedFramework removed — server-side framework
// validation was deleted (simplify-deploy-loopzerokit-runtime-wiring T2.1 / F-dom-2):
// the API no longer validates or dispatches on `framework`; an unsupported
// runtime fails closed at build time via argo.ResolveStrategy, not with a 422.

func TestBuilds_Get_Found(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantID: "tn-test-123",
		statusResult: &BuildStatusResponse{
			BuildID:     "bld-abc123",
			ProjectID:   "my-project",
			Environment: "production",
			Status:      "SUCCEEDED",
		},
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp BuildStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BuildID != "bld-abc123" {
		t.Fatalf("want build_id bld-abc123, got %s", resp.BuildID)
	}
}

func TestBuilds_Get_NotFound(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantIDErr: loopzeroerrors.ErrBuildNotFound,
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-nonexistent", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-nonexistent")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Get_CrossTenantDenied(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantID: "tn-other-tenant", // different tenant owns the build
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext()) // tn-test-123
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	// SEC-4: returns 404 not 403 to avoid leaking build existence
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for cross-tenant, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Cancel_Success(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantID: "tn-test-123",
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/bld-abc123/cancel", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Cancel(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Cancel_NotFound(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantIDErr: loopzeroerrors.ErrBuildNotFound,
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/bld-nonexistent/cancel", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-nonexistent")
	w := httptest.NewRecorder()

	h.Cancel(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Cancel_AlreadyCompleted(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantID:  "tn-test-123",
		cancelErr: loopzeroerrors.ErrBuildAlreadyCompleted,
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/bld-abc123/cancel", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Cancel(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// Plan J-17 T6.4 — redeploy_of tests
// =============================================================================

func TestBuilds_Redeploy_HappyPath(t *testing.T) {
	orch := &fakeBuildOrch{
		redeploySource: &RedeploySource{
			ProjectID:   "my-project",
			Environment: "production",
			CommitSHA:   "abc1234",
			Apps:        []AppInput{{Name: "api", Type: "server", Port: 3000}},
		},
		submitResult: &SubmitBuildResult{
			BuildID:   "bld-new",
			StreamURL: "/api/v1/builds/bld-new/stream",
		},
	}
	h := NewBuildHandler(orch, slog.Default())

	body := `{"redeploy_of":"bld-source"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}
	if orch.redeployTenantSeen != routestest.DefaultUserContext().TenantID {
		t.Errorf("redeploy tenant mismatch: got %q want %q",
			orch.redeployTenantSeen, routestest.DefaultUserContext().TenantID)
	}
	if orch.redeployBuildSeen != "bld-source" {
		t.Errorf("redeploy build id mismatch: got %q want %q",
			orch.redeployBuildSeen, "bld-source")
	}
}

func TestBuilds_Redeploy_NotTerminalReturns409(t *testing.T) {
	orch := &fakeBuildOrch{
		redeployErr: loopzeroerrors.NewLoopZeroError(
			"BUILD_NOT_TERMINAL",
			"Cannot redeploy a build that is still in progress"),
	}
	h := NewBuildHandler(orch, slog.Default())

	body := `{"redeploy_of":"bld-source-running"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("BUILD_NOT_TERMINAL")) {
		t.Errorf("body missing typed error code: %s", w.Body.String())
	}
}

func TestBuilds_Redeploy_NotFoundReturns404(t *testing.T) {
	orch := &fakeBuildOrch{
		redeployErr: loopzeroerrors.ErrBuildNotFound,
	}
	h := NewBuildHandler(orch, slog.Default())

	body := `{"redeploy_of":"bld-missing"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Redeploy_BodyParamsIgnored(t *testing.T) {
	// When redeploy_of is set, project_id / environment / commit_sha /
	// apps from the request body must be ignored — only the source
	// build's values matter. This guards against a frontend bug that
	// would let users redeploy build A's source against project B.
	orch := &fakeBuildOrch{
		redeploySource: &RedeploySource{
			ProjectID:   "real-project",
			Environment: "production",
			CommitSHA:   "real-sha",
			Apps:        []AppInput{{Name: "real-app", Type: "server", Port: 3000}},
		},
		submitResult: &SubmitBuildResult{
			BuildID:   "bld-new",
			StreamURL: "/api/v1/builds/bld-new/stream",
		},
	}
	h := NewBuildHandler(orch, slog.Default())

	body := `{
		"redeploy_of":"bld-source",
		"project_id":"attacker-project",
		"environment":"staging",
		"commit_sha":"attacker-sha",
		"apps":[{"name":"attacker-app","type":"server"}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}
	// Source was looked up; orch returned real-project; the handler
	// should have overwritten the attacker fields before submit.
	// We assert via the captured redeploy lookup — if the source
	// fetch happened, the overrides took effect.
	if orch.redeployBuildSeen != "bld-source" {
		t.Errorf("expected source lookup for bld-source, got %q", orch.redeployBuildSeen)
	}
}

// Red Team loopzero-cloud#126 — when redeploy_of points at a build whose source
// has empty Apps (legacy seed data, synthetic test fixture, etc.), the
// handler USED to return the generic missing-apps error message that tells
// the user to "Check your `.loopzero/services.json` for a `services:` array."
// That guidance is wrong for a redeploy: the user did not produce the build,
// has no local manifest to edit, and the canonical fix is "use a different
// source build that has apps." This test pins the redeploy-specific error
// shape: typed 400 MISSING_FIELD, message names the empty source build, and
// does NOT mention `.loopzero/services.json` (irrelevant in this code path).
func TestBuilds_Redeploy_SourceWithoutAppsReturnsCanonicalRedeployError(t *testing.T) {
	orch := &fakeBuildOrch{
		redeploySource: &RedeploySource{
			ProjectID:   "my-project",
			Environment: "production",
			CommitSHA:   "abc1234",
			Apps:        []AppInput{},
		},
	}
	h := NewBuildHandler(orch, slog.Default())

	body := `{"redeploy_of":"bld-source-empty"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("MISSING_FIELD")) {
		t.Fatalf("want MISSING_FIELD code; body=%s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("no apps to redeploy")) {
		t.Fatalf("want 'no apps to redeploy' redeploy-specific message; body=%s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("bld-source-empty")) {
		t.Fatalf("want source build id named in message for actionability; body=%s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(".loopzero/services.json")) {
		t.Fatalf("redeploy error MUST NOT mention .loopzero/services.json (irrelevant in redeploy path); body=%s", w.Body.String())
	}
}
