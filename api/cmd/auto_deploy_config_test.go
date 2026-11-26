// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/config"
)

// A push deploy uploads the repository's source bundle to the same bucket a CLI
// upload does. Without SourceBucket the auto-deployer fell back to its default,
// "loopzero-builds", which a deployment naming another bucket (staging names
// loopzero-staging-builds-000000000000) does not have, and every push failed
// with "The specified bucket does not exist".
func TestAutoDeployUploadsToTheConfiguredSourceBucket(t *testing.T) {
	cfg := config.Config{SourceBucket: "tenant-sources-example"}

	got := autoDeployConfig(cfg, nil, nil, nil, nil, nil)

	if got.SourceBucket != "tenant-sources-example" {
		t.Fatalf("SourceBucket = %q, want the configured %q", got.SourceBucket, cfg.SourceBucket)
	}
}
