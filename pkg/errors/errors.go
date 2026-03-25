// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package errors defines typed error types for all LoopZero components.
// All errors carry a code, message, and HTTP status for consistent
// error handling across the system. See CLAUDE.md Rule 8.
package errors

import "fmt"

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
}

// Error implements the error interface.
func (e *LoopZeroError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Is checks whether the target error has the same code as this LoopZeroError.
// This enables errors.Is() support for comparing error codes.
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
	}
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
)

// NewLockTimeout creates a LOCK_TIMEOUT error with optional detail.
func NewLockTimeout(detail string) *LoopZeroError {
	if detail == "" {
		return ErrLockTimeout
	}
	return &LoopZeroError{Code: "LOCK_TIMEOUT", Message: detail, HTTPStatus: 409}
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

// NewIdempotencyConflict creates an IDEMPOTENCY_CONFLICT error with key context.
func NewIdempotencyConflict(key string) *LoopZeroError {
	return &LoopZeroError{
		Code:       "IDEMPOTENCY_CONFLICT",
		Message:    fmt.Sprintf("idempotency key '%s' was already used with a different request payload", key),
		HTTPStatus: 422,
	}
}
