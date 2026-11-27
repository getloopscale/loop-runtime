// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import "testing"

// M9 Step 6: this test exercises the release context's unexported deriveDeployID
// helper; it was physically clustered in a build test file but belongs to release.

func TestDeriveDeployID_DeterministicFromReleaseID(t *testing.T) {
	id1 := deriveDeployID("rel-abc-123")
	id2 := deriveDeployID("rel-abc-123")
	if id1 != id2 {
		t.Errorf("expected deterministic deploy ID, got %q and %q", id1, id2)
	}
	if id1 == "" {
		t.Error("expected non-empty deploy ID")
	}
}
