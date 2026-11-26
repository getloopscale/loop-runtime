// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package errors defines typed error types for all LoopZero components.
// All errors carry a code and message; HTTP transport mapping lives
// in the sub-package pkg/errors/http (per ADR-0030 + gate PKG-G15).
//
// Stage C of the T2.3 staged rollout (CONDITION-2): the legacy
// HTTPStatus field has been REMOVED from LoopZeroError. Callers that
// need the HTTP status code MUST use:
//
//	import errorshttp "github.com/getloopscale/loop-runtime/pkg/errors/http"
//	status := errorshttp.Status(err)
//
// See CLAUDE.md Rule 8.
package errors

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"github.com/getloopscale/loop-runtime/pkg/domain"
)

// stackDepth bounds frame capture in WrapWithStack — 32 frames is
// enough for any realistic LoopZero call chain (handler → service → store →
// driver), and bounded so cost stays in the ~150ns range.
const stackDepth = 32

// Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T1.1 — precompiled regex
// for trace-id composition helpers (isUsefulDetail / composeMessageWithTraceID).
var (
	placeholderRE = regexp.MustCompile(`<[^>]+>`)
	whitespaceRE  = regexp.MustCompile(`\s+`)
)

// LoopZeroError is a typed, transport-agnostic error.
// It carries a machine-readable Code and a human Message; HTTP status
// mapping is the responsibility of pkg/errors/http.Status (ADR-0030).
type LoopZeroError struct {
	// Code is a machine-readable error code (e.g., "LOCK_TIMEOUT").
	Code string

	// Message is a human-readable error description with context.
	Message string

	// Cause is the underlying error that caused this LoopZeroError.
	// Exposed via Unwrap() for use with errors.Is/errors.As chains.
	Cause error

	// stack is an optional cached snapshot of program counters captured
	// by WrapWithStack. nil means no stack was requested. Resolved
	// lazily by StackTrace() to avoid the cost of symbolication when
	// the trace is never inspected.
	stack []uintptr
}

// Error implements the error interface.
func (e *LoopZeroError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Is checks whether the target error has the same code as this LoopZeroError.
// This enables errors.Is() support for comparing error codes.
//
// NOTE: Only the Code field is compared. HTTPStatus is intentionally excluded
// because factory functions (e.g. NewLockTimeout) may return errors with the
// same code but different messages/statuses. Callers that need the HTTP status
// should use errors.As to extract the actual LoopZeroError, not the sentinel.
func (e *LoopZeroError) Is(target error) bool {
	t, ok := target.(*LoopZeroError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// Unwrap returns the underlying cause, enabling errors.Is/errors.As chains.
func (e *LoopZeroError) Unwrap() error {
	return e.Cause
}

// NewLoopZeroError creates a new LoopZeroError with the given code and message.
//
// A LoopZeroError carries no HTTP status: it is resolved at the transport
// boundary by pkg/errors/http.Status from the code (ADR-0030).
func NewLoopZeroError(code, message string) *LoopZeroError {
	return &LoopZeroError{
		Code:    code,
		Message: message,
	}
}

// Wrap returns a new LoopZeroError with the same code but with additional
// context prepended to the message. HTTP status is resolved by the
// registry at the transport boundary, so wrapping does not carry an
// explicit status code (per ADR-0030 + gate PKG-G15).
func (e *LoopZeroError) Wrap(context string) *LoopZeroError {
	return &LoopZeroError{
		Code:    e.Code,
		Message: fmt.Sprintf("%s: %s", context, e.Message),
		Cause:   e,
		// Carry forward any stack the source already captured. Wrap()
		// itself does NOT capture — call WrapWithStack() to do that.
		stack: e.stack,
	}
}

// WrapWithStack behaves like Wrap but additionally captures the caller's
// stack frames (up to stackDepth). The capture happens once: if the
// receiver already has a stack, the existing frames are preserved
// (origin matters more than re-wrap site).
//
// Use this in error paths where you need full diagnostic context
// (request middleware, top-level recover, internal panics surfaced as
// errors). For routine domain errors prefer Wrap() — the stack capture
// costs ~150ns and most errors don't need it.
//
// StackTrace() resolves the captured PCs to a human-readable string
// when called. ToJSON() does NOT include the stack to avoid leaking
// internals through API responses.
//
// Per pkg-sota-faang-grade-plan T1.6 + gate PKG-G08.
func (e *LoopZeroError) WrapWithStack(context string) *LoopZeroError {
	wrapped := &LoopZeroError{
		Code:    e.Code,
		Message: fmt.Sprintf("%s: %s", context, e.Message),
		Cause:   e,
		stack:   e.stack,
	}
	if wrapped.stack == nil {
		wrapped.stack = captureStack(2) // skip captureStack + WrapWithStack
	}
	return wrapped
}

// StackTrace returns a human-readable representation of the captured
// stack. Returns "" when the receiver was not produced by
// WrapWithStack.
func (e *LoopZeroError) StackTrace() string {
	if len(e.stack) == 0 {
		return ""
	}
	frames := runtime.CallersFrames(e.stack)
	var b strings.Builder
	for {
		f, more := frames.Next()
		fmt.Fprintf(&b, "%s\n\t%s:%d\n", f.Function, f.File, f.Line)
		if !more {
			break
		}
	}
	return b.String()
}

// captureStack snapshots up to stackDepth program counters starting
// `skip` frames above the caller. Cost is ~150ns; safe to call from
// hot paths.
func captureStack(skip int) []uintptr {
	pcs := make([]uintptr, stackDepth)
	n := runtime.Callers(skip+1, pcs) // +1 to skip captureStack itself
	return pcs[:n]
}

// ToJSON returns a map suitable for JSON serialization in API responses.
func (e *LoopZeroError) ToJSON() map[string]string {
	return map[string]string{
		"code":    e.Code,
		"message": e.Message,
	}
}

// Predefined errors for common failure scenarios.
var (
	// ErrLockTimeout is returned when an environment lock cannot be acquired
	// within the 5-second timeout (spec §4.2).
	ErrLockTimeout = &LoopZeroError{
		Code:    "LOCK_TIMEOUT",
		Message: "could not acquire environment lock within timeout",
	}

	// ErrInvalidTransition is returned when a state machine transition is
	// attempted that violates the defined state machine (spec §5).
	ErrInvalidTransition = &LoopZeroError{
		Code:    "INVALID_TRANSITION",
		Message: "invalid state transition",
	}

	// ErrTenantNotFound is returned when a referenced tenant does not exist.
	ErrTenantNotFound = &LoopZeroError{
		Code:    "TENANT_NOT_FOUND",
		Message: "tenant not found",
	}

	// ErrProjectNotFound is returned when a referenced project does not exist.
	ErrProjectNotFound = &LoopZeroError{
		Code:    "PROJECT_NOT_FOUND",
		Message: "project not found",
	}

	// ErrBuildNotFound is returned when a referenced build does not exist.
	ErrBuildNotFound = &LoopZeroError{
		Code:    "BUILD_NOT_FOUND",
		Message: "build not found",
	}

	// ErrReleaseNotFound is returned when a referenced release does not exist.
	ErrReleaseNotFound = &LoopZeroError{
		Code:    "RELEASE_NOT_FOUND",
		Message: "release not found",
	}

	// ErrPreviewNotFound is returned when a referenced preview environment
	// does not exist or has been soft-deleted (Sprint 5).
	ErrPreviewNotFound = &LoopZeroError{
		Code:    "PREVIEW_NOT_FOUND",
		Message: "preview environment not found",
	}

	// ErrNoActiveRelease is returned when rollback or promote is attempted but
	// the target environment has no active release. Shared by both flows so the
	// message must stay neutral (no "rollback"/"promote"/"source environment"
	// wording). Regression: loopzero-cloud#166 (CRITICAL) — user hit the pre-fix
	// promote-flavored wording during a rollback path and mis-diagnosed the
	// upstream cause. Test pin: TestErrNoActiveRelease_MessageIsOperationNeutral.
	ErrNoActiveRelease = &LoopZeroError{
		Code:    "NO_ACTIVE_RELEASE",
		Message: "No active release found for this environment. Deploy at least once before running this operation.",
	}

	// ErrNoPreviousRelease is returned when rollback is attempted but no previous release exists.
	ErrNoPreviousRelease = &LoopZeroError{
		Code:    "NO_PREVIOUS_RELEASE",
		Message: "No previous release found to rollback to. Deploy at least twice before rolling back.",
	}

	// ErrBuildAlreadyCompleted is returned when attempting to cancel a build
	// that is already in a terminal state (SUCCEEDED, FAILED, or CANCELLED).
	ErrBuildAlreadyCompleted = &LoopZeroError{
		Code:    "BUILD_ALREADY_COMPLETED",
		Message: "Build already completed.",
	}

	// ErrDeployInProgress is returned when a deploy is already in progress
	// for the same (tenant, project, environment).
	ErrDeployInProgress = &LoopZeroError{
		Code:    "DEPLOY_IN_PROGRESS",
		Message: "a deploy is already in progress for this environment",
	}

	// ErrInvalidName is returned when a resource name does not conform to
	// Kubernetes naming rules (RFC 1123).
	ErrInvalidName = &LoopZeroError{
		Code:    "INVALID_NAME",
		Message: "resource name does not conform to RFC 1123",
	}

	// ErrIdempotencyConflict is returned when an idempotency key was already
	// used with a different request payload.
	ErrIdempotencyConflict = &LoopZeroError{
		Code:    "IDEMPOTENCY_CONFLICT",
		Message: "idempotency key was already used with a different request payload",
	}

	// ErrUnauthorized is returned when a request lacks valid authentication.
	ErrUnauthorized = &LoopZeroError{
		Code:    "UNAUTHORIZED",
		Message: "not authenticated",
	}

	// ErrInvalidRequest is returned when the request body cannot be parsed.
	ErrInvalidRequest = &LoopZeroError{
		Code:    "INVALID_REQUEST",
		Message: "invalid request body",
	}

	// ErrProjectExists is returned when a project with the same name
	// already exists within a tenant.
	ErrProjectExists = &LoopZeroError{
		Code:    "PROJECT_EXISTS",
		Message: "a project with this name already exists in your team",
	}

	// ErrInternalError is returned for unexpected server-side failures.
	ErrInternalError = &LoopZeroError{
		Code:    "INTERNAL_ERROR",
		Message: "Something went wrong. Try again, or check `loopzero status` for details. If the problem persists, contact support.",
	}

	// ErrPayloadTooLarge is returned when the request body exceeds the size limit.
	ErrPayloadTooLarge = &LoopZeroError{
		Code:    "PAYLOAD_TOO_LARGE",
		Message: "request body exceeds the maximum allowed size",
	}

	// ErrValidationFailed is returned when input validation fails with field-specific details.
	ErrValidationFailed = &LoopZeroError{
		Code:    "VALIDATION_FAILED",
		Message: "validation failed",
	}

	// ErrFrontendMustBeStatic is returned when a frontend app doesn't use render: static.
	// MEET-2026-03-29-016: All frontends are served from CDN, zero pods.
	ErrFrontendMustBeStatic = &LoopZeroError{
		Code:    "FRONTEND_MUST_BE_STATIC",
		Message: "frontend apps must use static delivery. Add 'render: static' to your loopzero.yaml",
	}

	// ErrPodLimitExceeded is returned when a project has more than 1 server or 1 worker.
	// MEET-2026-03-29-016: Max 1 API service + 1 background worker per project.
	ErrPodLimitExceeded = &LoopZeroError{
		Code:    "POD_LIMIT_EXCEEDED",
		Message: "project exceeds the maximum of 1 API service and 1 background worker",
	}

	// ErrMissingField is returned when a required field is missing from the request.
	ErrMissingField = &LoopZeroError{
		Code:    "MISSING_FIELD",
		Message: "a required field is missing",
	}

	// ErrSourceUploadFailed is the neutral fallback returned when pushing the
	// bundle to the source store fails AND the typed PushBundleError mapping
	// (PushBundleErrorReason.Summary in api/internal/source) cannot identify a
	// specific reason. Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T3.3
	// (closes loopzero-cloud#31): replaced the legacy actively-misleading
	// "Ensure your git history is clean and try again." text (the user's git
	// history WAS clean in the QA report; the real failure was elsewhere)
	// with a neutral fallback. Specific actionable text is now emitted by
	// reason-specific messages keyed off PushBundleError.Reason.
	ErrSourceUploadFailed = &LoopZeroError{
		Code:    "SOURCE_UPLOAD_FAILED",
		Message: "The source upload failed. Please retry; if the issue persists, contact support.",
	}

	// ErrInvalidTenantOrProject is returned when tenantID or projectID fails
	// DNS-1123 validation (Plan v1.2 T1.2). The detailed message is filled in
	// at the call site via NewLoopZeroError to surface which field + why.
	ErrInvalidTenantOrProject = &LoopZeroError{
		Code:    "INVALID_TENANT_OR_PROJECT",
		Message: "tenant or project ID must match DNS-1123 (1-63 chars, lowercase alphanumeric + hyphens, no leading/trailing hyphen)",
	}

	// ErrPlatformDegraded is returned when the deploy service circuit breaker is open.
	// The platform is temporarily unable to process sync requests (DA-7).
	ErrPlatformDegraded = &LoopZeroError{
		Code:    "PLATFORM_DEGRADED",
		Message: "deploy service is temporarily unavailable",
	}

	// ErrDeployServiceAuthFailed is returned when the deploy service rejects its credentials (401/403).
	// This is NOT a transient failure — the credentials are invalid and must be renewed.
	// The circuit breaker MUST NOT count this toward the open threshold (MEET-013).
	ErrDeployServiceAuthFailed = &LoopZeroError{
		Code:    "DEPLOY_SERVICE_AUTH_FAILED",
		Message: "deploy service credentials rejected — contact your platform administrator",
	}

	// ErrSyncOperationInProgress is returned when the deploy service has a sync already running (409).
	// MEET-2026-03-29-019: This is NOT a transient failure — the previous operation must
	// be terminated before a new sync can start. The circuit breaker MUST NOT count this
	// toward the open threshold (same exclusion pattern as ErrDeployServiceAuthFailed).
	ErrSyncOperationInProgress = &LoopZeroError{
		Code:    "SYNC_OPERATION_IN_PROGRESS",
		Message: "another sync operation is already in progress",
	}

	// ErrCellAtCapacity is returned when the cell's concurrent build budget
	// is full. Roadmap 07: the build admission gate returns 429 so clients
	// know to retry after a bounded wait. The response MUST include a
	// `Retry-After` header with a sensible duration.
	ErrCellAtCapacity = &LoopZeroError{
		Code:    "CELL_AT_CAPACITY",
		Message: "the platform is currently busy — retry in a few seconds",
	}

	// ErrTenantBuildQuota is returned when a single tenant already has the
	// maximum allowed concurrent builds in flight. Enforces per-tenant
	// fairness so one noisy tenant cannot starve others even when the cell
	// has free capacity. Same Retry-After semantics as ErrCellAtCapacity.
	ErrTenantBuildQuota = &LoopZeroError{
		Code:    "TENANT_BUILD_QUOTA",
		Message: "your team has too many builds in progress — wait for one to finish",
	}

	// ErrSameEnvironment is returned when a promote request specifies the same
	// source and target environment.
	ErrSameEnvironment = &LoopZeroError{
		Code:    "SAME_ENVIRONMENT",
		Message: "source and target environments must be different",
	}

	// ErrReplicasExceedPlan is returned when an app requests more replicas (or
	// autoscaling.max) than the tenant's plan allows.
	// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059).
	// Factory NewReplicasExceedPlan adds plan + cap + requested + app context.
	ErrReplicasExceedPlan = &LoopZeroError{
		Code:    "REPLICAS_EXCEED_PLAN",
		Message: "requested replicas exceed your plan's limit",
	}

	// ErrScalingAmbiguous is returned when both `replicas` and `autoscaling`
	// are set on the same app. The two are mutually exclusive — pick one.
	// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059).
	ErrScalingAmbiguous = &LoopZeroError{
		Code:    "SCALING_AMBIGUOUS",
		Message: "'replicas' and 'autoscaling' are mutually exclusive — choose one",
	}

	// ErrProjectsExceedPlan is returned at project creation when the Account
	// already holds its plan's project cap. M4 (account-level tenancy, Phase 2):
	// the cap is per-Account, enforced at the engine's project-creation path.
	// Factory NewProjectsExceedPlan adds plan + cap + current + account context.
	ErrProjectsExceedPlan = &LoopZeroError{
		Code:    "PROJECTS_EXCEED_PLAN",
		Message: "your account has reached its plan's project limit",
	}
)

// NewProjectsExceedPlan creates a PROJECTS_EXCEED_PLAN error with the plan name,
// the per-Account project cap, the account's current project count, and the
// account id. M4 — mirrors NewReplicasExceedPlan; the message carries the upgrade
// URL so the client has an actionable path (EC-4).
func NewProjectsExceedPlan(plan string, limit, current int, accountID string) *LoopZeroError {
	return &LoopZeroError{
		Code: "PROJECTS_EXCEED_PLAN",
		Message: fmt.Sprintf(
			"Account '%s' has %d projects — the %s plan allows up to %d. Upgrade at https://getloopscale.com/pricing or delete a project.",
			accountID, current, displayPlanName(plan), limit,
		),
	}
}

// NewReplicasExceedPlan creates a REPLICAS_EXCEED_PLAN error with plan name,
// plan cap, requested replica count, and the offending app name.
// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059).
func NewReplicasExceedPlan(plan string, limit, requested int, appName string) *LoopZeroError {
	return &LoopZeroError{
		Code: "REPLICAS_EXCEED_PLAN",
		Message: fmt.Sprintf(
			"App '%s' requested %d replicas — the %s plan allows up to %d per service. Upgrade at https://getloopscale.com/pricing or lower the replica count.",
			appName, requested, displayPlanName(plan), limit,
		),
	}
}

// displayPlanName returns the product name of a plan tier; the naming rule
// lives once, on domain.PlanTier.
func displayPlanName(plan string) string {
	return domain.PlanTier(plan).DisplayName()
}

// NewLockTimeout creates a LOCK_TIMEOUT error with optional detail.
// Always returns a fresh copy to prevent mutation of the global sentinel.
func NewLockTimeout(detail string) *LoopZeroError {
	msg := ErrLockTimeout.Message
	if detail != "" {
		msg = detail
	}
	return &LoopZeroError{Code: "LOCK_TIMEOUT", Message: msg}
}

// NewInvalidTransition creates an INVALID_TRANSITION error with state context.
func NewInvalidTransition(from, event string) *LoopZeroError {
	return &LoopZeroError{
		Code:    "INVALID_TRANSITION",
		Message: fmt.Sprintf("invalid state transition: cannot apply '%s' in state '%s'", event, from),
	}
}

// NewTenantNotFound creates a TENANT_NOT_FOUND error with tenant ID context.
func NewTenantNotFound(tenantID string) *LoopZeroError {
	return &LoopZeroError{
		Code:    "TENANT_NOT_FOUND",
		Message: fmt.Sprintf("tenant '%s' not found", tenantID),
	}
}

// NewBuildNotFound creates a BUILD_NOT_FOUND error with build ID context.
func NewBuildNotFound(buildID string) *LoopZeroError {
	return &LoopZeroError{
		Code:    "BUILD_NOT_FOUND",
		Message: fmt.Sprintf("build '%s' not found", buildID),
	}
}

// NewReleaseNotFound creates a RELEASE_NOT_FOUND error with release ID context.
func NewReleaseNotFound(releaseID string) *LoopZeroError {
	return &LoopZeroError{
		Code:    "RELEASE_NOT_FOUND",
		Message: fmt.Sprintf("release '%s' not found", releaseID),
	}
}

// NewProjectNotFound creates a PROJECT_NOT_FOUND error with project name context.
func NewProjectNotFound(name string) *LoopZeroError {
	return &LoopZeroError{
		Code:    "PROJECT_NOT_FOUND",
		Message: fmt.Sprintf("project '%s' not found", name),
	}
}

// NewEnvironmentNotFound creates an ENVIRONMENT_NOT_FOUND error with environment name context.
func NewEnvironmentNotFound(name string) *LoopZeroError {
	return &LoopZeroError{
		Code:    "ENVIRONMENT_NOT_FOUND",
		Message: fmt.Sprintf("environment '%s' not found", name),
	}
}

// NewDomainNotFound creates a DOMAIN_NOT_FOUND error with domain context.
// CHG-2026-04-23-AUDIT-13: brings domains handler into parity with other
// NewXNotFound factories (uniform JSON shape {code, message}).
func NewDomainNotFound(domain string) *LoopZeroError {
	return &LoopZeroError{
		Code:    "DOMAIN_NOT_FOUND",
		Message: fmt.Sprintf("domain '%s' not found", domain),
	}
}

// NewRepositoryNotLinked is returned by DELETE /projects/{name}/connect when
// the project has no linked repository to disconnect.
// CHG-2026-04-23-AUDIT-21: fixes silent-success anti-pattern — without this,
// the endpoint returned 200 OK even when there was nothing to disconnect,
// identical to the bug fixed in AUDIT-6 (team member remove) and AUDIT-12
// (environment delete).
func NewRepositoryNotLinked(project string) *LoopZeroError {
	return &LoopZeroError{
		Code:    "REPOSITORY_NOT_LINKED",
		Message: fmt.Sprintf("no repository linked to project '%s'", project),
	}
}

// ErrForbidden is returned when an operation is not permitted.
var ErrForbidden = &LoopZeroError{
	Code:    "FORBIDDEN",
	Message: "this operation is not permitted",
}

// NewIdempotencyConflict creates an IDEMPOTENCY_CONFLICT error with key context.
func NewIdempotencyConflict(key string) *LoopZeroError {
	return &LoopZeroError{
		Code:    "IDEMPOTENCY_CONFLICT",
		Message: fmt.Sprintf("idempotency key '%s' was already used with a different request payload", key),
	}
}

// NewConcurrentActivation creates a CONCURRENT_ACTIVATION error returned
// when the partial unique index `idx_releases_active_unique` (migration
// 000022) rejects a second ACTIVE release for the same (tenant, project,
// environment) tuple. T5.1 CV-M-1 of plan
// 2026-06-01-p3-infra-observability-test-debt (closes infra-007 DoD
// "HTTP 409 CONCURRENT_ACTIVATION documented in CLI error mapping").
//
// Distinguished from DEPLOY_IN_PROGRESS (held by environment lock, race
// at the API boundary) because this fires at the convergence-loop
// activation step — two replicas attempting concurrent activation of
// different releases for the same env. The DB enforces at-most-one
// ACTIVE per env via the partial unique index; the second UPDATE hits
// SQLSTATE 23505. Operator interpretation: race between sync_completion
// callbacks; the loser retries on next reconcile tick (loop is idempotent).
func NewConcurrentActivation(projectID, env string) *LoopZeroError {
	return &LoopZeroError{
		Code: "CONCURRENT_ACTIVATION",
		Message: fmt.Sprintf(
			"another release activation is in progress for env '%s/%s'; retry on next convergence tick",
			projectID, env,
		),
	}
}

// NewPlanUpgradeRequired creates a PLAN_UPGRADE_REQUIRED error returned when
// a feature is gated to a higher plan tier than the tenant currently holds.
// J-20 Scale plan v1.1 T1.2 + EC-3 (KEDA scale-to-zero requires Pro+).
//
// Maps to HTTP 402 "Payment Required" per ADR D3 — surfaces an upgrade CTA
// in the CLI / dashboard. The wire body carries `{required_plan, current_plan,
// feature}` so consumers can render a contextual upgrade prompt.
//
// Distinguished from REPLICAS_EXCEED_PLAN (422 — request shape is valid but
// quantity exceeds cap): PLAN_UPGRADE_REQUIRED means the feature itself is
// not available on this tier, not that a numeric cap was exceeded.
func NewPlanUpgradeRequired(required, current, feature string) *LoopZeroError {
	return &LoopZeroError{
		Code: "PLAN_UPGRADE_REQUIRED",
		Message: fmt.Sprintf(
			"%s requires the %s plan or higher (your current plan is %s). Upgrade at https://getloopscale.com/pricing.",
			feature, displayPlanName(required), displayPlanName(current),
		),
	}
}

// NewHPATargetBoundsViolated creates an HPA_TARGET_OUT_OF_BOUNDS error
// returned when a Horizontal Pod Autoscaler target falls outside the
// permitted range. J-20 Scale plan v1.1 T1.3 + EC-4.
//
// Maps to HTTP 400 (validation). Bounds are pinned to canonical Kubernetes
// recommendations: cpu_target ∈ [10,95], memory_target ∈ [10,90].
//
// `field` is the offending request key ("cpu_target" or "memory_target") so
// the dashboard can highlight the specific input. `min`/`max`/`got` echo the
// constants from pkg/enforcement.HPACPUTargetMin/Max + ...Memory equivalents.
func NewHPATargetBoundsViolated(field string, minBound, maxBound, got int) *LoopZeroError {
	return &LoopZeroError{
		Code: "HPA_TARGET_OUT_OF_BOUNDS",
		Message: fmt.Sprintf(
			"HPA %s must be between %d%% and %d%% (got %d%%). Targets below the minimum trigger autoscaling before the pod warms up; above the maximum leave no headroom for transient spikes.",
			field, minBound, maxBound, got,
		),
	}
}

// ErrBuildSubmitFailed is the canonical LoopZeroError for build-trigger failures
// that REACH the orchestrator (vs INTERNAL_ERROR which stays for unexpected
// failures outside the orchestrator's responsibility). Sprint 6 plan qa-
// sprint6-cleanup-2026-06-25 ADR D3 (closes loopzero-cloud#34) — distinct code
// so loopzero-cli + dashboards + telemetry can switch on it cleanly.
var ErrBuildSubmitFailed = &LoopZeroError{
	Code:    "BUILD_SUBMIT_FAILED",
	Message: "The build could not be triggered. Try again, or check `loopzero status` for details.",
}

// NewComposed returns an error with base's code whose message pairs an
// actionable summary with the underlying cause: "<summary>: <detail>".
//
// When detail carries nothing useful — empty, or reduced to redaction
// placeholders by the caller's scrubbing — the message falls back to
// "<summary>. Reference: <traceID>. Contact support if it persists." so the
// operator still has a handle on the server-side log without the cause
// leaking (Red Team loopzero-cloud#35). An empty summary (a reason no catalog
// names) uses base.Message and never the detail.
//
// The reason → summary catalogs live next to the reason enums that own them
// (api/internal/source PushBundleErrorReason, api/internal/builds
// SubmitBuildErrorReason), so an enum and its copy change together; this
// function is only the composition rule.
func NewComposed(base *LoopZeroError, summary, detail, traceID string) *LoopZeroError {
	if summary == "" {
		return &LoopZeroError{Code: base.Code, Message: appendTraceIDFallback(base.Message, traceID)}
	}
	return &LoopZeroError{Code: base.Code, Message: composeMessageWithTraceID(summary, detail, traceID)}
}

// Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T1.1 — composition helpers.
// Closes the Red Team's #35 suggestion of separating client-facing
// (generic + trace-id) from server-side log (verbatim cause).
//
// The Sprint 5/6 composition won when detail was a benign verbatim cause
// (e.g., "connection refused", "quota exceeded"). The Sprint 7 #35 scrub
// extension correctly redacted internal terms — but for SOME inputs the
// scrub collapsed detail to `<orchestrator>: <op>` placeholders only,
// which leaks zero info but ALSO surfaces zero diagnostic. The trace-id
// fallback gives the operator a correlation handle to support without
// leaking implementation details.

// composeMessageWithTraceID is the canonical "<summary>: <verbatim detail>"
// composer that DOWNGRADES to "<summary>. Reference: <trace-id>." when the
// scrubbed detail is empty or fully reduced to placeholders. KISS: a single
// branch on isUsefulDetail.
func composeMessageWithTraceID(summary, detail, traceID string) string {
	if isUsefulDetail(detail) {
		return summary + ": " + detail
	}
	return appendTraceIDFallback(summary, traceID)
}

// appendTraceIDFallback adds "Reference: <trace-id>. Contact support if it
// persists." to an existing summary when traceID is non-empty; otherwise
// returns the summary unchanged (preserves Sprint 5/6 backward-compat for
// callers without a correlation ID).
func appendTraceIDFallback(summary, traceID string) string {
	if traceID == "" {
		return summary
	}
	if !strings.HasSuffix(summary, ".") {
		summary += "."
	}
	return summary + " Reference: " + traceID + ". Contact support if it persists."
}

// isUsefulDetail reports whether the scrubbed detail still carries
// actionable information for the operator. "Useful" means: ≥ 20 chars
// AND not exclusively `<...>` placeholders + whitespace + punctuation.
// Threshold 20 chosen empirically — shorter than "Source backend is
// unreachable: " (28 chars) but longer than "<orchestrator>" (14 chars)
// so the heuristic flips at the right boundary.
//
// The threshold and the placeholder stripper live here, with NewComposed, so
// every reason catalog composes the same way.
func isUsefulDetail(detail string) bool {
	if len(detail) < 20 {
		return false
	}
	// Strip placeholders + collapse whitespace; if what remains is empty
	// or punctuation-only, the detail is non-informative.
	stripped := placeholderRE.ReplaceAllString(detail, "")
	stripped = whitespaceRE.ReplaceAllString(stripped, " ")
	stripped = strings.Trim(stripped, " :;.,!?-")
	return len(stripped) >= 8
}

// ErrSecretsBackendUnavailable marks a failure of the encryption backend that
// secret writes depend on — issue #138.
//
// Before this sentinel, a backend that could not encrypt surfaced as the same
// generic INTERNAL_ERROR as a database outage or any other 5xx. `loopzero secret
// set` answered "Something went wrong" and nothing persisted, while `loopzero
// secret list` kept working (reads do not encrypt). The issue author had to
// GUESS the cause from which project happened to work — their report offers a
// hypothesis, not a diagnosis, because the API supplied nothing to diagnose.
//
// The message stays in product language deliberately (Rule 3): the operator
// learns that the secrets backend refused, not which vendor implements it.
// Naming the component here would leak an internal term into CLI output and
// would also go stale the moment the implementation changes.
//
// Wrap it — do not return it bare — so the underlying cause survives for the
// logs while `errors.Is` still matches at the HTTP boundary:
//
//	fmt.Errorf("secret_store: vault transit encrypt: %w", ErrSecretsBackendUnavailable)
//
// The 503 mapping lives in pkg/errors/http/mapping.go, not here — per ADR-0030
// the transport boundary owns status resolution (gate PKG-G15).
var ErrSecretsBackendUnavailable = &LoopZeroError{
	Code:    "SECRETS_BACKEND_UNAVAILABLE",
	Message: "The secrets backend is not available for this project right now, so the value was not saved. Nothing was partially written — retry, and if it persists ask an operator to check the secrets backend for this environment.",
}
