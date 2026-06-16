// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import "fmt"

// ValidationError is the typed error returned by the source validators
// (services.json native validation in validator_services.go +
// validator_env.go). Field carries the offending wire field; Message is a
// product-language explanation safe to surface to the user.
//
// The legacy loopzero.yaml parser (ValidateLoopZeroYAML) + its LoopZeroYAML/AppConfig
// structs were removed by simplify-deploy-loopzerokit-only (review F-xval-1):
// services.json is the single ingestion contract (*ServicesManifest), so the
// loopzero.yaml types had zero production callers.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}
