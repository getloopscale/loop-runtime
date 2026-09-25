// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartpairs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// The two docs/decisions.md rows the committed registry cites.
const (
	recordedDriftDecision = "Chart-pair drift is recorded, never blessed per pair"
	openRBACDecision      = "Whether the bootstrap loopzero-api ClusterRole keeps cluster-wide secrets delete and pods/exec create"
)

// realRegistry loads the committed registry, so a fixture tree exercises the
// entries the gate actually runs with.
func realRegistry(t *testing.T) *Registry {
	t.Helper()
	reg, err := LoadRegistry(qtestutil.Root(t))
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	return &reg
}

// declareFixturePairs writes the pair registry for a fixture tree: every
// charts/<x> with an infra/helm/<x> twin, plus the admission-policies
// conceptual pair when both sides are present. The gate reads pairs only from
// the registry, so a fixture must declare the pairs it lays out.
func declareFixturePairs(t *testing.T, root string) {
	t.Helper()
	var lines []string
	entries, err := os.ReadDir(filepath.Join(root, "charts"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(root, "infra/helm", e.Name())); err == nil {
			lines = append(lines, e.Name()+" | "+e.Name()+" | fixture pair")
		}
	}
	if isDir(filepath.Join(root, "charts/loopzero-admission-policies")) && isDir(filepath.Join(root, "infra/helm/admission-policies")) {
		lines = append(lines, "admission-policies | loopzero-admission-policies | fixture conceptual pair")
	}
	p := filepath.Join(root, chartpairs.RegistryRelPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
