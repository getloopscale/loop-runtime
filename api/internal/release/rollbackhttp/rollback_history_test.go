// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package rollbackhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

type rollbackHistoryFake struct {
	tenantID, projectID string
	limit               int
	entries             []RollbackEntry
}

func (f *rollbackHistoryFake) ListRollbacks(_ context.Context, tenantID, projectID string, limit int) ([]RollbackEntry, error) {
	f.tenantID, f.projectID, f.limit = tenantID, projectID, limit
	return f.entries, nil
}

var historyNow = time.Date(2026, 11, 25, 20, 0, 0, 0, time.UTC)

func listRollbacks(t *testing.T, history RollbackHistory, url string) (int, rollbackListResponse) {
	t.Helper()
	h := NewRollbackHistoryHandler(history)
	h.now = func() time.Time { return historyNow }
	r := chi.NewRouter()
	r.Get("/api/v1/projects/{name}/rollbacks", func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(), auth.UserContextKeyPublic(),
			&auth.UserContext{UserID: "u-1", Email: "x@y", TenantID: "tn-1", Role: "owner"})
		h.List(w, req.WithContext(ctx))
	})
	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body rollbackListResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, body
}

// The list is what puts a rollback next to the deploys of a project: its id,
// who asked, the two releases and builds, and how it ended.
func TestRollbackHistory_ListsEachRollbackWithItsOutcome(t *testing.T) {
	finished := historyNow.Add(-58 * time.Minute)
	history := &rollbackHistoryFake{entries: []RollbackEntry{
		{
			ID: "rb-2", Environment: "production", Status: "FAILED", FailureReason: "sync timeout",
			FromReleaseID: "rel-b", FromBuildID: "build-b", ToReleaseID: "rel-a", ToBuildID: "build-a",
			CreatedBy: "u-1", CreatedAt: historyNow.Add(-time.Hour), FinishedAt: &finished,
		},
		{
			ID: "rb-1", Environment: "production", Status: "SUCCEEDED",
			FromReleaseID: "rel-a", FromBuildID: "build-a", ToReleaseID: "rel-0", ToBuildID: "build-0",
			CreatedBy: "u-2", CreatedAt: historyNow.Add(-2 * time.Hour), FinishedAt: &finished,
		},
	}}

	status, body := listRollbacks(t, history, "/api/v1/projects/app/rollbacks")

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if history.tenantID != "tn-1" || history.projectID != "app" {
		t.Errorf("read for tenant %q project %q", history.tenantID, history.projectID)
	}
	if len(body.Rollbacks) != 2 {
		t.Fatalf("rollbacks = %d, want 2", len(body.Rollbacks))
	}
	failed := body.Rollbacks[0]
	if failed.ID != "rb-2" || failed.Status != "failed" || failed.FailureReason != "sync timeout" {
		t.Errorf("first = %+v", failed)
	}
	if failed.From.ReleaseID != "rel-b" || failed.From.BuildID != "build-b" || failed.To.ReleaseID != "rel-a" || failed.To.BuildID != "build-a" {
		t.Errorf("from/to = %+v / %+v", failed.From, failed.To)
	}
	if failed.CreatedBy != "u-1" || failed.CreatedAt != "2026-11-25T19:00:00Z" || failed.FinishedAt == nil || *failed.FinishedAt != "2026-11-25T19:02:00Z" {
		t.Errorf("author and times = %+v", failed)
	}
	if body.Rollbacks[1].Status != "succeeded" {
		t.Errorf("second status = %q", body.Rollbacks[1].Status)
	}
}

// The outcome is written by the API process that started the rollback. A
// record still in progress long after the longest rollback can last belongs to
// a process that stopped, and is shown as interrupted, not as running.
func TestRollbackHistory_ARollbackLeftInProgressIsShownAsInterrupted(t *testing.T) {
	history := &rollbackHistoryFake{entries: []RollbackEntry{
		{ID: "rb-old", Status: "IN_PROGRESS", CreatedAt: historyNow.Add(-45 * time.Minute)},
		{ID: "rb-new", Status: "IN_PROGRESS", CreatedAt: historyNow.Add(-2 * time.Minute)},
	}}

	_, body := listRollbacks(t, history, "/api/v1/projects/app/rollbacks")

	if got := body.Rollbacks[0]; got.Status != "interrupted" || got.FinishedAt != nil || got.FailureReason == "" {
		t.Errorf("old = %+v", got)
	}
	if got := body.Rollbacks[1]; got.Status != "in_progress" || got.FailureReason != "" {
		t.Errorf("new = %+v", got)
	}
}

func TestRollbackHistory_BoundsTheLimit(t *testing.T) {
	for query, want := range map[string]int{"": 50, "?limit=5": 5, "?limit=100000": 200} {
		history := &rollbackHistoryFake{}
		if status, _ := listRollbacks(t, history, "/api/v1/projects/app/rollbacks"+query); status != http.StatusOK {
			t.Fatalf("%q: status = %d", query, status)
		}
		if history.limit != want {
			t.Errorf("%q: limit = %d, want %d", query, history.limit, want)
		}
	}
	if status, _ := listRollbacks(t, &rollbackHistoryFake{}, "/api/v1/projects/app/rollbacks?limit=abc"); status != http.StatusBadRequest {
		t.Errorf("limit=abc: status = %d, want 400", status)
	}
}

func TestRollbackHistory_AProjectWithNoRollbackAnswersAnEmptyList(t *testing.T) {
	h := NewRollbackHistoryHandler(&rollbackHistoryFake{})
	r := chi.NewRouter()
	r.Get("/api/v1/projects/{name}/rollbacks", func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(), auth.UserContextKeyPublic(),
			&auth.UserContext{UserID: "u-1", TenantID: "tn-1", Role: "owner"})
		h.List(w, req.WithContext(ctx))
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/projects/app/rollbacks", nil))

	if got := rec.Body.String(); got != "{\"rollbacks\":[]}\n" {
		t.Errorf("body = %q", got)
	}
}
