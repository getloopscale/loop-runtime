// Package errors defines typed error types for LoopZero operators.
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
	}
}

// Predefined errors for common failure scenarios.
var (
	// ErrLockTimeout is returned when an environment lock cannot be acquired
	// within the 5-second timeout (spec section 4.2).
	ErrLockTimeout = &LoopZeroError{
		Code:       "LOCK_TIMEOUT",
		Message:    "could not acquire environment lock within timeout",
		HTTPStatus: 409,
	}

	// ErrInvalidTransition is returned when a state machine transition is
	// attempted that violates the defined state machine (spec section 5).
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
)
