// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

// M14 gap #4 — CRD conversion Hub/Convertible pattern (plan ADR D5, blueprint
// Q2). This file installs the pattern in a DORMANT state: the Hub() marker
// declares Tenant (v1) as the storage/hub version, and roundTripThroughHub is
// the harness a future v2 spoke's ConvertTo/ConvertFrom will be validated
// against.
//
// Why dormant, not a full webhook: the /convert webhook + the CRD
// spec.conversion patch (kserve config/crd/minimal/llmisvc/*_conversion_webhook_patch.yaml)
// are 100% coupled to a spoke version that does not exist yet — installing the
// webhook without a v2 would be dead config. What ships now is the marker + the
// testable harness, so the FIRST backward-incompatible Tenant CRD change finds
// the conversion path ready instead of orphaning existing CRs. The step-by-step
// procedure for activating the webhook lives in
// docs/runbooks.md.

// Hub marks Tenant (v1) as the conversion hub (storage) version, implementing
// sigs.k8s.io/controller-runtime/pkg/conversion.Hub. Marker only — no runtime
// effect until a spoke version exists.
func (*Tenant) Hub() {}

// roundTripThroughHub is the conversion round-trip harness. While Tenant is the
// only version, the round-trip is identity (deep-copy through the hub). When a
// v2 spoke lands, this harness is where its ConvertTo→ConvertFrom is asserted
// to preserve every field (the kserve PreservesCriticality/NoCriticality test
// pattern, blueprint Q4).
func roundTripThroughHub(in *Tenant) *Tenant {
	return in.DeepCopy()
}
