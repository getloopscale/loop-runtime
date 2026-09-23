// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package httpio

import (
	"net/http"
	"testing"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	errorshttp "github.com/getloopscale/loopzero/pkg/errors/http"
)

func TestAPIErrorCodesAnswerTheirRegisteredStatus(t *testing.T) {
	cases := map[string]int{
		"BUILD_NOT_TERMINAL":      http.StatusConflict,
		"ENV_ALREADY_EXISTS":      http.StatusConflict,
		"USER_NOT_FOUND":          http.StatusNotFound,
		"MEMBER_NOT_FOUND":        http.StatusNotFound,
		"TEAM_MEMBER_NOT_FOUND":   http.StatusNotFound,
		"INVALID_ENVIRONMENT":     http.StatusBadRequest,
		"INVALID_CALLBACK":        http.StatusBadRequest,
		"MULTIPART_FIELD_ORDER":   http.StatusBadRequest,
		"INVALID_ARGUMENT":        http.StatusBadRequest, // Red Team #103
		"INVALID_DOMAIN":          http.StatusBadRequest, // Red Team #103
		"INVALID_REPLICAS":        http.StatusBadRequest, // Red Team #103
		"SERVICES_JSON_TOO_LARGE": http.StatusRequestEntityTooLarge,
		"NOT_MEMBER":              http.StatusForbidden,
		"NOT_AVAILABLE":           http.StatusNotImplemented,
		// Their authors passed these statuses to NewLoopZeroError, which ignored
		// them: all five answered 500.
		"INVALID_APP_NAME":          http.StatusBadRequest,
		"INVALID_APP_PATH":          http.StatusBadRequest,
		"ENVIRONMENT_AMBIGUOUS":     http.StatusBadRequest,
		"SCALE_TO_ZERO_UNAVAILABLE": http.StatusBadRequest,
		"ENV_PROVISION_TIMEOUT":     http.StatusGatewayTimeout,
	}
	for code, want := range cases {
		if got := errorshttp.Status(&loopzeroerrors.LoopZeroError{Code: code}); got != want {
			t.Errorf("Status(%s) = %d, want %d", code, got, want)
		}
	}
}
