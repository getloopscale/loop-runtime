// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"strings"
	"testing"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/vault"
)

// Per-tenant Transit key provisioning is **api-side** — decided in
// `docs/wiki/decisions/2026-08-12-tenant-transit-key-ownership.md` (T3,
// partly ACCEPTED), item `B-042`.
//
// The evidence that decided it: the `api` path is wired and runs
// (`api/cmd/main.go:162` builds the `pkg/vault.TransitClient`, which creates
// the key lazily on the first encrypt 404, `transit_client.go:391`), and the
// operator path **never had a production implementer** — the only
// `EnsureTenantKey` in the tree were two test mocks, and
// `TenantReconciler.VaultClient` was assigned at no construction site. Ten
// weeks of silent no-op.
//
// These tests lock the decision both ways: what left cannot come back by
// accident, and what stayed cannot leave as tidying.

// The PROVISIONING seam left. An accidental reintroduction — someone
// "rewiring" the step believing the no-op was an oversight — recreates the
// divergence that made the operator's fail-fast meaningless: the operator
// names the key `tenant-<id>` and the `api` names it `loopzero-tenants-<id>`, so
// the operator would guarantee a key nobody uses.
func TestTransitKeyProvisioning_IsNotAnOperatorReconcileStep(t *testing.T) {
	r := &TenantReconciler{}
	for _, step := range r.provisioningSteps(context.Background(), &loopzerov1.Tenant{}) {
		name := step.condType
		if strings.Contains(name, "TransitKey") {
			t.Fatalf(
				"step %q is back in the tenant-controller reconcile list.\n\n"+
					"Provisioning is api-side (ADR 2026-08-12-tenant-transit-key-ownership, "+
					"B-042). The operator names the key `tenant-<id>` and the api names it "+
					"`loopzero-tenants-<id>` — a step here guarantees a key nobody uses.",
				name,
			)
		}
	}
}

// SHREDDING stayed, and stayed on purpose. `DeleteTenantKey` is the
// right-to-erasure primitive: deleting a tenant's key makes all of its
// ciphertext permanently unrecoverable. The ADR refused to delete it along
// with provisioning — its ownership depends on the tenant-deletion lifecycle,
// which the slice did not examine (`B-075`).
//
// Without this test, the next cleanup of "an interface with a single method"
// deletes the compliance primitive because its other half died.
func TestTransitShredding_SurvivesTheProvisioningRemoval(t *testing.T) {
	// A type implementing ONLY shredding must satisfy the interface. If
	// someone reintroduces EnsureTenantKey into the contract, this stops
	// compiling — exactly the signal wanted, at the moment it is wanted.
	var _ vault.TenantTransitClient = shreddingOnly{}
	_ = t
}

type shreddingOnly struct{}

func (shreddingOnly) DeleteTenantKey(context.Context, string) error { return nil }

// The trap the step removal created, which would only show in a deployment
// that turns the shredder on: `requiredProvisioningConditions` added
// `TransitKeyAvailable` whenever `VaultClient != nil`, and `VaultClient`
// **still exists** — it carries the half that stayed (`DeleteTenantKey`,
// `B-075`).
//
// With the step removed, nobody writes the condition. Requiring it would make
// every Tenant wait forever for a step that no longer runs — and the symptom
// would appear only where the shredder is on, which is the environment where
// nobody is watching the tenant controller.
//
// The assertion is about THIS condition, not about "every required condition
// has a step that writes it". The general form looks stronger and is false:
// `QuotaEnforced` is required and written by `tenant_build.go:54`, inside the
// step whose `condType` is `BuildNamespaceReady` — one step writes more than
// one condition, so `condType` is not the set of what it produces. Asserting
// the general form would fail for a reason that does not exist, and that is
// how a test teaches people to ignore it.
func TestTenantActivation_DoesNotRequireTheConditionWhoseWriterWasRemoved(t *testing.T) {
	withShredder := &TenantReconciler{VaultClient: shreddingOnly{}}

	for _, required := range withShredder.requiredProvisioningConditions() {
		if strings.Contains(required, "TransitKey") {
			t.Fatalf(
				"activation requires %q, and the only step that wrote it was removed on "+
					"2026-08-16 (provisioning is api-side, B-042).\n\n"+
					"Every Tenant in a deployment with the shredder on would stay in "+
					"Provisioning forever, waiting for a step that does not run.",
				required,
			)
		}
	}
}
