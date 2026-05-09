// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

func runProbe(url string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck-probe: request build failed: %v\n", err)
		os.Exit(1)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck-probe: GET %s failed: %v\n", url, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		os.Exit(0)
	}
	fmt.Fprintf(os.Stderr, "healthcheck-probe: GET %s returned %d\n", url, resp.StatusCode)
	os.Exit(1)
}
