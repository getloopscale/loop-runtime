// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package httpio

import (
	"net/http"

	errorshttp "github.com/getloopscale/loopzero/pkg/errors/http"
)

// apiStatusByCode is the HTTP status of every error code the api defines
// outside pkg/errors. pkg/errors/http registers the codes pkg/errors owns;
// the api registers its own here, so adding one never edits the shared
// kernel. An unregistered code answers 500.
var apiStatusByCode = map[string]int{
	"BUILD_NOT_TERMINAL":    http.StatusConflict, // adapters/build_adapter.go — redeploy guard
	"ENV_ALREADY_EXISTS":    http.StatusConflict, // routes/environments — duplicate (project, env)
	"USER_NOT_FOUND":        http.StatusNotFound, // routes/teams
	"MEMBER_NOT_FOUND":      http.StatusNotFound, // routes/teams
	"TEAM_MEMBER_NOT_FOUND": http.StatusNotFound, // routes/teams (remove)
	"INVALID_ENVIRONMENT":   http.StatusBadRequest,
	"INVALID_CALLBACK":      http.StatusBadRequest, // services/build — build completion callback
	"MULTIPART_FIELD_ORDER": http.StatusBadRequest, // routes/source — services_json must precede bundle
	// Validation at the input boundary is a 4xx with a client-classified code,
	// never a 500 (Red Team loopzero-cloud#103).
	"INVALID_ARGUMENT":        http.StatusBadRequest, // db/store/scaling_types.go
	"INVALID_DOMAIN":          http.StatusBadRequest, // routes/domains — domain format
	"INVALID_REPLICAS":        http.StatusBadRequest, // routes/scale — replicas <= 0
	"SERVICES_JSON_TOO_LARGE": http.StatusRequestEntityTooLarge,
	"NOT_MEMBER":              http.StatusForbidden,      // routes/auth — team switch
	"NOT_AVAILABLE":           http.StatusNotImplemented, // routes/auth — feature not configured
}

func init() {
	for code, status := range apiStatusByCode {
		errorshttp.MustRegister(code, status)
	}
}
