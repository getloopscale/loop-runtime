// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package source

import "fmt"

// ValidationError is the typed error returned by the source validators
// (services.json native validation in validator_services.go +
// validator_env.go). Field carries the offending wire field; Message is a
// product-language explanation safe to surface to the user.
//
// The legacy loopzero.yaml parser (ValidateLoopRuntimeYAML) + its LoopRuntimeYAML/AppConfig
// structs were removed by simplify-deploy-loopzerokit-only (review F-xval-1):
// services.json is the single ingestion contract (*ServicesManifest), so the
// loopzero.yaml types had zero production callers.
//
// Sprint 4 (qa-sprint4-cleanup-2026-06-25 T2.2, closes loopruntime-cloud#29 multi-field
// error aggregation): the Errors slice now carries ALL field-level violations
// from the jsonschema Causes tree, not just the first one. Legacy Field/Message
// remain for backward-compat (they equal Errors[0].Field / Errors[0].Message via
// NewValidationError). EC-2 absorption: every legacy construction site is
// migrated to NewValidationError so len(Errors) >= 1 always holds.
type ValidationError struct {
	Field   string       `json:"field"`
	Message string       `json:"message"`
	Errors  []FieldError `json:"errors,omitempty"`
}

// FieldError is one field-level violation in the aggregated Errors slice.
// Sprint 4 T2.2: introduced so SourceHandler.Upload can surface ALL schema
// violations in the 400 response body's `errors[]` extension member (RFC 9457
// § 3.2 extension member; the field name `errors` avoids collision with the
// canonical scalar `detail` field — see pkg/errors/http/writer.go).
type FieldError struct {
	Field    string `json:"field"`
	Message  string `json:"message"`
	Expected string `json:"expected,omitempty"`
	Got      string `json:"got,omitempty"`
}

// NewValidationError constructs a *ValidationError with the legacy Field+Message
// scalars AND a single-element Errors slice mirroring them. EC-2 absorption:
// every pre-Sprint-4 `&ValidationError{Field:…, Message:…}` literal site is
// migrated to this constructor so downstream consumers that switched from the
// scalars to ve.Errors[0] always get at least one entry.
func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
		Errors:  []FieldError{{Field: field, Message: message}},
	}
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}
