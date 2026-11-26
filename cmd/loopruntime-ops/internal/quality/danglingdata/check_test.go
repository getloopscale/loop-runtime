// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package danglingdata

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

func run(t *testing.T, root string) *Outcome {
	t.Helper()
	out, err := Check(Config{RepoRoot: root, BaseRef: "main", HeadRef: "HEAD"})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	return out
}

// TestDanglingData_DeletedConfigReferencedByScript_Fails — the governance class:
// a script hardcodes a config path; the config is deleted; the script survives.
func TestDanglingData_DeletedConfigReferencedByScript_Fails(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/governance/verify-independence.sh":   "#!/usr/bin/env bash\nCONFIG_FILE=\"docs/governance/personas/independence-config.yaml\"\ntest -f \"$CONFIG_FILE\"\n",
		"docs/governance/personas/independence-config.yaml": "personas: {}\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
		"docs/governance/personas/independence-config.yaml": "<DELETE>",
	})
	out := run(t, root)
	if out.Pass {
		t.Fatalf("expected FAIL: deleted config still referenced by live script, got Pass")
	}
	if !strings.Contains(strings.Join(out.Violations, "\n"), "independence-config.yaml") {
		t.Fatalf("violation must name the deleted config, got: %v", out.Violations)
	}
}

// TestDanglingData_DeletedCatalogReferencedByWorkflow_Fails — a workflow passes
// a deleted data path as a CLI flag.
func TestDanglingData_DeletedCatalogReferencedByWorkflow_Fails(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		".github/workflows/decay-scan.yml":   "jobs:\n  scan:\n    steps:\n      - run: scan-decay --catalog docs/governance/claim-catalog.yaml\n",
		"docs/governance/claim-catalog.yaml": "claims: []\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
		"docs/governance/claim-catalog.yaml": "<DELETE>",
	})
	out := run(t, root)
	if out.Pass {
		t.Fatalf("expected FAIL: deleted catalog still referenced by workflow, got Pass")
	}
}

// TestDanglingData_BothGone_Passes — config deleted AND its only referencing
// script deleted in the same change → no live ref → Pass.
func TestDanglingData_BothGone_Passes(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/governance/verify-independence.sh": "#!/usr/bin/env bash\nCONFIG_FILE=docs/governance/x.yaml\n",
		"docs/governance/x.yaml":                          "a: 1\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
		"infra/scripts/governance/verify-independence.sh": "<DELETE>",
		"docs/governance/x.yaml":                          "<DELETE>",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("deletion with referencing script also gone must PASS, got: %v", out.Violations)
	}
}

// TestDanglingData_CommentOnly_Passes — a script mentions the deleted path only
// in a comment → not a live reference.
func TestDanglingData_CommentOnly_Passes(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/scripts/foo.sh":     "#!/usr/bin/env bash\n# was: docs/governance/old.yaml (deleted)\necho hi\n",
		"docs/governance/old.yaml": "a: 1\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
		"docs/governance/old.yaml": "<DELETE>",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("comment-only reference must PASS, got: %v", out.Violations)
	}
}

// TestDanglingData_ProseOnly_Passes — a deleted config referenced only by .md
// prose is not a live caller break.
func TestDanglingData_ProseOnly_Passes(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"docs/operations/runbook.md":   "See docs/governance/scoring.yaml for details.\n",
		"docs/governance/scoring.yaml": "score: 4\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
		"docs/governance/scoring.yaml": "<DELETE>",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("prose-only (.md) reference must PASS, got: %v", out.Violations)
	}
}

// TestDanglingData_HelmValuesDeletion_Skipped — deleting a Helm values file is
// routine; even if a script names it, the deleted-side exclusion skips it.
func TestDanglingData_HelmValuesDeletion_Skipped(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/helm/foo/values.yaml": "replicas: 1\n",
		"infra/scripts/deploy.sh":    "#!/usr/bin/env bash\nhelm install foo -f infra/helm/foo/values.yaml\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
		"infra/helm/foo/values.yaml": "<DELETE>",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("helm values deletion must be SKIPPED (routine churn), got: %v", out.Violations)
	}
}

// TestDanglingData_FixtureDeletion_Skipped — test fixtures churn routinely and
// are consumed by frameworks via globs, not hardcoded script paths.
func TestDanglingData_FixtureDeletion_Skipped(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/tests/parity/fixtures/golden.yaml": "x: 1\n",
		"infra/scripts/run.sh":                    "#!/usr/bin/env bash\ndiff out infra/tests/parity/fixtures/golden.yaml\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
		"infra/tests/parity/fixtures/golden.yaml": "<DELETE>",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("fixture deletion must be SKIPPED, got: %v", out.Violations)
	}
}

// TestDanglingData_ManifestReferencedByManifest_Passes — a deleted .yaml
// referenced only by another non-orchestration .yaml (not a workflow/taskfile)
// is not an executable caller break.
func TestDanglingData_ManifestReferencedByManifest_Passes(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"infra/manifests/base.yaml":    "kind: ConfigMap\n",
		"infra/manifests/overlay.yaml": "resources:\n  - base.yaml\n",
	})
	qtestutil.CommitOn(t, root, "feat", map[string]string{
		"infra/manifests/base.yaml": "<DELETE>",
	})
	out := run(t, root)
	if !out.Pass {
		t.Fatalf("manifest-to-manifest reference is not an executable caller, must PASS, got: %v", out.Violations)
	}
}

// TestDanglingData_BaseRefUnreachable_Errors — EC-04 parity with danglingrefs.
func TestDanglingData_BaseRefUnreachable_Errors(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{"a.yaml": "x: 1\n"})
	_, err := Check(Config{RepoRoot: root, BaseRef: "origin/does-not-exist", HeadRef: "HEAD"})
	if err == nil {
		t.Fatal("unreachable base ref must error LOUD (EC-04)")
	}
	if !strings.Contains(err.Error(), "BASE_REF_UNREACHABLE") {
		t.Fatalf("error must be BASE_REF_UNREACHABLE, got: %v", err)
	}
}
