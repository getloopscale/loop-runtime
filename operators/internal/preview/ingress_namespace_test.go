// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package preview

import (
	"testing"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
)

// CHG-2026-04-29-DOGFOOD-FB — Helper tests for PreviewReconciler.ingressNS().
//
// Mirrors EnvironmentReconciler.ingressNS() tests in
// internal/tenant/ingress_namespace_test.go to keep the
// allow-ingress-traefik NetworkPolicy semantics consistent between regular
// and preview environments. drift-baseline.md port #3.

func TestPreviewReconciler_IngressNS_DefaultIsTraefikNamespace(t *testing.T) {
	r := &PreviewReconciler{}
	got := r.ingressNS()
	if got != constants.NamespaceIngress {
		t.Errorf("preview ingressNS default = %q, want %q", got, constants.NamespaceIngress)
	}
}

func TestPreviewReconciler_IngressNS_OverrideTakesPrecedence(t *testing.T) {
	r := &PreviewReconciler{IngressNamespace: "alt-traefik"}
	if got := r.ingressNS(); got != "alt-traefik" {
		t.Errorf("preview ingressNS with override = %q, want %q", got, "alt-traefik")
	}
}
