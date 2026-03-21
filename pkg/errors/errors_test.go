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
		{"ErrBuildNotFound", ErrBuildNotFound, "BUILD_NOT_FOUND", 404},
		{"ErrReleaseNotFound", ErrReleaseNotFound, "RELEASE_NOT_FOUND", 404},
		{"ErrDeployInProgress", ErrDeployInProgress, "DEPLOY_IN_PROGRESS", 409},
		{"ErrInvalidName", ErrInvalidName, "INVALID_NAME", 400},
		{"ErrIdempotencyConflict", ErrIdempotencyConflict, "IDEMPOTENCY_CONFLICT", 422},
		{"ErrProjectNotFound", ErrProjectNotFound, "PROJECT_NOT_FOUND", 404},
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
	t.Run("NewLockTimeout with empty detail uses default", func(t *testing.T) {
		err := NewLockTimeout("")
		if err != ErrLockTimeout {
			t.Error("expected default ErrLockTimeout when detail is empty")
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
