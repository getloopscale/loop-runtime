// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package operatorboot

import (
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/cache"
)

// ErrSecretCacheUnscoped is returned for a Secret cache entry that would watch
// every namespace.
var ErrSecretCacheUnscoped = errors.New("secret cache must name every namespace it watches")

// SecretCache returns the manager cache entry for corev1.Secret that lists and
// watches Secrets only in namespaces.
//
// A manager reads through an informer cache, and controller-runtime's default
// informer for a type watches every namespace. For Secrets that forces the
// operator's RBAC to hold list/watch on every Secret in the cluster — the
// grant the operator charts no longer make. Each chart binds Secret access
// with a namespaced Role, so the cache must watch exactly the namespaces those
// Roles cover, and every binary scopes its Secret cache through this function.
//
// An empty set, or the empty string (cache.AllNamespaces), is how
// controller-runtime spells "every namespace"; both are refused rather than
// silently restoring the cluster-wide informer.
func SecretCache(namespaces ...string) (cache.ByObject, error) {
	if len(namespaces) == 0 {
		return cache.ByObject{}, ErrSecretCacheUnscoped
	}
	scoped := make(map[string]cache.Config, len(namespaces))
	for _, ns := range namespaces {
		if ns == cache.AllNamespaces {
			return cache.ByObject{}, fmt.Errorf("%w: got the all-namespaces value in %q", ErrSecretCacheUnscoped, namespaces)
		}
		scoped[ns] = cache.Config{}
	}
	return cache.ByObject{Namespaces: scoped}, nil
}
