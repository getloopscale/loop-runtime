// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartpairs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// This gate audits exactly the pairs the registry declares — the list the
// chart-pairs gate keeps honest against disk. Hermetic: no helm.
func TestAuditedPairsAreTheDeclaredRegistry(t *testing.T) {
	root := qtestutil.Root(t)
	pairs, err := discoverPairs(root)
	if err != nil {
		t.Fatalf("discoverPairs: %v", err)
	}
	declared, err := chartpairs.LoadRegistry(root)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(pairs) == 0 || len(pairs) != len(declared) {
		t.Fatalf("audited %d pair(s), registry declares %d", len(pairs), len(declared))
	}
	for i, d := range declared {
		if pairs[i].bootstrap != d.Bootstrap || pairs[i].published != d.Published {
			t.Errorf("pair %d: audited %+v, declared %+v", i, pairs[i], d)
		}
	}
}
