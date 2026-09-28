// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// RED test for issue #242 fix 3: loopzero-api caches the build-cluster client at
// startup, so a rotated `loopzero-cluster-kubeconfigs.build.kubeconfig` needs a POD
// RESTART before deploys work again.
//
// The watcher already NOTICES the problem — `ReportWatchError` logs
// "build cluster auth failed — rotate loopzero-workflow-submitter token" and bumps
// ArgoWatcherTokenExpiredTotal on 401/403. What it cannot do is act on it: the
// bearer token was baked into the rest.Config at process start, so rotating the
// Secret changes the file on disk and nothing else. Every deploy keeps failing
// against a credential that has already been replaced.
//
// The native Kubernetes answer is `tokenFile:` in the kubeconfig — client-go
// re-reads it, which is how projected SA tokens rotate without a restart. This
// deployment mounts the token INLINE (`token: <literal>`, see
// infra/helmfile/scripts/lib/scoped-kubeconfig.sh), so that path is unavailable
// without changing the generator, the Secret and the chart mount together.
// This does the equivalent in the one place that can change safely: re-read the
// kubeconfig from disk and pick up the current token.
package k8s

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const kubeconfigTemplate = `apiVersion: v1
kind: Config
clusters:
  - name: build
    cluster:
      server: https://build.example:6443
      insecure-skip-tls-verify: true
contexts:
  - name: build
    context:
      cluster: build
      user: build
current-context: build
users:
  - name: build
    user:
      token: %TOKEN%
`

func writeKubeconfig(t *testing.T, dir, token string) string {
	t.Helper()
	path := filepath.Join(dir, "build.kubeconfig")
	body := strings.ReplaceAll(kubeconfigTemplate, "%TOKEN%", token)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// TestKubeconfigTokenSource_PicksUpRotatedToken is the #242-fix-3 RED.
//
// The file on disk is what the mounted Secret becomes after a rotation. A
// source that answers with the startup token forever is the bug.
func TestKubeconfigTokenSource_PicksUpRotatedToken(t *testing.T) {
	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "token-before-rotation")

	src := newKubeconfigTokenSource(path, "")

	got, err := src.Token()
	if err != nil {
		t.Fatalf("initial Token(): %v", err)
	}
	if got != "token-before-rotation" {
		t.Fatalf("initial token = %q, want %q", got, "token-before-rotation")
	}

	// The operator rotates the Secret; kubelet rewrites the mounted file.
	writeKubeconfig(t, dir, "token-after-rotation")
	src.Invalidate() // what the 401/403 handler calls

	got, err = src.Token()
	if err != nil {
		t.Fatalf("Token() after rotation: %v", err)
	}
	if got != "token-after-rotation" {
		t.Errorf("after rotation Token() = %q, want %q (issue #242 fix 3).\n"+
			"    The build-cluster credential was baked into the rest.Config at process start, so\n"+
			"    rotating the Secret changed the file and nothing else — every deploy kept failing\n"+
			"    against a credential that had already been replaced, until somebody restarted the pod.",
			got, "token-after-rotation")
	}
}

// TestKubeconfigTokenSource_CachesBetweenInvalidations proves the re-read is
// not per-request. Parsing the kubeconfig on every API call would turn a
// once-a-week rotation into constant disk I/O on the hot path.
func TestKubeconfigTokenSource_CachesBetweenInvalidations(t *testing.T) {
	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "cached-token")

	src := newKubeconfigTokenSource(path, "")
	if _, err := src.Token(); err != nil {
		t.Fatalf("initial Token(): %v", err)
	}

	// Change the file WITHOUT invalidating: the cached value must survive.
	writeKubeconfig(t, dir, "should-not-be-read-yet")

	got, err := src.Token()
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	if got != "cached-token" {
		t.Errorf("Token() re-read the file without an Invalidate() — got %q.\n"+
			"    Re-parsing the kubeconfig on every request would put YAML parsing on the hot path\n"+
			"    of every workflow poll to serve a credential that changes weekly.", got)
	}
}

// TestKubeconfigTokenSource_MissingFileIsAnError keeps the failure honest: a
// deleted or unreadable kubeconfig must surface, not silently yield "".
// An empty bearer token produces a 401 that looks exactly like an expired
// credential, sending the operator to the wrong runbook.
func TestKubeconfigTokenSource_MissingFileIsAnError(t *testing.T) {
	src := newKubeconfigTokenSource(filepath.Join(t.TempDir(), "absent.kubeconfig"), "")

	got, err := src.Token()
	if err == nil {
		t.Errorf("absent kubeconfig returned token %q with no error — an empty bearer token yields a\n"+
			"    401 indistinguishable from an expired credential, pointing the operator at the\n"+
			"    rotation runbook for a file that is simply not there", got)
	}
}
