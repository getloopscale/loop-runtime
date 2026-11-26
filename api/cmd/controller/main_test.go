// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/leader"
	"github.com/getloopscale/loop-runtime/pkg/domain"
)

// cellID() must default to domain.DefaultCellID when the env var is unset,
// and honor LOOPRUNTIME_CELL_ID when set. This is the same contract as the
// store layer (roadmap 03) so the controller binary stays consistent.

func TestCellID_DefaultsToCellDefaultWhenUnset(t *testing.T) {
	_ = os.Unsetenv("LOOPRUNTIME_CELL_ID")
	got := cellID(stubConfig())
	if got != domain.DefaultCellID {
		t.Errorf("cellID() = %q, want %q", got, domain.DefaultCellID)
	}
}

func TestCellID_HonorsEnvOverride(t *testing.T) {
	t.Setenv("LOOPRUNTIME_CELL_ID", "cell-sfo3-02")
	got := cellID(stubConfig())
	if got != "cell-sfo3-02" {
		t.Errorf("cellID() = %q, want cell-sfo3-02", got)
	}
}

func TestPodName_PrefersHostname(t *testing.T) {
	t.Setenv("HOSTNAME", "loopruntime-controller-abc123")
	got := podName()
	if got != "loopruntime-controller-abc123" {
		t.Errorf("podName() = %q, want loopruntime-controller-abc123", got)
	}
}

func TestPodName_FallsBackWhenHostnameUnset(t *testing.T) {
	_ = os.Unsetenv("HOSTNAME")
	got := podName()
	if got != "loopruntime-controller" {
		t.Errorf("podName() = %q, want loopruntime-controller (fallback)", got)
	}
}

// The /healthz endpoint must always return 200 regardless of lease state:
// that is the contract a liveness probe relies on, because liveness says
// "the process is alive", not "I still hold a lease". /readyz is the
// endpoint that carries the lease state.

func TestHealthz_Always200(t *testing.T) {
	srv := newHealthServer(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) == "" {
		t.Error("/healthz body empty")
	}
}

// /readyz uses a real Acquirer.Validate call. We inject a fake DBTX that
// always returns a stale row (different holder) so Validate returns
// ErrFenced and the handler reports 503.

func TestReadyz_ReturnsFenced_WhenLeaseInvalid(t *testing.T) {
	fake := &fakeDBTX{
		row: fakeRow{
			scan: func(dest ...any) error {
				// Return a different generation so Validate fences us.
				*dest[0].(*int64) = 99
				*dest[1].(*string) = "another-pod"
				return nil
			},
		},
	}
	acq := leader.NewAcquirer(fake, leader.DefaultConfig())
	lease := &leader.Lease{
		Name: leaderLoopName, CellID: domain.DefaultCellID,
		Holder: "this-pod", GenerationID: 1,
	}

	srv := newHealthServer(nil, acq, lease)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("/readyz fenced case = %d, want 503", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "fenced" {
		t.Errorf("/readyz body status = %v, want fenced", body["status"])
	}
}

func TestReadyz_ReturnsOK_WhenLeaseValid(t *testing.T) {
	fake := &fakeDBTX{
		row: fakeRow{
			scan: func(dest ...any) error {
				*dest[0].(*int64) = 5
				*dest[1].(*string) = "this-pod"
				return nil
			},
		},
	}
	acq := leader.NewAcquirer(fake, leader.DefaultConfig())
	lease := &leader.Lease{
		Name: leaderLoopName, CellID: domain.DefaultCellID,
		Holder: "this-pod", GenerationID: 5,
	}

	srv := newHealthServer(nil, acq, lease)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/readyz valid case = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("/readyz body status = %v, want ok", body["status"])
	}
	if body["generation"] != float64(5) { // JSON numbers round-trip as float64
		t.Errorf("/readyz body generation = %v, want 5", body["generation"])
	}
}
