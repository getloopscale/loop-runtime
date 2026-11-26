// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package contract

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// M1.4 T1.1 — engine.yaml exists, is valid OpenAPI 3.0, and loads.
func TestEngineSpec_LoadsAndValidates(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("../../openapi/engine.yaml")
	if err != nil {
		t.Fatalf("load engine.yaml: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("engine.yaml invalid OpenAPI 3.0: %v", err)
	}
}
