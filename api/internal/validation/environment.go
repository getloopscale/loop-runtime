// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package validation

import (
	"fmt"
	"regexp"
)

// rfc1123Label is the Kubernetes name rule: lowercase alphanumerics and
// hyphens, 1-63 characters, no leading or trailing hyphen.
var rfc1123Label = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// environmentName is an RFC 1123 label that also starts with a letter.
var environmentName = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

// IsRFC1123Name reports whether s is a valid Kubernetes resource name (used
// for project names).
func IsRFC1123Name(s string) bool {
	return rfc1123Label.MatchString(s)
}

// ValidateEnvironmentName is the one rule for an environment name, applied
// by every route that accepts one (POST /builds, POST /environments). The
// name becomes a segment of the namespace loop-runtime-<tenant>-<project>-<env>, so
// it must be a DNS-1123 label (no trailing hyphen); it must also start with
// a letter. Two routes used to answer this with two different regexes, so a
// name could be deployable and not creatable, or the reverse.
func ValidateEnvironmentName(name string) error {
	if !environmentName.MatchString(name) {
		return fmt.Errorf("environment name %q must start with a lowercase letter, contain only lowercase letters, digits and hyphens, not end with a hyphen, and be at most 63 characters", name)
	}
	return nil
}
