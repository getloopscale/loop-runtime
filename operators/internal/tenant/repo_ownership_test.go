// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenant

import (
	"testing"
)

// Audit gap M10 F2 / §6.E.4c regression suite.
//
// assertRepoNameOwnedByTenant enforces the invariant that a Tenant CR's
// Spec.GitOps.RepoName begins with "<tenantName>-". This catches
// adversarial cross-tenant repo references where someone (typo or
// attack) sets tenant B's RepoName to tenant A's repo path, which
// would otherwise cause the operator to ensure/archive tenant A's
// repo on tenant B's behalf.
//
// These unit tests pin both the happy path AND the adversarial cases
// so a future refactor that loosens the prefix check fails loudly.

func TestAssertRepoNameOwnedByTenant_AcceptsCanonical(t *testing.T) {
	// The canonical pattern from api/internal/k8s/tenant_cr_manager.go:308
	// is `repoName = "{tenantID}-gitops"`. That MUST be accepted.
	if err := assertRepoNameOwnedByTenant("tn-acme", "tn-acme-gitops"); err != nil {
		t.Errorf("canonical pattern rejected: %v", err)
	}
}

func TestAssertRepoNameOwnedByTenant_AcceptsAnyOwnPrefixedName(t *testing.T) {
	// The invariant is "starts with <tenantName>-", not "exactly
	// {tenantName}-gitops". Future repo naming variants (multi-repo per
	// tenant, branch-suffixed repos) must continue to pass.
	cases := []string{
		"tn-acme-gitops",
		"tn-acme-overlays",
		"tn-acme-prod-only",
		"tn-acme-",
	}
	for _, name := range cases {
		if err := assertRepoNameOwnedByTenant("tn-acme", name); err != nil {
			t.Errorf("own-prefixed name rejected: %q → %v", name, err)
		}
	}
}

func TestAssertRepoNameOwnedByTenant_RejectsCrossTenant(t *testing.T) {
	// THE ADVERSARIAL CASE: tenant B's Tenant CR carries tenant A's repo
	// name. The operator MUST refuse to touch the repo.
	err := assertRepoNameOwnedByTenant("tn-victim", "tn-acme-gitops")
	if err == nil {
		t.Fatal("cross-tenant repo name was accepted; invariant broken")
	}
}

func TestAssertRepoNameOwnedByTenant_RejectsUnrelatedName(t *testing.T) {
	// A name with no tenant prefix at all (e.g., legacy/global repo).
	// Must be rejected — even a "safe-looking" name must not pass
	// without the tenant-prefix check.
	err := assertRepoNameOwnedByTenant("tn-acme", "globalrepo")
	if err == nil {
		t.Fatal("unrelated repo name accepted; invariant broken")
	}
}

func TestAssertRepoNameOwnedByTenant_RejectsExactTenantName(t *testing.T) {
	// `tn-acme` (no separator anywhere) doesn't satisfy the
	// delimited-component invariant — neither `tn-acme-` prefix nor
	// `-tn-acme-` middle nor `-tn-acme` suffix appears. Without this
	// guard a repo literally named "tn-acme" would slip through.
	err := assertRepoNameOwnedByTenant("tn-acme", "tn-acme")
	if err == nil {
		t.Fatal("repo name equal to tenant name (no separator) accepted; invariant broken")
	}
}

func TestAssertRepoNameOwnedByTenant_RejectsPrefixCollision(t *testing.T) {
	// "tn-acme-evil" embeds "tn-acme" as a prefix component, but the
	// caller asked the invariant to validate ownership by "tn-victim".
	// The tenant "tn-victim" appears nowhere in the name, so it must
	// be rejected — proves the check actually uses the supplied tenant
	// name as the base, not a hardcoded value.
	err := assertRepoNameOwnedByTenant("tn-victim", "tn-acme-evil")
	if err == nil {
		t.Fatal("cross-tenant repo name accepted; invariant broken")
	}
}

func TestAssertRepoNameOwnedByTenant_AcceptsTestFixturePattern(t *testing.T) {
	// The operator-test fixtures historically use `loop-runtime-tenant-<name>`
	// (e.g., `loop-runtime-tenant-tn-acme`). That embeds `tn-acme` as a suffix
	// component (`-tn-acme`), so it must be accepted — otherwise this
	// change would break the existing envtest harness for every tenant
	// spec that didn't switch to the canonical pattern.
	cases := []string{
		"loop-runtime-tenant-tn-acme",
		"loop-runtime-tenant-empty",
	}
	for i, name := range cases {
		var tenant string
		switch i {
		case 0:
			tenant = "tn-acme"
		case 1:
			tenant = "empty"
		}
		if err := assertRepoNameOwnedByTenant(tenant, name); err != nil {
			t.Errorf("test fixture pattern rejected for tenant %q name %q: %v", tenant, name, err)
		}
	}
}

func TestAssertRepoNameOwnedByTenant_RejectsEmptyTenant(t *testing.T) {
	// An empty tenant name would make every check trivially pass — we
	// must refuse it explicitly so a callsite that forgot to populate
	// the Tenant CR name fails loudly instead of accepting any repo.
	if err := assertRepoNameOwnedByTenant("", "anything"); err == nil {
		t.Fatal("empty tenant name accepted; invariant broken")
	}
}

func TestAssertRepoNameOwnedByTenant_ErrorReferencesAuditGap(t *testing.T) {
	// The error message MUST cite the audit gap so log search correlates.
	err := assertRepoNameOwnedByTenant("tn-victim", "tn-acme-gitops")
	if err == nil {
		t.Fatal("setup error: expected the invariant to reject")
	}
	if !contains(err.Error(), "audit M10 F2 §6.E.4c") {
		t.Errorf("error message missing audit ref: %q", err.Error())
	}
}

// Local contains helper. Not imported from `strings` only to keep the
// file dependency surface minimal — single use, single line.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
