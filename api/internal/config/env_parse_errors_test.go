// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"strings"
	"testing"
)

// A numeric or duration variable the API cannot parse must stop startup,
// naming the key and the value — never run with a default the operator did
// not choose.
func TestValidateRejectsAnUnparseableIntegerVariable(t *testing.T) {
	t.Setenv("MAX_TENANTS_PER_CELL", "5k")

	err := Load().Validate()

	if err == nil || !strings.Contains(err.Error(), "MAX_TENANTS_PER_CELL") || !strings.Contains(err.Error(), "5k") {
		t.Fatalf("Validate() = %v, want an error naming MAX_TENANTS_PER_CELL=\"5k\"", err)
	}
}

func TestValidateRejectsADurationWithoutAUnit(t *testing.T) {
	t.Setenv("ARGOCD_SYNC_TIMEOUT", "300")

	err := Load().Validate()

	if err == nil || !strings.Contains(err.Error(), "ARGOCD_SYNC_TIMEOUT") {
		t.Fatalf("Validate() = %v, want an error naming ARGOCD_SYNC_TIMEOUT", err)
	}
}
