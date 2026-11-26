// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environment

import (
	"testing"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
)

// CHG-2026-04-29-DOGFOOD-FB — Helper tests for ingressNS() (Environment side).
//
// Background: the operator-generated `allow-ingress-traefik` NetworkPolicy
// MUST allow traffic from the namespace where Traefik runs. LoopRuntime's helmfile
// installs Traefik in `traefik` for ALL environments. The previous default
// (`loop-runtime-system`) silently blocked Traefik from reaching tenant pods,
// surfacing as HTTP 502 in any deploy. Tests pin both the default and the
// env-var override path.
//
// Note: PreviewReconciler.ingressNS() tests live in
// internal/preview/ingress_namespace_test.go (split per ADR-0023 D1).

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
