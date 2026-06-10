// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C22.
// Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
//
// Pins that .github/workflows/build-images.yml has the 4 split-operator
// jobs wired. If a future refactor drops one (e.g., consolidating back
// to a single build), this test catches it before merge.

package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readBuildImagesYaml(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	wfPath := filepath.Join(
		filepath.Dir(thisFile),
		"..", "..", "..", ".github", "workflows", "build-images.yml",
	)
	data, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("build-images.yml not found: %v", err)
	}
	return string(data)
}

func TestBuildImages_HasFourSplitOperatorJobs(t *testing.T) {
	yaml := readBuildImagesYaml(t)

	jobs := []string{
		"build-tenant-operator",
		"build-app-operator",
		"build-preview-operator",
		"build-platform-operator",
	}
	for _, job := range jobs {
		if !strings.Contains(yaml, job+":") {
			t.Errorf("build-images.yml missing job: %s", job)
		}
	}
}

func TestBuildImages_HasFourSplitDockerfiles(t *testing.T) {
	yaml := readBuildImagesYaml(t)

	for _, df := range []string{
		"operators/Dockerfile.tenant-operator",
		"operators/Dockerfile.app-operator",
		"operators/Dockerfile.preview-operator",
		"operators/Dockerfile.platform-operator",
	} {
		if !strings.Contains(yaml, df) {
			t.Errorf("build-images.yml missing Dockerfile reference: %s", df)
		}
	}
}

func TestBuildImages_DockerfilesExist(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")

	for _, df := range []string{
		"operators/Dockerfile.tenant-operator",
		"operators/Dockerfile.app-operator",
		"operators/Dockerfile.preview-operator",
		"operators/Dockerfile.platform-operator",
	} {
		path := filepath.Join(repoRoot, df)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("Dockerfile missing: %s (%v)", df, err)
		}
	}
}

func TestBuildImages_ResolveOutputsHaveSplitFlags(t *testing.T) {
	yaml := readBuildImagesYaml(t)

	for _, flag := range []string{
		"build_tenant_operator:",
		"build_app_operator:",
		"build_preview_operator:",
		"build_platform_operator:",
	} {
		if !strings.Contains(yaml, flag) {
			t.Errorf("resolve job missing output flag: %s", flag)
		}
	}
}
