// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argo

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// T2.1 (plan simplify-deploy-loopzerokit-only v2.0) — collapse to node/python.
// The fixed LoopZeroKit stack supports only node + python. All other dispatch
// targets are removed; unsupported runtimes fail closed (no loopruntime-packs, no
// silent fallback).

func TestResolveStrategy_NodePython_OK(t *testing.T) {
	if got := ResolveStrategy("node").Name(); got != "node" {
		t.Errorf(`ResolveStrategy("node").Name() = %q, want "node"`, got)
	}
	if got := ResolveStrategy("python").Name(); got != "python" {
		t.Errorf(`ResolveStrategy("python").Name() = %q, want "python"`, got)
	}
}

func TestResolveStrategy_UnsupportedRuntime_FailsClosed(t *testing.T) {
	app := []deploycontracts.AppSpec{{Name: "x", Type: "server", Port: 8080, Start: "x"}}
	for _, fw := range []string{"go", "ruby", "java", "php", "rust"} {
		c := ResolveStrategy(fw).DockerfileStep(app, 1)
		if c == nil {
			t.Fatalf("ResolveStrategy(%q).DockerfileStep returned nil", fw)
		}
		script := strings.Join(c.Args, "\n")
		if strings.Contains(script, "loopruntimepacks-generate") {
			t.Errorf("runtime %q must NOT route to loopruntime-packs after collapse", fw)
		}
		if !strings.Contains(script, "exit 1") {
			t.Errorf("unsupported runtime %q must fail closed (exit 1); script:\n%s", fw, script)
		}
	}
}
