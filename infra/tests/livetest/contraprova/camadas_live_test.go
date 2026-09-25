// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contraprova

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T3.4 layers 4-7 hermetic tests. The layers themselves exec out to $LOOPZERO
// CLI + kubectl + HTTP, so hermetic tests focus on input validation +
// fixture creation. LIVE behavior is exercised by sponsor JUNTOS Phase 5.

func TestDefaultLiveConfig_SetsCanonicalDefaults(t *testing.T) {
	cfg := DefaultLiveConfig("20260617")
	if cfg.ProjectName != "contra-prova-20260617" {
		t.Errorf("ProjectName = %q", cfg.ProjectName)
	}
	if cfg.EnvName != "production" {
		t.Errorf("EnvName = %q", cfg.EnvName)
	}
	if cfg.BuildTimeout != 15*time.Minute {
		t.Errorf("BuildTimeout = %v", cfg.BuildTimeout)
	}
	if cfg.KubeContext != "k3d-loopzero-cell-0" {
		t.Errorf("KubeContext = %q", cfg.KubeContext)
	}
}

func TestCreateMinimalFixture_WritesAllFiles(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "fixture")
	if err := createMinimalFixture(dir, "test-proj"); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"loopzero.yaml", "package.json", "package-lock.json", "index.js"} {
		path := filepath.Join(dir, expected)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", expected, err)
		}
	}
	// Verify loopzero.yaml contains project name
	loopzeroYaml, err := os.ReadFile(filepath.Join(dir, "loopzero.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(loopzeroYaml), "project: test-proj") {
		t.Errorf("loopzero.yaml does not contain project name: %s", loopzeroYaml)
	}
}

func TestCamada5_ProvaDeVida_SkippedWhenHTTPProbeURLEmpty(t *testing.T) {
	cfg := DefaultLiveConfig("20260617")
	c := Camada5_ProvaDeVida(context.Background(), cfg)
	if !c.Skipped {
		t.Errorf("Skipped = false, want true when HTTPProbeURL empty")
	}
	if !strings.Contains(c.Detail, "HTTPProbeURL not set") {
		t.Errorf("Detail message: %q", c.Detail)
	}
}
