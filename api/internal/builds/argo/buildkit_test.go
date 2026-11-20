// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"

	forge "github.com/getloopscale/argo-workflows-go"
	"github.com/getloopscale/argo-workflows-go/model"
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
	c, err := b.buildkitContainer(buildkitParams{
		AppName:        "api",
		ImageTag:       "registry.example.com/org/api:abc",
		ContextPath:    "/workspace/context",
		DockerfilePath: "/workspace/Dockerfile",
		CacheRef:       "registry.example.com/cache:api",
	})
	if err != nil {
		t.Fatal(err)
	}

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
	script, err := buildkitEntrypointScript("/run/buildkit/buildkitd.sock", false, "registry.example.com", metadataFileFor("api"))
	if err != nil {
		t.Fatal(err)
	}

	// Must reference this app's metadata file.
	if !strings.Contains(script, "metadata-api.json") {
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
	digestSection := script[max(0, digestIdx-200):min(len(script), digestIdx+200)]
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

// Sprint B T3.1 (plan 2026-06-01-p2-interface-contracts-robustness, ADR
// D3): asserts the BuildKit container exposes an explicit
// ActiveDeadlineSeconds = 1800 so retry-window + execution stay aligned
// with the DB reaper's 30min RunningTimeout. EC-7 of the plan: assert
// the struct field directly, NOT a YAML grep (forge serialization is
// not the contract — the field value is).
func TestBuildKit_DeadlineCappedAt1800s(t *testing.T) {
	b := &WorkflowBuilder{
		infraParams: InfraParams{RegistryURL: "registry.example.com", RegistryInsecure: false},
	}
	c, err := b.buildkitContainer(buildkitParams{
		AppName:        "api",
		ImageTag:       "registry.example.com/org/api:abc",
		ContextPath:    "/workspace/context",
		DockerfilePath: "/workspace/Dockerfile",
		CacheRef:       "registry.example.com/cache:api",
	})
	if err != nil {
		t.Fatal(err)
	}

	if c.ActiveDeadlineSeconds == nil {
		t.Fatal("buildkit container missing ActiveDeadlineSeconds — orphan-execution window vs reaper RunningTimeout (30min)")
	}
	if got, want := *c.ActiveDeadlineSeconds, 1800; got != want {
		t.Errorf("ActiveDeadlineSeconds = %ds, want %ds (matches DB reaper RunningTimeout)", got, want)
	}
}

// TestBuildKit_RetryStrategy_RemainsBounded — guard rail: the 3×900s
// retry budget pre-T3.1 was 45min, exceeding the reaper's 30min limit
// and creating the orphan window we just closed. This test makes the
// invariant explicit: ActiveDeadlineSeconds (single attempt cap or
// hard cap depending on Argo semantics) MUST NOT permit a retry chain
// exceeding the deadline. With deadline=1800 and per-attempt=900,
// at most 2 attempts can fit — the Argo controller hard-stops on
// activeDeadlineSeconds regardless of remaining retries.
func TestBuildKit_RetryStrategy_RemainsBounded(t *testing.T) {
	b := &WorkflowBuilder{
		infraParams: InfraParams{RegistryURL: "registry.example.com", RegistryInsecure: false},
	}
	c, err := b.buildkitContainer(buildkitParams{
		AppName:        "api",
		ImageTag:       "registry.example.com/org/api:abc",
		ContextPath:    "/workspace/context",
		DockerfilePath: "/workspace/Dockerfile",
		CacheRef:       "registry.example.com/cache:api",
	})
	if err != nil {
		t.Fatal(err)
	}

	if c.RetryStrategy == nil {
		t.Fatal("buildkit container missing RetryStrategy")
	}
	if c.ActiveDeadlineSeconds == nil {
		t.Fatal("buildkit container missing ActiveDeadlineSeconds")
	}
	// Sanity: deadline must be > a single retry attempt's max duration
	// (900s) so at least one attempt can complete. Less than that would
	// be a misconfiguration where retry never fires meaningfully.
	if *c.ActiveDeadlineSeconds <= 900 {
		t.Errorf("ActiveDeadlineSeconds %ds too small vs single-attempt 900s max", *c.ActiveDeadlineSeconds)
	}
}
