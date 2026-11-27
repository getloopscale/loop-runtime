// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// The nightly rebuild runs `task staging:up`, and only staging:host installed the emulator
// binary and its systemd units: moving EMULATOR_VERSION to v0.4.0 left the rebuilt
// environment on v0.3.1. The emulator task starts the emulator right after `down` stopped
// it, so it installs the pinned release and the units first.
func TestTheEmulatorTaskInstallsThePinnedReleaseBeforeStartingIt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Tasks map[string]struct {
			Cmds []any `yaml:"cmds"`
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	cmds := file.Tasks["emulator"].Cmds
	want := []string{"host:emulator", "host:units"}
	if len(cmds) < len(want) {
		t.Fatalf("task emulator has %d cmds, want it to start with task: %v", len(cmds), want)
	}
	for i, name := range want {
		cmd, _ := cmds[i].(map[string]any)
		if cmd["task"] != name {
			t.Errorf("task emulator cmd %d is %v, want task: %s", i, cmds[i], name)
		}
	}
}
