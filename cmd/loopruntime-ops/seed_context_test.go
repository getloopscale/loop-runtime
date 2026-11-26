// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The composition root must know WHICH context it resolved, not only resolve one.
//
// B-024: buildKubeClientAndRest("") lets ConfigFlags pick the active context, and
// the comment beside it called the result "the mgmt client". It was not — it was
// "whatever context is active". After `k3d:secrets:create`, which switches the
// context as a side effect, the seeds operated the secrets cluster believing it
// was core.
func TestResolveContextName_ReadsTheActiveContext(t *testing.T) {
	dir := t.TempDir()
	kc := filepath.Join(dir, "config")
	if err := os.WriteFile(kc, []byte(`apiVersion: v1
kind: Config
current-context: k3d-loopruntime-secrets
clusters:
- name: c1
  cluster: {server: https://127.0.0.1:6443}
contexts:
- name: k3d-loopruntime-core
  context: {cluster: c1, user: u1}
- name: k3d-loopruntime-secrets
  context: {cluster: c1, user: u1}
users:
- name: u1
  user: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", kc)

	got := resolveContextName("")
	if got != "k3d-loopruntime-secrets" {
		t.Errorf("active context resolved as %q, want k3d-loopruntime-secrets", got)
	}
}

// An explicitly requested context beats the active one — and the operator must
// see what they asked for reflected back, or the message lies the other way.
func TestResolveContextName_ExplicitWins(t *testing.T) {
	// Isolate the environment: the explicit branch short-circuits before reading
	// the kubeconfig, so without this the test would pass even if the order
	// flipped.
	t.Setenv("KUBECONFIG", writeKubeconfig(t, "k3d-loopruntime-secrets"))

	if got := resolveContextName("k3d-loopruntime-core"); got != "k3d-loopruntime-core" {
		t.Errorf("the explicit context became %q", got)
	}
}

// With no context to name, the function returns empty instead of inventing one —
// a wrongly named message is worse than an unnamed one.
//
// Measured in review (finding 6): a KUBECONFIG pointing at an ABSENT file is not
// an error in clientcmd — it just resolves an empty current-context. This test
// exercises that branch, and its name says so; the read-error branch (a
// malformed file) stays uncovered, and that is stated rather than assumed.
func TestResolveContextName_AbsentKubeconfigIsEmptyNotInvented(t *testing.T) {
	t.Setenv("CORE_CONTEXT", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))

	if got := resolveContextName(""); got != "" {
		t.Errorf("with no kubeconfig it should return empty, got %q", got)
	}
}

// CORE_CONTEXT pins the cluster the seeds operate instead of inheriting the
// active one.
//
// B-024 review: naming the context in the message improves the diagnosis but
// does NOT end the inheritance — it only changed WHICH wrong cluster was hit.
// Before the taskfile fix the seeds landed on the secrets cluster; after it,
// wherever `task setup` left the context. Both are wrong.
//
// The convention already existed — seed-post-cleanroom-all.sh uses
// `${CORE_CONTEXT:-k3d-loopruntime-core}` for its own kubectl calls. The ladder
// stopped at rung 4: reuse what is installed rather than invent a variable.
func TestResolveContextName_CoreContextPins(t *testing.T) {
	t.Setenv("CORE_CONTEXT", "k3d-loopruntime-core")

	if got := resolveContextName(""); got != "k3d-loopruntime-core" {
		t.Errorf("CORE_CONTEXT should pin the context, got %q", got)
	}
}

// An explicitly requested context still beats the variable: whoever types it knows what they want.
func TestResolveContextName_ExplicitBeatsCoreContext(t *testing.T) {
	t.Setenv("CORE_CONTEXT", "k3d-loopruntime-secrets")

	if got := resolveContextName("k3d-loopruntime-build"); got != "k3d-loopruntime-build" {
		t.Errorf("explicit should beat CORE_CONTEXT, got %q", got)
	}
}

// Without the variable the behaviour does not change: the active context.
// Pinning a hard default here would break anyone running seeds outside k3d.
func TestResolveContextName_UnsetKeepsInheriting(t *testing.T) {
	t.Setenv("CORE_CONTEXT", "")
	t.Setenv("KUBECONFIG", writeKubeconfig(t, "k3d-loopruntime-cell-1"))

	if got := resolveContextName(""); got != "k3d-loopruntime-cell-1" {
		t.Errorf("without CORE_CONTEXT it should inherit the active context, got %q", got)
	}
}

// writeKubeconfig writes a minimal kubeconfig with the requested active context.
func writeKubeconfig(t *testing.T, current string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	body := "apiVersion: v1\nkind: Config\ncurrent-context: " + current + "\n" +
		"clusters:\n- name: c1\n  cluster: {server: https://127.0.0.1:6443}\n" +
		"contexts:\n- name: " + current + "\n  context: {cluster: c1, user: u1}\n" +
		"users:\n- name: u1\n  user: {}\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The context the message NAMES must be the one the client USES.
//
// Review finding 1: if the name is resolved on one path and the client built on
// another, the message names a cluster nobody searched — worse than naming none,
// because it sends the operator to check the wrong place with confidence.
func TestResolvedContextIsTheOneTheClientUses(t *testing.T) {
	t.Setenv("CORE_CONTEXT", "ctx-pinned")
	t.Setenv("KUBECONFIG", writeTwoContextKubeconfig(t))

	resolved := resolveContextName("")
	used, err := contextUsedByClientConfig("")
	if err != nil {
		t.Fatalf("resolve the client config: %v", err)
	}
	if resolved != used {
		t.Errorf("the message would name %q and the client would use %q — they diverge", resolved, used)
	}
}

// writeTwoContextKubeconfig writes a kubeconfig whose ACTIVE context differs
// from the pinned one, so a divergence shows if it exists.
func writeTwoContextKubeconfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	body := `apiVersion: v1
kind: Config
current-context: ctx-active
clusters:
- name: cl-active
  cluster: {server: https://127.0.0.1:6443}
- name: cl-pinned
  cluster: {server: https://127.0.0.1:6447}
contexts:
- name: ctx-active
  context: {cluster: cl-active, user: u1}
- name: ctx-pinned
  context: {cluster: cl-pinned, user: u1}
users:
- name: u1
  user: {}
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
