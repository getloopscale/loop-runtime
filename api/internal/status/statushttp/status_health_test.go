// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statushttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

type fakeStatusMetrics struct {
	ready      *int
	readyErr   error
	errPct     *float64
	errPctErr  error
	gotEnv     []string
	gotApps    []string
	gotWindows []time.Duration
}

func (f *fakeStatusMetrics) ReadyReplicas(_ context.Context, _, _, env string, apps []string) (*int, error) {
	f.gotEnv = append(f.gotEnv, env)
	f.gotApps = apps
	return f.ready, f.readyErr
}

func (f *fakeStatusMetrics) ServerErrorPercent(_ context.Context, _, _, env string, window time.Duration) (*float64, error) {
	f.gotEnv = append(f.gotEnv, env)
	f.gotWindows = append(f.gotWindows, window)
	return f.errPct, f.errPctErr
}

type fakeStatusScale struct {
	apps       []StatusAppScale
	err        error
	gotBuildID string
}

func (f *fakeStatusScale) DesiredScale(_ context.Context, _, _, _, buildID string) ([]StatusAppScale, error) {
	f.gotBuildID = buildID
	return f.apps, f.err
}

// getStatus calls the handler for project my-api and returns the decoded body.
func getStatus(t *testing.T, h *StatusHandler, env string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env="+env, nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func deployedHandler(env string, deployedAt time.Time) *StatusHandler {
	return NewStatusHandler(
		&fakeStatusProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "my-api"}},
		&fakeStatusBuildStore{builds: []StatusBuild{{
			ID: "bld-1", Environment: env, Status: "SUCCEEDED", CreatedAt: deployedAt,
			Scaling: &StatusBuildScaling{MinReplicas: 1, MaxReplicas: 3},
		}}},
		&fakeStatusReleaseStore{release: &StatusRelease{
			ID: "rel-1", BuildID: "bld-1", Status: "ACTIVE", CreatedAt: deployedAt,
		}},
		"dev.getloopscale.com", "", "",
	)
}

var healthFields = []string{
	"replicas_ready", "replicas_desired", "deploy_status", "traffic_5xx_pct_1h", "alerts_active", "last_deploy_at",
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
func assertAbsent(t *testing.T, body map[string]any, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if v, ok := body[field]; ok {
			t.Errorf("%s = %v, want the field omitted", field, v)
		}
	}
}

// After a rollback the active release is an old one: last_deploy_at is when it
// became active again.
func TestStatus_Get_LastDeployIsWhenTheReleaseBecameActive(t *testing.T) {
	createdAt := time.Date(2026, 11, 25, 18, 17, 0, 0, time.UTC)
	rolledBackAt := time.Date(2026, 11, 25, 22, 3, 0, 0, time.UTC)
	h := NewStatusHandler(
		&fakeStatusProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "my-api"}},
		&fakeStatusBuildStore{builds: []StatusBuild{{ID: "bld-1", Environment: "production", Status: "SUCCEEDED", CreatedAt: createdAt}}},
		&fakeStatusReleaseStore{release: &StatusRelease{
			ID: "rel-1", BuildID: "bld-1", Status: "ACTIVE", CreatedAt: createdAt, DeployedAt: rolledBackAt,
		}},
		"dev.getloopscale.com", "", "",
	)

	body := getStatus(t, h, "production")

	if body["last_deploy_at"] != "2026-11-25T22:03:00Z" {
		t.Errorf("last_deploy_at = %v, want the end of the rollback", body["last_deploy_at"])
	}
}

func TestStatus_Get_SendsTheHealthOfTheRequestedEnvironment(t *testing.T) {
	deployedAt := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	h := deployedHandler("staging", deployedAt)
	metrics := &fakeStatusMetrics{ready: intPtr(2), errPct: floatPtr(0.5)}
	scale := &fakeStatusScale{apps: []StatusAppScale{{App: "web", Replicas: 2}, {App: "worker", Replicas: 1}}}
	h.SetMetricsSource(metrics)
	h.SetScaleReader(scale)

	body := getStatus(t, h, "staging")

	if body["replicas_ready"] != float64(2) {
		t.Errorf("replicas_ready = %v, want 2", body["replicas_ready"])
	}
	if body["replicas_desired"] != float64(3) {
		t.Errorf("replicas_desired = %v, want 3, the sum over the apps", body["replicas_desired"])
	}
	if body["deploy_status"] != "succeeded" {
		t.Errorf("deploy_status = %v, want succeeded for an ACTIVE release", body["deploy_status"])
	}
	if body["traffic_5xx_pct_1h"] != 0.5 {
		t.Errorf("traffic_5xx_pct_1h = %v, want 0.5", body["traffic_5xx_pct_1h"])
	}
	if body["last_deploy_at"] != "2026-11-25T12:00:00Z" {
		t.Errorf("last_deploy_at = %v, want the active release's creation time", body["last_deploy_at"])
	}
	for _, env := range metrics.gotEnv {
		if env != "staging" {
			t.Errorf("metrics were read for environment %q, want staging", env)
		}
	}
	if len(metrics.gotWindows) != 1 || metrics.gotWindows[0] != time.Hour {
		t.Errorf("5xx share was read over %v, want one read over 1h", metrics.gotWindows)
	}
	if scale.gotBuildID != "bld-1" {
		t.Errorf("desired scale was read for build %q, want the active release's build", scale.gotBuildID)
	}
	// Nothing in the platform counts firing alerts per project.
	assertAbsent(t, body, "alerts_active")
}

func TestStatus_Get_SendsZeroReadyReplicasAsZero(t *testing.T) {
	h := deployedHandler("production", time.Now())
	h.SetMetricsSource(&fakeStatusMetrics{ready: intPtr(0), errPct: floatPtr(0)})

	body := getStatus(t, h, "production")

	if v, ok := body["replicas_ready"]; !ok || v != float64(0) {
		t.Errorf("replicas_ready = %v (present=%v), want a measured 0", v, ok)
	}
	if v, ok := body["traffic_5xx_pct_1h"]; !ok || v != float64(0) {
		t.Errorf("traffic_5xx_pct_1h = %v (present=%v), want a measured 0", v, ok)
	}
}

func TestStatus_Get_OmitsHealthFieldsThatHaveNoSource(t *testing.T) {
	t.Run("nothing deployed", func(t *testing.T) {
		h := NewStatusHandler(
			&fakeStatusProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "my-api"}},
			&fakeStatusBuildStore{}, &fakeStatusReleaseStore{}, "dev.getloopscale.com", "", "",
		)
		h.SetMetricsSource(&fakeStatusMetrics{})
		h.SetScaleReader(&fakeStatusScale{})

		assertAbsent(t, getStatus(t, h, "production"), healthFields...)
	})

	t.Run("deployed, no series yet and no scale reader", func(t *testing.T) {
		h := deployedHandler("production", time.Now())
		h.SetMetricsSource(&fakeStatusMetrics{})

		body := getStatus(t, h, "production")

		assertAbsent(t, body, "replicas_ready", "replicas_desired", "traffic_5xx_pct_1h", "alerts_active")
		if body["deploy_status"] != "succeeded" {
			t.Errorf("deploy_status = %v, want succeeded: the release is the source", body["deploy_status"])
		}
	})
}

func TestStatus_Get_AnswersWhenPrometheusFails(t *testing.T) {
	h := deployedHandler("production", time.Now())
	h.SetMetricsSource(&fakeStatusMetrics{
		ready: intPtr(7), readyErr: errors.New("prometheus down"),
		errPct: floatPtr(9), errPctErr: errors.New("prometheus down"),
	})
	h.SetScaleReader(&fakeStatusScale{apps: []StatusAppScale{{App: "web", Replicas: 2}}})

	body := getStatus(t, h, "production")

	assertAbsent(t, body, "replicas_ready", "traffic_5xx_pct_1h")
	if body["replicas_desired"] != float64(2) {
		t.Errorf("replicas_desired = %v, want 2: it does not come from Prometheus", body["replicas_desired"])
	}
}

func TestStatus_Get_ScalingReportsTheSavedConfigOverTheBuild(t *testing.T) {
	// The build rendered 1..3; a later scale saved a fixed 4.
	h := deployedHandler("production", time.Now())
	h.SetScaleReader(&fakeStatusScale{apps: []StatusAppScale{
		{App: "worker", Replicas: 2},
		{App: "web", Replicas: 4, Min: 4, Max: 4, Saved: true},
	}})

	body := getStatus(t, h, "production")

	scaling, _ := body["scaling"].(map[string]any)
	if scaling == nil {
		t.Fatalf("scaling is absent, want the saved config; body=%v", body)
	}
	if scaling["replicas"] != float64(4) || scaling["min"] != float64(4) || scaling["max"] != float64(4) {
		t.Errorf("scaling = %v, want replicas=4 min=4 max=4 from the saved config", scaling)
	}
	if body["replicas_desired"] != float64(6) {
		t.Errorf("replicas_desired = %v, want 6", body["replicas_desired"])
	}
}

func TestStatus_Get_ScalingFallsBackToTheBuildWhenTheScaleReadFails(t *testing.T) {
	h := deployedHandler("production", time.Now())
	h.SetScaleReader(&fakeStatusScale{err: errors.New("db down")})

	body := getStatus(t, h, "production")

	assertAbsent(t, body, "replicas_desired")
	scaling, _ := body["scaling"].(map[string]any)
	if scaling == nil || scaling["min"] != float64(1) || scaling["max"] != float64(3) {
		t.Errorf("scaling = %v, want the build's 1..3", scaling)
	}
}

func TestStatus_Get_ScalingCarriesTheAutoscalingTargets(t *testing.T) {
	h := deployedHandler("production", time.Now())
	h.SetScaleReader(&fakeStatusScale{apps: []StatusAppScale{
		{App: "web", Replicas: 1, Min: 1, Max: 3, TargetCPU: 70, TargetMemory: 60, Saved: true},
	}})

	scaling, _ := getStatus(t, h, "production")["scaling"].(map[string]any)

	if scaling["target_cpu"] != float64(70) || scaling["target_memory"] != float64(60) {
		t.Errorf("scaling = %v, want target_cpu 70 and target_memory 60", scaling)
	}
}

func TestStatus_Get_ScalingOmitsTargetsOfAFixedScale(t *testing.T) {
	h := deployedHandler("production", time.Now())
	h.SetScaleReader(&fakeStatusScale{apps: []StatusAppScale{{App: "web", Replicas: 4, Min: 4, Max: 4, Saved: true}}})

	scaling, _ := getStatus(t, h, "production")["scaling"].(map[string]any)

	if _, sent := scaling["target_cpu"]; sent {
		t.Errorf("scaling = %v, want no target_cpu for a fixed count", scaling)
	}
	if _, sent := scaling["target_memory"]; sent {
		t.Errorf("scaling = %v, want no target_memory for a fixed count", scaling)
	}
}

// Ready replicas are counted over the apps of the active release, so an app a
// rollback or a deploy removed stops counting at once.
func TestStatus_Get_CountsReadyReplicasOverTheAppsOfTheActiveRelease(t *testing.T) {
	h := deployedHandler("production", time.Now())
	metrics := &fakeStatusMetrics{ready: intPtr(1)}
	h.SetMetricsSource(metrics)
	h.SetScaleReader(&fakeStatusScale{apps: []StatusAppScale{{App: "web", Replicas: 2}, {App: "worker", Replicas: 1}}})

	getStatus(t, h, "production")

	if got := strings.Join(metrics.gotApps, ","); got != "web,worker" {
		t.Errorf("apps counted = %q, want web,worker", got)
	}
}
