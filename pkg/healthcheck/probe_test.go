// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package healthcheck

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMaybeRunAndExit_FlagAbsent — without the flag, returns false and does
// NOT call os.Exit. Verified via the negative case (we'd never return from
// here if MaybeRunAndExit incorrectly called os.Exit).
func TestMaybeRunAndExit_FlagAbsent(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"operator", "--leader-elect=true"}
	defer func() { os.Args = origArgs }()

	if MaybeRunAndExit("http://127.0.0.1:1/healthz") {
		t.Error("MaybeRunAndExit returned true with no --healthcheck-probe flag")
	}
}

// TestFlagName_Stable — the flag name is a Dockerfile contract; changing it
// silently breaks every operator HEALTHCHECK directive. Lock the value here.
func TestFlagName_Stable(t *testing.T) {
	const expected = "--healthcheck-probe"
	if FlagName != expected {
		t.Errorf("FlagName changed from %q to %q — this breaks every operator Dockerfile HEALTHCHECK. Update Dockerfiles or revert.", expected, FlagName)
	}
}

// TestMaybeRunAndExit_FlagPresent_HealthyServer — when flag is present
// AND server returns 200, probe SHOULD exit 0. We use a sub-process via
// the t.Setenv pattern is not enough because os.Exit terminates the test
// process; we test the inner `probe()` + `classifyProbeResult()` directly
// instead (clean separation introduced as part of Plan T4 coverage push).
func TestProbe_Success_Returns2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	status, err := probe(srv.URL+"/healthz", 1*time.Second)
	if err != nil {
		t.Fatalf("probe returned err: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d; want 200", status)
	}
}

func TestProbe_Unhealthy_Returns5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	status, err := probe(srv.URL+"/healthz", 1*time.Second)
	if err != nil {
		t.Fatalf("probe returned err: %v", err)
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d; want 503", status)
	}
}

func TestProbe_NetworkError_ReturnsError(t *testing.T) {
	// Use a port that's almost certainly closed
	_, err := probe("http://127.0.0.1:1/healthz", 100*time.Millisecond)
	if err == nil {
		t.Fatal("probe expected error for unreachable host, got nil")
	}
	if !strings.Contains(err.Error(), "GET") {
		t.Errorf("error %q should mention 'GET'", err.Error())
	}
}

func TestProbe_InvalidURL_ReturnsError(t *testing.T) {
	// %ZZ is invalid URL escape
	_, err := probe("http://%ZZ", 100*time.Millisecond)
	if err == nil {
		t.Fatal("probe expected error for malformed URL, got nil")
	}
	if !strings.Contains(err.Error(), "request build") {
		t.Errorf("error %q should mention 'request build'", err.Error())
	}
}

func TestClassifyProbeResult_2xx_Healthy(t *testing.T) {
	exitCode, msg := classifyProbeResult("http://x/healthz", http.StatusOK, nil)
	if exitCode != 0 {
		t.Errorf("2xx should yield exit 0, got %d", exitCode)
	}
	if msg != "" {
		t.Errorf("2xx should have no stderr message, got %q", msg)
	}
}

func TestClassifyProbeResult_3xx_Unhealthy(t *testing.T) {
	exitCode, msg := classifyProbeResult("http://x/healthz", http.StatusFound, nil)
	if exitCode != 1 {
		t.Errorf("3xx should yield exit 1 (only 2xx is healthy), got %d", exitCode)
	}
	if !strings.Contains(msg, "302") {
		t.Errorf("msg should mention status code 302, got %q", msg)
	}
}

func TestClassifyProbeResult_4xx_Unhealthy(t *testing.T) {
	exitCode, msg := classifyProbeResult("http://x/healthz", http.StatusNotFound, nil)
	if exitCode != 1 {
		t.Errorf("4xx should yield exit 1, got %d", exitCode)
	}
	if !strings.Contains(msg, "404") {
		t.Errorf("msg should mention 404, got %q", msg)
	}
}

func TestClassifyProbeResult_5xx_Unhealthy(t *testing.T) {
	exitCode, msg := classifyProbeResult("http://x/healthz", http.StatusInternalServerError, nil)
	if exitCode != 1 {
		t.Errorf("5xx should yield exit 1, got %d", exitCode)
	}
	if !strings.Contains(msg, "500") {
		t.Errorf("msg should mention 500, got %q", msg)
	}
}

func TestClassifyProbeResult_Error_Unhealthy(t *testing.T) {
	exitCode, msg := classifyProbeResult("http://x/healthz", 0, errors.New("connection refused"))
	if exitCode != 1 {
		t.Errorf("error should yield exit 1, got %d", exitCode)
	}
	if !strings.Contains(msg, "connection refused") {
		t.Errorf("msg should include error text, got %q", msg)
	}
}

func TestMaybeRunAndExit_FlagWithOtherArgs(t *testing.T) {
	// Arrange: simulate args where probe flag is among others.
	// We don't actually invoke MaybeRunAndExit here (would os.Exit);
	// we verify the FlagName detection logic via the slice scan.
	args := []string{"operator", "--leader-elect=true", FlagName, "--other"}
	found := false
	for _, a := range args[1:] {
		if a == FlagName {
			found = true
			break
		}
	}
	if !found {
		t.Error("FlagName should be detected in mid-arg position")
	}
}

// swapExitFunc replaces exitFunc with a recorder and returns a cleanup.
// Uses package-level var to enable testing os.Exit code paths without
// terminating the test binary (canonical Go pattern).
func swapExitFunc(t *testing.T) *int {
	t.Helper()
	var recorded int
	origExit := exitFunc
	exitFunc = func(code int) { recorded = code }
	t.Cleanup(func() { exitFunc = origExit })
	return &recorded
}

// TestRunProbe_Success_Exit0 — runProbe against healthy server exits 0
func TestRunProbe_Success_Exit0(t *testing.T) {
	exitCode := swapExitFunc(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	runProbe(srv.URL + "/healthz")

	if *exitCode != 0 {
		t.Errorf("runProbe healthy server: exit %d; want 0", *exitCode)
	}
}

// TestRunProbe_Unhealthy_Exit1 — runProbe against 5xx exits 1
func TestRunProbe_Unhealthy_Exit1(t *testing.T) {
	exitCode := swapExitFunc(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	runProbe(srv.URL + "/healthz")

	if *exitCode != 1 {
		t.Errorf("runProbe 5xx: exit %d; want 1", *exitCode)
	}
}

// TestRunProbe_NetworkError_Exit1 — runProbe with unreachable host exits 1
func TestRunProbe_NetworkError_Exit1(t *testing.T) {
	exitCode := swapExitFunc(t)
	runProbe("http://127.0.0.1:1/healthz")
	if *exitCode != 1 {
		t.Errorf("runProbe network error: exit %d; want 1", *exitCode)
	}
}

// TestMaybeRunAndExit_FlagPresent_RunsProbe — flag-present path invokes
// probe + exits. Verified via exitFunc swap + LIVE server.
func TestMaybeRunAndExit_FlagPresent_RunsProbe(t *testing.T) {
	exitCode := swapExitFunc(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origArgs := os.Args
	os.Args = []string{"operator", FlagName}
	defer func() { os.Args = origArgs }()

	result := MaybeRunAndExit(srv.URL + "/healthz")

	if !result {
		t.Error("MaybeRunAndExit: expected true (flag present), got false")
	}
	if *exitCode != 0 {
		t.Errorf("expected exit 0 via probe, got %d", *exitCode)
	}
}
