// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package naming

import (
	"strings"
	"testing"
)

// T1.1 of plan 2026-05-28-meet-001-engineering-closure v1.1.
// Step name helpers SoT — prevents the 3-layer SQL bug class from recurring
// in workflow output param consumption (cosign_attest.go writes "rekor-url"
// to step `attest-image-{app}`, build_reconcile_complete.go consumes it).

func TestSignStepName_FormatStable(t *testing.T) {
	got := SignStepName("api")
	want := "sign-image-api"
	if got != want {
		t.Errorf("SignStepName(api): got %q, want %q", got, want)
	}
}

func TestAttestStepName_FormatStable(t *testing.T) {
	got := AttestStepName("worker")
	want := "attest-image-worker"
	if got != want {
		t.Errorf("AttestStepName(worker): got %q, want %q", got, want)
	}
}

func TestBuildImageStepName_FormatStable(t *testing.T) {
	got := BuildImageStepName("frontend")
	want := "build-image-frontend"
	if got != want {
		t.Errorf("BuildImageStepName(frontend): got %q, want %q", got, want)
	}
}

func TestSBOMStepName_FormatStable(t *testing.T) {
	got := SBOMStepName("hello")
	want := "sbom-hello"
	if got != want {
		t.Errorf("SBOMStepName(hello): got %q, want %q", got, want)
	}
}

// TestStepName_RoundTrip asserts producer name == consumer map key.
// This is the invariant that the 3-layer SQL bug closure (commits f75a19bc
// + b3375ed0) protected via TDD. Adding a typo in one of the 5 producer
// sites OR the consumer site would silently produce empty Outputs maps
// and DB columns go NULL.
func TestStepName_RoundTrip(t *testing.T) {
	apps := []string{"api", "worker", "frontend", "hello"}
	for _, app := range apps {
		signName := SignStepName(app)
		attestName := AttestStepName(app)
		buildName := BuildImageStepName(app)
		sbomName := SBOMStepName(app)

		// Producer must use the helper; consumer must use the same helper.
		// Simulating consumer parse: extract `app` from the step name.
		signParsed := strings.TrimPrefix(signName, "sign-image-")
		attestParsed := strings.TrimPrefix(attestName, "attest-image-")
		buildParsed := strings.TrimPrefix(buildName, "build-image-")
		sbomParsed := strings.TrimPrefix(sbomName, "sbom-")

		if signParsed != app {
			t.Errorf("SignStepName round-trip failed for %q: parsed=%q", app, signParsed)
		}
		if attestParsed != app {
			t.Errorf("AttestStepName round-trip failed for %q: parsed=%q", app, attestParsed)
		}
		if buildParsed != app {
			t.Errorf("BuildImageStepName round-trip failed for %q: parsed=%q", app, buildParsed)
		}
		if sbomParsed != app {
			t.Errorf("SBOMStepName round-trip failed for %q: parsed=%q", app, sbomParsed)
		}
	}
}

// EC-10 fix: empty app name → panic with descriptive message (explicit
// decision per plan v1.1 edge-case-plan review). Silent trailing-dash
// retorno would propagate "sign-image-" as a step name and cause invisible
// map lookup failures.
func TestSignStepName_EmptyAppName_PanicsExplicitly(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("SignStepName(\"\") expected panic, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value not a string: %T %v", r, r)
		}
		if !strings.Contains(msg, "empty app name") {
			t.Errorf("panic message missing 'empty app name': %q", msg)
		}
	}()
	_ = SignStepName("")
}

func TestAttestStepName_EmptyAppName_PanicsExplicitly(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("AttestStepName(\"\") expected panic, got none")
		}
	}()
	_ = AttestStepName("")
}

func TestBuildImageStepName_EmptyAppName_PanicsExplicitly(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("BuildImageStepName(\"\") expected panic, got none")
		}
	}()
	_ = BuildImageStepName("")
}

func TestSBOMStepName_EmptyAppName_PanicsExplicitly(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SBOMStepName(\"\") expected panic, got none")
		}
	}()
	_ = SBOMStepName("")
}
