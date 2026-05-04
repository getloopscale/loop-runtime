// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package http maps LoopZero domain errors (pkg/errors.LoopZeroError) to HTTP
// transport-level concerns: status codes and RFC 9457 problem+json
// payloads.
//
// This sub-package exists per ADR-0030 + gate PKG-G15: pkg/errors
// itself stays transport-agnostic, while consumers that need HTTP
// semantics import from pkg/errors/http. CLI, operators, and audit
// code do not import this — they use pkg/errors directly with code
// strings and messages.
//
// Stage A of CONDITION-2 (api-engineer staged rollout): pkg/errors
// retains the HTTPStatus field for now. Status(err) prefers explicit
// lookup via the registry and falls back to the legacy field for
// backward compatibility. Stage B migrates writers to WriteJSON.
// Stage C removes the field — separate change record.
package http

import (
	stderrors "errors"
	"net/http"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// statusByCode maps machine-readable error codes to HTTP status codes.
// Authoritative registry — adding a new LoopZeroError code SHOULD register
// it here so transport semantics are explicit and reviewable.
//
// Codes ABSENT from this registry default to 500 via the fallback in
// Status(). That keeps the lookup robust during migration but new
// codes should be registered to avoid silent 500s.
var statusByCode = map[string]int{
	"LOCK_TIMEOUT":              http.StatusConflict,            // 409
	"INVALID_TRANSITION":        http.StatusConflict,            // 409
	"DEPLOY_IN_PROGRESS":        http.StatusConflict,            // 409
	"BUILD_ALREADY_COMPLETED":   http.StatusConflict,            // 409
	"PROJECT_EXISTS":            http.StatusConflict,            // 409
	"SYNC_OPERATION_IN_PROGRESS": http.StatusConflict,           // 409

	"TENANT_NOT_FOUND":     http.StatusNotFound,         // 404
	"PROJECT_NOT_FOUND":    http.StatusNotFound,         // 404
	"BUILD_NOT_FOUND":      http.StatusNotFound,         // 404
	"RELEASE_NOT_FOUND":    http.StatusNotFound,         // 404
	"PREVIEW_NOT_FOUND":    http.StatusNotFound,         // 404
	"NO_ACTIVE_RELEASE":    http.StatusNotFound,         // 404
	"NO_PREVIOUS_RELEASE":  http.StatusNotFound,         // 404
	"DOMAIN_NOT_FOUND":     http.StatusNotFound,         // 404
	"REPOSITORY_NOT_LINKED": http.StatusNotFound,        // 404
	"ENVIRONMENT_NOT_FOUND": http.StatusNotFound,        // 404
	"USER_NOT_FOUND":         http.StatusNotFound, // 404 (teams.go)
	"MEMBER_NOT_FOUND":       http.StatusNotFound, // 404 (teams.go)
	"TEAM_MEMBER_NOT_FOUND":  http.StatusNotFound, // 404 (teams.go remove)

	"INVALID_NAME":               http.StatusBadRequest, // 400
	"INVALID_REQUEST":            http.StatusBadRequest, // 400
	"INVALID_ENVIRONMENT":        http.StatusBadRequest, // 400 (builds.go DA-10)
	"INVALID_CALLBACK":           http.StatusBadRequest, // 400 (build_orchestrator_complete.go DA-15)
	"VALIDATION_ERROR":           http.StatusBadRequest, // 400 (db/scaling_store.go)
	"MISSING_FIELD":              http.StatusBadRequest, // 400
	"SAME_ENVIRONMENT":           http.StatusBadRequest, // 400

	"UNAUTHORIZED": http.StatusUnauthorized, // 401
	"FORBIDDEN":    http.StatusForbidden,    // 403

	"PAYLOAD_TOO_LARGE": http.StatusRequestEntityTooLarge, // 413

	"VALIDATION_FAILED":          422,
	"IDEMPOTENCY_CONFLICT":       422,
	"FRONTEND_MUST_BE_STATIC":    422,
	"POD_LIMIT_EXCEEDED":         422,
	"REPLICAS_EXCEED_PLAN":       422,
	"SCALING_AMBIGUOUS":          422,
	"UNSUPPORTED_FRAMEWORK":      422, // (builds.go framework validation)

	"CELL_AT_CAPACITY":     http.StatusTooManyRequests, // 429
	"TENANT_BUILD_QUOTA":   http.StatusTooManyRequests, // 429

	"PLATFORM_DEGRADED":          http.StatusServiceUnavailable, // 503
	"DEPLOY_SERVICE_AUTH_FAILED": http.StatusServiceUnavailable, // 503

	"SOURCE_UPLOAD_FAILED": http.StatusInternalServerError, // 500
	"INTERNAL_ERROR":       http.StatusInternalServerError, // 500
}

// Status returns the HTTP status code for the given error.
//
// Lookup precedence (Stage C — HTTPStatus field removed):
//
//  1. The registered code in statusByCode (authoritative).
//  2. http.StatusInternalServerError (500) for non-LoopZeroError or
//     unregistered codes.
//
// Adding a new LoopZeroError code without registering its status here
// produces a silent 500 — pair every new sentinel with a registry
// entry. The IsRegistered() helper is suitable for CI lint to enforce
// this invariant.
func Status(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var loopzeroErr *loopzeroerrors.LoopZeroError
	if !stderrors.As(err, &loopzeroErr) {
		return http.StatusInternalServerError
	}
	if s, ok := statusByCode[loopzeroErr.Code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// IsRegistered reports whether the given code has an explicit HTTP
// status mapping. Useful for CI lint that asserts every LoopZeroError
// code has a registered transport status.
func IsRegistered(code string) bool {
	_, ok := statusByCode[code]
	return ok
}
