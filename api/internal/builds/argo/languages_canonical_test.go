// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argo

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// T1.2 (plan simplify-deploy-loopzerokit-only v2.0) — wire the canonical generator
// into Node/PythonStrategy.DockerfileStep, replacing loopRuntimePacksDockerfileStep.

func TestNodeStrategy_DockerfileStep_UsesCanonical(t *testing.T) {
	c := (&NodeStrategy{}).DockerfileStep([]deploycontracts.AppSpec{{Name: "web", Type: "server", Port: 8080, Start: "node x.js"}}, 1)
	if c == nil {
		t.Fatal("DockerfileStep returned nil")
	}
	if c.Name != "generate-dockerfile" {
		t.Errorf("step name must be preserved (loop_runtime_log marker), got %q", c.Name)
	}
	if c.Image != imageAlpine {
		t.Errorf("canonical step must use the minimal writer image %q, not loop-runtime-packs; got %q", imageAlpine, c.Image)
	}
	script := strings.Join(c.Args, "\n")
	if !strings.Contains(script, "syntax=docker/dockerfile:1") || !strings.Contains(script, "node x.js") {
		t.Errorf("canonical Dockerfile not written by step; script:\n%s", script)
	}
}

func TestPythonStrategy_DockerfileStep_UsesCanonical(t *testing.T) {
	c := (&PythonStrategy{}).DockerfileStep([]deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 8000, Start: "python -m app"}}, 1)
	if c == nil {
		t.Fatal("DockerfileStep returned nil")
	}
	if c.Image != imageAlpine {
		t.Errorf("canonical step must use the minimal writer image; got %q", c.Image)
	}
	script := strings.Join(c.Args, "\n")
	if !strings.Contains(script, "FROM "+imagePythonBase) {
		t.Errorf("python canonical base not in step script:\n%s", script)
	}
}

func TestNodeStrategy_DockerfileStep_FailsClosedOnError(t *testing.T) {
	// Missing start → fail-closed: a failing container (exit 1), never loop-runtime-packs,
	// never a panic. Step name preserved so the DAG wiring stays intact.
	c := (&NodeStrategy{}).DockerfileStep([]deploycontracts.AppSpec{{Name: "x", Type: "server", Port: 8080}}, 1)
	if c == nil {
		t.Fatal("DockerfileStep returned nil")
	}
	if c.Name != "generate-dockerfile" {
		t.Errorf("step name must be preserved even on error, got %q", c.Name)
	}
	script := strings.Join(c.Args, "\n")
	if !strings.Contains(script, "exit 1") {
		t.Errorf("missing-start must yield a failing container; script:\n%s", script)
	}
}
