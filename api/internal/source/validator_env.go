// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
)

// Plan v1.2 T1.3 (cutover-deep-review-hardening): reserved environment
// variable key blocklist. Schema-level patternProperties + maxProperties +
// maxLength enforce POSIX key shape + bounded payload; this blocklist runs
// AFTER schema validation to reject keys whose presence would compromise the
// runtime container — even when the key shape matches POSIX.
//
// Categories blocked:
//   - PATH, HOME, USER, SHELL: shell/process discovery hijack
//   - LD_PRELOAD, LD_LIBRARY_PATH: dynamic linker hijack (Linux glibc)
//   - DYLD_*: macOS dynamic linker hijack (defensive — runtime is Linux)
//   - KUBERNETES_*: K8s service-discovery override (downward API conflict)
//   - LOOPZERO_* (prefix): platform-reserved variables (audit, billing, telemetry)
//
// EC-8 (v1.1 absorbed): `LOOPZERO_ENV_BLOCKLIST_MODE` env var lets ops roll the
// blocklist out as warn-only first. Values:
//
//	"warn"    — log violations, accept payload (default during 30-day window)
//	"enforce" — reject payload with VALIDATION_FAILED (target steady state)
//
// Empty / unknown values default to "enforce" — fail-closed per CLAUDE.md
// Error Handling §8 (fail fast, fail clear, fail high).
var reservedEnvKeys = map[string]struct{}{
	"PATH":            {},
	"HOME":            {},
	"USER":            {},
	"SHELL":           {},
	"LD_PRELOAD":      {},
	"LD_LIBRARY_PATH": {},
}

// reservedEnvKeyPrefixes blocks any key starting with one of these strings.
// Order is insignificant — matching is OR over the slice.
var reservedEnvKeyPrefixes = []string{
	"DYLD_",
	"KUBERNETES_",
	"LOOPZERO_",
}

// envBlocklistMode reports the active enforcement mode for the reserved key
// blocklist. Reads `LOOPZERO_ENV_BLOCKLIST_MODE` at call time so the test suite
// can toggle behavior via t.Setenv without process restart.
func envBlocklistMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOOPZERO_ENV_BLOCKLIST_MODE"))) {
	case "warn":
		return "warn"
	default:
		// fail-closed: empty / unknown / "enforce" all collapse to enforce
		return "enforce"
	}
}

// isReservedEnvKey returns true when `key` matches the exact-block list OR
// any of the reserved prefixes. Case-sensitive — POSIX env vars are
// conventionally upper case and schema patternProperties already enforces it.
func isReservedEnvKey(key string) bool {
	if _, ok := reservedEnvKeys[key]; ok {
		return true
	}
	for _, prefix := range reservedEnvKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// validateReservedEnvKeys scans every service's env map and returns a
// ValidationError describing the first reserved key encountered (sorted
// deterministically for stable error messages). When the active mode is
// "warn", violations are logged but no error is returned.
//
// Returns nil when no violations are found OR when running in warn mode.
func validateReservedEnvKeys(manifest *ServicesManifest, logger *slog.Logger) *ValidationError {
	if manifest == nil {
		return nil
	}
	mode := envBlocklistMode()

	type violation struct {
		service string
		key     string
	}
	var violations []violation

	for _, svc := range manifest.Services {
		// Sort keys for deterministic error output.
		keys := make([]string, 0, len(svc.Env))
		for k := range svc.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if isReservedEnvKey(k) {
				violations = append(violations, violation{service: svc.Name, key: k})
			}
		}
	}

	if len(violations) == 0 {
		return nil
	}

	first := violations[0]
	msg := fmt.Sprintf(
		"E-010: service %q sets reserved env var %q. Reserved keys: PATH, HOME, USER, SHELL, LD_PRELOAD, LD_LIBRARY_PATH, DYLD_*, KUBERNETES_*, LOOPZERO_*. Rename to an application-scoped key (e.g., APP_%s).",
		first.service, first.key, first.key,
	)

	if mode == "warn" {
		// Warn mode: log every violation but allow the payload through so
		// platform operators can observe blast radius before flipping to
		// enforce. NOTE: per CLAUDE.md Honesty §3, the upload IS accepted
		// here — telemetry MUST be captured upstream to drive the cutover.
		if logger != nil {
			for _, v := range violations {
				logger.Warn("services.json reserved env key (warn-only mode)",
					"service", v.service, "key", v.key, "mode", mode)
			}
		}
		return nil
	}

	// Enforce mode: reject with field-anchored validation error.
	// Sprint 4 EC-2 absorption: NewValidationError populates the Errors slice
	// alongside the legacy Field+Message scalars.
	return NewValidationError(
		fmt.Sprintf("/services/%s/env/%s", first.service, first.key),
		msg,
	)
}
