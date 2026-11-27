// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package healthcheck provides a tiny self-probe utility for distroless
// container HEALTHCHECK directives.
//
// T8.1 of deep-review-remediation-plan (closes find_infra_001 HIGH):
// the four operator binaries run on gcr.io/distroless/static-debian12:nonroot
// which has no shell, no curl, no wget — so a traditional HEALTHCHECK exec
// form cannot self-probe. The operator binary itself is the only executable
// in the image. This helper turns the binary into its own HEALTHCHECK by
// implementing a `--healthcheck-probe` flag that issues a single net/http
// GET against the controller-runtime health endpoint and exits 0 (healthy)
// or 1 (unhealthy/unreachable).
//
// Use from `func main()`:
//
//	if healthcheck.MaybeRunAndExit("http://127.0.0.1:8081/healthz") {
//	    return // unreachable; MaybeRunAndExit already called os.Exit
//	}
//
// The function checks the OS args for `--healthcheck-probe`. If absent,
// returns false and main continues normally.
package healthcheck

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
)

// FlagName is the CLI flag that toggles the self-probe mode.
const FlagName = "--healthcheck-probe"

// exitFunc is the function used to exit the probe process. Indirected
// through a package-level var so tests can substitute it without
// terminating the test binary (canonical Go pattern for os.Exit).
var exitFunc = os.Exit

// MaybeRunAndExit checks os.Args for FlagName. If present, performs an HTTP
// GET against url with a 3-second timeout, then calls os.Exit(0) on 2xx or
// os.Exit(1) otherwise. Returns false (and does NOT call os.Exit) if the
// flag is absent — caller should continue normally.
//
// Note: MaybeRunAndExit ignores other args. It is safe to call BEFORE any
// flag.Parse() so that the operator's normal argument parsing is bypassed
// in probe mode. This is important because the operator's flag.Parse() might
// fail or have side-effects (logger init) that should not run during a
// HEALTHCHECK probe.
func MaybeRunAndExit(url string) bool {
	for _, a := range os.Args[1:] {
		if a == FlagName {
			runProbe(url)
			return true // unreachable; runProbe always exits
		}
	}
	return false
}

// probe performs the HTTP GET and returns a probe result.
// Separated from runProbe so it is testable without forking a process
// or trapping os.Exit. runProbe is a thin wrapper that translates the
// result into stderr message + exit code.
func probe(url string, timeout time.Duration) (statusCode int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("request build failed: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// classifyProbeResult is pure: maps (statusCode, err) → (exitCode, stderrMsg).
// Tested directly; runProbe calls it before os.Exit.
func classifyProbeResult(url string, statusCode int, err error) (exitCode int, stderrMsg string) {
	if err != nil {
		return 1, fmt.Sprintf("healthcheck-probe: %v\n", err)
	}
	if statusCode >= 200 && statusCode < 300 {
		return 0, ""
	}
	return 1, fmt.Sprintf("healthcheck-probe: GET %s returned %d\n", url, statusCode)
}

func runProbe(url string) {
	statusCode, err := probe(url, 3*time.Second)
	exitCode, stderrMsg := classifyProbeResult(url, statusCode, err)
	if stderrMsg != "" {
		fmt.Fprint(os.Stderr, stderrMsg)
	}
	exitFunc(exitCode)
}
