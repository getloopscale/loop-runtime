// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package httpio holds the HTTP transport helpers every route handler needs:
// serialising a success body, and serialising a typed domain error.
//
// # Why it exists as a package
//
// Three of these four used to live in `routes/projects.go` — a DOMAIN file —
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

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	errorshttp "github.com/getloopscale/loop-runtime/pkg/errors/http"
)

// WriteJSON writes data as a JSON body with the given status.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// WriteLoopRuntimeError writes a LoopRuntimeError as a JSON response with the correct HTTP status.
//
// Per ADR-0030 + T2.3 closure: emits RFC 9457 problem+json shape via
// errorshttp.WriteJSON. The CLI reads `body.code` (preserved across
// formats) so backward compat is maintained for clients that only
// inspect the code field.
//
// The legacy ToJSON shape is no longer emitted from production
// handlers; pkg/errors.LoopRuntimeError.ToJSON remains for audit-log /
// internal-tool callers that don't need RFC 9457 transport semantics.
func WriteLoopRuntimeError(w http.ResponseWriter, err *loopruntimeerrors.LoopRuntimeError) {
	// Pass nil for *http.Request — instance field is omitted by
	// WriteJSON when r is nil. Future refactor can thread r through
	// the call sites to populate instance with r.URL.Path.
	errorshttp.WriteJSON(w, nil, err)
}

// WriteLoopRuntimeErrorWithFieldErrors is the multi-field variant of WriteLoopRuntimeError.
// Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T2.3 closes loop-console#29:
// when a validator surfaces an aggregated list of field violations
// (jsonschema Causes walk), this helper emits them in the response body's
// `errors[]` extension member (RFC 9457 § 3.2) without changing the legacy
// scalar `detail` field. Pass nil/empty for the single-error case.
func WriteLoopRuntimeErrorWithFieldErrors(w http.ResponseWriter, err *loopruntimeerrors.LoopRuntimeError, fieldErrors []errorshttp.FieldError) {
	errorshttp.WriteJSONWithErrors(w, nil, err, fieldErrors)
}

// WriteErrorJSON writes a JSON error body with a caller-supplied code and
// message, in the {"error","message"} shape.
//
// It is NOT the same as WriteLoopRuntimeError: that one renders a typed domain error as
// RFC 9457 problem+json and preserves `body.code`, which the CLI reads and
// Unbreakable Rule 5 makes a stability contract. This one is the older ad-hoc
// shape, kept because two handlers already emit it and changing what they emit
// is a contract change, not a refactor.
//
// It lived as an unexported helper inside `tenants.go` — a domain file — and was
// called from `billing.go`. slice-5 moved three response writers out of a domain
// file for exactly that reason and this one was missed: it is used by two files,
// and the item's measurement counted only helpers used by MORE than three. The
// split of the tenants family is what surfaced it, because the compiler refused
// to let billing keep reaching into another family.
func WriteErrorJSON(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}

// WriteCodeErrorJSON emits `{"code": …, "message": …}`.
//
// It is NOT WriteErrorJSON with a different name. The two differ in one byte
// sequence that matters: this one keys the machine-readable field `code`, the
// other keys it `error`. Merging them would silently change the response body of
// every endpoint that uses this one — and `--json` output is a stability contract
// (CLAUDE.md Rule 5), so a key rename is a breaking change wearing the costume of
// a deduplication.
//
// It moved here from the audit family when that family was extracted (B-080).
// Two files of the parent package reached it, which is what the compiler said the
// moment the family left: a helper that serves the whole package does not belong
// to one of its members.
func WriteCodeErrorJSON(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}
