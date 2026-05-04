// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package errors

import (
	stderrors "errors"
	"fmt"
	"strings"
	"testing"
)

func TestLoopZeroError_Error(t *testing.T) {
	err := &LoopZeroError{Code: "TEST_CODE", Message: "test message", HTTPStatus: 500}
	expected := "[TEST_CODE] test message"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestLoopZeroError_ImplementsError(t *testing.T) {
	var _ error = &LoopZeroError{}
}

func TestLoopZeroError_Is(t *testing.T) {
	t.Run("matches by code", func(t *testing.T) {
		err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "custom", HTTPStatus: 409}
		if !stderrors.Is(err, ErrLockTimeout) {
			t.Error("expected errors.Is to match by code")
		}
	})

	t.Run("does not match different code", func(t *testing.T) {
		err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "custom", HTTPStatus: 409}
		if stderrors.Is(err, ErrBuildNotFound) {
			t.Error("expected errors.Is not to match different code")
		}
	})

	t.Run("does not match non-LoopZeroError", func(t *testing.T) {
		err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "custom", HTTPStatus: 409}
		if stderrors.Is(err, stderrors.New("some other error")) {
			t.Error("expected errors.Is not to match non-LoopZeroError")
		}
	})
}

func TestLoopZeroError_Wrap(t *testing.T) {
	original := ErrBuildNotFound
	wrapped := original.Wrap("deploy-service")
	if wrapped.Code != original.Code {
		t.Errorf("expected code %q, got %q", original.Code, wrapped.Code)
	}
	if !strings.HasPrefix(wrapped.Message, "deploy-service: ") {
		t.Errorf("expected message to start with context, got %q", wrapped.Message)
	}
	if wrapped.HTTPStatus != original.HTTPStatus {
		t.Errorf("expected status %d, got %d", original.HTTPStatus, wrapped.HTTPStatus)
	}
}

func TestLoopZeroError_ToJSON(t *testing.T) {
	err := &LoopZeroError{Code: "MY_CODE", Message: "details here", HTTPStatus: 409}
	j := err.ToJSON()
	if j["code"] != "MY_CODE" {
		t.Errorf("expected code MY_CODE, got %q", j["code"])
	}
	if j["message"] != "details here" {
		t.Errorf("expected message 'details here', got %q", j["message"])
	}
}

func TestNewLoopZeroError(t *testing.T) {
	err := NewLoopZeroError("CUSTOM", "custom message", 418)
	if err.Code != "CUSTOM" {
		t.Errorf("expected code CUSTOM, got %q", err.Code)
	}
	if err.Message != "custom message" {
		t.Errorf("expected message 'custom message', got %q", err.Message)
	}
	if err.HTTPStatus != 418 {
		t.Errorf("expected status 418, got %d", err.HTTPStatus)
	}
}

func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        *LoopZeroError
		code       string
		httpStatus int
	}{
		{"ErrLockTimeout", ErrLockTimeout, "LOCK_TIMEOUT", 409},
		{"ErrInvalidTransition", ErrInvalidTransition, "INVALID_TRANSITION", 409},
		{"ErrTenantNotFound", ErrTenantNotFound, "TENANT_NOT_FOUND", 404},
		{"ErrProjectNotFound", ErrProjectNotFound, "PROJECT_NOT_FOUND", 404},
		{"ErrBuildNotFound", ErrBuildNotFound, "BUILD_NOT_FOUND", 404},
		{"ErrReleaseNotFound", ErrReleaseNotFound, "RELEASE_NOT_FOUND", 404},
		{"ErrNoActiveRelease", ErrNoActiveRelease, "NO_ACTIVE_RELEASE", 404},
		{"ErrNoPreviousRelease", ErrNoPreviousRelease, "NO_PREVIOUS_RELEASE", 404},
		{"ErrBuildAlreadyCompleted", ErrBuildAlreadyCompleted, "BUILD_ALREADY_COMPLETED", 409},
		{"ErrDeployInProgress", ErrDeployInProgress, "DEPLOY_IN_PROGRESS", 409},
		{"ErrInvalidName", ErrInvalidName, "INVALID_NAME", 400},
		{"ErrIdempotencyConflict", ErrIdempotencyConflict, "IDEMPOTENCY_CONFLICT", 422},
		{"ErrUnauthorized", ErrUnauthorized, "UNAUTHORIZED", 401},
		{"ErrInvalidRequest", ErrInvalidRequest, "INVALID_REQUEST", 400},
		{"ErrProjectExists", ErrProjectExists, "PROJECT_EXISTS", 409},
		{"ErrInternalError", ErrInternalError, "INTERNAL_ERROR", 500},
		{"ErrPayloadTooLarge", ErrPayloadTooLarge, "PAYLOAD_TOO_LARGE", 413},
		{"ErrValidationFailed", ErrValidationFailed, "VALIDATION_FAILED", 422},
		{"ErrFrontendMustBeStatic", ErrFrontendMustBeStatic, "FRONTEND_MUST_BE_STATIC", 422},
		{"ErrPodLimitExceeded", ErrPodLimitExceeded, "POD_LIMIT_EXCEEDED", 422},
		{"ErrMissingField", ErrMissingField, "MISSING_FIELD", 400},
		{"ErrSourceUploadFailed", ErrSourceUploadFailed, "SOURCE_UPLOAD_FAILED", 500},
		{"ErrPlatformDegraded", ErrPlatformDegraded, "PLATFORM_DEGRADED", 503},
		{"ErrDeployServiceAuthFailed", ErrDeployServiceAuthFailed, "DEPLOY_SERVICE_AUTH_FAILED", 503},
		{"ErrSyncOperationInProgress", ErrSyncOperationInProgress, "SYNC_OPERATION_IN_PROGRESS", 409},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Errorf("expected code %q, got %q", tt.code, tt.err.Code)
			}
			if tt.err.HTTPStatus != tt.httpStatus {
				t.Errorf("expected status %d, got %d", tt.httpStatus, tt.err.HTTPStatus)
			}
			if tt.err.Message == "" {
				t.Error("expected non-empty message")
			}
		})
	}
}

func TestLoopZeroErrorUnwrap(t *testing.T) {
	t.Run("Unwrap returns the cause", func(t *testing.T) {
		cause := fmt.Errorf("database connection refused")
		err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "lock timed out", Cause: cause}

		if stderrors.Unwrap(err) != cause {
			t.Error("Unwrap should return the cause")
		}
	})

	t.Run("errors.Is finds cause through Unwrap chain", func(t *testing.T) {
		cause := fmt.Errorf("database connection refused")
		err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "lock timed out", Cause: cause}

		if !stderrors.Is(err, cause) {
			t.Error("errors.Is should find cause through Unwrap chain")
		}
	})

	t.Run("Unwrap returns nil when no cause", func(t *testing.T) {
		err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "lock timed out"}

		if stderrors.Unwrap(err) != nil {
			t.Error("Unwrap should return nil when no cause is set")
		}
	})
}

func TestLoopZeroErrorWrapPreservesCause(t *testing.T) {
	original := NewLockTimeout("test-env")
	wrapped := original.Wrap("during deploy")

	if stderrors.Unwrap(wrapped) != original {
		t.Error("Wrap should set the original as cause")
	}
}

func TestErrorFactories(t *testing.T) {
	t.Run("NewLockTimeout with empty detail returns fresh copy with default message", func(t *testing.T) {
		err := NewLockTimeout("")
		if err.Code != ErrLockTimeout.Code {
			t.Errorf("expected code %q, got %q", ErrLockTimeout.Code, err.Code)
		}
		if err.Message != ErrLockTimeout.Message {
			t.Errorf("expected message %q, got %q", ErrLockTimeout.Message, err.Message)
		}
		if err == ErrLockTimeout {
			t.Error("NewLockTimeout must return a fresh copy, not the global sentinel pointer")
		}
	})

	t.Run("NewLockTimeout with custom detail", func(t *testing.T) {
		err := NewLockTimeout("custom detail")
		if err.Message != "custom detail" {
			t.Errorf("expected 'custom detail', got %q", err.Message)
		}
	})

	t.Run("NewInvalidTransition includes from and event", func(t *testing.T) {
		err := NewInvalidTransition("QUEUED", "BuildSucceeded")
		if !strings.Contains(err.Message, "QUEUED") || !strings.Contains(err.Message, "BuildSucceeded") {
			t.Errorf("expected message to contain from and event, got %q", err.Message)
		}
		if err.HTTPStatus != 409 {
			t.Errorf("expected status 409, got %d", err.HTTPStatus)
		}
	})

	t.Run("NewTenantNotFound includes tenant ID", func(t *testing.T) {
		err := NewTenantNotFound("tenant-abc")
		if !strings.Contains(err.Message, "tenant-abc") {
			t.Errorf("expected message to contain tenant ID, got %q", err.Message)
		}
	})

	t.Run("NewBuildNotFound includes build ID", func(t *testing.T) {
		err := NewBuildNotFound("build-deadbeef")
		if !strings.Contains(err.Message, "build-deadbeef") {
			t.Errorf("expected message to contain build ID, got %q", err.Message)
		}
	})

	t.Run("NewReleaseNotFound includes release ID", func(t *testing.T) {
		err := NewReleaseNotFound("rel-12345678")
		if !strings.Contains(err.Message, "rel-12345678") {
			t.Errorf("expected message to contain release ID, got %q", err.Message)
		}
	})

	t.Run("NewIdempotencyConflict includes key", func(t *testing.T) {
		err := NewIdempotencyConflict("key-abc")
		if !strings.Contains(err.Message, "key-abc") {
			t.Errorf("expected message to contain key, got %q", err.Message)
		}
		if err.HTTPStatus != 422 {
			t.Errorf("expected status 422, got %d", err.HTTPStatus)
		}
	})

	t.Run("NewProjectNotFound includes project name", func(t *testing.T) {
		err := NewProjectNotFound("my-app")
		if err.Code != "PROJECT_NOT_FOUND" {
			t.Errorf("expected code PROJECT_NOT_FOUND, got %s", err.Code)
		}
		if err.HTTPStatus != 404 {
			t.Errorf("expected status 404, got %d", err.HTTPStatus)
		}
		expected := "project 'my-app' not found"
		if err.Message != expected {
			t.Errorf("expected message %q, got %q", expected, err.Message)
		}
	})
}

func TestLoopZeroError_Is_CodeOnlyComparison(t *testing.T) {
	// Is() compares only Code, not HTTPStatus. This is documented behavior.
	err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "custom", HTTPStatus: 500}
	if !stderrors.Is(err, ErrLockTimeout) {
		t.Error("expected errors.Is to match by code even with different HTTPStatus")
	}
}

func TestLoopZeroError_DoubleWrap_ErrorsIs(t *testing.T) {
	original := NewBuildNotFound("build-abc")
	wrap1 := original.Wrap("service layer")
	wrap2 := wrap1.Wrap("route handler")

	if !stderrors.Is(wrap2, ErrBuildNotFound) {
		t.Error("errors.Is should find ErrBuildNotFound through double-wrapped chain")
	}
}

func TestLoopZeroError_ErrorsAs(t *testing.T) {
	original := NewBuildNotFound("build-abc")
	wrapped := original.Wrap("service layer")

	var target *LoopZeroError
	if !stderrors.As(wrapped, &target) {
		t.Fatal("errors.As should extract LoopZeroError from wrapped error")
	}
	// errors.As returns the outermost match — the wrapped error, not the original
	if target.Code != "BUILD_NOT_FOUND" {
		t.Errorf("expected code BUILD_NOT_FOUND, got %q", target.Code)
	}
	if !strings.Contains(target.Message, "service layer") {
		t.Errorf("expected wrapped message context, got %q", target.Message)
	}
}

func TestNoInternalTermsInErrorMessages(t *testing.T) {
	// CLAUDE.md Rule 3: no internal terms in user-facing output.
	// LoopZeroError messages are sent to CLI via ToJSON().
	forbidden := []string{"ArgoCD", "argocd", "Gitea", "MinIO", "CRD",
		"tenant_id", "advisory lock", "pg_advisory", "kubebuilder", "envtest", "Kaniko"}

	allErrors := []*LoopZeroError{
		ErrLockTimeout, ErrInvalidTransition, ErrTenantNotFound,
		ErrProjectNotFound, ErrBuildNotFound, ErrReleaseNotFound,
		ErrNoActiveRelease, ErrNoPreviousRelease, ErrBuildAlreadyCompleted,
		ErrDeployInProgress, ErrInvalidName, ErrIdempotencyConflict,
		ErrUnauthorized, ErrInvalidRequest, ErrProjectExists,
		ErrInternalError, ErrPayloadTooLarge, ErrValidationFailed,
		ErrFrontendMustBeStatic, ErrPodLimitExceeded, ErrMissingField,
		ErrSourceUploadFailed, ErrPlatformDegraded, ErrDeployServiceAuthFailed,
		ErrSyncOperationInProgress,
		ErrReplicasExceedPlan, ErrScalingAmbiguous, // B15
	}

	for _, e := range allErrors {
		for _, term := range forbidden {
			if strings.Contains(e.Message, term) {
				t.Errorf("[%s] message contains forbidden term %q: %s", e.Code, term, e.Message)
			}
			if strings.Contains(e.Code, term) {
				t.Errorf("[%s] code contains forbidden term %q", e.Code, term)
			}
		}
	}
}

// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): scaling-related errors.
func TestErrReplicasExceedPlan_Shape(t *testing.T) {
	if ErrReplicasExceedPlan == nil {
		t.Fatal("ErrReplicasExceedPlan must be defined")
	}
	if ErrReplicasExceedPlan.Code != "REPLICAS_EXCEED_PLAN" {
		t.Errorf("expected code REPLICAS_EXCEED_PLAN, got %q", ErrReplicasExceedPlan.Code)
	}
	if ErrReplicasExceedPlan.HTTPStatus != 422 {
		t.Errorf("expected HTTP 422, got %d", ErrReplicasExceedPlan.HTTPStatus)
	}
}

func TestErrScalingAmbiguous_Shape(t *testing.T) {
	if ErrScalingAmbiguous == nil {
		t.Fatal("ErrScalingAmbiguous must be defined")
	}
	if ErrScalingAmbiguous.Code != "SCALING_AMBIGUOUS" {
		t.Errorf("expected code SCALING_AMBIGUOUS, got %q", ErrScalingAmbiguous.Code)
	}
	if ErrScalingAmbiguous.HTTPStatus != 422 {
		t.Errorf("expected HTTP 422, got %d", ErrScalingAmbiguous.HTTPStatus)
	}
	if !strings.Contains(ErrScalingAmbiguous.Message, "replicas") ||
		!strings.Contains(ErrScalingAmbiguous.Message, "autoscaling") {
		t.Errorf("ErrScalingAmbiguous must name both fields, got: %s", ErrScalingAmbiguous.Message)
	}
}

func TestNewReplicasExceedPlan_FormatsContext(t *testing.T) {
	err := NewReplicasExceedPlan("starter", 2, 5, "api")
	if err.Code != "REPLICAS_EXCEED_PLAN" || err.HTTPStatus != 422 {
		t.Fatalf("unexpected shape: %+v", err)
	}
	msg := err.Message
	if !strings.Contains(msg, "Starter") && !strings.Contains(msg, "starter") {
		t.Errorf("message missing plan name: %s", msg)
	}
	if !strings.Contains(msg, "2") || !strings.Contains(msg, "5") {
		t.Errorf("message missing cap/requested: %s", msg)
	}
	if !strings.Contains(msg, "api") {
		t.Errorf("message missing app name: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// T1.6 — Stack capture (WrapWithStack) per gate PKG-G08 + ADR-0028 follow-up.
// ---------------------------------------------------------------------------

func TestWrapWithStack_captures_call_site(t *testing.T) {
	err := ErrBuildNotFound.WrapWithStack("submit")
	stack := err.StackTrace()
	if stack == "" {
		t.Fatal("WrapWithStack produced empty StackTrace()")
	}
	// Expect this test function name to appear in the captured frames.
	if !strings.Contains(stack, "TestWrapWithStack_captures_call_site") {
		t.Errorf("stack missing test function name; got:\n%s", stack)
	}
}

func TestWrap_without_stack_returns_empty_stack_trace(t *testing.T) {
	err := ErrBuildNotFound.Wrap("submit")
	if got := err.StackTrace(); got != "" {
		t.Errorf("legacy Wrap should not capture stack, got: %s", got)
	}
}

func TestStackTrace_on_sentinel_returns_empty(t *testing.T) {
	if got := ErrBuildNotFound.StackTrace(); got != "" {
		t.Errorf("sentinel without Wrap should have empty stack, got: %s", got)
	}
}

func TestToJSON_omits_stack_field(t *testing.T) {
	err := ErrBuildNotFound.WrapWithStack("submit")
	out := err.ToJSON()
	if _, present := out["stack"]; present {
		t.Errorf("ToJSON() must NOT leak stack into wire payload; got keys: %v", out)
	}
}

func TestDoubleWrapWithStack_preserves_first_capture(t *testing.T) {
	first := ErrBuildNotFound.WrapWithStack("first")
	firstStack := first.StackTrace()
	if firstStack == "" {
		t.Fatal("first WrapWithStack returned empty stack")
	}
	// Wrap again with stack — second should keep first's frames since
	// already captured (call-site origin is what matters).
	second := first.WrapWithStack("second")
	secondStack := second.StackTrace()
	if secondStack != firstStack {
		t.Errorf("double WrapWithStack lost or replaced first capture")
	}
}
