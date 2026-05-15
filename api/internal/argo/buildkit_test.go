// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"

	forge "github.com/getloopscale/loopzero-forge"
	"github.com/getloopscale/loopzero-forge/model"
)

// CHG-2026-04-25-ATTESTATION-WIRE Phase 1 (AC-1.2): the buildImage
// step MUST declare an output parameter exporting the container image
// digest from BuildKit metadata. The cosign step also emits a digest
// (redundantly) — having the buildkit-side output makes the digest
// available even if cosign is later optional/skipped, and acts as a
// cross-check.
//
// The entrypoint script is responsible for parsing the metadata file
// AFTER buildctl returns 0 and writing the digest to a container-local
// path so Argo can capture it post-pod-termination.
func TestBuildImageTemplate_DeclaresDigestOutput(t *testing.T) {
	b := &WorkflowBuilder{
		infraParams: InfraParams{RegistryURL: "registry.example.com", RegistryInsecure: false},
	}
	c := b.buildkitContainer(buildkitParams{
		AppName:        "api",
		ImageTag:       "registry.example.com/org/api:abc",
		ContextPath:    "/workspace/context",
		DockerfilePath: "/workspace/Dockerfile",
		CacheRef:       "registry.example.com/cache:api",
	})

	digestParam := findOutputParam(c, "digest")
	if digestParam == nil {
		t.Fatalf("buildkit container missing required output parameter %q (declared outputs: %v)", "digest", c.Outputs)
	}
	if digestParam.ValueFrom == nil || digestParam.ValueFrom.Path == "" {
		t.Fatalf("digest output has no ValueFrom.Path")
	}

	// Trust-model invariant: path must be on container-local /tmp,
	// NOT on shared /workspace where another container could forge it.
	path := digestParam.ValueFrom.Path
	if !strings.HasPrefix(path, "/tmp/") {
		t.Errorf("digest output path %q must be under /tmp (container-local); /workspace is shared and tamperable", path)
	}
}

// AC-1.2 cont: the entrypoint script must actually populate the
// digest output file after a successful build, by parsing the
// BuildKit --metadata-file. Without this, the output param declaration
// captures an empty value.
func TestBuildkitEntrypointScript_WritesDigestAfterSuccessfulBuild(t *testing.T) {
	script := buildkitEntrypointScript("/run/buildkit/buildkitd.sock", false, "registry.example.com")

	// Must reference the metadata path glob (we don't know appName here;
	// the entrypoint extracts it post-build by globbing).
	if !strings.Contains(script, "metadata-") {
		t.Errorf("entrypoint does not reference metadata file in post-build hook:\n%s", script)
	}

	// Must reference the output path.
	if !strings.Contains(script, "/tmp/build-digest.txt") {
		t.Errorf("entrypoint does not write to /tmp/build-digest.txt:\n%s", script)
	}

	// The post-build hook must run AFTER the build's exit code is
	// captured but BEFORE the script exits — otherwise the digest is
	// lost on a successful build.
	scriptIdx := strings.Index(script, `"$@"`)
	digestIdx := strings.Index(script, "/tmp/build-digest.txt")
	exitIdx := strings.Index(script, "exit $BUILD_EXIT")
	if scriptIdx == -1 || digestIdx == -1 || exitIdx == -1 {
		t.Fatalf("script missing required markers: build=%d digest=%d exit=%d", scriptIdx, digestIdx, exitIdx)
	}
	if scriptIdx >= digestIdx || digestIdx >= exitIdx {
		t.Errorf("post-build digest hook out of order: must come after `$@` and before `exit $BUILD_EXIT`")
	}

	// Defensive: digest extraction must not fail the build. Look for
	// `|| true` or equivalent error-tolerance near the digest extraction.
	digestSection := script[digestIdx-200 : digestIdx+200]
	if !strings.Contains(digestSection, "|| true") && !strings.Contains(digestSection, "2>/dev/null") {
		t.Errorf("digest extraction must be defensive (|| true / 2>/dev/null) so a missing/malformed metadata file does not fail the build:\n%s", digestSection)
	}
}

// findOutputParam looks up an output parameter by name in a forge.Container.
// Helper for AC-1.1 / AC-1.2 tests.
func findOutputParam(c *forge.Container, name string) *forge.Parameter {
	for i := range c.Outputs {
		if c.Outputs[i].Name == name {
			return &c.Outputs[i]
		}
	}
	return nil
}

// Compile-time type assertions to fail fast if the forge package shape
// drifts. If the struct shape changes, AC-1 tests should fail BEFORE
// implementation as well.
var (
	_ forge.Parameter = forge.Parameter{}
	_ model.ValueFrom = model.ValueFrom{}
)
