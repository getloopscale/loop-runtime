// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package errors defines typed error types for all LoopZero components.
// All errors carry a code, message, and HTTP status for consistent
// error handling across the system. See CLAUDE.md Rule 8.
package errors

import (
	"fmt"
	"runtime"
	"strings"
)

// stackDepth bounds frame capture in WrapWithStack — 32 frames is
// enough for any realistic LoopZero call chain (handler → service → store →
// driver), and bounded so cost stays in the ~150ns range.
const stackDepth = 32

// LoopZeroError is a typed error with a code and HTTP status.
// It implements the error interface and provides structured context
// for error handling and API responses.
type LoopZeroError struct {
	// Code is a machine-readable error code (e.g., "LOCK_TIMEOUT").
	Code string

	// Message is a human-readable error description with context.
	Message string

	// HTTPStatus is the suggested HTTP status code for API responses.
	HTTPStatus int

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

// NewLoopZeroError creates a new LoopZeroError with the given code, message, and HTTP status.
func NewLoopZeroError(code, message string, httpStatus int) *LoopZeroError {
	return &LoopZeroError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
	}
}

// Wrap returns a new LoopZeroError with the same code and HTTP status but with
// additional context prepended to the message.
func (e *LoopZeroError) Wrap(context string) *LoopZeroError {
	return &LoopZeroError{
		Code:       e.Code,
		Message:    fmt.Sprintf("%s: %s", context, e.Message),
		HTTPStatus: e.HTTPStatus,
		Cause:      e,
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
		Code:       e.Code,
		Message:    fmt.Sprintf("%s: %s", context, e.Message),
		HTTPStatus: e.HTTPStatus,
		Cause:      e,
		stack:      e.stack,
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
		Code:       "LOCK_TIMEOUT",
		Message:    "could not acquire environment lock within timeout",
		HTTPStatus: 409,
	}

	// ErrInvalidTransition is returned when a state machine transition is
	// attempted that violates the defined state machine (spec §5).
	ErrInvalidTransition = &LoopZeroError{
		Code:       "INVALID_TRANSITION",
		Message:    "invalid state transition",
		HTTPStatus: 409,
	}

	// ErrTenantNotFound is returned when a referenced tenant does not exist.
	ErrTenantNotFound = &LoopZeroError{
		Code:       "TENANT_NOT_FOUND",
		Message:    "tenant not found",
		HTTPStatus: 404,
	}

	// ErrProjectNotFound is returned when a referenced project does not exist.
	ErrProjectNotFound = &LoopZeroError{
		Code:       "PROJECT_NOT_FOUND",
		Message:    "project not found",
		HTTPStatus: 404,
	}

	// ErrBuildNotFound is returned when a referenced build does not exist.
	ErrBuildNotFound = &LoopZeroError{
		Code:       "BUILD_NOT_FOUND",
		Message:    "build not found",
		HTTPStatus: 404,
	}

	// ErrReleaseNotFound is returned when a referenced release does not exist.
	ErrReleaseNotFound = &LoopZeroError{
		Code:       "RELEASE_NOT_FOUND",
		Message:    "release not found",
		HTTPStatus: 404,
	}

	// ErrPreviewNotFound is returned when a referenced preview environment
	// does not exist or has been soft-deleted (Sprint 5).
	ErrPreviewNotFound = &LoopZeroError{
		Code:       "PREVIEW_NOT_FOUND",
		Message:    "preview environment not found",
		HTTPStatus: 404,
	}

	// ErrNoActiveRelease is returned when rollback or promote is attempted but
	// the source environment has no active release. Shared by both flows so the
	// message must stay neutral (no "rollback"/"promote" wording).
	ErrNoActiveRelease = &LoopZeroError{
		Code:       "NO_ACTIVE_RELEASE",
		Message:    "No active release found in the source environment. Deploy at least once before promoting or rolling back.",
		HTTPStatus: 404,
	}

	// ErrNoPreviousRelease is returned when rollback is attempted but no previous release exists.
	ErrNoPreviousRelease = &LoopZeroError{
		Code:       "NO_PREVIOUS_RELEASE",
		Message:    "No previous release found to rollback to. Deploy at least twice before rolling back.",
		HTTPStatus: 404,
	}

	// ErrBuildAlreadyCompleted is returned when attempting to cancel a build
	// that is already in a terminal state (SUCCEEDED, FAILED, or CANCELLED).
	ErrBuildAlreadyCompleted = &LoopZeroError{
		Code:       "BUILD_ALREADY_COMPLETED",
		Message:    "Build already completed.",
		HTTPStatus: 409,
	}

	// ErrDeployInProgress is returned when a deploy is already in progress
	// for the same (tenant, project, environment).
	ErrDeployInProgress = &LoopZeroError{
		Code:       "DEPLOY_IN_PROGRESS",
		Message:    "a deploy is already in progress for this environment",
		HTTPStatus: 409,
	}

	// ErrInvalidName is returned when a resource name does not conform to
	// Kubernetes naming rules (RFC 1123).
	ErrInvalidName = &LoopZeroError{
		Code:       "INVALID_NAME",
		Message:    "resource name does not conform to RFC 1123",
		HTTPStatus: 400,
	}

	// ErrIdempotencyConflict is returned when an idempotency key was already
	// used with a different request payload.
	ErrIdempotencyConflict = &LoopZeroError{
		Code:       "IDEMPOTENCY_CONFLICT",
		Message:    "idempotency key was already used with a different request payload",
		HTTPStatus: 422,
	}

	// ErrUnauthorized is returned when a request lacks valid authentication.
	ErrUnauthorized = &LoopZeroError{
		Code:       "UNAUTHORIZED",
		Message:    "not authenticated",
		HTTPStatus: 401,
	}

	// ErrInvalidRequest is returned when the request body cannot be parsed.
	ErrInvalidRequest = &LoopZeroError{
		Code:       "INVALID_REQUEST",
		Message:    "invalid request body",
		HTTPStatus: 400,
	}

	// ErrProjectExists is returned when a project with the same name
	// already exists within a tenant.
	ErrProjectExists = &LoopZeroError{
		Code:       "PROJECT_EXISTS",
		Message:    "a project with this name already exists in your team",
		HTTPStatus: 409,
	}

	// ErrInternalError is returned for unexpected server-side failures.
	ErrInternalError = &LoopZeroError{
		Code:       "INTERNAL_ERROR",
		Message:    "Something went wrong. Try again, or check `loopzero status` for details. If the problem persists, contact support.",
		HTTPStatus: 500,
	}

	// ErrPayloadTooLarge is returned when the request body exceeds the size limit.
	ErrPayloadTooLarge = &LoopZeroError{
		Code:       "PAYLOAD_TOO_LARGE",
		Message:    "request body exceeds the maximum allowed size",
		HTTPStatus: 413,
	}

	// ErrValidationFailed is returned when input validation fails with field-specific details.
	ErrValidationFailed = &LoopZeroError{
		Code:       "VALIDATION_FAILED",
		Message:    "validation failed",
		HTTPStatus: 422,
	}

	// ErrFrontendMustBeStatic is returned when a frontend app doesn't use render: static.
	// MEET-2026-03-29-016: All frontends are served from CDN, zero pods.
	ErrFrontendMustBeStatic = &LoopZeroError{
		Code:       "FRONTEND_MUST_BE_STATIC",
		Message:    "frontend apps must use static delivery. Add 'render: static' to your loopzero.yaml",
		HTTPStatus: 422,
	}

	// ErrPodLimitExceeded is returned when a project has more than 1 server or 1 worker.
	// MEET-2026-03-29-016: Max 1 API service + 1 background worker per project.
	ErrPodLimitExceeded = &LoopZeroError{
		Code:       "POD_LIMIT_EXCEEDED",
		Message:    "project exceeds the maximum of 1 API service and 1 background worker",
		HTTPStatus: 422,
	}

	// ErrMissingField is returned when a required field is missing from the request.
	ErrMissingField = &LoopZeroError{
		Code:       "MISSING_FIELD",
		Message:    "a required field is missing",
		HTTPStatus: 400,
	}

	// ErrSourceUploadFailed is returned when pushing the bundle to the source store fails.
	ErrSourceUploadFailed = &LoopZeroError{
		Code:       "SOURCE_UPLOAD_FAILED",
		Message:    "Failed to upload source code. Ensure your git history is clean and try again.",
		HTTPStatus: 500,
	}

	// ErrPlatformDegraded is returned when the deploy service circuit breaker is open.
	// The platform is temporarily unable to process sync requests (DA-7).
	ErrPlatformDegraded = &LoopZeroError{
		Code:       "PLATFORM_DEGRADED",
		Message:    "deploy service is temporarily unavailable",
		HTTPStatus: 503,
	}

	// ErrDeployServiceAuthFailed is returned when the deploy service rejects its credentials (401/403).
	// This is NOT a transient failure — the credentials are invalid and must be renewed.
	// The circuit breaker MUST NOT count this toward the open threshold (MEET-013).
	ErrDeployServiceAuthFailed = &LoopZeroError{
		Code:       "DEPLOY_SERVICE_AUTH_FAILED",
		Message:    "deploy service credentials rejected — contact your platform administrator",
		HTTPStatus: 503,
	}

	// ErrSyncOperationInProgress is returned when the deploy service has a sync already running (409).
	// MEET-2026-03-29-019: This is NOT a transient failure — the previous operation must
	// be terminated before a new sync can start. The circuit breaker MUST NOT count this
	// toward the open threshold (same exclusion pattern as ErrDeployServiceAuthFailed).
	ErrSyncOperationInProgress = &LoopZeroError{
		Code:       "SYNC_OPERATION_IN_PROGRESS",
		Message:    "another sync operation is already in progress",
		HTTPStatus: 409,
	}

	// ErrCellAtCapacity is returned when the cell's concurrent build budget
	// is full. Roadmap 07: the build admission gate returns 429 so clients
	// know to retry after a bounded wait. The response MUST include a
	// `Retry-After` header with a sensible duration.
	ErrCellAtCapacity = &LoopZeroError{
		Code:       "CELL_AT_CAPACITY",
		Message:    "the platform is currently busy — retry in a few seconds",
		HTTPStatus: 429,
	}

	// ErrTenantBuildQuota is returned when a single tenant already has the
	// maximum allowed concurrent builds in flight. Enforces per-tenant
	// fairness so one noisy tenant cannot starve others even when the cell
	// has free capacity. Same Retry-After semantics as ErrCellAtCapacity.
	ErrTenantBuildQuota = &LoopZeroError{
		Code:       "TENANT_BUILD_QUOTA",
		Message:    "your team has too many builds in progress — wait for one to finish",
		HTTPStatus: 429,
	}

	// ErrSameEnvironment is returned when a promote request specifies the same
	// source and target environment.
	ErrSameEnvironment = &LoopZeroError{
		Code:       "SAME_ENVIRONMENT",
		Message:    "source and target environments must be different",
		HTTPStatus: 400,
	}

	// ErrReplicasExceedPlan is returned when an app requests more replicas (or
	// autoscaling.max) than the tenant's plan allows.
	// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059).
	// Factory NewReplicasExceedPlan adds plan + cap + requested + app context.
	ErrReplicasExceedPlan = &LoopZeroError{
		Code:       "REPLICAS_EXCEED_PLAN",
		Message:    "requested replicas exceed your plan's limit",
		HTTPStatus: 422,
	}

	// ErrScalingAmbiguous is returned when both `replicas` and `autoscaling`
	// are set on the same app. The two are mutually exclusive — pick one.
	// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059).
	ErrScalingAmbiguous = &LoopZeroError{
		Code:       "SCALING_AMBIGUOUS",
		Message:    "'replicas' and 'autoscaling' are mutually exclusive — choose one",
		HTTPStatus: 422,
	}
)

// NewReplicasExceedPlan creates a REPLICAS_EXCEED_PLAN error with plan name,
// plan cap, requested replica count, and the offending app name.
// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059).
func NewReplicasExceedPlan(plan string, limit, requested int, appName string) *LoopZeroError {
	return &LoopZeroError{
		Code: "REPLICAS_EXCEED_PLAN",
		Message: fmt.Sprintf(
			"App '%s' requested %d replicas — the %s plan allows up to %d per service. Upgrade at https://loopzero.example/pricing or lower the replica count.",
			appName, requested, displayPlanName(plan), limit,
		),
		HTTPStatus: 422,
	}
}

// displayPlanName returns a human-friendly plan name.
func displayPlanName(plan string) string {
	switch plan {
	case "starter":
		return "Starter"
	case "pro":
		return "Pro"
	case "team":
		return "Team"
	case "enterprise":
		return "Enterprise"
	default:
		return plan
	}
}

// NewLockTimeout creates a LOCK_TIMEOUT error with optional detail.
// Always returns a fresh copy to prevent mutation of the global sentinel.
func NewLockTimeout(detail string) *LoopZeroError {
	msg := ErrLockTimeout.Message
	if detail != "" {
		msg = detail
	}
	return &LoopZeroError{Code: "LOCK_TIMEOUT", Message: msg, HTTPStatus: 409}
}

// NewInvalidTransition creates an INVALID_TRANSITION error with state context.
func NewInvalidTransition(from, event string) *LoopZeroError {
	return &LoopZeroError{
		Code:       "INVALID_TRANSITION",
		Message:    fmt.Sprintf("invalid state transition: cannot apply '%s' in state '%s'", event, from),
		HTTPStatus: 409,
	}
}

// NewTenantNotFound creates a TENANT_NOT_FOUND error with tenant ID context.
func NewTenantNotFound(tenantID string) *LoopZeroError {
	return &LoopZeroError{
		Code:       "TENANT_NOT_FOUND",
		Message:    fmt.Sprintf("tenant '%s' not found", tenantID),
		HTTPStatus: 404,
	}
}

// NewBuildNotFound creates a BUILD_NOT_FOUND error with build ID context.
func NewBuildNotFound(buildID string) *LoopZeroError {
	return &LoopZeroError{
		Code:       "BUILD_NOT_FOUND",
		Message:    fmt.Sprintf("build '%s' not found", buildID),
		HTTPStatus: 404,
	}
}

// NewReleaseNotFound creates a RELEASE_NOT_FOUND error with release ID context.
func NewReleaseNotFound(releaseID string) *LoopZeroError {
	return &LoopZeroError{
		Code:       "RELEASE_NOT_FOUND",
		Message:    fmt.Sprintf("release '%s' not found", releaseID),
		HTTPStatus: 404,
	}
}

// NewProjectNotFound creates a PROJECT_NOT_FOUND error with project name context.
func NewProjectNotFound(name string) *LoopZeroError {
	return &LoopZeroError{
		Code:       "PROJECT_NOT_FOUND",
		Message:    fmt.Sprintf("project '%s' not found", name),
		HTTPStatus: 404,
	}
}

// NewEnvironmentNotFound creates an ENVIRONMENT_NOT_FOUND error with environment name context.
func NewEnvironmentNotFound(name string) *LoopZeroError {
	return &LoopZeroError{
		Code:       "ENVIRONMENT_NOT_FOUND",
		Message:    fmt.Sprintf("environment '%s' not found", name),
		HTTPStatus: 404,
	}
}

// NewDomainNotFound creates a DOMAIN_NOT_FOUND error with domain context.
// CHG-2026-04-23-AUDIT-13: brings domains handler into parity with other
// NewXNotFound factories (uniform JSON shape {code, message}).
func NewDomainNotFound(domain string) *LoopZeroError {
	return &LoopZeroError{
		Code:       "DOMAIN_NOT_FOUND",
		Message:    fmt.Sprintf("domain '%s' not found", domain),
		HTTPStatus: 404,
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
		Code:       "REPOSITORY_NOT_LINKED",
		Message:    fmt.Sprintf("no repository linked to project '%s'", project),
		HTTPStatus: 404,
	}
}

// ErrForbidden is returned when an operation is not permitted.
var ErrForbidden = &LoopZeroError{
	Code:       "FORBIDDEN",
	Message:    "this operation is not permitted",
	HTTPStatus: 403,
}

// NewIdempotencyConflict creates an IDEMPOTENCY_CONFLICT error with key context.
func NewIdempotencyConflict(key string) *LoopZeroError {
	return &LoopZeroError{
		Code:       "IDEMPOTENCY_CONFLICT",
		Message:    fmt.Sprintf("idempotency key '%s' was already used with a different request payload", key),
		HTTPStatus: 422,
	}
}
