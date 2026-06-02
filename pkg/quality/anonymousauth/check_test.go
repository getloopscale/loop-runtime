// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package anonymousauth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// allowed location: anonymous-auth=true is the documented LOCAL k3d marker.
func TestCheck_AllowsInK3dDir(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/k3d/build-cluster.yaml", "      - arg: \"--kube-apiserver-arg=anonymous-auth=true\"\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("infra/k3d/ must PASS: %+v", out)
	}
}

// outside allowlist: must fail. This is the CR-3 defense-in-depth: prevent the
// flag from drifting into a prod chart / Pulumi / DOKS manifest.
func TestCheck_FailsInProdHelm(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/helm/loopzero-api/values.yaml", "extraArgs:\n  - --kube-apiserver-arg=anonymous-auth=true\n")
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass || len(out.Violations) != 1 {
		t.Fatalf("anonymous-auth=true outside infra/k3d/ must FAIL: %+v", out)
	}
	if !strings.Contains(out.Violations[0], "infra/helm/loopzero-api/values.yaml") {
		t.Fatalf("violation must name the file: %v", out.Violations)
	}
}

func TestCheck_FailsInPulumi(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/pulumi/src/cluster.ts", "kubernetesVersion: \"--kube-apiserver-arg=anonymous-auth=true\"")
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass {
		t.Fatal("anonymous-auth=true in infra/pulumi/ must FAIL")
	}
}

// comment line containing the literal: must NOT trigger the gate (so ADRs and
// runbooks can describe the flag in prose without breaking CI).
func TestCheck_IgnoresCommentLines(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/operations/notes.md", "# we forbid --kube-apiserver-arg=anonymous-auth=true outside k3d\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("comment-only mention must PASS: %+v", out)
	}
}

// anonymous-auth=false is fine everywhere — only the dangerous =true literal
// is gated. K3d itself often disables anonymous-auth explicitly elsewhere.
func TestCheck_AllowsAnonymousAuthFalse(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/helm/foo/values.yaml", "--kube-apiserver-arg=anonymous-auth=false\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass {
		t.Fatalf("anonymous-auth=false must PASS everywhere: %+v", out)
	}
}

// gate test file referencing the literal must not self-trip CI (the test files
// are *_test.go, excluded from the scan by mirror of advisorylock convention).
func TestCheck_IgnoresGoTestFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "pkg/quality/something/check_test.go", "const sample = `--kube-apiserver-arg=anonymous-auth=true`\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass {
		t.Fatalf("_test.go must be excluded: %+v", out)
	}
}

// EC: subpath of infra/k3d/ (e.g., infra/k3d/legacy/old.yaml) is also allowed.
func TestCheck_AllowsK3dSubdir(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/k3d/legacy/dev-cluster.yaml", "--kube-apiserver-arg=anonymous-auth=true\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass {
		t.Fatalf("infra/k3d/<sub>/ must PASS: %+v", out)
	}
}

// EC: LOCAL k3s diagnostic scenario fixtures in pulumi-platform overrides
// (applied via docker exec to k3d) are explicitly allowlisted by path.
func TestCheck_AllowsFulcioOIDCOverrides(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/pulumi-platform/values-fulcio-oidc-overrides/cenario-a.yaml",
		"kube-apiserver-arg:\n  - \"anonymous-auth=true\"\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass {
		t.Fatalf("infra/pulumi-platform/values-fulcio-oidc-overrides/ must PASS: %+v", out)
	}
}

// EC: the gate's own source defines the constant and must not self-trip.
func TestCheck_AllowsOwnSource(t *testing.T) {
	root := t.TempDir()
	write(t, root, "pkg/quality/anonymousauth/check.go",
		"const forbiddenLiteral = \"anonymous-auth=true\"\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass {
		t.Fatalf("pkg/quality/anonymousauth/ must PASS: %+v", out)
	}
}

// EC: Markdown docs (plans, ADRs, READMEs) may freely describe the flag.
// The gate prevents executable-artifact drift, not documentation prose.
func TestCheck_AllowsMarkdown(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/plans/some-plan.md",
		"This task adds a gate that blocks `anonymous-auth=true` outside k3d.\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass {
		t.Fatalf("Markdown files must PASS: %+v", out)
	}
}

// T2.1 of plan 2026-06-01-p3-infra-observability-test-debt-plan v1.1
// (BONUS-simplified: code already correct; tests lock the invariant).
//
// The canonical k3d build-cluster.yaml is the ONE production-bound file that
// declares anonymous-auth=true (LOCAL-only per ADR 2026-05-28-T1.4). This test
// locks the literal path against future drift: if anyone renames the file or
// removes the allowlist entry, the gate MUST still pass for the canonical use.
func TestAnonymousAuthGate_K3dBuildAllowlisted(t *testing.T) {
	root := t.TempDir()
	// EXACT file the canonical allowlist anchors on; do NOT change without
	// updating allowedPrefixes simultaneously (governance: ADR amendment).
	write(t, root, "infra/k3d/build-cluster.yaml",
		"servers: 1\nagents: 0\noptions:\n  k3s:\n    extraArgs:\n      - arg: \"--kube-apiserver-arg=anonymous-auth=true\"\n        nodeFilters: [\"server:*\"]\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("canonical infra/k3d/build-cluster.yaml must PASS (allowlist anchor): %+v", out)
	}
	if len(out.Violations) != 0 {
		t.Fatalf("zero violations expected for allowlisted file; got: %v", out.Violations)
	}
}

// T2.1 companion test: a synthetic file OUTSIDE every allowlist entry that
// nevertheless contains the literal MUST trip the gate. Uses a neutral path
// (`api/internal/somewhere.go`) instead of a specific known directory so the
// test does NOT silently weaken if a new allowlist entry is added that happens
// to match infra/helm or infra/pulumi.
func TestAnonymousAuthGate_NonAllowlistedFileFails(t *testing.T) {
	root := t.TempDir()
	// Neutral path: api/internal/ is NOT in allowedPrefixes; should never be.
	write(t, root, "api/internal/server/setup.go",
		"// extraArgs configures the apiserver\nconst k3sArg = \"--kube-apiserver-arg=anonymous-auth=true\"\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatalf("non-allowlisted path with literal must FAIL")
	}
	if len(out.Violations) != 1 {
		t.Fatalf("expected exactly 1 violation, got %d: %v", len(out.Violations), out.Violations)
	}
	if !strings.Contains(out.Violations[0], "api/internal/server/setup.go") {
		t.Fatalf("violation must name the offending file path; got: %v", out.Violations[0])
	}
}
