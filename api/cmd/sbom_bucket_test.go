// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/config"
)

// The build uploads SBOMs to STATIC_ASSETS_BUCKET. The download link was presigned
// in SOURCE_BUCKET, where the object never exists, so every download answered 404.
func TestSBOMDownloadPresignsInTheStaticAssetsBucket(t *testing.T) {
	cfg := config.Config{SourceBucket: "example-builds", StaticAssetsBucket: "example-static-assets"}

	if got := sbomBucket(cfg); got != "example-static-assets" {
		t.Errorf("sbomBucket = %q, want the static assets bucket", got)
	}
}

func TestSBOMDownloadUsesTheBuildDefaultBucketWhenNoneIsConfigured(t *testing.T) {
	cfg := config.Config{SourceBucket: "example-builds"}

	if got := sbomBucket(cfg); got != "loopruntime-static-assets" {
		t.Errorf("sbomBucket = %q, want the bucket the build uploads to by default", got)
	}
}
