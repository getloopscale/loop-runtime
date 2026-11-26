// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Helpers shared by CLI command E2E tests.
//
// These tests exercise individual `loop-runtime` subcommands against the running k3d
// stack. They follow the 5-contract template per command:
//
//  1. Happy path: command exits 0 in the documented success scenario
//  2. Failure path: violated preconditions yield the documented exit code
//  3. --json schema: machine output is parseable JSON with required fields
//  4. Internal terms: no leak per Rule 3 (ArgoCD/Gitea/MinIO/etc.)
//  5. Exit codes: only 0/1/2/3/4/5 are produced (Rule 4)
//
// Set LOOP_RUNTIME_E2E=1 to enable. Tests skip when k3d not running.
package e2e

import (
	"encoding/json"
	"errors"
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

// loopRuntimeInvocation describes one run of the `loop-runtime` CLI. The zero value is the
// suite default: the E2E API URL, auth from the per-suite configStore, and the
// current working directory.
type loopRuntimeInvocation struct {
	// dir is the working directory; empty means the test process's own.
	dir string
	// env entries are applied AFTER the defaults, so a KEY=value here wins
	// (os/exec keeps the last value of a duplicated key).
	env []string
	// unset names keys removed from the inherited environment entirely —
	// "absent", which an empty value is not.
	unset []string
}

// execLoopRuntime is the one place an E2E spec spawns `loop-runtime`. It captures stdout,
// stderr and the exit code; a CLI that never started reports -1, never 0, so an
// exit-0 assertion cannot pass with no binary on PATH.
//
// T2 amendment 2026-05-13 (finding-2026-05-13-e2e-token-refresh-divergence):
// auth comes from the per-suite XDG_CONFIG_HOME populated by BeforeSuite via
// `loopscale login --token`, NOT from LOOP_RUNTIME_TOKEN env injection. The CLI reads the
// token from configStore (production parity per Rule 10). LOOP_RUNTIME_TOKEN is
// therefore hard-stripped by default — if the parent process had it set it
// would silently override configStore. A spec that deliberately sends a
// token passes it in loopRuntimeInvocation.env.
func execLoopRuntime(inv loopRuntimeInvocation, args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command("loop-runtime", args...)
	cmd.Dir = inv.dir
	env := make([]string, 0, len(os.Environ())+2+len(inv.env))
	for _, kv := range os.Environ() {
		if !hasEnvKey(kv, inv.unset) {
			env = append(env, kv)
		}
	}
	env = append(env, "LOOP_RUNTIME_API_URL="+apiURL(), "LOOP_RUNTIME_TOKEN=")
	cmd.Env = append(env, inv.env...)

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			errBuf.WriteString("\n[execLoopRuntime] loop-runtime did not start: " + err.Error())
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// hasEnvKey reports whether the KEY=value entry kv names one of keys.
func hasEnvKey(kv string, keys []string) bool {
	for _, k := range keys {
		if strings.HasPrefix(kv, k+"=") {
			return true
		}
	}
	return false
}

// runLoopRuntime executes `loop-runtime <args...>` with the suite defaults.
func runLoopRuntime(args ...string) (stdout, stderr string, exitCode int) {
	return execLoopRuntime(loopRuntimeInvocation{}, args...)
}

// longAPITimeout bumps LOOP_RUNTIME_API_TIMEOUT to 120s (vs CLI default 30s in
// cli/src/config/constants.ts:53): project delete cascades through cancel
// builds + gitops remove + sync and exceeds 30s on LIVE k3d.
const longAPITimeout = "LOOP_RUNTIME_API_TIMEOUT=120000"

// runLoopRuntimeInDir executes `loop-runtime <args...>` in dir, for commands like `validate`,
// `project delete` and `deploy` that operate on cwd.
func runLoopRuntimeInDir(dir string, args ...string) (stdout, stderr string, exitCode int) {
	return execLoopRuntime(loopRuntimeInvocation{dir: dir, env: []string{longAPITimeout}}, args...)
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
