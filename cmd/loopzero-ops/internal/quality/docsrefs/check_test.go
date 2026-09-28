// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package docsrefs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// writeTree lays out a synthetic repo. Keys are repo-relative paths.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func future() string { return time.Now().AddDate(0, 0, 30).Format("2006-01-02") }
func past() string   { return time.Now().AddDate(0, 0, -1).Format("2006-01-02") }
func today() string  { return time.Now().Format("2006-01-02") }

// --- rule references ------------------------------------------------------

func TestCheck_DanglingRuleRefFails(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":                "Read `.claude/rules/ghost.md` before doing the work.\n",
		".claude/rules/present.md": "x\n",
		"docsrefs-allowlist.txt":   "",
	})
	out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")})
	if err != nil || out.Verdict() != verdict.Fail {
		t.Fatalf("a dangling rule reference must fail; got err=%v", err)
	}
	if len(out.Unresolved) != 1 || !strings.Contains(out.Unresolved[0].Raw, "ghost.md") {
		t.Errorf("the failure must NAME the reference; got %+v", out.Unresolved)
	}
}

// The list at CLAUDE.md:206 writes the contracts as BARE filenames on a line
// that also names the directory. That line is the most important one in the
// file and a path-shaped matcher misses it entirely.
func TestCheck_BareFilenameOnADirectoryLineIsAReference(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":              "Other contracts live in `.claude/rules/` — `alive.md`, `ghost.md`.\n",
		".claude/rules/alive.md": "x\n",
		"docsrefs-allowlist.txt": "",
	})
	out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")})
	if err != nil || out.Verdict() != verdict.Fail {
		t.Fatalf("a bare filename on a .claude/rules/ line must be treated as a reference; got err=%v", err)
	}
	if len(out.Unresolved) != 1 || !strings.Contains(out.Unresolved[0].Raw, "ghost.md") {
		t.Errorf("expected exactly the missing one; got %+v", out.Unresolved)
	}
}

// A reference written `../.claude/rules/X.md` points at the umbrella, not at
// this repo. Resolving it against the repo root would report a false miss.
func TestCheck_ParentRelativeRefResolvesAgainstParent(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	if err := os.MkdirAll(filepath.Join(parent, ".claude", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".claude", "rules", "umbrella.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("see `../.claude/rules/umbrella.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	al := filepath.Join(root, "al.txt")
	if err := os.WriteFile(al, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: root, AllowlistPath: al}); err != nil {
		t.Errorf("a parent-relative reference that exists in the parent must resolve; got %v", err)
	}
}

// --- ADR references -------------------------------------------------------

func TestCheck_DanglingADRRefFails(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":              "x\n",
		"pkg/a/a.go":             "package a\n\n// per ADR-9999 this is fine.\nconst A = 1\n",
		"docsrefs-allowlist.txt": "",
	})
	out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")})
	if err != nil || out.Verdict() != verdict.Fail {
		t.Fatalf("an ADR cited by Go code and documented nowhere must fail; got %v", err)
	}
	if len(out.Unresolved) != 1 || out.Unresolved[0].Raw != "ADR-9999" {
		t.Errorf("expected ADR-9999; got %+v", out.Unresolved)
	}
}

// EC-7: the citation lives in a COMMENT. An identifier or a URL that happens to
// contain the token is not a citation, which is why the scan is over AST
// comment groups and not raw text.
func TestCheck_ADRInStringLiteralIsNotACitation(t *testing.T) {
	root := writeTree(t, map[string]string{
		// One resolvable reference so the enumeration floor does not fire and
		// mask the property under test.
		"CLAUDE.md":                "Read `.claude/rules/present.md`.\n",
		".claude/rules/present.md": "x\n",
		"pkg/a/a.go":               "package a\n\nconst URL = \"https://example.test/ADR-9999\"\n",
		"docsrefs-allowlist.txt":   "",
	})
	if _, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); err != nil {
		t.Errorf("an ADR token inside a string literal is not a citation; got %v", err)
	}
}

func TestCheck_ADRDocumentedAnywhereResolves(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":                  "Read `.claude/rules/present.md`.\n",
		".claude/rules/present.md":   "x\n",
		"pkg/a/a.go":                 "package a\n\n// per ADR-0042.\nconst A = 1\n",
		"docs/architecture/thing.md": "decided in ADR-0042\n",
		"docsrefs-allowlist.txt":     "",
	})
	if _, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); err != nil {
		t.Errorf("an ADR documented in docs/ must resolve; got %v", err)
	}
}

// --- allowlist ------------------------------------------------------------

func TestCheck_AllowlistedRefPasses(t *testing.T) {
	root := writeTree(t, map[string]string{
		".claude/rules/present.md": "x\n", // the kit is installed
		"CLAUDE.md":                "Read `.claude/rules/ghost.md`.\n",
		"docsrefs-allowlist.txt":   "rule | .claude/rules/ghost.md | " + future() + " | owner decision pending\n",
	})
	if _, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); err != nil {
		t.Errorf("an allowlisted reference within its sunset must pass; got %v", err)
	}
}

func TestCheck_ExpiredAllowlistEntryFails(t *testing.T) {
	root := writeTree(t, map[string]string{
		".claude/rules/present.md": "x\n", // the kit is installed
		"CLAUDE.md":                "Read `.claude/rules/ghost.md`.\n",
		"docsrefs-allowlist.txt":   "rule | .claude/rules/ghost.md | " + past() + " | deadline passed\n",
	})
	if out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); err != nil || out.Verdict() != verdict.Fail || len(out.Unresolved) == 0 {
		t.Errorf("an expired entry must stop exempting; got %v", err)
	}
}

// EC-9: the sunset date is inclusive — it expires the day AFTER.
func TestCheck_SunsetTodayStillPasses(t *testing.T) {
	root := writeTree(t, map[string]string{
		".claude/rules/present.md": "x\n", // the kit is installed
		"CLAUDE.md":                "Read `.claude/rules/ghost.md`.\n",
		"docsrefs-allowlist.txt":   "rule | .claude/rules/ghost.md | " + today() + " | last day\n",
	})
	if _, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); err != nil {
		t.Errorf("the sunset date is inclusive; got %v", err)
	}
}

// EC-8: a malformed allowlist must fail loudly. Silently ignoring the line
// would exempt nothing while reading as an exemption — the shape this whole
// slice exists to remove.
func TestCheck_MalformedAllowlistIsAnError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":              "x\n",
		"docsrefs-allowlist.txt": "rule | .claude/rules/ghost.md | no-date\n",
	})
	out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")})
	if err != nil || !errors.Is(out.MalformedAllowlist, ErrMalformedAllowlist) || out.Verdict() != verdict.Fail {
		t.Errorf("a malformed allowlist line must be a typed error, never ignored; got %v", err)
	}
}

func TestCheck_AllowlistCommentsAndBlanksAreSkipped(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":                "Read `.claude/rules/present.md`.\n",
		".claude/rules/present.md": "x\n",
		"docsrefs-allowlist.txt":   "# comment\n\n   \n",
	})
	if _, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); err != nil {
		t.Errorf("comments and blank lines are not entries; got %v", err)
	}
}

// Negative control: the checker must be able to pass. Without this, every test
// above would be satisfied by a Check that always errors.
func TestCheck_CleanTreePasses(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":                "Read `.claude/rules/present.md`.\n",
		".claude/rules/present.md": "x\n",
		"pkg/a/a.go":               "package a\n\n// per ADR-0042.\nconst A = 1\n",
		"docs/d.md":                "ADR-0042 says so\n",
		"docsrefs-allowlist.txt":   "# nothing exempt\n",
	})
	out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")})
	if err != nil {
		t.Fatalf("a clean tree must pass; got %v", err)
	}
	if out.Checked == 0 {
		t.Errorf("a pass with zero references checked is a vacuous pass — the same defect B-040 fixes in route-auth-coverage")
	}
}

// Regression: a line that cites a full rules PATH and also mentions an
// unrelated backticked markdown file must not turn the second into a claim.
// Measured on CLAUDE.md:215, which cites `.claude/rules/auto-docs.md` and, in
// the same sentence, `index.md` — a knowledge-base file, not a contract.
func TestCheck_BareNameOnlyCountsWhenTheDirectoryIsQuoted(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":                "Read `index.md` first; never auto-generate (`.claude/rules/present.md`).\n",
		".claude/rules/present.md": "x\n",
		"docsrefs-allowlist.txt":   "",
	})
	if _, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); err != nil {
		t.Errorf("`index.md` on a line citing a rules PATH is not a contract claim; got %v", err)
	}
}

// --- review findings #1 and #2 --------------------------------------------

// #1: an empty tree must FAIL. The gate reproduced, in its own code, the
// PASS-with-zero shape B-040 removed next door.
func TestCheck_EmptyTreeIsAFailure(t *testing.T) {
	root := writeTree(t, map[string]string{"docsrefs-allowlist.txt": ""})
	if _, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); !errors.Is(err, ErrNoReferencesEnumerated) {
		t.Errorf("a tree with no references must fail, not pass vacuously; got %v", err)
	}
}

// #1b: the floor must not mask real findings. A tree whose ONLY references are
// broken must report the broken references, never an empty scan.
func TestCheck_BrokenRefsBeatTheEmptyScanFloor(t *testing.T) {
	root := writeTree(t, map[string]string{
		".claude/rules/present.md": "x\n", // the kit is installed
		"CLAUDE.md":                "Read `.claude/rules/ghost.md`.\n",
		"docsrefs-allowlist.txt":   "",
	})
	if out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); err != nil || out.Verdict() != verdict.Fail || len(out.Unresolved) == 0 {
		t.Errorf("broken references must be reported as such, not as an empty scan; got %v", err)
	}
}

// The .claude/ tree is gitignored: a mention there reaches nobody who clones,
// so it must not document an ADR the code cites.
func TestCheck_MentionUnderGitignoredKitIsNotADRDocumentation(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":  "x\n",
		"pkg/a/a.go": "package a\n\n// per ADR-0024.\nconst A = 1\n",
		".claude/knowledge-base/decisions/2026-01-01-thing.md": "This is ADR-0024.\n",
		"docsrefs-allowlist.txt":                               "",
	})
	if out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); err != nil || out.Verdict() != verdict.Fail || len(out.Unresolved) == 0 {
		t.Errorf("a record under .claude/ must not resolve the ADR; got %v", err)
	}
}

// Without the installed kit a rules reference is neither confirmed nor
// refuted: it is reported as not verified and the verdict stays green.
func TestCheck_RuleRefWithoutInstalledKitIsNotVerified(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":              "Read `.claude/rules/anything.md`.\n",
		"pkg/a/a.go":             "package a\n\n// per ADR-0042.\nconst A = 1\n",
		"docs/d.md":              "ADR-0042 says so\n",
		"docsrefs-allowlist.txt": "",
	})
	out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")})
	if err != nil {
		t.Fatalf("an unverifiable rule reference must not fail the gate; got %v", err)
	}
	if len(out.NotVerified) != 1 || out.NotVerified[0].Raw != ".claude/rules/anything.md" || out.Checked != 1 {
		t.Fatalf("want one not-verified rule reference and one checked ADR, got %+v", out)
	}
}

// ...but a genuine decision record still counts.
func TestCheck_DecisionRecordIsADRDocumentation(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":                          "Read `.claude/rules/present.md`.\n",
		".claude/rules/present.md":           "x\n",
		"pkg/a/a.go":                         "package a\n\n// per ADR-0024.\nconst A = 1\n",
		"docs/decisions/2026-01-01-thing.md": "This is ADR-0024.\n",
		"docsrefs-allowlist.txt":             "",
	})
	if _, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")}); err != nil {
		t.Errorf("a real decision record must resolve the ADR; got %v", err)
	}
}
