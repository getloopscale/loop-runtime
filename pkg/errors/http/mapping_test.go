// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package http

import (
	stderrors "errors"
	"net/http"
	"testing"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

func TestStatus_KnownCodeUsesRegistry(t *testing.T) {
	got := Status(loopzeroerrors.ErrLockTimeout)
	if got != http.StatusConflict {
		t.Errorf("ErrLockTimeout should map to 409, got %d", got)
	}
}

func TestStatus_NotFoundCodes(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{loopzeroerrors.ErrTenantNotFound, http.StatusNotFound},
		{loopzeroerrors.ErrBuildNotFound, http.StatusNotFound},
		{loopzeroerrors.ErrReleaseNotFound, http.StatusNotFound},
		{loopzeroerrors.ErrPreviewNotFound, http.StatusNotFound},
	}
	for _, c := range cases {
		if got := Status(c.err); got != c.want {
			t.Errorf("Status(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestStatus_UnauthorizedAndForbidden(t *testing.T) {
	if got := Status(loopzeroerrors.ErrUnauthorized); got != http.StatusUnauthorized {
		t.Errorf("ErrUnauthorized should map to 401, got %d", got)
	}
	if got := Status(loopzeroerrors.ErrForbidden); got != http.StatusForbidden {
		t.Errorf("ErrForbidden should map to 403, got %d", got)
	}
}

func TestStatus_NilReturnsOK(t *testing.T) {
	if got := Status(nil); got != http.StatusOK {
		t.Errorf("Status(nil) = %d, want 200", got)
	}
}

func TestStatus_NonLoopZeroErrorReturns500(t *testing.T) {
	if got := Status(stderrors.New("plain")); got != http.StatusInternalServerError {
		t.Errorf("non-LoopZeroError should map to 500, got %d", got)
	}
}

func TestStatus_UnregisteredCodeReturns500(t *testing.T) {
	// Stage C: HTTPStatus field removed; registry is now the only
	// source of truth. Unregistered codes default to 500.
	custom := &loopzeroerrors.LoopZeroError{Code: "NOT_REGISTERED_YET", Message: "x"}
	if got := Status(custom); got != http.StatusInternalServerError {
		t.Errorf("unregistered code must default to 500; got %d", got)
	}
}

func TestIsRegistered(t *testing.T) {
	if !IsRegistered("LOCK_TIMEOUT") {
		t.Error("LOCK_TIMEOUT must be registered")
	}
	if IsRegistered("MADE_UP_CODE") {
		t.Error("MADE_UP_CODE must not be registered")
	}
}

func TestStatus_PreservesIsAsCompatibility(t *testing.T) {
	wrapped := loopzeroerrors.ErrLockTimeout.WrapWithStack("submit")
	if got := Status(wrapped); got != http.StatusConflict {
		t.Errorf("Status must traverse wrap chains; got %d", got)
	}
}

// TestRegistry_CoversAllSentinelsCommonInAPI is a regression guard
// ensuring the registry stays in sync with codes emitted by the api/
// module. Newly introduced LoopZeroError codes that surface at the HTTP
// boundary MUST be registered here or they default to 500 (silent).
//
// The list below is the minimum coverage discovered during T2.3
// Stage C migration (2026-05-04). Add new entries when adding new
// LoopZeroError codes; pair with sentinel definition for traceability.
func TestRegistry_CoversAllSentinelsCommonInAPI(t *testing.T) {
	required := []string{
		"INVALID_ENVIRONMENT", "INVALID_CALLBACK", "VALIDATION_ERROR",
		"USER_NOT_FOUND", "MEMBER_NOT_FOUND", "TEAM_MEMBER_NOT_FOUND",
		"UNSUPPORTED_FRAMEWORK",
	}
	for _, code := range required {
		if !IsRegistered(code) {
			t.Errorf("registry must include %q (used by api/ — silent 500 if missing)", code)
		}
	}
}

// J-20 Scale plan v1.1 T1.2 + T1.3:
// PLAN_UPGRADE_REQUIRED MUST map to 402 (Payment Required) so dashboards
// and CLIs can branch on the canonical upgrade-prompt surface.
// HPA_TARGET_OUT_OF_BOUNDS MUST map to 400 (BadRequest validation).
func TestStatus_J20ScaleErrors(t *testing.T) {
	tests := []struct {
		code string
		want int
	}{
		{"PLAN_UPGRADE_REQUIRED", http.StatusPaymentRequired},
		{"HPA_TARGET_OUT_OF_BOUNDS", http.StatusBadRequest},
	}
	for _, tc := range tests {
		err := &loopzeroerrors.LoopZeroError{Code: tc.code, Message: "x"}
		if got := Status(err); got != tc.want {
			t.Errorf("Status(%s)=%d, want %d", tc.code, got, tc.want)
		}
		if !IsRegistered(tc.code) {
			t.Errorf("registry must include %q (J-20 Scale)", tc.code)
		}
	}
}
