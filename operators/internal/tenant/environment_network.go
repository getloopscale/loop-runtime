// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"fmt"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/shared"
)

// reconcileNetworkPolicies ensures deny-all + allow-ingress + allow-egress policies exist.
// fix-all-platform-issues-plan T1.1 — cell-aware via runtimeClientForEnv.
//
// Phase 3 v37: delegates to shared.ApplyTenantNetworkPolicies which collapses
// the 3 reconcile*Policy functions (previously ~100 lines here) into a
// single cross-package helper shared with the Preview controller.
func (r *EnvironmentReconciler) reconcileNetworkPolicies(ctx context.Context, env *loopzerov1.Environment, nsName string) error {
	ctx, span := shared.StartChildSpan(ctx, "environment.network_policies.ensure")
	defer span.End()
	runtimeClient, err := r.runtimeClientForEnv(ctx, env)
	if err != nil {
		return fmt.Errorf("network policies: cell-aware client: %w", err)
	}
	return shared.ApplyTenantNetworkPolicies(ctx, runtimeClient, nsName, r.ingressNS(), "networkpolicy")
}

// ingressNS returns the namespace where the ingress controller runs.
// CHG-2026-04-29-DOGFOOD-FB / drift-baseline.md port #4: defaults to
// constants.NamespaceIngress ("traefik") so allow-ingress-traefik
// NetworkPolicies admit traffic from the actual Traefik namespace, not
// loopzero-system. Previously this returned NamespacePlatformSystem which
// silently produced HTTP 502 in every deploy because Traefik couldn't
// reach tenant pods. Override via EnvironmentReconciler.IngressNamespace
// for operators that install Traefik elsewhere.
func (r *EnvironmentReconciler) ingressNS() string {
	if r.IngressNamespace != "" {
		return r.IngressNamespace
	}
	return constants.NamespaceIngress
}
