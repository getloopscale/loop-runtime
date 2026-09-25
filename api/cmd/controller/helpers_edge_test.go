// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/leader"
)

// ---------------------------------------------------------------------------
// silenceKlog
// ---------------------------------------------------------------------------

func TestSilenceKlog_DoesNotPanic(t *testing.T) {
	// silenceKlog should be safe to call multiple times without panicking.
	silenceKlog()
	silenceKlog()
}

// ---------------------------------------------------------------------------
// cellID — additional edge cases
// ---------------------------------------------------------------------------

func TestCellID_EmptyEnvVar_ReturnsCellDefault(t *testing.T) {
	t.Setenv("LOOPZERO_CELL_ID", "")
	got := cellID(config.Config{})
	if got != db.CellDefault {
		t.Errorf("cellID() = %q, want %q for empty env var", got, db.CellDefault)
	}
}

func TestCellID_WhitespaceEnvVar_ReturnsWhitespace(t *testing.T) {
	// The function returns the raw value, no trimming — verify contract
	t.Setenv("LOOPZERO_CELL_ID", " cell-with-spaces ")
	got := cellID(config.Config{})
	if got != " cell-with-spaces " {
		t.Errorf("cellID() = %q, want raw env value with spaces", got)
	}
}

// ---------------------------------------------------------------------------
// podName — additional edge cases
// ---------------------------------------------------------------------------

func TestPodName_EmptyHostname_ReturnsFallback(t *testing.T) {
	t.Setenv("HOSTNAME", "")
	got := podName()
	if got != "loopzero-controller" {
		t.Errorf("podName() = %q, want loopzero-controller for empty HOSTNAME", got)
	}
}

// ---------------------------------------------------------------------------
// newHealthServer — additional coverage
// ---------------------------------------------------------------------------

func TestHealthz_ReturnsJSON(t *testing.T) {
	srv := newHealthServer(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("healthz should return valid JSON: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("status = %q, want ok", result["status"])
	}
}

func TestReadyz_IncludesAllFields_WhenValid(t *testing.T) {
	fake := &fakeDBTX{
		row: fakeRow{
			scan: func(dest ...any) error {
				*dest[0].(*int64) = 42
				*dest[1].(*string) = "my-pod"
				return nil
			},
		},
	}
	acq := leader.NewAcquirer(fake, leader.DefaultConfig())
	lease := &leader.Lease{
		Name:         leaderLoopName,
		CellID:       "cell-sfo3",
		Holder:       "my-pod",
		GenerationID: 42,
	}

	srv := newHealthServer(nil, acq, lease)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/readyz = %d, want 200", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Verify all expected fields are present
	expected := map[string]any{
		"status":     "ok",
		"generation": float64(42),
		"holder":     "my-pod",
		"name":       leaderLoopName,
		"cell_id":    "cell-sfo3",
	}
	for k, want := range expected {
		got, ok := body[k]
		if !ok {
			t.Errorf("missing field %q in readyz response", k)
			continue
		}
		if got != want {
			t.Errorf("readyz[%q] = %v, want %v", k, got, want)
		}
	}
}

func TestReadyz_Fenced_IncludesErrorField(t *testing.T) {
	fake := &fakeDBTX{
		row: fakeRow{
			scan: func(dest ...any) error {
				*dest[0].(*int64) = 999
				*dest[1].(*string) = "other-pod"
				return nil
			},
		},
	}
	acq := leader.NewAcquirer(fake, leader.DefaultConfig())
	lease := &leader.Lease{
		Name:         leaderLoopName,
		CellID:       db.CellDefault,
		Holder:       "my-pod",
		GenerationID: 1,
	}

	srv := newHealthServer(nil, acq, lease)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz = %d, want 503", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == nil || body["error"] == "" {
		t.Error("fenced readyz should include non-empty error field")
	}
}

func TestHealthServer_MetricsEndpoint_Returns200(t *testing.T) {
	srv := newHealthServer(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/metrics = %d, want 200", w.Code)
	}
}

func TestHealthServer_UnknownPath_Returns404(t *testing.T) {
	srv := newHealthServer(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("/unknown = %d, want 404", w.Code)
	}
}

// ---------------------------------------------------------------------------
// leaderLoopName constant
// ---------------------------------------------------------------------------

func TestLeaderLoopName_IsLoopZeroController(t *testing.T) {
	if leaderLoopName != "loopzero-controller" {
		t.Errorf("leaderLoopName = %q, want loopzero-controller", leaderLoopName)
	}
}

// ---------------------------------------------------------------------------
// healthPort constant
// ---------------------------------------------------------------------------

func TestHealthPort_Is9091(t *testing.T) {
	if healthPort != ":9091" {
		t.Errorf("healthPort = %q, want :9091", healthPort)
	}
}

// ---------------------------------------------------------------------------
// runHeartbeat — cancel propagation
// ---------------------------------------------------------------------------

func TestRunHeartbeat_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Use a fakeDBTX that returns the same generation+holder so
	// heartbeat succeeds (no fencing).
	fake := &fakeDBTX{
		row: fakeRow{
			scan: func(dest ...any) error {
				*dest[0].(*int64) = 1
				*dest[1].(*string) = "test-pod"
				return nil
			},
		},
	}
	acq := leader.NewAcquirer(fake, leader.DefaultConfig())
	lease := &leader.Lease{
		Name:         leaderLoopName,
		CellID:       db.CellDefault,
		Holder:       "test-pod",
		GenerationID: 1,
	}

	wg.Add(1)
	go runHeartbeat(ctx, &wg, acq, lease, slog.Default(), func() {})

	// Cancel immediately — runHeartbeat should return quickly
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		// passed
	case <-time.After(3 * time.Second):
		t.Fatal("runHeartbeat did not stop within 3s after context cancellation")
	}
}

func TestRunHeartbeat_InvokesCancelRoot_WhenFenced(t *testing.T) {
	// Simulate fencing: heartbeat sees a different generation → ErrFenced.
	// The heartbeat loop should call cancelRoot and return.
	fake := &fakeDBTX{
		row: fakeRow{
			scan: func(dest ...any) error {
				// Return a higher generation from a different holder
				*dest[0].(*int64) = 100
				*dest[1].(*string) = "other-pod"
				return nil
			},
		},
	}
	acq := leader.NewAcquirer(fake, leader.DefaultConfig())
	lease := &leader.Lease{
		Name:         leaderLoopName,
		CellID:       db.CellDefault,
		Holder:       "my-pod",
		GenerationID: 1,
	}

	// Use a context that will NOT be cancelled externally, so only
	// cancelRoot from fencing stops the heartbeat.
	ctx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	cancelCalled := make(chan struct{}, 1)
	cancelFn := func() {
		cancelCalled <- struct{}{}
		rootCancel()
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go runHeartbeat(ctx, &wg, acq, lease, slog.Default(), cancelFn)

	select {
	case <-cancelCalled:
		// cancelRoot was invoked — heartbeat detected fencing
	case <-time.After(10 * time.Second):
		rootCancel() // clean up
		t.Fatal("heartbeat did not invoke cancelRoot within 10s after fencing")
	}

	// Wait for goroutine cleanup
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runHeartbeat goroutine did not exit after fencing")
	}
}

// ---------------------------------------------------------------------------
// fakeDBTX — verify test helpers work
// ---------------------------------------------------------------------------

func TestFakeDBTX_Query_ReturnsNil(t *testing.T) {
	f := &fakeDBTX{}
	rows, err := f.Query(context.Background(), "SELECT 1")
	if rows != nil || err != nil {
		t.Errorf("Query should return nil, nil; got %v, %v", rows, err)
	}
}

func TestFakeDBTX_Exec_ReturnsZero(t *testing.T) {
	f := &fakeDBTX{}
	result, err := f.Exec(context.Background(), "UPDATE x SET y=1")
	if err != nil {
		t.Fatalf("Exec should return nil error; got %v", err)
	}
	_ = result // zero value is valid
}

func TestFakeRow_Scan_InvokesFn(t *testing.T) {
	called := false
	row := fakeRow{
		scan: func(dest ...any) error {
			called = true
			return nil
		},
	}
	_ = row.Scan()
	if !called {
		t.Error("scan function was not called")
	}
}

func TestStubConfig_ReturnsZeroValue(t *testing.T) {
	cfg := stubConfig()
	if cfg.BaseDomain != "" {
		t.Errorf("stubConfig().BaseDomain = %q, want empty", cfg.BaseDomain)
	}
}

// ---------------------------------------------------------------------------
// Env var isolation — verify t.Setenv cleanup works
// ---------------------------------------------------------------------------

func TestCellID_EnvIsolation(t *testing.T) {
	// First subtest sets the env var
	t.Run("set", func(t *testing.T) {
		t.Setenv("LOOPZERO_CELL_ID", "cell-isolated")
		if got := cellID(config.Config{}); got != "cell-isolated" {
			t.Errorf("got %q", got)
		}
	})

	// Second subtest should see the original value (unset by cleanup)
	t.Run("after_cleanup", func(t *testing.T) {
		// We cannot guarantee LOOPZERO_CELL_ID is unset in the outer scope,
		// but we can verify cellID returns SOMETHING without panicking
		got := cellID(config.Config{})
		if got == "" {
			t.Error("cellID should never return empty string")
		}
	})
}

func init() {
	// Ensure HOSTNAME is accessible during tests
	_ = os.Getenv("HOSTNAME")
}
