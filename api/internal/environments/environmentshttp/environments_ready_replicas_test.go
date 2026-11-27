// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environmentshttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

type fakeReadiness struct {
	out      map[EnvRef]EnvReadiness
	err      error
	projects []string
}

func (f *fakeReadiness) ReadyReplicasPerEnv(_ context.Context, _ string, projects []string) (map[EnvRef]EnvReadiness, error) {
	f.projects = projects
	return f.out, f.err
}

func listEnvironments(t *testing.T, h *EnvironmentHandler) []map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Environments []map[string]any `json:"environments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Environments
}

// The dashboard's Environments table has a cell for the ready replicas of each
// environment, and the list answered nothing to fill it.
func TestEnvs_List_AnswersTheReadyReplicasOfEachEnvironment(t *testing.T) {
	mgr := &fakeEnvMgr{envs: []EnvironmentEntry{
		{Name: "production", Project: "web", Status: "active"},
		{Name: "production", Project: "api", Status: "active"},
		{Name: "preview", Project: "web", Status: "active"}, // nothing observed
	}}
	readiness := &fakeReadiness{out: map[EnvRef]EnvReadiness{
		{Project: "web", Name: "production"}: {Replicas: 2, At: "2026-11-27T17:00:00Z"},
		{Project: "api", Name: "production"}: {Replicas: 0, At: "2026-11-27T17:00:05Z"},
	}}

	envs := listEnvironments(t, NewEnvironmentHandler(mgr).WithReadiness(readiness))

	if got := envs[0]["replicas_ready"]; got != float64(2) {
		t.Errorf("web/production replicas_ready = %v, want 2", got)
	}
	if got := envs[0]["replicas_ready_at"]; got != "2026-11-27T17:00:00Z" {
		t.Errorf("web/production replicas_ready_at = %v", got)
	}
	// Zero observed replicas is an answer, and a different one from no observation.
	if got, ok := envs[1]["replicas_ready"]; !ok || got != float64(0) {
		t.Errorf("api/production replicas_ready = %v (present %v), want 0", got, ok)
	}
	if _, ok := envs[2]["replicas_ready"]; ok {
		t.Error("web/preview carries replicas_ready although nothing was observed")
	}
	if len(readiness.projects) != 2 {
		t.Errorf("asked for projects %v, want each project once", readiness.projects)
	}
}

func TestEnvs_List_StillAnswersWhenTheReadyReplicasCannotBeRead(t *testing.T) {
	mgr := &fakeEnvMgr{envs: []EnvironmentEntry{{Name: "production", Project: "web", Status: "active"}}}

	envs := listEnvironments(t, NewEnvironmentHandler(mgr).WithReadiness(&fakeReadiness{err: errors.New("connection refused")}))

	if len(envs) != 1 {
		t.Fatalf("environments = %v, want the one environment", envs)
	}
	if _, ok := envs[0]["replicas_ready"]; ok {
		t.Error("replicas_ready answered although the read failed")
	}
}
