// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"encoding/json"
	"strings"
	"testing"
)

// The signer and scanner images come from the API's configuration
// (InfraParams), not from the process environment read deep in a template.
func TestWorkflowUsesTheConfiguredSignerAndScannerImages(t *testing.T) {
	// Arrange
	infra := testInfraParams()
	infra.CosignImage = "reg.local/loopzero-cosign-signer:test"
	infra.SyftImage = "reg.local/loopzero-syft-scanner:test"
	infra.CosignAttestEnabled = true

	// Act
	wf, err := NewWorkflowBuilder(infra).Build(testSpec())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	raw, err := json.Marshal(wf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Assert
	for _, img := range []string{infra.CosignImage, infra.SyftImage} {
		if !strings.Contains(string(raw), img) {
			t.Errorf("workflow does not use configured image %q", img)
		}
	}
	if strings.Contains(string(raw), imageCosignSigner) || strings.Contains(string(raw), imageSyftScanner) {
		t.Error("workflow still uses a built-in default image although one was configured")
	}
}

func TestWorkflowFallsBackToTheBuiltInSignerImages(t *testing.T) {
	wf, err := NewWorkflowBuilder(testInfraParams()).Build(testSpec())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	raw, _ := json.Marshal(wf)
	if !strings.Contains(string(raw), imageCosignSigner) || !strings.Contains(string(raw), imageSyftScanner) {
		t.Error("workflow does not use the built-in images when none is configured")
	}
}
