// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package operatorboot

import (
	"errors"
	"sort"
	"testing"
)

func TestSecretCache_WatchesExactlyTheGrantedNamespaces(t *testing.T) {
	entry, err := SecretCache("loop-runtime-system", "argocd")
	if err != nil {
		t.Fatalf("SecretCache: %v", err)
	}
	var got []string
	for ns := range entry.Namespaces {
		got = append(got, ns)
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "argocd" || got[1] != "loop-runtime-system" {
		t.Fatalf("Secret cache namespaces = %v, want [argocd loop-runtime-system]", got)
	}
}

// An entry with no namespace is how controller-runtime spells "every
// namespace": returning one would silently restore the cluster-wide Secret
// informer this function exists to prevent.
func TestSecretCache_RefusesAnEmptyNamespaceSet(t *testing.T) {
	if _, err := SecretCache(); !errors.Is(err, ErrSecretCacheUnscoped) {
		t.Fatalf("SecretCache() error = %v, want ErrSecretCacheUnscoped", err)
	}
}

// "" is cache.AllNamespaces — the same widening, spelled as a value.
func TestSecretCache_RefusesTheAllNamespacesValue(t *testing.T) {
	if _, err := SecretCache("loop-runtime-system", ""); !errors.Is(err, ErrSecretCacheUnscoped) {
		t.Fatalf(`SecretCache("loop-runtime-system", "") error = %v, want ErrSecretCacheUnscoped`, err)
	}
}
