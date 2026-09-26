package clusterrenameguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// writeBaseline materializes a baseline file plus the source files it describes.
func writeBaseline(t *testing.T, root string, b Baseline, files map[string]string) string {
	t.Helper()
	for name, body := range files {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "baseline.json")
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A generic occurrence is a false positive of the rename: a line where the token
// means something else. Renaming one is the failure this gate exists to catch,
// and it is invisible to a reviewer walking 1.388 changed occurrences by eye.
func TestFailsWhenAGenericOccurrenceWasRenamed(t *testing.T) {
	root := t.TempDir()
	baseline := Baseline{
		TreeSHA: "deadbeef",
		Generic: []Occurrence{
			{Path: "api/main.go", Token: "vault", Content: `// dispatches through the vault (OpenBao transit)`},
		},
	}
	// The line was rewritten by a blind rename: `vault` became `secrets`, but here
	// `vault` named the OpenBao transit, not the cluster.
	path := writeBaseline(t, root, baseline, map[string]string{
		"api/main.go": "package main\n// dispatches through the secrets (OpenBao transit)\n",
	})

	outcome, err := Check(Config{RepoRoot: root, BaselinePath: path, SkipAncestryCheck: true})
	if err != nil {
		t.Fatalf("a renamed occurrence is a finding, not an error: %v", err)
	}
	if outcome.Verdict() != verdict.Fail {
		t.Fatal("expected the gate to FAIL when a generic occurrence was renamed")
	}
	if len(outcome.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %d: %v", len(outcome.Failures), outcome.Failures)
	}
}

func TestPassesWhenGenericOccurrencesAreUntouched(t *testing.T) {
	root := t.TempDir()
	baseline := Baseline{
		TreeSHA: "deadbeef",
		Generic: []Occurrence{
			{Path: "api/main.go", Token: "vault", Content: `// dispatches through the vault (OpenBao transit)`},
		},
	}
	// The cluster identifier changed; the generic line did not. This is the whole
	// point of the rename, so it must pass.
	path := writeBaseline(t, root, baseline, map[string]string{
		"api/main.go": "package main\n// dispatches through the vault (OpenBao transit)\n// cluster loopzero-secrets\n",
	})

	outcome, err := Check(Config{RepoRoot: root, BaselinePath: path, SkipAncestryCheck: true})
	if err != nil || outcome.Verdict() != verdict.Pass {
		t.Fatalf("expected PASS, got err=%v failures: %v", err, outcome.Failures)
	}
}

// EC-2 of the edge-case review: a generic occurrence that appears in a file the
// baseline never saw is not a regression. `B-005` will write prose about CAPI's
// "management cluster", which the frozen list classifies generic for `mgmt` — a
// gate comparing totals instead of baseline members would accuse it.
func TestNewFileWithGenericOccurrenceIsNotARegression(t *testing.T) {
	root := t.TempDir()
	baseline := Baseline{TreeSHA: "deadbeef", Generic: []Occurrence{}}
	path := writeBaseline(t, root, baseline, map[string]string{
		"docs/adr.md": "The CAPI management cluster is named fleet.\n",
	})

	if out, err := Check(Config{RepoRoot: root, BaselinePath: path, SkipAncestryCheck: true}); err != nil || out.Verdict() != verdict.Pass {
		t.Fatalf("a generic occurrence in a file absent from the baseline must not fail: err=%v failures=%v", err, out.Failures)
	}
}

// EC-1: a baseline captured AFTER the first edit compares the new state with
// itself and passes forever, proving nothing. The gate must refuse to run rather
// than report a green it cannot support.
func TestRefusesWhenBaselineIsNotAnAncestorOfHead(t *testing.T) {
	root := t.TempDir()
	baseline := Baseline{TreeSHA: "0000000000000000000000000000000000000000", Generic: []Occurrence{}}
	path := writeBaseline(t, root, baseline, nil)

	_, err := Check(Config{RepoRoot: root, BaselinePath: path})
	if err == nil {
		t.Fatal("expected refusal when the baseline SHA cannot be proven an ancestor of HEAD")
	}
}

// A baseline pointing at a file that no longer exists is a rename of the whole
// file, which the plan does via `git mv` — the gate reports it rather than
// crashing, because a missing file is a finding, not an error.
func TestMissingFileIsReportedAsAFailure(t *testing.T) {
	root := t.TempDir()
	baseline := Baseline{
		TreeSHA: "deadbeef",
		Generic: []Occurrence{{Path: "gone.go", Token: "vault", Content: "// vault means OpenBao"}},
	}
	path := writeBaseline(t, root, baseline, nil)

	outcome, err := Check(Config{RepoRoot: root, BaselinePath: path, SkipAncestryCheck: true})
	if err != nil {
		t.Fatalf("a missing file is a finding, not an error: %v", err)
	}
	if outcome.Verdict() != verdict.Fail {
		t.Fatal("expected FAIL when a baseline file no longer exists")
	}
	if len(outcome.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %v", outcome.Failures)
	}
}

// The plan renames files with `git mv` (EC-6, so `git log --follow` keeps reaching the
// history). A guard that tracks occurrences by PATH turns every legitimate file rename
// into a false accusation — and the claim it defends is "this generic line still
// exists", not "it still lives at this exact path". Caught when
// loopzero-vault-secrets.yaml was moved to loopzero-secrets-secrets.yaml to match the resource
// it declares.
func TestFollowsAGenericOccurrenceAcrossAFileRename(t *testing.T) {
	root := t.TempDir()
	baseline := Baseline{
		TreeSHA: "deadbeef",
		Generic: []Occurrence{
			{Path: "manifests/old-name.yaml", Token: "vault", Content: "  # backed by the vault (OpenBao)"},
		},
	}
	// Same content, new path — the file moved, the generic line did not change.
	path := writeBaseline(t, root, baseline, map[string]string{
		"manifests/new-name.yaml": "kind: ClusterSecretStore\n  # backed by the vault (OpenBao)\n",
	})

	if out, err := Check(Config{RepoRoot: root, BaselinePath: path, SkipAncestryCheck: true}); err != nil || out.Verdict() != verdict.Pass {
		t.Fatalf("a moved file must not read as a renamed false positive: err=%v failures=%v", err, out.Failures)
	}
}

// The relaxation above must not swallow the defect the guard exists for: when the line
// is gone from the whole tree, it was renamed, and that still fails.
func TestStillFailsWhenTheLineIsGoneFromTheWholeTree(t *testing.T) {
	root := t.TempDir()
	baseline := Baseline{
		TreeSHA: "deadbeef",
		Generic: []Occurrence{
			{Path: "manifests/old-name.yaml", Token: "vault", Content: "  # backed by the vault (OpenBao)"},
		},
	}
	path := writeBaseline(t, root, baseline, map[string]string{
		"manifests/new-name.yaml": "kind: ClusterSecretStore\n  # backed by the secrets (OpenBao)\n",
	})

	if out, err := Check(Config{RepoRoot: root, BaselinePath: path, SkipAncestryCheck: true}); err != nil || out.Verdict() != verdict.Fail {
		t.Fatalf("a generic line absent from the entire tree is a renamed false positive and must FAIL (err=%v)", err)
	}
}
