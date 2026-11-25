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
	"ENV_ALREADY_EXISTS":    http.StatusConflict, // environments/environmentshttp — duplicate (project, env)
	"TENANT_SLUG_TAKEN":     http.StatusConflict, // bootstrap/bootstraphttp — slug owned by another identity
	"USER_NOT_FOUND":        http.StatusNotFound, // teams/teamshttp
	"MEMBER_NOT_FOUND":      http.StatusNotFound, // teams/teamshttp
	"TEAM_MEMBER_NOT_FOUND": http.StatusNotFound, // teams/teamshttp (remove)
	"APP_NOT_FOUND":         http.StatusNotFound, // scaling/scalinghttp — app the project does not declare
	"INVALID_ENVIRONMENT":   http.StatusBadRequest,
	"INVALID_CALLBACK":      http.StatusBadRequest, // builds — build completion callback
	"MULTIPART_FIELD_ORDER": http.StatusBadRequest, // source/sourcehttp — services_json must precede bundle
	// Validation at the input boundary is a 4xx with a client-classified code,
	// never a 500 (Red Team loopzero-cloud#103).
	"INVALID_ARGUMENT":        http.StatusBadRequest, // db/store/scaling_types.go
	"INVALID_DOMAIN":          http.StatusBadRequest, // domains/domainshttp — domain format
	"INVALID_REPLICAS":        http.StatusBadRequest, // scaling/scalinghttp — replicas <= 0
	"SERVICES_JSON_TOO_LARGE": http.StatusRequestEntityTooLarge,
	"NOT_MEMBER":              http.StatusForbidden,      // auth/authhttp — team switch
	"NOT_AVAILABLE":           http.StatusNotImplemented, // auth/authhttp — feature not configured
	// These five answered 500 until they were registered: their authors
	// passed the intended status to NewLoopZeroError, which ignored it.
	"INVALID_APP_NAME":          http.StatusBadRequest,     // builds/buildshttp — app name validation
	"INVALID_APP_PATH":          http.StatusBadRequest,     // builds/buildshttp — app path validation
	"ENVIRONMENT_AMBIGUOUS":     http.StatusBadRequest,     // environments/environmentshttp — name in several projects
	"SCALE_TO_ZERO_UNAVAILABLE": http.StatusBadRequest,     // builds — min 0 without KEDA
	"ENV_PROVISION_TIMEOUT":     http.StatusGatewayTimeout, // environments — env not ready in time
	// A first-deploy environment could not be provisioned: a server-side
	// failure, registered so it is an explicit 500 rather than a default one.
	"ENV_PROVISION_FAILED": http.StatusInternalServerError, // environments
	// GET /builds/{id}/logs — the log backend could not be reached. 503, not
	// 200-with-no-entries: an outage must not read as "this build has no logs".
	"LOGS_UNAVAILABLE": http.StatusServiceUnavailable, // logs/logshttp
}

func init() {
	for code, status := range apiStatusByCode {
		errorshttp.MustRegister(code, status)
	}
}
