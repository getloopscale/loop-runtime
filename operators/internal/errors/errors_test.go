package errors

import (
	"errors"
	"testing"
)

func TestLoopZeroError_ImplementsErrorInterface(t *testing.T) {
	var err error = ErrLockTimeout
	if err.Error() == "" {
		t.Error("LoopZeroError.Error() should return non-empty string")
	}
}

func TestLoopZeroError_ErrorFormat(t *testing.T) {
	err := NewLoopZeroError("TEST_CODE", "test message", 500)
	expected := "[TEST_CODE] test message"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestLoopZeroError_Is(t *testing.T) {
	err := NewLoopZeroError("LOCK_TIMEOUT", "custom message", 409)
	if !errors.Is(err, ErrLockTimeout) {
		t.Error("expected err to match ErrLockTimeout via errors.Is")
	}
}

func TestLoopZeroError_IsNotMatch(t *testing.T) {
	if errors.Is(ErrLockTimeout, ErrTenantNotFound) {
		t.Error("ErrLockTimeout should not match ErrTenantNotFound")
	}
}

func TestLoopZeroError_Wrap(t *testing.T) {
	wrapped := ErrTenantNotFound.Wrap("tenant acme")
	if wrapped.Code != "TENANT_NOT_FOUND" {
		t.Errorf("expected code %q, got %q", "TENANT_NOT_FOUND", wrapped.Code)
	}
	expected := "tenant acme: tenant not found"
	if wrapped.Message != expected {
		t.Errorf("expected message %q, got %q", expected, wrapped.Message)
	}
	if wrapped.HTTPStatus != 404 {
		t.Errorf("expected HTTP status 404, got %d", wrapped.HTTPStatus)
	}
}

func TestPredefinedErrors_HaveCorrectCodes(t *testing.T) {
	tests := []struct {
		err        *LoopZeroError
		code       string
		httpStatus int
	}{
		{ErrLockTimeout, "LOCK_TIMEOUT", 409},
		{ErrInvalidTransition, "INVALID_TRANSITION", 409},
		{ErrTenantNotFound, "TENANT_NOT_FOUND", 404},
		{ErrBuildNotFound, "BUILD_NOT_FOUND", 404},
		{ErrReleaseNotFound, "RELEASE_NOT_FOUND", 404},
		{ErrDeployInProgress, "DEPLOY_IN_PROGRESS", 409},
		{ErrInvalidName, "INVALID_NAME", 400},
	}

	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			if tc.err.Code != tc.code {
				t.Errorf("expected code %q, got %q", tc.code, tc.err.Code)
			}
			if tc.err.HTTPStatus != tc.httpStatus {
				t.Errorf("expected HTTP status %d, got %d", tc.httpStatus, tc.err.HTTPStatus)
			}
			if tc.err.Message == "" {
				t.Error("message should not be empty")
			}
		})
	}
}

func TestLoopZeroError_IsWithNonLoopZeroError(t *testing.T) {
	stdErr := errors.New("standard error")
	if errors.Is(ErrLockTimeout, stdErr) {
		t.Error("LoopZeroError should not match a standard error")
	}
}
