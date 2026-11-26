// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildshttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

func TestDeployStatusOf(t *testing.T) {
	cases := []struct {
		release string
		want    string
	}{
		{"", ""},
		{"READY", "in_progress"},
		{"PENDING", "in_progress"},
		{"ACTIVE", "succeeded"},
		{"DRIFT_ALERT", "succeeded"},
		{"SYNC_FAILED", "failed"},
		{"GIT_MIRROR_FAILED", "failed"},
		{"SUPERSEDED", "superseded"},
		{"ROLLING_BACK", "rolling_back"},
		{"ROLLED_BACK", "rolled_back"},
		{"ROLLBACK_FAILED", "rollback_failed"},
		{"CANCELLED", "cancelled"},
		{"ARCHIVED", "archived"},
		{"SOMETHING_NEW", ""},
	}
	for _, tc := range cases {
		if got := DeployStatusOf(tc.release); got != tc.want {
			t.Errorf("DeployStatusOf(%q) = %q, want %q", tc.release, got, tc.want)
		}
	}
}

type fakeReleaseStatuses struct {
	byBuild map[string]string
	err     error
	asked   []string
	tenant  string
}

func (f *fakeReleaseStatuses) LatestReleaseStatuses(_ context.Context, tenantID string, buildIDs []string) (map[string]string, error) {
	f.tenant = tenantID
	f.asked = append(f.asked, buildIDs...)
	return f.byBuild, f.err
}

func listBuilds(t *testing.T, h *BuildListHandler) (int, []map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds", nil)
	req = routestest.WithChiParam(req, "name", "my-api")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)
	var body struct {
		Builds []map[string]any `json:"builds"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body.Builds
}

// A build whose image was built and whose deploy then failed was listed as
// Succeeded, with nothing in the response to say otherwise
// (getloopscale/loopzero#135).
func TestBuildList_CarriesTheOutcomeOfEachBuildsDeploy(t *testing.T) {
	now := time.Now()
	store := &fakeBuildListStore{total: 4, builds: []BuildListItem{
		{ID: "bld-live", Status: "SUCCEEDED", CreatedAt: now},
		{ID: "bld-broken", Status: "SUCCEEDED", CreatedAt: now},
		{ID: "bld-old", Status: "SUCCEEDED", CreatedAt: now},
		{ID: "bld-nobuild", Status: "FAILED", CreatedAt: now},
	}}
	releases := &fakeReleaseStatuses{byBuild: map[string]string{
		"bld-live": "ACTIVE", "bld-broken": "SYNC_FAILED", "bld-old": "SUPERSEDED",
	}}
	h := NewBuildListHandler(store).WithReleaseStatuses(releases)

	code, builds := listBuilds(t, h)

	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	want := map[string]any{"bld-live": "succeeded", "bld-broken": "failed", "bld-old": "superseded", "bld-nobuild": nil}
	for _, b := range builds {
		id := b["build_id"].(string)
		if b["deploy_status"] != want[id] {
			t.Errorf("%s: deploy_status = %v, want %v", id, b["deploy_status"], want[id])
		}
		if b["status"] == nil {
			t.Errorf("%s: the build status is gone", id)
		}
	}
	if releases.tenant != routestest.DefaultUserContext().TenantID {
		t.Errorf("releases were looked up for tenant %q", releases.tenant)
	}
	if len(releases.asked) != 4 {
		t.Errorf("asked for %v, want the four builds of the page", releases.asked)
	}
}

func TestBuildList_FailsWhenTheDeployOutcomeCannotBeRead(t *testing.T) {
	store := &fakeBuildListStore{total: 1, builds: []BuildListItem{{ID: "bld-1", Status: "SUCCEEDED", CreatedAt: time.Now()}}}
	h := NewBuildListHandler(store).WithReleaseStatuses(&fakeReleaseStatuses{err: errors.New("db down")})

	code, _ := listBuilds(t, h)

	if code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: a list without deploy outcomes would read as builds with no deploy", code)
	}
}

func TestBuildList_AnEmptyPageAsksForNoRelease(t *testing.T) {
	releases := &fakeReleaseStatuses{}
	h := NewBuildListHandler(&fakeBuildListStore{}).WithReleaseStatuses(releases)

	code, _ := listBuilds(t, h)

	if code != http.StatusOK || len(releases.asked) != 0 {
		t.Errorf("status = %d, asked = %v", code, releases.asked)
	}
}

func getBuild(h *BuildHandler) (int, map[string]any) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-1", nil)
	req = routestest.WithChiParam(req, "id", "bld-1")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Get(w, req)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func TestBuildGet_CarriesTheOutcomeOfItsDeploy(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantID:     routestest.DefaultUserContext().TenantID,
		statusResult: &BuildStatusResponse{BuildID: "bld-1", Status: "SUCCEEDED"},
	}
	h := NewBuildHandler(orch, nil).WithReleaseStatuses(&fakeReleaseStatuses{byBuild: map[string]string{"bld-1": "SYNC_FAILED"}})

	code, body := getBuild(h)

	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body["deploy_status"] != "failed" || body["status"] != "SUCCEEDED" {
		t.Errorf("deploy_status = %v, status = %v", body["deploy_status"], body["status"])
	}
}

func TestBuildGet_OmitsTheOutcomeOfABuildWithNoRelease(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantID:     routestest.DefaultUserContext().TenantID,
		statusResult: &BuildStatusResponse{BuildID: "bld-1", Status: "BUILDING"},
	}
	h := NewBuildHandler(orch, nil).WithReleaseStatuses(&fakeReleaseStatuses{})

	_, body := getBuild(h)

	if _, present := body["deploy_status"]; present {
		t.Errorf("deploy_status = %v for a build that has no release", body["deploy_status"])
	}
}
