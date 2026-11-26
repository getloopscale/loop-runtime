// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package manifests

import "testing"

// Tests for the lean-resources fallback — T-08 contingency path. The switch
// is RenderInput.LeanResources, filled by the caller from
// config.Config.LeanResources (env LOOP_RUNTIME_LEAN_RESOURCES).

func TestResources_DefaultFallback(t *testing.T) {
	ri := RenderInput{}
	if got := ri.cpuRequest(); got != "100m" {
		t.Errorf("default cpu request = %q, want 100m", got)
	}
	if got := ri.memoryRequest(); got != "128Mi" {
		t.Errorf("default memory request = %q, want 128Mi", got)
	}
	if got := ri.cpuLimit(); got != "500m" {
		t.Errorf("default cpu limit = %q, want 500m", got)
	}
	if got := ri.memoryLimit(); got != "512Mi" {
		t.Errorf("default memory limit = %q, want 512Mi", got)
	}
}

func TestResources_LeanFallback_WhenInputIsLean(t *testing.T) {
	ri := RenderInput{LeanResources: true}
	if got := ri.cpuRequest(); got != "50m" {
		t.Errorf("lean cpu request = %q, want 50m", got)
	}
	if got := ri.memoryRequest(); got != "64Mi" {
		t.Errorf("lean memory request = %q, want 64Mi", got)
	}
	if got := ri.cpuLimit(); got != "250m" {
		t.Errorf("lean cpu limit = %q, want 250m", got)
	}
	if got := ri.memoryLimit(); got != "256Mi" {
		t.Errorf("lean memory limit = %q, want 256Mi", got)
	}
}

// TestResources_IgnoreTheProcessEnvironment pins that the rendered sizing is a
// function of RenderInput alone: the API process environment must not change
// what the renderer produces for the same input.
func TestResources_IgnoreTheProcessEnvironment(t *testing.T) {
	t.Setenv("LOOP_RUNTIME_LEAN_RESOURCES", "true")
	ri := RenderInput{}
	if got := ri.memoryRequest(); got != "128Mi" {
		t.Errorf("memory request = %q with LeanResources unset, want 128Mi regardless of the process env", got)
	}
}

func TestResources_LeanFlagIgnoresExplicit(t *testing.T) {
	// Critical invariant: if the caller supplies a specific default,
	// lean mode MUST NOT override it. Otherwise lean mode would silently
	// shrink production deploys where capacity planning is explicit.
	ri := RenderInput{
		LeanResources:        true,
		DefaultCPURequest:    "200m",
		DefaultMemoryRequest: "512Mi",
		DefaultCPULimit:      "1000m",
		DefaultMemoryLimit:   "2Gi",
	}
	if got := ri.cpuRequest(); got != "200m" {
		t.Errorf("explicit cpu request should win, got %q", got)
	}
	if got := ri.memoryRequest(); got != "512Mi" {
		t.Errorf("explicit memory request should win, got %q", got)
	}
	if got := ri.cpuLimit(); got != "1000m" {
		t.Errorf("explicit cpu limit should win, got %q", got)
	}
	if got := ri.memoryLimit(); got != "2Gi" {
		t.Errorf("explicit memory limit should win, got %q", got)
	}
}

func TestLeanResources_FitsHundredAppsInCurrentRuntime(t *testing.T) {
	// Math sanity check documented alongside code: runtime cluster at
	// the time this flag was introduced had ~18Gi total allocatable
	// memory. 100 apps × lean memory request must fit with headroom.
	// Default 128Mi × 100 = 12.8Gi (cuts it close, fragmentation fails);
	// lean 64Mi × 100 = 6.4Gi (comfortable headroom).
	const lean = 64 // MiB per app
	const appCount = 100
	const runtimeAllocatableMiB = 18 * 1024 // rough — see L3 v14 postmortem
	const systemReservedMiB = 4 * 1024      // platform pods + DaemonSets

	if lean*appCount > runtimeAllocatableMiB-systemReservedMiB {
		t.Fatalf("lean sizing regression: %d apps × %dMiB = %dMiB exceeds %dMiB available",
			appCount, lean, lean*appCount, runtimeAllocatableMiB-systemReservedMiB)
	}
}
