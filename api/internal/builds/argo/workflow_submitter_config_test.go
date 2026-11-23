// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import "testing"

func TestWorkflowSubmitterConfigHandsTheObjectStoreToTheBuilder(t *testing.T) {
	got := WorkflowSubmitterConfig{ObjectStoreIRSA: true, StaticAssetsBucket: "assets"}.infraParams()
	if !got.ObjectStoreIRSA || got.StaticAssetsBucket != "assets" {
		t.Errorf("infraParams() ObjectStoreIRSA=%v StaticAssetsBucket=%q, want true and assets", got.ObjectStoreIRSA, got.StaticAssetsBucket)
	}
}
