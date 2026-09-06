// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"
)

// TestConstants_SyftScannerImageDefined ensures the syft scanner image
// constant is non-empty and pinned to a specific version (no :latest).
// Sprint 6 / T0.2 — paired with infra/images/syft-scanner/Dockerfile.
func TestConstants_SyftScannerImageDefined(t *testing.T) {
	if imageSyftScanner == "" {
		t.Fatal("imageSyftScanner constant must be defined")
	}
	if !strings.Contains(imageSyftScanner, "loopzero-syft-scanner") {
		t.Errorf("imageSyftScanner should reference loopzero-syft-scanner, got %q", imageSyftScanner)
	}
	if strings.HasSuffix(imageSyftScanner, ":latest") {
		t.Errorf("imageSyftScanner must not use :latest tag, got %q", imageSyftScanner)
	}
	if !strings.Contains(imageSyftScanner, ":v") {
		t.Errorf("imageSyftScanner should be pinned to a version (v0.x.y), got %q", imageSyftScanner)
	}
}

// TestConstants_CosignSignerImageDefined documents the existing cosign
// signer image is also pinned (regression guard against accidental :latest).
func TestConstants_CosignSignerImageDefined(t *testing.T) {
	if imageCosignSigner == "" {
		t.Fatal("imageCosignSigner constant must be defined")
	}
	if strings.HasSuffix(imageCosignSigner, ":latest") {
		t.Errorf("imageCosignSigner must not use :latest tag, got %q", imageCosignSigner)
	}
}
