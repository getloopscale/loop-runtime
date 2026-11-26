// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"slices"
	"testing"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/operatortest"
)

// The chart grants this operator Secret read in the platform namespace only,
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
