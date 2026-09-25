// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package naming

import "testing"

func TestUserSecretNameCarriesTheAppSegmentWhenAnAppIsGiven(t *testing.T) {
	got := UserSecretName("tn-1", "shop", "production", "api")

	if want := "user-secrets-tn-1-shop-production-api"; got != want {
		t.Errorf("UserSecretName = %q, want %q", got, want)
	}
}

func TestUserSecretNameOmitsTheAppSegmentForATeamWideSecret(t *testing.T) {
	got := UserSecretName("tn-1", "shop", "production", "")

	if want := "user-secrets-tn-1-shop-production"; got != want {
		t.Errorf("UserSecretName = %q, want %q (no trailing hyphen)", got, want)
	}
}

func TestTenantGitOpsRepoNameIsTheTenantWithTheGitOpsSuffix(t *testing.T) {
	if got, want := TenantGitOpsRepoName("tn-1"), "tn-1-gitops"; got != want {
		t.Errorf("TenantGitOpsRepoName = %q, want %q", got, want)
	}
}
