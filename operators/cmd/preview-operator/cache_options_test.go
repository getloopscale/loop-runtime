// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"slices"
	"testing"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/operatortest"
)

// The preview-operator reads one kind of Secret on this cluster: a cell's
// kubeconfig, in the platform namespace. The chart grants exactly that
// through a Role. The manager's Secret informer must watch exactly that
// namespace, or it needs a cluster-wide list the operator does not hold.
func TestManagerCache_WatchesSecretsOnlyInThePlatformNamespace(t *testing.T) {
	opts, err := managerCacheOptions()
	if err != nil {
		t.Fatalf("managerCacheOptions: %v", err)
	}
	want := []string{constants.NamespacePlatformSystem}
	if got := operatortest.SecretCacheNamespaces(opts); !slices.Equal(got, want) {
		t.Fatalf("Secret cache namespaces = %v, want %v", got, want)
	}
}
