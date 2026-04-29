// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"testing"

	"github.com/loopzero/loopzero/operators/internal/constants"
)

// CHG-2026-04-29-DOGFOOD-FB — Helper tests for ingressNS().
//
// Background: the operator-generated `allow-ingress-traefik` NetworkPolicy
// MUST allow traffic from the namespace where Traefik runs. LoopZero's helmfile
// installs Traefik in `traefik` for ALL environments. The previous default
// (`loopzero-system`) silently blocked Traefik from reaching tenant pods,
// surfacing as HTTP 502 in any deploy. Tests pin both the default and the
// env-var override path.

func TestEnvironmentReconciler_IngressNS_DefaultIsTraefikNamespace(t *testing.T) {
	r := &EnvironmentReconciler{}
	got := r.ingressNS()
	if got != constants.NamespaceIngress {
		t.Errorf("ingressNS default = %q, want %q (constants.NamespaceIngress)", got, constants.NamespaceIngress)
	}
	if got != "traefik" {
		t.Errorf("ingressNS default = %q, want %q (literal — guards against silent constant rename)", got, "traefik")
	}
}

func TestEnvironmentReconciler_IngressNS_OverrideTakesPrecedence(t *testing.T) {
	r := &EnvironmentReconciler{IngressNamespace: "custom-traefik"}
	if got := r.ingressNS(); got != "custom-traefik" {
		t.Errorf("ingressNS with override = %q, want %q", got, "custom-traefik")
	}
}

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

// Guard: the constant must remain `traefik` because the helmfile
// (`infra/helmfile/{mgmt,runtime}.helmfile.yaml.gotmpl`) installs Traefik
// in this namespace. Changing this constant without updating the helmfile
// in lockstep produces silent HTTP 502 in every environment.
func TestNamespaceIngress_ConstantValue(t *testing.T) {
	if constants.NamespaceIngress != "traefik" {
		t.Fatalf("constants.NamespaceIngress = %q, want %q — must match helmfile install namespace",
			constants.NamespaceIngress, "traefik")
	}
}
