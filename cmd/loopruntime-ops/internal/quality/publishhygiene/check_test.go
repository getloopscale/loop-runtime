// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package publishhygiene

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

// realHome builds a NON-synthetic home path at runtime.
//
// Composed instead of written literally because this file is scanned by the very lock
// it tests: a fixture with a literal `/home/<name>/` would — correctly — be a finding.
// Exempting the file would solve it from the outside and leave the lock with a blind
// spot exactly where it is audited. Composing keeps the assertion whole and the scan honest.
func realHome(sub string) string { return "/" + "home" + "/alice/" + sub }

// gitTree makes a real git repo — the gate reads `git ls-files`, which is what will be
// published, not the working directory.
func gitTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
	return root
}

func TestRealHomePathIsALeak(t *testing.T) {
	root := gitTree(t, map[string]string{"a.md": "see " + realHome("project/x") + "\n"})
	out, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}}))
	if !errors.Is(err, ErrLeak) {
		t.Fatalf("a real user home is a leak; err=%v", err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Kind != "home" {
		t.Fatalf("the finding must be classified; got %+v", out.Findings)
	}
}

// The boundary that keeps the lock from becoming noise — it is what makes the difference
// between this lock and one that fails the security test itself.
func TestSyntheticHomesAndNonRoutableIPsAreNotLeaks(t *testing.T) {
	root := gitTree(t, map[string]string{
		"ci.yml":   "workdir: /home/runner/work/x\n",
		"ssrf.go":  "const metadata = \"169.254.169.254\"\n",
		"dns.go":   "const exampleCom = \"93.184.216.34\"\n",
		"local.go": "const dev = \"127.0.0.1\"\nconst lan = \"192.168.1.10\"\n",
		"fix.go":   "const home = \"/home/dev/loopruntime\"\n",
	})
	out, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}}))
	if err != nil {
		t.Fatalf("test constants and CI homes are not leaks: %v (%+v)", err, out.Findings)
	}
}

// loopruntime-rag's IP half was NOT carried over, and this is an assertion of scope — not
// an oversight. Against this tree it gave 104 findings, almost all false: version
// strings, the `1.2.3.4` placeholder, the Cloudflare CIDR ranges the code carries on
// purpose, and the live target declared in `rules/live-target.txt`, which is a contract.
func TestIPHalfIsDeliberatelyOutOfScope(t *testing.T) {
	root := gitTree(t, map[string]string{
		"runbook.md":  "ssh operator@165.227.121.20\n",
		"versions.md": "Accepted tags: `13.3.2.1-1`, `15.5-10`\n",
		"cf.go":       `const cf = "173.245.48.0/20"` + "\n",
	})
	if _, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}})); err != nil {
		t.Fatalf("this lock measures HOME PATH; addresses are loopruntime-rag's scope: %v", err)
	}
}

// The dated trail is exempt by DECLARATION, and the reason is another repository rule:
// `docs/README.md` forbids rewriting a dated record to match the present.
func TestAuditTrailIsNotScanned(t *testing.T) {
	root := gitTree(t, map[string]string{
		"docs/wiki/reviews/old.md":              "ran in " + realHome("loopruntime") + "\n",
		"docs/operations/baselines/b.md":        "measured in " + realHome("loopruntime") + "\n",
		"CHANGELOG.md":                          "fixed in " + realHome("loopruntime") + "\n",
		".claude/knowledge-base/decisions/x.md": "measured in " + realHome("m") + "\n",
		"live.md":                               "no leak\n",
	})
	out, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}}))
	if err != nil {
		t.Fatalf("a dated record is not scanned: %v (%+v)", err, out.Findings)
	}
	if out.Scanned != 1 {
		t.Fatalf("only the live file should be scanned; scanned=%d", out.Scanned)
	}
}

// An exemption needs a reason AND a sunset. Without both it declares nothing — it only silences.
func TestExemptionNeedsReasonAndSunset(t *testing.T) {
	root := gitTree(t, map[string]string{
		"deploy.conf": "root " + realHome("dist") + "\n",
		"clean.md":    "no leak\n", // without a scanned file, the verdict would be ErrNothingScanned
	})
	for _, e := range []Exemption{
		{Path: "deploy.conf", Sunset: "2026-11-17"},
		{Path: "deploy.conf", Reason: "deployment path"},
	} {
		if _, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{e}})); err == nil {
			t.Fatalf("an incomplete exemption must not pass: %+v", e)
		}
	}
	ok := Exemption{Path: "deploy.conf", Reason: "live deployment path", Sunset: "2026-11-17"}
	out, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{ok}}))
	if err != nil {
		t.Fatalf("a complete exemption must pass: %v", err)
	}
	if out.Exempt != 1 {
		t.Fatalf("the exemption must be COUNTED so its existence shows; got %d", out.Exempt)
	}
}

// Absence is a FAILURE, never a skip.
func TestEmptyTreeIsAFailure(t *testing.T) {
	root := gitTree(t, map[string]string{})
	if _, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}})); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("scanning zero files must fail; err=%v", err)
	}
}

// The real population (B-109). This assertion is what gives the gate its meaning: it is
// born measured against the tree that ships, not against fixtures.
func TestRealTreeIsClean(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: repoRoot}))
	if err != nil {
		for i, f := range out.Findings {
			if i == 12 {
				t.Errorf("... and %d more", len(out.Findings)-12)
				break
			}
			t.Errorf("%s:%d [%s] %s", f.File, f.Line, f.Kind, f.Text)
		}
		t.Fatalf("%d leak(s) in the real tree: %v", len(out.Findings), err)
	}
	if out.Scanned < 500 {
		t.Fatalf("scanned=%d — the scan is reaching little of the tree", out.Scanned)
	}
}

// A line longer than any scanner buffer must not cost the findings before it:
// a workstation path next to a minified or base64 line is still a leak.
func TestLeakBeforeAVeryLongLineIsKept(t *testing.T) {
	long := strings.Repeat("x", 2*1024*1024)
	root := gitTree(t, map[string]string{"a.md": "see " + realHome("x") + "\n" + long + "\n"})

	out, err := violationOf(Check(Config{RepoRoot: root}))

	if !errors.Is(err, ErrLeak) || len(out.Findings) != 1 {
		t.Fatalf("the leak before the long line must be reported; findings=%v err=%v", out.Findings, err)
	}
}

// A versioned file the gate cannot read is not a clean file.
func TestUnreadableVersionedFileIsUnverified(t *testing.T) {
	root := gitTree(t, map[string]string{"a.md": "clean\n", "b.md": "clean\n"})
	if err := os.Remove(filepath.Join(root, "b.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "b.md")); err != nil {
		t.Fatal(err)
	}

	out, err := violationOf(Check(Config{RepoRoot: root}))

	if !errors.Is(err, ErrUnverified) || len(out.Unverified) != 1 {
		t.Fatalf("an unreadable versioned file must be unverified; out=%+v err=%v", out, err)
	}
}

// A binary file is not a text leak and is counted apart, not as scanned text.
func TestBinaryFileIsSkippedAndCounted(t *testing.T) {
	root := gitTree(t, map[string]string{"a.md": "clean\n", "logo.png": "\x89PNG\x00" + realHome("x")})

	out, err := violationOf(Check(Config{RepoRoot: root}))

	if err != nil || out.Binary != 1 {
		t.Fatalf("binary file must be skipped and counted; out=%+v err=%v", out, err)
	}
}
