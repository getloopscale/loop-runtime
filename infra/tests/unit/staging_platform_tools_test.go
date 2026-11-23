// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// The every-commit deploy runs `task staging:platform` alone, so a tool that task calls
// must reach the host through it: when seed-aws-cosign-key.sh started calling cosign and
// only host:tools installed it, every deploy failed until the next rebuild.
func TestThePlatformTaskInstallsTheHostToolsFirst(t *testing.T) {
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
	cmds := file.Tasks["platform"].Cmds
	if len(cmds) == 0 {
		t.Fatal("task platform has no cmds")
	}
	first, _ := cmds[0].(map[string]any)
	if first["task"] != "host:tools" {
		t.Errorf("task platform starts with %v, want task: host:tools", cmds[0])
	}
}
