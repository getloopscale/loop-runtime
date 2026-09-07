// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Helpers shared by CLI command E2E tests.
//
// These tests exercise individual `loopzero` subcommands against the running k3d
// stack. They follow the 5-contract template per command:
//
//  1. Happy path: command exits 0 in the documented success scenario
//  2. Failure path: violated preconditions yield the documented exit code
//  3. --json schema: machine output is parseable JSON with required fields
//  4. Internal terms: no leak per Rule 3 (ArgoCD/Gitea/MinIO/etc.)
//  5. Exit codes: only 0/1/2/3/4/5 are produced (Rule 4)
//
// Set LOOPZERO_E2E=1 to enable. Tests skip when k3d not running.
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
)

// internalTerms references the canonical list from constants_test.go.
// Kept as alias for backward compatibility with existing tests.
var internalTerms = forbiddenInternalTerms

// allowedExitCodes references the canonical map from constants_test.go.
// Kept as alias for backward compatibility with existing tests.
var allowedExitCodes = allowedExitCodesMap

// runLoopZero executes `loopzero <args...>` with E2E env vars and captures
// stdout/stderr/exitCode.
//
// T2 amendment 2026-05-13 (finding-2026-05-13-e2e-token-refresh-divergence):
// auth comes from the per-suite XDG_CONFIG_HOME populated by BeforeSuite via
// `loopzero login --token`, NOT from LOOPZERO_TOKEN env injection. The CLI reads the
// token from configStore (production parity per Rule 10).
//
// LOOPZERO_TOKEN env is intentionally NOT forwarded — if it's set in the parent
// process it would silently override configStore (configStore.getToken()
// checks env first), defeating the refactor. Callers that need to override
// the resolved token should write to the test XDG configStore directly.
func runLoopZero(args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command("loopzero", args...)
	cmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		// Hard-strip the parent LOOPZERO_TOKEN so configStore is the only source.
		"LOOPZERO_TOKEN=",
	)

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// runLoopZeroInDir executes `loopzero <args...>` with the given working directory.
// Used for commands like `validate`, `project delete`, `deploy` that operate on cwd.
//
// Migrated from cli_validate_test.go on 2026-05-25 when that file (and 50
// other cli_*_test.go specs) was deleted per user mandate to absorb only
// journey-relevant tests. LOOPZERO_API_TIMEOUT is bumped to 120s (vs CLI
// default 30s in cli/src/config/constants.ts:53) because project delete
// cascades through cancel builds + gitops remove + ArgoCD sync and exceeds
// 30s on LIVE k3d. Auth via configStore (XDG_CONFIG_HOME from BeforeSuite),
// matching runLoopZero per T2 amendment 2026-05-13.
func runLoopZeroInDir(dir string, args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command("loopzero", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		"LOOPZERO_API_TIMEOUT=120000",
		"LOOPZERO_TOKEN=",
	)

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// expectAllowedExitCode asserts that `code` is one of 0/1/2/3/4/5 per Rule 4.
func expectAllowedExitCode(code int, context string) {
	_, ok := allowedExitCodes[code]
	if !ok {
		Fail(fmt.Sprintf(
			"Rule 4 violation: exit code %d in %s is not allowed (only 0/1/2/3/4/5)",
			code, context,
		))
	}
}

// parseJSON parses the first JSON object found in `text`. Returns the parsed
// map plus an error describing why parsing failed. Empty/non-JSON text is an
// error so callers can assert on it.
func parseJSON(text string) (map[string]interface{}, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, fmt.Errorf("empty output")
	}
	// Common case: output is a single JSON line. Fall back to scanning if not.
	var first map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &first); err == nil {
		return first, nil
	}

	// Fallback: scan line by line for a JSON object (some commands print
	// human-friendly text + a JSON line in --json mode if implementation
	// drifts; we still want to find structured output).
	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err == nil {
			return obj, nil
		}
	}
	return nil, fmt.Errorf("no parseable JSON object found in output")
}
