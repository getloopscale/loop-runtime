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
	err := &LoopZeroError{Code: "TEST_CODE", Message: "test message"}
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
		err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "custom"}
		if !stderrors.Is(err, ErrLockTimeout) {
			t.Error("expected errors.Is to match by code")
		}
	})

	t.Run("does not match different code", func(t *testing.T) {
		err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "custom"}
		if stderrors.Is(err, ErrBuildNotFound) {
			t.Error("expected errors.Is not to match different code")
		}
	})

	t.Run("does not match non-LoopZeroError", func(t *testing.T) {
		err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "custom"}
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
	// Stage C: HTTP status now comes from pkg/errors/http.Status(err);
	// Wrap preserves Code so the registered status carries through.
}

func TestLoopZeroError_ToJSON(t *testing.T) {
	err := &LoopZeroError{Code: "MY_CODE", Message: "details here"}
	j := err.ToJSON()
	if j["code"] != "MY_CODE" {
		t.Errorf("expected code MY_CODE, got %q", j["code"])
	}
	if j["message"] != "details here" {
		t.Errorf("expected message 'details here', got %q", j["message"])
	}
}

func TestNewLoopZeroError(t *testing.T) {
	// Stage C: NewLoopZeroError still accepts the int third parameter for
	// backward compatibility with pre-T2.3 callers, but the value is
	// ignored — HTTP status now resolves via pkg/errors/http.Status.
	err := NewLoopZeroError("CUSTOM", "custom message", 418)
	if err.Code != "CUSTOM" {
		t.Errorf("expected code CUSTOM, got %q", err.Code)
	}
	if err.Message != "custom message" {
		t.Errorf("expected message 'custom message', got %q", err.Message)
	}
}

func TestPredefinedErrors(t *testing.T) {
	// Stage C: HTTP status validation moved to pkg/errors/http
	// mapping_test.go (registry coverage). This test now pins only
	// Code + non-empty Message — the cross-cutting transport contract
	// belongs to errorshttp.Status, not to the domain struct.
	tests := []struct {
		name string
		err  *LoopZeroError
		code string
	}{
		{"ErrLockTimeout", ErrLockTimeout, "LOCK_TIMEOUT"},
		{"ErrInvalidTransition", ErrInvalidTransition, "INVALID_TRANSITION"},
		{"ErrTenantNotFound", ErrTenantNotFound, "TENANT_NOT_FOUND"},
		{"ErrProjectNotFound", ErrProjectNotFound, "PROJECT_NOT_FOUND"},
		{"ErrBuildNotFound", ErrBuildNotFound, "BUILD_NOT_FOUND"},
		{"ErrReleaseNotFound", ErrReleaseNotFound, "RELEASE_NOT_FOUND"},
		{"ErrNoActiveRelease", ErrNoActiveRelease, "NO_ACTIVE_RELEASE"},
		{"ErrNoPreviousRelease", ErrNoPreviousRelease, "NO_PREVIOUS_RELEASE"},
		{"ErrBuildAlreadyCompleted", ErrBuildAlreadyCompleted, "BUILD_ALREADY_COMPLETED"},
		{"ErrDeployInProgress", ErrDeployInProgress, "DEPLOY_IN_PROGRESS"},
		{"ErrInvalidName", ErrInvalidName, "INVALID_NAME"},
		{"ErrIdempotencyConflict", ErrIdempotencyConflict, "IDEMPOTENCY_CONFLICT"},
		{"ErrUnauthorized", ErrUnauthorized, "UNAUTHORIZED"},
		{"ErrInvalidRequest", ErrInvalidRequest, "INVALID_REQUEST"},
		{"ErrProjectExists", ErrProjectExists, "PROJECT_EXISTS"},
		{"ErrInternalError", ErrInternalError, "INTERNAL_ERROR"},
		{"ErrPayloadTooLarge", ErrPayloadTooLarge, "PAYLOAD_TOO_LARGE"},
		{"ErrValidationFailed", ErrValidationFailed, "VALIDATION_FAILED"},
		{"ErrFrontendMustBeStatic", ErrFrontendMustBeStatic, "FRONTEND_MUST_BE_STATIC"},
		{"ErrPodLimitExceeded", ErrPodLimitExceeded, "POD_LIMIT_EXCEEDED"},
		{"ErrMissingField", ErrMissingField, "MISSING_FIELD"},
		{"ErrSourceUploadFailed", ErrSourceUploadFailed, "SOURCE_UPLOAD_FAILED"},
		{"ErrPlatformDegraded", ErrPlatformDegraded, "PLATFORM_DEGRADED"},
		{"ErrDeployServiceAuthFailed", ErrDeployServiceAuthFailed, "DEPLOY_SERVICE_AUTH_FAILED"},
		{"ErrSyncOperationInProgress", ErrSyncOperationInProgress, "SYNC_OPERATION_IN_PROGRESS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Errorf("expected code %q, got %q", tt.code, tt.err.Code)
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
		// Status check belongs to pkg/errors/http (Stage C).
	})

	t.Run("NewProjectNotFound includes project name", func(t *testing.T) {
		err := NewProjectNotFound("my-app")
		if err.Code != "PROJECT_NOT_FOUND" {
			t.Errorf("expected code PROJECT_NOT_FOUND, got %s", err.Code)
		}
		expected := "project 'my-app' not found"
		if err.Message != expected {
			t.Errorf("expected message %q, got %q", expected, err.Message)
		}
	})
}

func TestLoopZeroError_Is_CodeOnlyComparison(t *testing.T) {
	// Is() compares only Code, which is the entire identity model after
	// Stage C — HTTPStatus is no longer a LoopZeroError field.
	err := &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "custom"}
	if !stderrors.Is(err, ErrLockTimeout) {
		t.Error("expected errors.Is to match by code")
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
	// Status check moved to pkg/errors/http (Stage C).
}

func TestErrScalingAmbiguous_Shape(t *testing.T) {
	if ErrScalingAmbiguous == nil {
		t.Fatal("ErrScalingAmbiguous must be defined")
	}
	if ErrScalingAmbiguous.Code != "SCALING_AMBIGUOUS" {
		t.Errorf("expected code SCALING_AMBIGUOUS, got %q", ErrScalingAmbiguous.Code)
	}
	if !strings.Contains(ErrScalingAmbiguous.Message, "replicas") ||
		!strings.Contains(ErrScalingAmbiguous.Message, "autoscaling") {
		t.Errorf("ErrScalingAmbiguous must name both fields, got: %s", ErrScalingAmbiguous.Message)
	}
}

func TestNewReplicasExceedPlan_FormatsContext(t *testing.T) {
	err := NewReplicasExceedPlan("starter", 2, 5, "api")
	if err.Code != "REPLICAS_EXCEED_PLAN" {
		t.Fatalf("unexpected code: %+v", err)
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

// Coverage push (Plan 2026-05-21 v1.1 T4.2): close NotFound constructor gaps.

func TestNewEnvironmentNotFound(t *testing.T) {
	err := NewEnvironmentNotFound("staging")
	if err.Code != "ENVIRONMENT_NOT_FOUND" {
		t.Errorf("Code = %q; want ENVIRONMENT_NOT_FOUND", err.Code)
	}
	if !strings.Contains(err.Message, "staging") {
		t.Errorf("Message %q should mention 'staging'", err.Message)
	}
}

func TestNewDomainNotFound(t *testing.T) {
	err := NewDomainNotFound("example.com")
	if err.Code != "DOMAIN_NOT_FOUND" {
		t.Errorf("Code = %q; want DOMAIN_NOT_FOUND", err.Code)
	}
	if !strings.Contains(err.Message, "example.com") {
		t.Errorf("Message %q should mention 'example.com'", err.Message)
	}
}

func TestNewRepositoryNotLinked(t *testing.T) {
	err := NewRepositoryNotLinked("my-project")
	if err.Code != "REPOSITORY_NOT_LINKED" {
		t.Errorf("Code = %q; want REPOSITORY_NOT_LINKED", err.Code)
	}
	if !strings.Contains(err.Message, "my-project") {
		t.Errorf("Message %q should mention 'my-project'", err.Message)
	}
}

func TestDisplayPlanName_AllValues(t *testing.T) {
	cases := []struct{ in, want string }{
		{"starter", "Starter"},
		{"pro", "Pro"},
		{"team", "Team"},
		{"enterprise", "Enterprise"},
		{"unknown-plan", "unknown-plan"},  // default branch
		{"", ""},                          // empty defaults
	}
	for _, c := range cases {
		got := displayPlanName(c.in)
		if got != c.want {
			t.Errorf("displayPlanName(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
