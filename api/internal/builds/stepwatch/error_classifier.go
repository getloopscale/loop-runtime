// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package stepwatch

import (
	"context"
	"errors"
	"strings"
)

// ErrorClass is one of a small fixed set of categories that group build
// failures with similar root causes. Used by the dashboard to render an
// actionable suggestion (Plan J-17 D7).
type ErrorClass string

// Canonical error classes. Order matters in Classify (most specific
// wins). Frontend mirrors this list in sse-events.ts.
const (
	ErrorClassOOM                 ErrorClass = "OOM"
	ErrorClassBuildTimeout        ErrorClass = "BUILD_TIMEOUT"
	ErrorClassImagePullFail       ErrorClass = "IMAGE_PULL_FAILED"
	ErrorClassCosignFailed        ErrorClass = "COSIGN_FAILED"
	ErrorClassRegistryAuth        ErrorClass = "REGISTRY_AUTH"
	ErrorClassDockerfileLint      ErrorClass = "DOCKERFILE_LINT"
	ErrorClassLoopZeroYAMLInvalid ErrorClass = "LOOPZERO_YAML_INVALID"
	ErrorClassCancelledByUser     ErrorClass = "CANCELLED_BY_USER"
	ErrorClassWorkflowDeleted     ErrorClass = "WORKFLOW_DELETED"
	ErrorClassUnknown             ErrorClass = "UNKNOWN"
)

// suggestions maps an error class to a user-facing actionable hint. Rule 3:
// no internal terms ("Argo", "Workflow", "K8s") — phrased for the app
// developer who reads the dashboard.
var suggestions = map[ErrorClass]string{
	ErrorClassOOM:            "Container exceeded memory limit. Increase plan resources or optimize image.",
	ErrorClassBuildTimeout:   "Build exceeded 30 minutes. Check for stuck dependencies or split large monorepos.",
	ErrorClassImagePullFail:  "Could not pull base image. Verify Dockerfile FROM reference or registry access.",
	ErrorClassCosignFailed:   "Image signing failed. Operator may need to rotate the signing key.",
	ErrorClassRegistryAuth:   "Could not push to image registry. Verify pull secret and registry credentials.",
	ErrorClassDockerfileLint: "Dockerfile failed validation. See lint output for the specific violation.",
	// Red Team loopzero-cloud#123 — guide the customer to the canonical manifest
	// (loopzerokit v2 emits `.loopzero/services.json`; the legacy `loopzero.yaml` has
	// been retired). The class name stays `LOOPZERO_YAML_INVALID` for engine
	// compat — Classify() still matches "loopzero.yaml" substrings upstream — but
	// the user-visible suggestion is honest.
	ErrorClassLoopZeroYAMLInvalid: "Project manifest could not be parsed. Check your `.loopzero/services.json` and run `loopzero validate` locally.",
	ErrorClassCancelledByUser:     "Deploy was cancelled. Trigger a new deploy when ready.",
	ErrorClassWorkflowDeleted:     "Build was interrupted by an operator. Trigger a new deploy.",
	ErrorClassUnknown:             "Build step failed. See build logs for details.",
}

// SuggestionFor returns the user-facing actionable hint for an error class.
func SuggestionFor(class ErrorClass) string {
	if s, ok := suggestions[class]; ok {
		return s
	}
	return suggestions[ErrorClassUnknown]
}

// Classify inspects the raw error string and an optional log preview
// (last 1-5 KB of pod stderr) and returns the most specific error class
// it recognises. Heuristic: substring + exact-keyword matching. Order
// matters — OOM precedes generic "timeout" because an OOMKilled pod
// emits both signals.
//
// EC-7: cancelled-by-user is identified by the special phrase
// "cancelled" in the Argo node message OR a presence of context cancel
// in the raw error.
func Classify(rawError string, logPreview string) ErrorClass {
	combined := strings.ToLower(rawError + "\n" + logPreview)

	// CancelledByUser must precede UNKNOWN because raw cancel errors
	// otherwise leak through with generic "context canceled" message.
	switch {
	case strings.Contains(combined, "cancelled by user"),
		strings.Contains(combined, "cancelled_by_user"),
		strings.Contains(combined, "argo: terminated"):
		return ErrorClassCancelledByUser
	case strings.Contains(combined, "workflow_deleted"),
		strings.Contains(combined, "workflow was deleted"):
		return ErrorClassWorkflowDeleted
	case strings.Contains(combined, "oomkilled"),
		strings.Contains(combined, "out of memory"),
		strings.Contains(combined, "exit code 137"):
		return ErrorClassOOM
	case strings.Contains(combined, "context deadline exceeded"),
		strings.Contains(combined, "build timeout"),
		strings.Contains(combined, "deadlineexceeded"):
		return ErrorClassBuildTimeout
	case strings.Contains(combined, "imagepullbackoff"),
		strings.Contains(combined, "errimagepull"),
		strings.Contains(combined, "manifest unknown"):
		return ErrorClassImagePullFail
	case strings.Contains(combined, "cosign"),
		strings.Contains(combined, "signing failed"),
		strings.Contains(combined, "rekor"):
		return ErrorClassCosignFailed
	case strings.Contains(combined, "denied: access"),
		strings.Contains(combined, "authentication required"),
		strings.Contains(combined, "registry: unauthorized"):
		return ErrorClassRegistryAuth
	case strings.Contains(combined, "dockerfile_lint_failed"),
		strings.Contains(combined, "dockerfile lint"),
		strings.Contains(combined, "from_registry_not_allowed"):
		return ErrorClassDockerfileLint
	case strings.Contains(combined, "loopzero.yaml"),
		strings.Contains(combined, "invalid loopzero yaml"):
		return ErrorClassLoopZeroYAMLInvalid
	}
	return ErrorClassUnknown
}

// ClassifyFromErr is a Go-error-aware adapter around Classify. Errors
// wrapping context.Canceled propagate as CancelledByUser.
func ClassifyFromErr(err error, logPreview string) ErrorClass {
	if errors.Is(err, context.Canceled) {
		return ErrorClassCancelledByUser
	}
	if err == nil {
		return ErrorClassUnknown
	}
	return Classify(err.Error(), logPreview)
}
