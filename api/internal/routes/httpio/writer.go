// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package httpio holds the HTTP transport helpers every route handler needs:
// serialising a success body, and serialising a typed domain error.
//
// # Why it exists as a package
//
// These three functions used to live in `routes/projects.go` — a DOMAIN file —
// and were called from 27 and 24 files respectively — 399 call sites across 29 files.
// (The plan first said 402; that counted the three declarations as calls.)
// Measured across the 41 unexported functions declared in the 43 route files,
// exactly two are referenced by more than three files, and both were these.
//
// That placement was the concrete blocker on splitting `routes` into per-family
// subpackages (B-041): every future subpackage would have had to depend on the
// `projects` subpackage to render a response — which is precisely the edge the
// split exists to remove. Moving them here converts that edge into a leaf
// dependency every family may take without depending on a sibling.
//
// # Contract
//
// The bodies were moved byte for byte. Nothing about the rendered status, body
// or headers changed, and the tests in this package assert that shape directly
// rather than trusting the move.
package httpio

import (
	"encoding/json"
	"net/http"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	errorshttp "github.com/getloopscale/loopzero/pkg/errors/http"
)

// WriteJSON writes data as a JSON body with the given status.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// WriteLoopZeroError writes a LoopZeroError as a JSON response with the correct HTTP status.
//
// Per ADR-0030 + T2.3 closure: emits RFC 9457 problem+json shape via
// errorshttp.WriteJSON. The CLI reads `body.code` (preserved across
// formats) so backward compat is maintained for clients that only
// inspect the code field.
//
// The legacy ToJSON shape is no longer emitted from production
// handlers; pkg/errors.LoopZeroError.ToJSON remains for audit-log /
// internal-tool callers that don't need RFC 9457 transport semantics.
func WriteLoopZeroError(w http.ResponseWriter, err *loopzeroerrors.LoopZeroError) {
	// Pass nil for *http.Request — instance field is omitted by
	// WriteJSON when r is nil. Future refactor can thread r through
	// the call sites to populate instance with r.URL.Path.
	errorshttp.WriteJSON(w, nil, err)
}

// WriteLoopZeroErrorWithFieldErrors is the multi-field variant of WriteLoopZeroError.
// Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T2.3 closes loopzero-cloud#29:
// when a validator surfaces an aggregated list of field violations
// (jsonschema Causes walk), this helper emits them in the response body's
// `errors[]` extension member (RFC 9457 § 3.2) without changing the legacy
// scalar `detail` field. Pass nil/empty for the single-error case.
func WriteLoopZeroErrorWithFieldErrors(w http.ResponseWriter, err *loopzeroerrors.LoopZeroError, fieldErrors []errorshttp.FieldError) {
	errorshttp.WriteJSONWithErrors(w, nil, err, fieldErrors)
}
