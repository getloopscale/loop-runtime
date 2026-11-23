// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package backgroundloops

import (
	"testing"

	"github.com/getloopscale/loopzero/api/internal/config"
)

// A build's object store settings come from the API's own: with an IRSA role and no
// static key the API signs S3 as AWS, and so do the build steps; the static assets
// bucket is the one the environment named.
func TestWorkflowSubmitterConfigCarriesTheObjectStore(t *testing.T) {
	got := workflowSubmitterConfig(config.Config{
		ObjectStoreAWSCredentials: true,
		StaticAssetsBucket:        "loopzero-staging-static-assets-000000000000",
	})
	if !got.ObjectStoreIRSA {
		t.Error("ObjectStoreIRSA = false, want true for an API that signs S3 with its IRSA role")
	}
	if got.StaticAssetsBucket != "loopzero-staging-static-assets-000000000000" {
		t.Errorf("StaticAssetsBucket = %q, want the configured bucket", got.StaticAssetsBucket)
	}
}
