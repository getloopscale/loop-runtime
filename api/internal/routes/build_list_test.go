// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Fake BuildListStore ---

type fakeBuildListStore struct {
	builds []BuildListItem
	total  int
	err    error
}

func (f *fakeBuildListStore) ListByProjectAndEnv(_ context.Context, _, _, _ string, _, _ int) ([]BuildListItem, int, error) {
	return f.builds, f.total, f.err
}

// --- Tests ---

func TestBuildList_HappyPath(t *testing.T) {
	now := time.Now()
	store := &fakeBuildListStore{
		builds: []BuildListItem{
			{
				ID:          "bld-1",
				ProjectID:   "my-api",
				Environment: "production",
				Status:      "SUCCEEDED",
				CommitSHA:   "abc123",
				CreatedAt:   now,
			},
			{
				ID:          "bld-2",
				ProjectID:   "my-api",
				Environment: "production",
				Status:      "FAILED",
				CommitSHA:   "def456",
				CreatedAt:   now.Add(-1 * time.Hour),
			},
		},
		total: 2,
	}
	h := NewBuildListHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds?env=production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp buildListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Builds) != 2 {
		t.Fatalf("want 2 builds, got %d", len(resp.Builds))
	}
	if resp.Total != 2 {
		t.Fatalf("want total=2, got %d", resp.Total)
	}
}

func TestBuildList_Empty(t *testing.T) {
	store := &fakeBuildListStore{
		builds: []BuildListItem{},
		total:  0,
	}
	h := NewBuildListHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp buildListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Builds) != 0 {
		t.Fatalf("want 0 builds, got %d", len(resp.Builds))
	}
}

func TestBuildList_Unauthenticated(t *testing.T) {
	h := NewBuildListHandler(&fakeBuildListStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds", nil)
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildList_WithPagination(t *testing.T) {
	store := &fakeBuildListStore{
		builds: []BuildListItem{},
		total:  50,
	}
	h := NewBuildListHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds?limit=10&offset=20", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildList_WithTimestamps(t *testing.T) {
	now := time.Now()
	started := now.Add(-5 * time.Minute)
	finished := now
	duration := 300
	store := &fakeBuildListStore{
		builds: []BuildListItem{
			{
				ID:              "bld-1",
				ProjectID:       "my-api",
				Environment:     "production",
				Status:          "SUCCEEDED",
				CommitSHA:       "abc123",
				CreatedAt:       now.Add(-10 * time.Minute),
				StartedAt:       &started,
				FinishedAt:      &finished,
				DurationSeconds: &duration,
			},
		},
		total: 1,
	}
	h := NewBuildListHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp buildListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Builds[0].StartedAt == "" {
		t.Fatal("want started_at to be set")
	}
	if resp.Builds[0].FinishedAt == "" {
		t.Fatal("want finished_at to be set")
	}
}

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		input      string
		defaultVal int
		want       int
	}{
		{"", 20, 20},
		{"10", 20, 10},
		{"abc", 20, 20},
		{"0", 20, 0},
	}

	for _, tt := range tests {
		got := parseIntParam(tt.input, tt.defaultVal)
		if got != tt.want {
			t.Errorf("parseIntParam(%q, %d) = %d, want %d", tt.input, tt.defaultVal, got, tt.want)
		}
	}
}
