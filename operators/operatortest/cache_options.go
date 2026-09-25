// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package operatortest

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

// SecretCacheNamespaces returns, sorted, the namespaces opts confines the
// Secret informer to. nil means opts carries no Secret entry, which
// controller-runtime reads as "watch Secrets in every namespace".
func SecretCacheNamespaces(opts cache.Options) []string {
	for obj, entry := range opts.ByObject {
		if _, ok := obj.(*corev1.Secret); !ok {
			continue
		}
		namespaces := make([]string, 0, len(entry.Namespaces))
		for ns := range entry.Namespaces {
			namespaces = append(namespaces, ns)
		}
		sort.Strings(namespaces)
		return namespaces
	}
	return nil
}
