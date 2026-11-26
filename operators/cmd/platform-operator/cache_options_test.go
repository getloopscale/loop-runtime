// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"slices"
	"testing"

	"github.com/getloopscale/loop-runtime/operators/operatortest"
)

// The chart grants this operator Secret access in two namespaces only, each
// through a Role: loopzero-system (kubeconfigs, platform tokens, Git credentials)
// and the ArgoCD namespace (the Secrets it writes for ArgoCD). The manager's
// Secret informer — which also feeds the ClusterCredential controller's
// Secret watch — must watch exactly those two.
func TestManagerCache_WatchesSecretsOnlyInThePlatformAndArgoCDNamespaces(t *testing.T) {
	opts, err := managerCacheOptions("argocd-custom")
	if err != nil {
		t.Fatalf("managerCacheOptions: %v", err)
	}
	want := []string{"argocd-custom", "loopzero-system"}
	if got := operatortest.SecretCacheNamespaces(opts); !slices.Equal(got, want) {
		t.Fatalf("Secret cache namespaces = %v, want %v", got, want)
	}
}
