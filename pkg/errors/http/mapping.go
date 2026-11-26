// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package http maps LoopRuntime domain errors (pkg/errors.LoopRuntimeError) to HTTP
// transport-level concerns: status codes and RFC 9457 problem+json
// payloads.
//
// This sub-package exists per ADR-0030 + gate PKG-G15: pkg/errors
// itself stays transport-agnostic, while consumers that need HTTP
// semantics import from pkg/errors/http. CLI, operators, and audit
// code do not import this — they use pkg/errors directly with code
// strings and messages.
//
// The registry below holds the status of every code pkg/errors defines (a
// test reads errors.go and fails on an unregistered one). A code owned by
// another module is registered by its owner with Register, beside the code,
// so adding one never requires editing this shared package. LoopRuntimeError itself
// carries no HTTP status (ADR-0030, T2.3 Stage C).
package http

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"sync"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// registry is the code → status table: the pkg/errors codes below plus the
// codes other modules Register. Reads happen per request; writes happen at
// program start.
var (
	registryMu   sync.RWMutex
	statusByCode = defaultStatusByCode()
)

// Register maps an error code owned outside pkg/errors to its HTTP status.
// Registering the same status again is a no-op; re-mapping a registered code
// to a different status is refused, because two owners would then disagree on
// what the client sees. status must be a 4xx or 5xx.
func Register(code string, status int) error {
	if code == "" {
		return stderrors.New("errors/http: Register needs a code")
	}
	if status < http.StatusBadRequest || status > 599 {
		return fmt.Errorf("errors/http: code %s: status %d is not a 4xx or 5xx", code, status)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if existing, ok := statusByCode[code]; ok && existing != status {
		return fmt.Errorf("errors/http: code %s is already registered as %d, refusing %d", code, existing, status)
	}
	statusByCode[code] = status
	return nil
}

// MustRegister is Register for package initialisation: a conflicting or
// invalid registration is a programming error and panics.
func MustRegister(code string, status int) {
	if err := Register(code, status); err != nil {
		panic(err)
	}
}

// defaultStatusByCode is the status of every code pkg/errors defines.
func defaultStatusByCode() map[string]int {
	return map[string]int{
		"LOCK_TIMEOUT":               http.StatusConflict, // 409
		"INVALID_TRANSITION":         http.StatusConflict, // 409
		"DEPLOY_IN_PROGRESS":         http.StatusConflict, // 409
		"BUILD_ALREADY_COMPLETED":    http.StatusConflict, // 409
		"PROJECT_EXISTS":             http.StatusConflict, // 409
		"SYNC_OPERATION_IN_PROGRESS": http.StatusConflict, // 409
		"CONCURRENT_ACTIVATION":      http.StatusConflict, // 409 — partial unique index idx_releases_active_unique (migration 000022)

		"TENANT_NOT_FOUND":      http.StatusNotFound, // 404
		"PROJECT_NOT_FOUND":     http.StatusNotFound, // 404
		"BUILD_NOT_FOUND":       http.StatusNotFound, // 404
		"RELEASE_NOT_FOUND":     http.StatusNotFound, // 404
		"PREVIEW_NOT_FOUND":     http.StatusNotFound, // 404
		"NO_ACTIVE_RELEASE":     http.StatusNotFound, // 404
		"NO_PREVIOUS_RELEASE":   http.StatusNotFound, // 404
		"DOMAIN_NOT_FOUND":      http.StatusNotFound, // 404
		"REPOSITORY_NOT_LINKED": http.StatusNotFound, // 404
		"ENVIRONMENT_NOT_FOUND": http.StatusNotFound, // 404

		"INVALID_NAME":              http.StatusBadRequest,      // 400
		"INVALID_REQUEST":           http.StatusBadRequest,      // 400
		"MISSING_FIELD":             http.StatusBadRequest,      // 400
		"SAME_ENVIRONMENT":          http.StatusBadRequest,      // 400
		"INVALID_TENANT_OR_PROJECT": http.StatusBadRequest,      // 400 (Plan v1.2 T1.2 — DNS-1123 validation)
		"HPA_TARGET_OUT_OF_BOUNDS":  http.StatusBadRequest,      // 400 (J-20 Scale plan v1.1 T1.3 + EC-4)
		"PLAN_UPGRADE_REQUIRED":     http.StatusPaymentRequired, // 402 (J-20 Scale plan v1.1 T1.2 + EC-3 — KEDA scale-to-zero requires Pro+)

		"UNAUTHORIZED": http.StatusUnauthorized, // 401
		"FORBIDDEN":    http.StatusForbidden,    // 403

		"PAYLOAD_TOO_LARGE": http.StatusRequestEntityTooLarge, // 413

		"VALIDATION_FAILED":       422,
		"IDEMPOTENCY_CONFLICT":    422,
		"FRONTEND_MUST_BE_STATIC": 422,
		"POD_LIMIT_EXCEEDED":      422,
		"REPLICAS_EXCEED_PLAN":    422,
		"PROJECTS_EXCEED_PLAN":    422,
		"SCALING_AMBIGUOUS":       422,

		"CELL_AT_CAPACITY":   http.StatusTooManyRequests, // 429
		"TENANT_BUILD_QUOTA": http.StatusTooManyRequests, // 429

		"PLATFORM_DEGRADED":          http.StatusServiceUnavailable, // 503
		"DEPLOY_SERVICE_AUTH_FAILED": http.StatusServiceUnavailable, // 503
		// #138 — the encryption backend a secret write depends on is unreachable.
		// 503, not 500: the request was VALID and a dependency was down; a 500 would
		// blame the caller's input for an infrastructure fault, and clients that
		// retry on 5xx cannot tell the two apart.
		"SECRETS_BACKEND_UNAVAILABLE": http.StatusServiceUnavailable, // 503

		"SOURCE_UPLOAD_FAILED": http.StatusInternalServerError, // 500
		"BUILD_SUBMIT_FAILED":  http.StatusInternalServerError, // 500 — the build could not be handed to the build plane
		"INTERNAL_ERROR":       http.StatusInternalServerError, // 500
	}
}

// Status returns the HTTP status code for the given error.
//
// Lookup precedence (Stage C — HTTPStatus field removed):
//
//  1. The registered code in statusByCode (authoritative).
//  2. http.StatusInternalServerError (500) for non-LoopRuntimeError or
//     unregistered codes.
//
// A code nobody registered answers 500. pkg/errors codes are held
// registered by TestEveryPkgErrorsCodeIsRegistered; a code owned elsewhere
// is registered by its owner (see Register).
func Status(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var loopruntimeErr *loopruntimeerrors.LoopRuntimeError
	if !stderrors.As(err, &loopruntimeErr) {
		return http.StatusInternalServerError
	}
	registryMu.RLock()
	s, ok := statusByCode[loopruntimeErr.Code]
	registryMu.RUnlock()
	if ok {
		return s
	}
	return http.StatusInternalServerError
}

// IsRegistered reports whether the given code has an explicit HTTP
// status mapping. Useful for CI lint that asserts every LoopRuntimeError
// code has a registered transport status.
func IsRegistered(code string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := statusByCode[code]
	return ok
}
