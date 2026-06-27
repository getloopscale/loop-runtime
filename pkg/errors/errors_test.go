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

// T5.1 CV-M-1 of plan 2026-06-01-p3-infra-observability-test-debt v1.1:
// NewConcurrentActivation produces the CONCURRENT_ACTIVATION typed error
// returned when partial unique index idx_releases_active_unique (migration
// 000022) rejects a second ACTIVE release for the same env.

func TestNewConcurrentActivation_CodeAndContext(t *testing.T) {
	err := NewConcurrentActivation("tn-A", "proj-x", "production")
	if err.Code != "CONCURRENT_ACTIVATION" {
		t.Errorf("Code mismatch: got %q, want CONCURRENT_ACTIVATION", err.Code)
	}
	if !strings.Contains(err.Message, "proj-x") {
		t.Errorf("Message must include project; got %q", err.Message)
	}
	if !strings.Contains(err.Message, "production") {
		t.Errorf("Message must include environment; got %q", err.Message)
	}
	// Cause is unset (fresh sentinel-class error, not wrapped from PG).
	if err.Cause != nil {
		t.Errorf("Cause must be nil; got %v", err.Cause)
	}
}

// TestNewConcurrentActivation_DistinctInstances: factory MUST return a
// fresh struct each call so callers can wrap context without mutating
// a shared global. Mirrors the discipline established by NewLockTimeout.
func TestNewConcurrentActivation_DistinctInstances(t *testing.T) {
	a := NewConcurrentActivation("tn", "p", "production")
	b := NewConcurrentActivation("tn", "p", "production")
	if a == b {
		t.Error("factory MUST return distinct pointers (no shared global)")
	}
	if a.Code != b.Code {
		t.Errorf("Code MUST be deterministic: %q != %q", a.Code, b.Code)
	}
}

// J-20 Scale plan v1.1 T1.2 + EC-3:
// NewPlanUpgradeRequired produces the PLAN_UPGRADE_REQUIRED typed error
// returned when KEDA scale-to-zero is opt-in'd by a Starter tenant.

func TestNewPlanUpgradeRequired_CodeAndContext(t *testing.T) {
	err := NewPlanUpgradeRequired("pro", "starter", "KEDA scale-to-zero")
	if err.Code != "PLAN_UPGRADE_REQUIRED" {
		t.Errorf("Code mismatch: got %q, want PLAN_UPGRADE_REQUIRED", err.Code)
	}
	// Message MUST mention both the required and current tier so the
	// CLI / dashboard can render a contextual upgrade prompt.
	for _, frag := range []string{"KEDA scale-to-zero", "Pro", "Starter", "pricing"} {
		if !strings.Contains(err.Message, frag) {
			t.Errorf("Message must include %q; got %q", frag, err.Message)
		}
	}
	if err.Cause != nil {
		t.Errorf("Cause must be nil; got %v", err.Cause)
	}
}

func TestNewPlanUpgradeRequired_DistinctInstances(t *testing.T) {
	a := NewPlanUpgradeRequired("pro", "starter", "KEDA")
	b := NewPlanUpgradeRequired("pro", "starter", "KEDA")
	if a == b {
		t.Error("factory MUST return distinct pointers (no shared global)")
	}
}

// J-20 Scale plan v1.1 T1.3 + EC-4:
// NewHPATargetBoundsViolated produces the HPA_TARGET_OUT_OF_BOUNDS typed
// error returned when cpu/memory target violates [min,max] range.

func TestNewHPATargetBoundsViolated_CodeAndContext(t *testing.T) {
	err := NewHPATargetBoundsViolated("cpu_target", 10, 95, 1)
	if err.Code != "HPA_TARGET_OUT_OF_BOUNDS" {
		t.Errorf("Code mismatch: got %q, want HPA_TARGET_OUT_OF_BOUNDS", err.Code)
	}
	// Message MUST include field + bounds + got so the dashboard input
	// component can highlight the specific violation.
	for _, frag := range []string{"cpu_target", "10%", "95%", "1%"} {
		if !strings.Contains(err.Message, frag) {
			t.Errorf("Message must include %q; got %q", frag, err.Message)
		}
	}
}

func TestNewHPATargetBoundsViolated_MemoryField(t *testing.T) {
	err := NewHPATargetBoundsViolated("memory_target", 10, 90, 91)
	for _, frag := range []string{"memory_target", "10%", "90%", "91%"} {
		if !strings.Contains(err.Message, frag) {
			t.Errorf("Message must include %q; got %q", frag, err.Message)
		}
	}
}

// Sprint 6 plan qa-sprint6-cleanup-2026-06-25 T2.3 (closes loopzero-cloud#34):
// NewBuildSubmitFailedFromReason composes "<summary>: <detail>" mirroring
// Sprint 5's NewSourceUploadFailedFromPushReason. The #34 regression guard
// asserts the misleading "Something went wrong. Try again, or check `loopzero
// status`" text NEVER surfaces when a discriminated reason is provided.

func TestNewBuildSubmitFailedFromReason_ComposesDetail(t *testing.T) {
	cases := []struct {
		name        string
		reasonLabel string
		detail      string
		wantSubs    []string
		wantNotSubs []string
	}{
		{
			name:        "WorkflowFailed composes verbatim cause",
			reasonLabel: "BuildSubmitWorkflowFailed",
			detail:      "argo client: 503 Service Unavailable",
			wantSubs:    []string{"Build trigger failed at workflow submit", "503"},
			// Sprint 6 ADR D3: the #34 catchall anti-pattern is replacing
			// detail with a generic template — assert it NEVER reappears.
			wantNotSubs: []string{"Something went wrong"},
		},
		{
			name:        "ProjectEnsureFailed preserves operator diagnostic",
			reasonLabel: "BuildSubmitProjectEnsureFailed",
			detail:      "tenant lookup failed: no rows",
			wantSubs:    []string{"ensuring the project exists", "tenant lookup failed"},
			wantNotSubs: []string{"Something went wrong"},
		},
		{
			name:        "DBWriteFailed preserves DB error verbatim",
			reasonLabel: "BuildSubmitDBWriteFailed",
			detail:      "pq: duplicate key value violates unique constraint",
			wantSubs:    []string{"database write step", "duplicate key"},
			wantNotSubs: []string{"Something went wrong"},
		},
		{
			name:        "AppNormalizeFailed preserves shape error",
			reasonLabel: "BuildSubmitAppNormalizeFailed",
			detail:      "invalid runtime declared for app foo",
			wantSubs:    []string{"validating one of the apps", "invalid runtime"},
			wantNotSubs: []string{"Something went wrong"},
		},
		{
			name:        "UnknownDownstream surfaces verbatim cause",
			reasonLabel: "BuildSubmitUnknown",
			detail:      "some genuinely-unknown downstream failure",
			wantSubs:    []string{"cause not classified", "genuinely-unknown"},
			wantNotSubs: []string{"Something went wrong"},
		},
		{
			name:        "Empty detail uses summary alone (no trailing colon)",
			reasonLabel: "BuildSubmitWorkflowFailed",
			detail:      "",
			wantSubs:    []string{"Build trigger failed at workflow submit"},
			wantNotSubs: []string{": "},
		},
		{
			name:        "Unknown reason label falls back to neutral generic",
			reasonLabel: "BogusUnknownLabel",
			detail:      "ignored when label is unknown",
			wantSubs:    []string{ErrBuildSubmitFailed.Message},
			wantNotSubs: []string{"ignored when label"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewBuildSubmitFailedFromReason(tc.reasonLabel, tc.detail)
			if err.Code != "BUILD_SUBMIT_FAILED" {
				t.Errorf("Code = %q, want BUILD_SUBMIT_FAILED (Rule 4 — exit-code stability)", err.Code)
			}
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Message, want) {
					t.Errorf("Message %q must contain %q", err.Message, want)
				}
			}
			for _, notWant := range tc.wantNotSubs {
				if strings.Contains(err.Message, notWant) {
					t.Errorf("Message %q must NOT contain %q (#34 regression guard)", err.Message, notWant)
				}
			}
		})
	}
}

// Sprint 5 plan qa-sprint5-cleanup-2026-06-25 T2.4 (closes loopzero-cloud#33):
// NewSourceUploadFailedFromPushReason composes "<summary>: <detail>" so the
// verbatim cause is always preserved in the user-facing message. Vercel CLI
// canonical pattern — see knowledge-base/references/vercel/packages/cli/
// test/unit/util/deploy/process-deployment.test.ts:17-19 (original + hint).

func TestNewSourceUploadFailedFromPushReason_ComposesDetail(t *testing.T) {
	cases := []struct {
		name        string
		reasonLabel string
		detail      string
		wantSubs    []string
		wantNotSubs []string
	}{
		{
			name:        "RemoteRejectedPush composes verbatim cause",
			reasonLabel: "RemoteRejectedPush",
			detail:      "pre-receive hook declined",
			wantSubs:    []string{"The remote rejected the push", "pre-receive hook declined"},
			// Sprint 5 ADR D3: replacing the detail with "Verify your project
			// name is unused" is the QA #33 anti-pattern — assert the
			// misleading text never appears.
			wantNotSubs: []string{"Verify your project name is unused"},
		},
		{
			name:        "UnknownDownstream surfaces verbatim cause",
			reasonLabel: "UnknownDownstream",
			detail:      "repo locked by another process",
			wantSubs:    []string{"Source upload failed", "repo locked by another process"},
		},
		{
			name:        "SourceBackendAuthFailure preserves operator diagnostic",
			reasonLabel: "SourceBackendAuthFailure",
			detail:      "server returned 401 Unauthorized",
			wantSubs:    []string{"Source backend rejected authentication", "401"},
		},
		{
			name:        "SourceBackendCapacity preserves quota detail",
			reasonLabel: "SourceBackendCapacity",
			detail:      "quota exceeded on remote",
			wantSubs:    []string{"Source backend is out of capacity", "quota exceeded"},
		},
		{
			name:        "SourceBackendUnreachable preserves network diagnostic",
			reasonLabel: "SourceBackendUnreachable",
			detail:      "dial tcp: connection refused",
			wantSubs:    []string{"Source backend is unreachable", "connection refused"},
		},
		{
			name:        "Empty detail uses summary alone (no trailing colon)",
			reasonLabel: "RemoteRejectedPush",
			detail:      "",
			wantSubs:    []string{"The remote rejected the push"},
			wantNotSubs: []string{": "},
		},
		{
			name:        "Unknown reason label falls back to neutral generic message",
			reasonLabel: "BogusUnknownLabel",
			detail:      "ignored when label is unknown",
			wantSubs:    []string{ErrSourceUploadFailed.Message},
			wantNotSubs: []string{"ignored when label"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewSourceUploadFailedFromPushReason(tc.reasonLabel, tc.detail)
			if err.Code != "SOURCE_UPLOAD_FAILED" {
				t.Errorf("Code = %q, want SOURCE_UPLOAD_FAILED (Rule 4 — exit-code stability)", err.Code)
			}
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Message, want) {
					t.Errorf("Message %q must contain %q", err.Message, want)
				}
			}
			for _, notWant := range tc.wantNotSubs {
				if strings.Contains(err.Message, notWant) {
					t.Errorf("Message %q must NOT contain %q (regression guard)", err.Message, notWant)
				}
			}
		})
	}
}

// Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T1.1 — trace-id fallback per
// Red Team #35 original suggestion ("Separar mensagem-de-cliente (genérica
// + trace-id) de causa-de-log (verbatim, server-side)"). The Sprint 7
// aggressive scrub closes the info-disclosure, but for some inputs the
// scrub collapses detail to placeholder-only — at which point trace-id
// is the operator's correlation handle to server-side log.

func TestNewSourceUploadFailedFromPushReasonWithTrace(t *testing.T) {
	cases := []struct {
		name        string
		reason      string
		detail      string
		traceID     string
		wantSubs    []string
		wantNotSubs []string
	}{
		{
			name:        "Sprint 5 backward-compat — useful detail composes verbatim, no trace-id appended",
			reason:      "RemoteRejectedPush",
			detail:      "pre-receive hook declined for branch main",
			traceID:     "abc-123",
			wantSubs:    []string{"pre-receive hook declined for branch main"},
			wantNotSubs: []string{"Reference: abc-123", "Contact support"},
		},
		{
			name:        "Sprint 8 #35 fallback — fully-scrubbed placeholder-only detail uses trace-id",
			reason:      "RemoteRejectedPush",
			detail:      "<orchestrator>: <op>",
			traceID:     "trace-xyz",
			wantSubs:    []string{"The remote rejected the push", "Reference: trace-xyz", "Contact support"},
			wantNotSubs: []string{"<orchestrator>", "<op>"},
		},
		{
			name:        "Sprint 8 #35 fallback — empty detail + trace-id present",
			reason:      "BundleMalformed",
			detail:      "",
			traceID:     "tid-empty",
			wantSubs:    []string{"source bundle is malformed", "Reference: tid-empty"},
		},
		{
			name:        "Sprint 5 backward-compat — empty trace-id leaves summary without Reference suffix",
			reason:      "BundleMalformed",
			detail:      "",
			traceID:     "",
			wantSubs:    []string{"source bundle is malformed"},
			wantNotSubs: []string{"Reference:", "Contact support"},
		},
		{
			name:        "Unknown reason + trace-id — neutral generic + Reference",
			reason:      "BogusUnknownLabel",
			detail:      "ignored",
			traceID:     "tid-unknown",
			wantSubs:    []string{"Reference: tid-unknown"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewSourceUploadFailedFromPushReasonWithTrace(tc.reason, tc.detail, tc.traceID)
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Message, want) {
					t.Errorf("Message %q must contain %q", err.Message, want)
				}
			}
			for _, notWant := range tc.wantNotSubs {
				if strings.Contains(err.Message, notWant) {
					t.Errorf("Message %q must NOT contain %q", err.Message, notWant)
				}
			}
		})
	}
}

func TestNewBuildSubmitFailedFromReasonWithTrace(t *testing.T) {
	cases := []struct {
		name        string
		reason      string
		detail      string
		traceID     string
		wantSubs    []string
		wantNotSubs []string
	}{
		{
			name:        "Useful detail composes verbatim — Sprint 6 backward-compat",
			reason:      "BuildSubmitWorkflowFailed",
			detail:      "argo: connection refused dialing 10.0.0.5:2746",
			traceID:     "ignored-when-useful",
			wantSubs:    []string{"connection refused"},
			wantNotSubs: []string{"Reference: ignored-when-useful"},
		},
		{
			name:        "Scrubbed-to-placeholders detail falls back to trace-id",
			reason:      "BuildSubmitWorkflowFailed",
			detail:      "<orchestrator>: <op>",
			traceID:     "build-trace-1",
			wantSubs:    []string{"Build trigger failed at workflow submit", "Reference: build-trace-1"},
			wantNotSubs: []string{"<orchestrator>"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewBuildSubmitFailedFromReasonWithTrace(tc.reason, tc.detail, tc.traceID)
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Message, want) {
					t.Errorf("Message %q must contain %q", err.Message, want)
				}
			}
			for _, notWant := range tc.wantNotSubs {
				if strings.Contains(err.Message, notWant) {
					t.Errorf("Message %q must NOT contain %q", err.Message, notWant)
				}
			}
		})
	}
}

func TestIsUsefulDetail_Boundaries(t *testing.T) {
	cases := []struct {
		name   string
		detail string
		want   bool
	}{
		{"empty is not useful", "", false},
		{"short string is not useful", "fail", false},
		{"placeholder-only is not useful", "<orchestrator>: <op>", false},
		{"placeholder + short prose is not useful", "<scope> <op>", false},
		{"genuine cause is useful", "connection refused dialing host:2746", true},
		{"cause with embedded placeholder is useful when prose remains", "<orchestrator>: connection refused on retry", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUsefulDetail(tc.detail); got != tc.want {
				t.Errorf("isUsefulDetail(%q) = %v, want %v", tc.detail, got, tc.want)
			}
		})
	}
}
