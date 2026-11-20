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
	err := NewLoopZeroError("CUSTOM", "custom message")
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

// TestErrNoActiveRelease_MessageIsOperationNeutral pins the docstring
// invariant on ErrNoActiveRelease (line 227-229 of errors.go): the
// message is shared between promote AND rollback flows, so it must
// NOT include the operation-specific verbs "promoting" or "rolling
// back", nor the source-only noun "source environment".
//
// Regression protection for loopzero-cloud#166 (CRITICAL): user hit this
// misleading message during a rollback path — the promote-flavored
// wording ("source environment", "promoting or rolling back") sent
// operators looking for a promote-side problem when the actual
// upstream cause was state-divergence between env_targets.active_release_id
// and releases.status. Fix: message operation-neutral so the CLI can
// still surface it in both flows without falsely blaming one side.
func TestErrNoActiveRelease_MessageIsOperationNeutral(t *testing.T) {
	msg := ErrNoActiveRelease.Message

	forbidden := []string{
		"source environment", // promote-specific noun
		"promoting",          // promote-specific verb
	}
	for _, term := range forbidden {
		if strings.Contains(msg, term) {
			t.Errorf("ErrNoActiveRelease.Message contains %q — docstring pins operation-neutral wording (loopzero-cloud#166). Full message: %q", term, msg)
		}
	}
	if msg == "" {
		t.Error("ErrNoActiveRelease.Message must not be empty")
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

// M4 T1.1 — per-Account project-cap typed error. Mirrors NewReplicasExceedPlan;
// the message MUST carry plan name, cap, current count, the offending account,
// and the upgrade URL (EC-4 — an actionable path like every canonical cap error).
func TestNewProjectsExceedPlan_FormatsContext(t *testing.T) {
	err := NewProjectsExceedPlan("starter", 1, 1, "acct-abc123")
	if err.Code != "PROJECTS_EXCEED_PLAN" {
		t.Fatalf("unexpected code: %+v", err)
	}
	msg := err.Message
	if !strings.Contains(msg, "Starter") && !strings.Contains(msg, "starter") {
		t.Errorf("message missing plan name: %s", msg)
	}
	if !strings.Contains(msg, "1") {
		t.Errorf("message missing cap/count: %s", msg)
	}
	if !strings.Contains(msg, "acct-abc123") {
		t.Errorf("message missing account: %s", msg)
	}
	if !strings.Contains(msg, "getloopscale.com/pricing") {
		t.Errorf("EC-4: message missing the upgrade URL: %s", msg)
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
		{"unknown-plan", "unknown-plan"}, // default branch
		{"", ""},                         // empty defaults
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
	err := NewConcurrentActivation("proj-x", "production")
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
	a := NewConcurrentActivation("p", "production")
	b := NewConcurrentActivation("p", "production")
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

// NewComposed pairs an actionable summary with the verbatim cause, and falls
// back to a trace reference when the cause says nothing (Red Team #35). The
// "Something went wrong" catchall that hid causes (#34) must never return.
func TestNewComposed_ComposesSummaryAndCause(t *testing.T) {
	err := NewComposed(ErrBuildSubmitFailed, "Build trigger failed at workflow submit", "workflow client: 503 Service Unavailable", "tid-1")

	if err.Code != "BUILD_SUBMIT_FAILED" {
		t.Errorf("Code = %q, want the base code (Rule 4 — exit-code stability)", err.Code)
	}
	if want := "Build trigger failed at workflow submit: workflow client: 503 Service Unavailable"; err.Message != want {
		t.Errorf("Message = %q, want %q", err.Message, want)
	}
}

func TestNewComposed_EmptyDetailWithoutTraceUsesTheSummaryAlone(t *testing.T) {
	err := NewComposed(ErrSourceUploadFailed, "The remote rejected the push", "", "")

	if err.Message != "The remote rejected the push" {
		t.Errorf("Message = %q, want the summary with no trailing colon", err.Message)
	}
}

func TestNewComposed_PlaceholderOnlyDetailFallsBackToTheTraceReference(t *testing.T) {
	err := NewComposed(ErrSourceUploadFailed, "The remote rejected the push", "<orchestrator>: <op>", "trace-xyz")

	for _, want := range []string{"The remote rejected the push", "Reference: trace-xyz", "Contact support"} {
		if !strings.Contains(err.Message, want) {
			t.Errorf("Message %q must contain %q", err.Message, want)
		}
	}
	if strings.Contains(err.Message, "<orchestrator>") {
		t.Errorf("Message %q must not surface the redaction placeholders", err.Message)
	}
}

func TestNewComposed_UnknownReasonUsesTheNeutralBaseMessage(t *testing.T) {
	err := NewComposed(ErrBuildSubmitFailed, "", "ignored when the reason is unknown", "tid-unknown")

	if !strings.HasPrefix(err.Message, ErrBuildSubmitFailed.Message) || !strings.Contains(err.Message, "Reference: tid-unknown") {
		t.Errorf("Message = %q, want the base message plus the trace reference", err.Message)
	}
	if strings.Contains(err.Message, "ignored when") {
		t.Errorf("an unknown reason must not surface the raw detail: %q", err.Message)
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
