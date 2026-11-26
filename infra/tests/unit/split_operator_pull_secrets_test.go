// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C12.
// Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
// Source: docs/decisions/operator-split-invariants.md (I-6)
//
// Pins ImagePullSecret consistency across 4 split-operator charts × 4
// environment overlays. Drift between charts in same env (chart A
// configures secret X, chart B configures secret Y) silently breaks
// deployment.

package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func chartsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "charts")
}

var splitOperators = []string{
	"loop-runtime-tenant-operator",
	"loop-runtime-app-operator",
	"loop-runtime-preview-operator",
	"loop-runtime-platform-operator",
}

func TestSplitOperators_HaveProdValuesOverlay(t *testing.T) {
	for _, op := range splitOperators {
		path := filepath.Join(chartsDir(), op, "values-prod.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: values-prod.yaml missing — required by I-6 (C12)", op)
			continue
		}
		// Each prod overlay MUST set imagePullSecrets to the canonical
		// loop-runtime-registry-pull (DOCR auth provisioned by Pulumi L1).
		if !strings.Contains(string(data), "name: loop-runtime-registry-pull") {
			t.Errorf("%s/values-prod.yaml missing 'name: loop-runtime-registry-pull'", op)
		}
	}
}

func TestSplitOperators_HaveStandaloneMultiOverlay(t *testing.T) {
	for _, op := range splitOperators {
		path := filepath.Join(chartsDir(), op, "values-standalone-multi.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: values-standalone-multi.yaml missing — required by I-6 (C12)", op)
			continue
		}
		// k3d uses in-cluster registry, no auth. Empty array.
		if !strings.Contains(string(data), "imagePullSecrets: []") {
			t.Errorf("%s/values-standalone-multi.yaml must declare 'imagePullSecrets: []' (k3d no-auth)", op)
		}
		if !strings.Contains(string(data), "k3d-loop-runtime-registry:5000") {
			t.Errorf("%s/values-standalone-multi.yaml must override registry to k3d-loop-runtime-registry:5000", op)
		}
	}
}

func TestSplitOperators_DefaultValuesMentionsImagePullSecrets(t *testing.T) {
	// All 4 default values.yaml MUST declare global.imagePullSecrets
	// (even if empty default). Without it the deployment template's
	// `{{- with .Values.global.imagePullSecrets }}` block silently
	// produces no auth, which works in k3d but breaks in DOCR if an
	// operator forgets to use values-prod.yaml.
	for _, op := range splitOperators {
		path := filepath.Join(chartsDir(), op, "values.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: values.yaml missing: %v", op, err)
		}
		if !strings.Contains(string(data), "imagePullSecrets:") {
			t.Errorf("%s/values.yaml must declare global.imagePullSecrets (was missing)", op)
		}
	}
}
