// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package github

import (
	"errors"
	"strings"
	"testing"
)

const deployableDescriptor = "project: %s\napps:\n  web:\n    runtime: node\n    start: node server.js\n"

func descriptorFor(project string) string {
	return strings.Replace(deployableDescriptor, "%s", project, 1)
}

// The CLI wrote theo.yaml and the push deploy read loopzero.yaml, so a project
// the CLI deployed could not be connected (getloopscale/loop-runtime#89). Every name
// a published CLI or an earlier API has used stays deployable.
func TestExtractDescriptor_ReadsEveryKnownName(t *testing.T) {
	for _, name := range []string{"loopscale.yaml", "loopzero.yaml", "theo.yaml"} {
		t.Run(name, func(t *testing.T) {
			tarball := makeTarGz(t, map[string]string{
				"owner-repo-abc123/" + name:   descriptorFor("hello"),
				"owner-repo-abc123/server.js": "",
			})
			cfg, err := extractLoopRuntimeYaml(tarball)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if cfg.Project != "hello" {
				t.Errorf("project = %q, want hello", cfg.Project)
			}
		})
	}
}

func TestExtractDescriptor_PrefersLoopscaleThenLoopzeroThenTheo(t *testing.T) {
	cases := []struct {
		files map[string]string
		want  string
	}{
		{map[string]string{"r/theo.yaml": descriptorFor("from-theo"), "r/loopzero.yaml": descriptorFor("from-loopruntime"), "r/loopscale.yaml": descriptorFor("from-loopscale")}, "from-loopscale"},
		{map[string]string{"r/theo.yaml": descriptorFor("from-theo"), "r/loopzero.yaml": descriptorFor("from-loopruntime")}, "from-loopruntime"},
	}
	for _, tc := range cases {
		// The tar order is the map's, so run it several times: the choice must not depend on it.
		for i := 0; i < 20; i++ {
			cfg, err := extractLoopRuntimeYaml(makeTarGz(t, tc.files))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Project != tc.want {
				t.Fatalf("project = %q, want %q", cfg.Project, tc.want)
			}
		}
	}
}

func TestExtractDescriptor_ErrorNamesTheFileThatWasRead(t *testing.T) {
	_, err := extractLoopRuntimeYaml(makeTarGz(t, map[string]string{"r/theo.yaml": "project: hello\n"}))
	var invalid *InvalidDeployDescriptorError
	if !errors.As(err, &invalid) {
		t.Fatalf("want InvalidDeployDescriptorError, got %v", err)
	}
	if !strings.HasPrefix(err.Error(), "theo.yaml ") {
		t.Errorf("error = %q, want it to start with theo.yaml", err.Error())
	}
}

func TestExtractDescriptor_MissingNamesTheCurrentFile(t *testing.T) {
	_, err := extractLoopRuntimeYaml(makeTarGz(t, map[string]string{"r/server.js": ""}))
	if err == nil || !strings.Contains(err.Error(), "loopscale.yaml") {
		t.Errorf("error = %v, want it to name loopscale.yaml", err)
	}
}

func TestExtractDescriptor_IgnoresADescriptorBelowTheRoot(t *testing.T) {
	_, err := extractLoopRuntimeYaml(makeTarGz(t, map[string]string{"r/sub/loopscale.yaml": descriptorFor("nested")}))
	if err == nil {
		t.Error("a descriptor in a subdirectory was read as the repository's")
	}
}
