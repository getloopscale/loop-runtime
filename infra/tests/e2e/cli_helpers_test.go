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
	"regexp"
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

// runLoopZeroWithStdin runs `loopzero <args...>` and pipes `stdin` into the process
// stdin. Used to exercise interactive-prompt paths (e.g., logout confirmation).
// Auth resolves from configStore (XDG_CONFIG_HOME populated in BeforeSuite),
// per T2 amendment 2026-05-13. See runLoopZero for rationale.
func runLoopZeroWithStdin(stdin string, args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command("loopzero", args...)
	cmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		"LOOPZERO_TOKEN=",
	)
	cmd.Stdin = strings.NewReader(stdin)

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

// runLoopZeroNoToken runs `loopzero <args...>` with LOOPZERO_TOKEN deliberately empty.
// Used to exercise auth-failure paths.
//
// Plan 2026-05-19-drill-skills-rigor-restoration: per T2 amendment 2026-05-13
// (finding-2026-05-13-e2e-token-refresh-divergence), CLI now reads token from
// configStore (XDG_CONFIG_HOME) populated by BeforeSuite. Setting LOOPZERO_TOKEN=""
// alone is NOT enough — configStore would still have a token. Fix: point
// XDG_CONFIG_HOME to a temp empty dir for this invocation so configStore is
// also empty. This restores intended auth-failure path coverage.
func runLoopZeroNoToken(args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command("loopzero", args...)
	emptyXDG, xdgErr := os.MkdirTemp("", "loopzero-e2e-noauth-")
	if xdgErr != nil {
		emptyXDG = "/tmp/loopzero-e2e-noauth-fallback"
		_ = os.MkdirAll(emptyXDG, 0o755)
	}
	defer os.RemoveAll(emptyXDG)
	cmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		"LOOPZERO_TOKEN=",
		"XDG_CONFIG_HOME="+emptyXDG,
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

// expectNoInternalTerms asserts that `text` does not contain any forbidden
// internal term per Rule 3. Used against both stdout and stderr for every CLI
// E2E test.
func expectNoInternalTerms(text string, context string) {
	lowered := strings.ToLower(text)
	for _, term := range internalTerms {
		if strings.Contains(lowered, strings.ToLower(term)) {
			Fail(fmt.Sprintf(
				"Rule 3 violation: %q found in %s output\n--- output ---\n%s\n",
				term, context, text,
			))
		}
	}
}

// expectNoConcurrencyTerms asserts that `text` does not contain any forbidden
// term from the stricter concurrencyForbiddenTerms list (Rule 3 + concurrency
// hardening). Used by concurrent-deploy rejection tests where leaking "lock"
// or "advisory" would expose internal locking primitives.
func expectNoConcurrencyTerms(text string, context string) {
	lowered := strings.ToLower(text)
	for _, term := range concurrencyForbiddenTerms {
		if strings.Contains(lowered, strings.ToLower(term)) {
			Fail(fmt.Sprintf(
				"Rule 3 (concurrency) violation: %q found in %s output\n--- output ---\n%s\n",
				term, context, text,
			))
		}
	}
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

// loopzeroCLIAvailable returns true if `loopzero` binary is on PATH.
func loopzeroCLIAvailable() bool {
	_, err := exec.LookPath("loopzero")
	return err == nil
}

// loopzeroTokenAvailable returns true if LOOPZERO_TOKEN env var is non-empty.
// Many CLI E2E tests need a valid auth token to reach the API.
func loopzeroTokenAvailable() bool {
	return os.Getenv("LOOPZERO_TOKEN") != ""
}

// jsonStringField extracts a string field from a parsed JSON object.
// Returns "" if absent or wrong type. Caller asserts on emptiness.
func jsonStringField(obj map[string]interface{}, key string) string {
	v, ok := obj[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// jsonBoolField extracts a bool field. Returns the value plus whether the
// field was present and of bool type.
func jsonBoolField(obj map[string]interface{}, key string) (value bool, ok bool) {
	v, exists := obj[key]
	if !exists {
		return false, false
	}
	b, isBool := v.(bool)
	return b, isBool
}

// looksLikeJSONLine returns true when `s` starts with `{` and ends with `}`,
// a quick syntactic check used before full JSON parse for clearer failures.
var jsonLineRegex = regexp.MustCompile(`^\s*\{.*\}\s*$`)

func looksLikeJSONLine(s string) bool {
	return jsonLineRegex.MatchString(strings.TrimSpace(s))
}
