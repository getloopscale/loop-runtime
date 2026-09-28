// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package http

import (
	stderrors "errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// M4 T1.1 — the per-Account project-cap typed error maps to 422 (mirrors
// REPLICAS_EXCEED_PLAN; the canonical loopzero cap status).
func TestStatus_ProjectsExceedPlan_422(t *testing.T) {
	if got := Status(loopzeroerrors.NewProjectsExceedPlan("starter", 1, 1, "acct-x")); got != 422 {
		t.Errorf("Status(PROJECTS_EXCEED_PLAN) = %d, want 422", got)
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

// Every code pkg/errors defines must have an explicit transport status: an
// unregistered code silently answers 500. Read from the source, so a new
// sentinel or factory cannot be added without registering it.
func TestEveryPkgErrorsCodeIsRegistered(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "errors.go"))
	if err != nil {
		t.Fatalf("read errors.go: %v", err)
	}
	codes := regexp.MustCompile(`Code:\s+"([A-Z][A-Z0-9_]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(codes) < 30 {
		t.Fatalf("found only %d codes in errors.go; the scan is broken", len(codes))
	}
	for _, c := range codes {
		if !IsRegistered(c[1]) {
			t.Errorf("code %q is defined in pkg/errors but has no registered HTTP status", c[1])
		}
	}
}

func TestRegister_AddsAStatusForACodeOwnedOutsidePkg(t *testing.T) {
	if err := Register("TEST_OWNED_ELSEWHERE", http.StatusGone); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if got := Status(&loopzeroerrors.LoopZeroError{Code: "TEST_OWNED_ELSEWHERE"}); got != http.StatusGone {
		t.Fatalf("Status = %d, want 410", got)
	}
	if err := Register("TEST_OWNED_ELSEWHERE", http.StatusGone); err != nil {
		t.Fatalf("re-registering the same status must be idempotent: %v", err)
	}
}

func TestRegister_RefusesAConflictingStatus(t *testing.T) {
	if err := Register("LOCK_TIMEOUT", http.StatusTeapot); err == nil {
		t.Fatal("re-mapping a registered code to another status must be refused")
	}
	if got := Status(&loopzeroerrors.LoopZeroError{Code: "LOCK_TIMEOUT"}); got != http.StatusConflict {
		t.Fatalf("the original mapping must survive a refused registration, got %d", got)
	}
}

func TestRegister_RefusesAnInvalidCodeOrStatus(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
	}{{"", http.StatusBadRequest}, {"TEST_OK_STATUS", http.StatusOK}, {"TEST_BAD_STATUS", 700}} {
		if err := Register(tc.code, tc.status); err == nil {
			t.Errorf("Register(%q, %d) must be refused", tc.code, tc.status)
		}
	}
}
