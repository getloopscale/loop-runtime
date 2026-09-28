// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cleanroom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// `task cleanroom:v2` is the command people cite as "a cleanroom ran". Its specs
// destroy the k3d clusters, so they refuse to run without explicit consent — and
// until #393 they refused unconditionally, so the task exited 0 having run 0 of 3
// specs. These tests pin the task to the same consent the specs read, and to the
// one spec that runs the whole chain, so the task cannot drift back to a green
// that proves nothing.

func cleanroomTaskCommand(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "taskfiles", "test.yml"))
	if err != nil {
		t.Fatalf("read taskfiles/test.yml: %v", err)
	}
	var doc struct {
		Tasks map[string]struct {
			Cmds []any `yaml:"cmds"`
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse taskfiles/test.yml: %v", err)
	}
	task, ok := doc.Tasks["cleanroom:v2"]
	if !ok {
		t.Fatal("taskfiles/test.yml declares no cleanroom:v2 task")
	}
	var cmds []string
	for _, c := range task.Cmds {
		if s, ok := c.(string); ok {
			cmds = append(cmds, s)
		}
	}
	return strings.Join(cmds, "\n")
}

func TestCleanroomTaskGrantsTheConsentTheSpecsRead(t *testing.T) {
	cmd := cleanroomTaskCommand(t)
	if want := DestroyConsentEnv + "=1"; !strings.Contains(cmd, want) {
		t.Fatalf("cleanroom:v2 does not set %s; its specs skip without it, so the task runs nothing\ncommand: %s", want, cmd)
	}
}

func TestCleanroomTaskRunsOnlyTheFullChainSpec(t *testing.T) {
	cmd := cleanroomTaskCommand(t)
	if !strings.Contains(cmd, "-ginkgo.label-filter=full") {
		t.Fatalf("cleanroom:v2 runs every spec; with consent granted that is three cleanrooms in a row\ncommand: %s", cmd)
	}
}

func TestDestroyConsentIsOnlyTheExactValueOne(t *testing.T) {
	for value, want := range map[string]bool{"1": true, "": false, "0": false, "true": false, "yes": false} {
		t.Setenv(DestroyConsentEnv, value)
		if got := DestroyConsented(); got != want {
			t.Errorf("%s=%q: DestroyConsented() = %v, want %v", DestroyConsentEnv, value, got, want)
		}
	}
}
