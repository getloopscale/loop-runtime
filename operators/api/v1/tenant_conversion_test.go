// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// TestTenantConversion_HubMarkerPresent (M14 gap #4): Tenant is marked as the
// storage/hub version via the Hub/Convertible pattern (blueprint Q2, kserve
// pkg/apis/serving/v1beta1/inference_service_conversion.go:19). The webhook path
// is dormant until a v2 spoke exists (plan ADR D5); the marker + harness ship
// now so the first breaking CRD change finds the path ready.
func TestTenantConversion_HubMarkerPresent(t *testing.T) {
	var _ conversion.Hub = &Tenant{}
}

// TestTenantConversion_RoundTripIdentity: until a v2 spoke exists, converting a
// Tenant through the hub round-trips to an equal object (the harness that a
// future spoke's ConvertTo/ConvertFrom will be validated against).
func TestTenantConversion_RoundTripIdentity(t *testing.T) {
	original := &Tenant{}
	original.Name = "tn-abc"
	original.Spec.DisplayName = "Acme"
	original.Spec.CellID = "cell-0"
	original.Spec.Environments = []TenantEnvironmentEntry{{Name: "production"}}

	got := roundTripThroughHub(original)
	if got.Spec.DisplayName != original.Spec.DisplayName ||
		got.Spec.CellID != original.Spec.CellID ||
		len(got.Spec.Environments) != len(original.Spec.Environments) {
		t.Errorf("round-trip changed the object: got %+v, want %+v", got.Spec, original.Spec)
	}
}

// TestTenantConversion_RoundTripWithNilOptionals (EC-7): zero-valued optionals
// (empty CellID, nil Environments) survive the round-trip as zero — not turned
// into non-nil. Mirrors kserve's NoCriticality conversion case
// (llm_inference_service_conversion_test.go:152).
func TestTenantConversion_RoundTripWithNilOptionals(t *testing.T) {
	original := &Tenant{}
	original.Name = "tn-empty"
	// CellID="" and Environments=nil deliberately

	got := roundTripThroughHub(original)
	if got.Spec.CellID != "" {
		t.Errorf("CellID = %q, want empty (zero-value must survive round-trip)", got.Spec.CellID)
	}
	if got.Spec.Environments != nil {
		t.Errorf("Environments = %v, want nil (zero-value must not become non-nil)", got.Spec.Environments)
	}
}
