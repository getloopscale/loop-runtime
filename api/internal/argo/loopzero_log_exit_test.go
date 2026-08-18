// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"os/exec"
	"strings"
	"testing"

	forge "github.com/getloopscale/loopzero-forge"
)

// runInContainerShell executes the wrapped script with `sh`, which is what the build
// container runs (`Command: []string{"sh", "-c"}`). Asserting in bash would not measure
// the shell that actually runs in production.
func runInContainerShell(t *testing.T, script string) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			return ee.ExitCode()
		}
		t.Fatalf("execução falhou por outro motivo: %v", err)
	}
	return 0
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// The step's exit status MUST survive the log-marker wrapper.
//
// Measured 2026-08-18 (B-153): the wrapper ran the script inside `{ ... } | tee`, whose
// POSIX status is tee's — always 0. Every step built by buildContainer reported success
// no matter what it did, which made the fail-closed path for an unsupported runtime inert
// and turned a legible error into `Failed to build manifests with custom domains`.
func TestWrapper_PropagatesNonZeroExit(t *testing.T) {
	wrapped := wrapWithLoopZeroMarkers("generate-dockerfile", `echo "boom" >&2
exit 1`)

	if rc := runInContainerShell(t, wrapped); rc == 0 {
		t.Fatal("um passo que sai 1 reportou 0 — o status morreu no pipeline do tee")
	}
}

// The exact code matters: 2 is not 1, and a wrapper that normalizes every failure to 1
// loses the distinction the caller relied on.
func TestWrapper_PreservesTheExactCode(t *testing.T) {
	wrapped := wrapWithLoopZeroMarkers("build-apps", "exit 3")

	if rc := runInContainerShell(t, wrapped); rc != 3 {
		t.Fatalf("esperava código 3 preservado, veio %d", rc)
	}
}

// A step that succeeds must still succeed — the fix must not invert the failure.
func TestWrapper_KeepsSuccessSuccessful(t *testing.T) {
	wrapped := wrapWithLoopZeroMarkers("clone-repo", "echo ok")

	if rc := runInContainerShell(t, wrapped); rc != 0 {
		t.Fatalf("passo bem-sucedido reprovou com %d", rc)
	}
}

// The markers are the reason the wrapper exists; propagating the status must not cost them.
func TestWrapper_StillEmitsMarkersOnFailure(t *testing.T) {
	wrapped := wrapWithLoopZeroMarkers("install-deps", `echo "detalhe do erro" >&2
exit 1`)

	out, _ := exec.Command("sh", "-c", wrapped).CombinedOutput()
	s := string(out)
	if !strings.Contains(s, "level=error") {
		t.Fatalf("marcador de erro ausente na saída:\n%s", s)
	}
	if !strings.Contains(s, "detalhe do erro") {
		t.Fatalf("a última saída do passo deve chegar ao usuário:\n%s", s)
	}
}

// The population: every step the strategies build for an unsupported runtime is a
// fail-closed step, and all five were inert. rules/testing.md § 4.2 — a synthetic case
// proves the wrapper propagates; this proves the real construction path does.
func TestUnsupportedRuntimeStepsActuallyFail(t *testing.T) {
	s := &unsupportedStrategy{runtime: "unsupported"}
	steps := map[string]*forge.Container{
		"validate-workspace":  s.ValidateStep(nil),
		"install-deps":        s.InstallStep(nil),
		"build-apps":          s.BuildStep(nil),
		"security-scan":       s.ScanStep(),
		"generate-dockerfile": s.DockerfileStep(nil, 1),
	}
	for name, c := range steps {
		if len(c.Args) == 0 {
			t.Fatalf("%s: container sem script", name)
		}
		if rc := runInContainerShell(t, c.Args[len(c.Args)-1]); rc == 0 {
			t.Errorf("%s: passo de falha-fechada reportou sucesso", name)
		}
	}
}
