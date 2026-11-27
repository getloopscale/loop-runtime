// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	forge "github.com/getloopscale/argo-workflows-go"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// runFailStep runs the container's script in a fresh directory and returns its
// combined output and that directory.
func runFailStep(t *testing.T, c *forge.Container) (string, string) {
	t.Helper()
	if len(c.Args) == 0 {
		t.Fatal("container has no script")
	}
	dir := t.TempDir()
	cmd := exec.Command("sh", "-c", c.Args[len(c.Args)-1])
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("fail step exited 0; output:\n%s", out)
	}
	return string(out), dir
}

// A failed step writes its error message into the script. The message sat inside a
// double-quoted echo, so `start` in it ran as a command (`/bin/sh: start: not found`).
func TestDockerfileGenerationFailurePrintsBackticksLiterally(t *testing.T) {
	c := canonicalDockerfileStepOrFail("node",
		[]deploycontracts.AppSpec{{Name: "web", Runtime: "node"}}, 1)

	out, _ := runFailStep(t, c)

	if !strings.Contains(out, "services.json `start` is required") {
		t.Errorf("output lacks the literal message:\n%s", out)
	}
	if strings.Contains(out, "not found") {
		t.Errorf("a word of the message ran as a command:\n%s", out)
	}
}

// The runtime comes from the user's loopzero.yaml and lands in the message, so no
// character in it may be interpreted by the shell.
func TestUnsupportedRuntimeMessageDoesNotExecuteItsText(t *testing.T) {
	runtime := "go`touch backtick-ran`$(touch subst-ran)'quote\"${HOME}"
	c := (&unsupportedStrategy{runtime: runtime}).failStep("validate-workspace")

	out, dir := runFailStep(t, c)

	for _, marker := range []string{"backtick-ran", "subst-ran"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			t.Errorf("the message ran a command: %s exists", marker)
		}
	}
	if !strings.Contains(out, "`touch backtick-ran`$(touch subst-ran)'quote") {
		t.Errorf("output lacks the literal runtime:\n%s", out)
	}
	if !strings.Contains(out, "${HOME}") {
		t.Errorf("a variable in the message was expanded:\n%s", out)
	}
}
