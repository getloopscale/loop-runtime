// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package stepwatch

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestClassify_OOMKilled(t *testing.T) {
	if got := Classify("Container exited with OOMKilled", ""); got != ErrorClassOOM {
		t.Fatalf("expected OOM, got %s", got)
	}
}

func TestClassify_OOMExitCode137(t *testing.T) {
	if got := Classify("pod exit code 137", ""); got != ErrorClassOOM {
		t.Fatalf("expected OOM from exit 137, got %s", got)
	}
}

func TestClassify_BuildTimeout(t *testing.T) {
	if got := Classify("context deadline exceeded", ""); got != ErrorClassBuildTimeout {
		t.Fatalf("expected BUILD_TIMEOUT, got %s", got)
	}
}

func TestClassify_ImagePullBackOff(t *testing.T) {
	if got := Classify("step failed", "ImagePullBackOff: error pulling base"); got != ErrorClassImagePullFail {
		t.Fatalf("expected IMAGE_PULL_FAILED, got %s", got)
	}
}

func TestClassify_Unknown(t *testing.T) {
	if got := Classify("random unmatched error", ""); got != ErrorClassUnknown {
		t.Fatalf("expected UNKNOWN, got %s", got)
	}
}

func TestClassify_PrecedenceOOMOverTimeout(t *testing.T) {
	// pod was OOMKilled AFTER hitting timeout — OOM wins (more specific
	// root cause).
	if got := Classify("OOMKilled after context deadline exceeded", ""); got != ErrorClassOOM {
		t.Fatalf("expected OOM precedence, got %s", got)
	}
}

func TestClassify_CancelledByUser(t *testing.T) {
	if got := Classify("argo: terminated", ""); got != ErrorClassCancelledByUser {
		t.Fatalf("expected CANCELLED_BY_USER, got %s", got)
	}
}

func TestClassify_WorkflowDeleted(t *testing.T) {
	if got := Classify("", "workflow was deleted while running"); got != ErrorClassWorkflowDeleted {
		t.Fatalf("expected WORKFLOW_DELETED, got %s", got)
	}
}

func TestClassify_CosignFailed(t *testing.T) {
	if got := Classify("cosign sign failed: rekor unavailable", ""); got != ErrorClassCosignFailed {
		t.Fatalf("expected COSIGN_FAILED, got %s", got)
	}
}

func TestClassify_DockerfileLint(t *testing.T) {
	if got := Classify("DOCKERFILE_LINT_FAILED: FROM_REGISTRY_NOT_ALLOWED", ""); got != ErrorClassDockerfileLint {
		t.Fatalf("expected DOCKERFILE_LINT, got %s", got)
	}
}

func TestSuggestionFor_AllClassesHaveText(t *testing.T) {
	classes := []ErrorClass{
		ErrorClassOOM, ErrorClassBuildTimeout, ErrorClassImagePullFail,
		ErrorClassCosignFailed, ErrorClassRegistryAuth, ErrorClassDockerfileLint,
		ErrorClassLoopRuntimeYAMLInvalid, ErrorClassCancelledByUser, ErrorClassWorkflowDeleted,
		ErrorClassUnknown,
	}
	for _, c := range classes {
		s := SuggestionFor(c)
		if s == "" {
			t.Fatalf("class %s has empty suggestion", c)
		}
	}
}

func TestSuggestionFor_UnknownClassFallsBack(t *testing.T) {
	got := SuggestionFor("NOVEL_CLASS_NOT_IN_MAP")
	if got != suggestions[ErrorClassUnknown] {
		t.Fatalf("expected fallback to UNKNOWN suggestion, got %q", got)
	}
}

// Red Team loopruntime-cloud#123 — the LOOPRUNTIME_YAML_INVALID suggestion (the only
// user-facing string in this map that named a manifest) must guide the
// customer to the canonical `.loopzero/services.json`, not the legacy
// `loopzero.yaml`. ErrorClassLoopRuntimeYAMLInvalid stays as an internal class name
// (engine-side errors may still mention "loopzero.yaml" during the v2
// transition; Classify() still matches that substring upstream); the
// SUGGESTION the dashboard renders is what must be honest.
func TestSuggestionFor_LoopRuntimeYAMLInvalid_CanonicalManifest_123(t *testing.T) {
	got := SuggestionFor(ErrorClassLoopRuntimeYAMLInvalid)
	if strings.Contains(got, "loopzero.yaml") {
		t.Fatalf("RED #123: suggestion must NOT reference legacy loopzero.yaml; got %q", got)
	}
	if !strings.Contains(got, ".loopzero/services.json") {
		t.Fatalf("RED #123: suggestion MUST reference canonical .loopzero/services.json; got %q", got)
	}
}

func TestClassifyFromErr_NilReturnsUnknown(t *testing.T) {
	if got := ClassifyFromErr(nil, ""); got != ErrorClassUnknown {
		t.Fatalf("expected UNKNOWN for nil err, got %s", got)
	}
}

func TestClassifyFromErr_ContextCanceledReturnsCancelled(t *testing.T) {
	err := context.Canceled
	if got := ClassifyFromErr(err, ""); got != ErrorClassCancelledByUser {
		t.Fatalf("expected CANCELLED_BY_USER for context.Canceled, got %s", got)
	}
}

func TestClassifyFromErr_WrappedContextCanceledReturnsCancelled(t *testing.T) {
	// fmt.Errorf("%w", context.Canceled) preserves errors.Is — so
	// ClassifyFromErr early-returns CancelledByUser via the errors.Is
	// branch, not via the substring path.
	err := fmt.Errorf("submit: %w", context.Canceled)
	if got := ClassifyFromErr(err, ""); got != ErrorClassCancelledByUser {
		t.Fatalf("expected CANCELLED_BY_USER for wrapped context.Canceled, got %s", got)
	}
}
