// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package envprefixparity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// twoSites writes two synthetic Go files, each declaring the symbol with the
// given literal body, and returns a Config pointing at both.
//
// Each fixture also carries a matching loop, because every real site has one and
// Check now refuses a site without it (ErrNoMatchingLoop): a case check that
// found nothing to inspect verified nothing, and reporting that as success is
// the `PASS — 0 routes` shape of B-040. A fixture that would not survive the
// gate is a fixture that tests a gate nobody ships.
func twoSites(t *testing.T, aBody, bBody string) Config {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const match = "\n\nfunc match(k string) bool {\n\tfor _, p := range reservedEnvKeyPrefixes {\n\t\tif strings.HasPrefix(k, p) {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn false\n}\n"
	write("a/a.go", "package a\n\nvar reservedEnvKeyPrefixes = []string{"+aBody+"}"+match)
	write("b/b.go", "package b\n\nvar reservedEnvKeyPrefixes = []string{"+bBody+"}"+match)
	return Config{
		RepoRoot: root,
		Sites: []Site{
			{RelPath: "a/a.go", Symbol: "reservedEnvKeyPrefixes"},
			{RelPath: "b/b.go", Symbol: "reservedEnvKeyPrefixes"},
		},
	}
}

func TestCheck_DivergentListsFail(t *testing.T) {
	out, err := Check(twoSites(t, `"LOOPZERO_", "AWS_", "GOOGLE_"`, `"LOOPZERO_", "AWS_"`))
	if !errors.Is(err, ErrPrefixSetsDiverge) {
		t.Fatalf("divergent sets must fail; got %v", err)
	}
	only := out.OnlyIn["a/a.go"]
	if len(only) != 1 || only[0] != "GOOGLE_" {
		t.Errorf("the failure must NAME the prefix and the file that carries it; got %+v", out.OnlyIn)
	}
}

// EC-5: order is not part of the rule.
func TestCheck_SameSetDifferentOrderPasses(t *testing.T) {
	if _, err := Check(twoSites(t, `"AWS_", "LOOPZERO_"`, `"LOOPZERO_", "AWS_"`)); err != nil {
		t.Errorf("the same set written in a different order is the same rule; got %v", err)
	}
}

// EC-6: repetition is not part of the rule either.
func TestCheck_DuplicateWithinOneListIsNormalised(t *testing.T) {
	if _, err := Check(twoSites(t, `"LOOPZERO_", "LOOPZERO_", "AWS_"`, `"LOOPZERO_", "AWS_"`)); err != nil {
		t.Errorf("a repeated element does not change the set; got %v", err)
	}
}

// R4 / negative control: a declaration that is no longer a plain list must be a
// LOUD error. Passing because the subject could not be found is the shape this
// whole slice exists to remove.
func TestCheck_MissingLiteralIsAnError(t *testing.T) {
	root := t.TempDir()
	writeFile := func(rel, body string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("a/a.go", "package a\n\nvar reservedEnvKeyPrefixes = []string{\"LOOPZERO_\"}\n")
	// The symbol exists but is built by a call, not declared as a literal.
	writeFile("b/b.go", "package b\n\nfunc load() []string { return nil }\n\nvar reservedEnvKeyPrefixes = load()\n")

	cfg := Config{RepoRoot: root, Sites: []Site{
		{RelPath: "a/a.go", Symbol: "reservedEnvKeyPrefixes"},
		{RelPath: "b/b.go", Symbol: "reservedEnvKeyPrefixes"},
	}}
	if _, err := Check(cfg); !errors.Is(err, ErrDeclarationNotFound) {
		t.Errorf("a declaration that is not a plain literal must be a typed error, never a silent pass; got %v", err)
	}
}

func TestCheck_AbsentFileIsAnError(t *testing.T) {
	cfg := Config{RepoRoot: t.TempDir(), Sites: []Site{{RelPath: "nope/x.go", Symbol: "reservedEnvKeyPrefixes"}}}
	if _, err := Check(cfg); err == nil {
		t.Errorf("a missing site must fail; a gate that cannot read its subject has verified nothing")
	}
}

// The symbol name must be matched exactly — a comment or a string mentioning it
// is not the declaration. This is why the extractor is AST-based.
func TestCheck_MentionInACommentIsNotTheDeclaration(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a", "a.go")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	body := "package a\n\n// reservedEnvKeyPrefixes lives elsewhere now.\nconst note = \"reservedEnvKeyPrefixes\"\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{RepoRoot: root, Sites: []Site{{RelPath: "a/a.go", Symbol: "reservedEnvKeyPrefixes"}}}
	if _, err := Check(cfg); !errors.Is(err, ErrDeclarationNotFound) {
		t.Errorf("a comment or string mentioning the symbol is not a declaration; got %v", err)
	}
}

// Against the real tree. The two copies still diverge on GOOGLE_, and the
// DIRECTION of that fix remains a pending owner decision (B-051 /
// MEET-ASYNC-AMENDMENT-2026-08-11-001 dado A2): the two directions have opposite
// blast radius — adding it to the linter breaks tenant builds that pass today,
// removing it from the API weakens find_sec_004.
//
// What changed on 2026-08-12 (B-070) is where that gap LIVES. It used to keep
// the whole gate unwired, so none of the three axes was watched. It is now a
// dated exemption, and the gate runs.
//
// So this test asserts something STRONGER than the old "it fails": the
// divergence must still be measured, must be reported as exempt, and must NOT
// vanish. A gate that passed because it stopped looking would satisfy an
// err==nil assertion; it cannot satisfy this one.
func TestCheck_RealTreeGoogleDivergenceIsExemptNotGone(t *testing.T) {
	root := repoRoot(t)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("err = %v; the only divergence is exempt and the case axis agrees", err)
	}
	if len(out.Exempted) != 1 || out.Exempted[0] != "GOOGLE_" {
		t.Fatalf("Exempted = %v; want exactly [GOOGLE_] — if this is empty the gate stopped "+
			"measuring the divergence rather than exempting it", out.Exempted)
	}
	if len(out.CaseFindings) != 0 {
		t.Errorf("CaseFindings = %+v; the real tree must agree on case matching after B-070", out.CaseFindings)
	}
	var api, lint []string
	for _, d := range out.Declarations {
		if strings.HasPrefix(d.File, "api/") {
			api = d.Prefixes
		} else {
			lint = d.Prefixes
		}
	}
	if len(api) != 5 || len(lint) != 4 {
		t.Errorf("declarations = api:%v lint:%v; want 5 and 4 — the exemption records a real gap, "+
			"and if the sets became equal the exemption must be RETIRED, not left standing", api, lint)
	}
}

// repoRoot walks up until it finds go.work, so the test does not depend on the
// working directory the runner chose.
//
// Not finding it is a FAILURE, never a skip. A skip here would make the one test
// that measures the REAL tree pass by not running — which is precisely the
// vacuous-pass shape this whole slice exists to remove (see B-040:
// `route-auth-coverage: PASS — 0 routes`). The same reasoning is already written
// into TestOracle_MatchesFrozenBaseline: "Absence is a FAILURE, not a skip …
// a skip would report green while the protection silently vanished."
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	start := dir
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("go.work not found within 8 levels above %s — the real-tree assertion cannot run, and skipping it would report green while measuring nothing", start)
	return ""
}

// --- B-070: the CASE axis, and the dated exemption on the SET axis ----------

const siteTemplate = `package p

var reservedEnvKeyPrefixes = []string{
%s}

func containsReservedEnvKey(line string) (bool, string) {
	upper := strings.ToUpper(line) // instruction keyword — legitimately folded
	_ = upper
	for _, p := range reservedEnvKeyPrefixes {
		if strings.HasPrefix(%s, p) {
			return true, line
		}
	}
	return false, ""
}
`

func writeSite(t *testing.T, dir, name, prefixes, keyExpr string) string {
	t.Helper()
	body := ""
	for _, p := range strings.Split(prefixes, ",") {
		body += "\t\"" + p + "\",\n"
	}
	rel := name + ".go"
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(fmt.Sprintf(siteTemplate, body, keyExpr)), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

func caseSites(t *testing.T, aPrefixes, aKey, bPrefixes, bKey string) (string, Config) {
	t.Helper()
	dir := t.TempDir()
	ra := writeSite(t, dir, "a", aPrefixes, aKey)
	rb := writeSite(t, dir, "b", bPrefixes, bKey)
	return dir, Config{
		RepoRoot:      dir,
		Sites:         []Site{{RelPath: ra, Symbol: "reservedEnvKeyPrefixes"}, {RelPath: rb, Symbol: "reservedEnvKeyPrefixes"}},
		AllowlistPath: filepath.Join(dir, "none.txt"),
		Now:           time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC),
	}
}

// The axis this slice decided: one side folding case must fail.
func TestCheck_CaseFoldingInMatchFails(t *testing.T) {
	_, cfg := caseSites(t, "AWS_,DO_", "line", "AWS_,DO_", "strings.ToUpper(line)")
	out, err := Check(cfg)
	if !errors.Is(err, ErrCaseFoldingInMatch) {
		t.Fatalf("err = %v; want ErrCaseFoldingInMatch", err)
	}
	if len(out.CaseFindings) != 1 || out.CaseFindings[0].Call != "strings.ToUpper" {
		t.Errorf("findings = %+v; want exactly one strings.ToUpper", out.CaseFindings)
	}
	if out.CaseFindings[0].Line == 0 {
		t.Error("finding carries no line; a gate that cannot say WHERE forces a manual hunt")
	}
}

// The distinction that makes the check precise rather than a grep: folding the
// INSTRUCTION keyword is correct (Dockerfile instructions are case-insensitive)
// and lives one line from the key comparison.
func TestCheck_FoldingOutsideTheLoopIsNotAFinding(t *testing.T) {
	_, cfg := caseSites(t, "AWS_,DO_", "line", "AWS_,DO_", "line")
	if _, err := Check(cfg); err != nil {
		t.Fatalf("err = %v; the strings.ToUpper on the instruction keyword must not count", err)
	}
}

// EC-4: the exemption names a PREFIX, not the axis.
func TestCheck_ExemptionDoesNotCoverAnotherPrefix(t *testing.T) {
	dir, cfg := caseSites(t, "AWS_,GOOGLE_,AZURE_", "line", "AWS_", "line")
	al := filepath.Join(dir, "allow.txt")
	os.WriteFile(al, []byte("GOOGLE_ | 2026-11-09 | known gap\n"), 0o644)
	cfg.AllowlistPath = al

	out, err := Check(cfg)
	if !errors.Is(err, ErrPrefixSetsDiverge) {
		t.Fatalf("err = %v; AZURE_ diverges and is not exempt, so the gate must fail", err)
	}
	if len(out.Exempted) != 1 || out.Exempted[0] != "GOOGLE_" {
		t.Errorf("Exempted = %v; want [GOOGLE_]", out.Exempted)
	}
}

func TestCheck_ExemptionCoversItsOwnPrefix(t *testing.T) {
	dir, cfg := caseSites(t, "AWS_,GOOGLE_", "line", "AWS_", "line")
	al := filepath.Join(dir, "allow.txt")
	os.WriteFile(al, []byte("GOOGLE_ | 2026-11-09 | known gap\n"), 0o644)
	cfg.AllowlistPath = al
	if _, err := Check(cfg); err != nil {
		t.Fatalf("err = %v; the only divergence is exempt and unexpired", err)
	}
}

// EC-3: the date is the whole point of a dated exemption.
func TestCheck_ExpiredExemptionStopsCovering(t *testing.T) {
	dir, cfg := caseSites(t, "AWS_,GOOGLE_", "line", "AWS_", "line")
	al := filepath.Join(dir, "allow.txt")
	os.WriteFile(al, []byte("GOOGLE_ | 2026-01-01 | known gap\n"), 0o644)
	cfg.AllowlistPath = al
	if _, err := Check(cfg); !errors.Is(err, ErrPrefixSetsDiverge) {
		t.Fatalf("err = %v; the exemption expired 2026-01-01 and the clock is 2026-08-12", err)
	}
}

// A malformed entry must be loud. A silently-skipped line is an exemption that
// looks present and is not — the worst of both.
func TestCheck_MalformedAllowlistEntryIsAnError(t *testing.T) {
	dir, cfg := caseSites(t, "AWS_", "line", "AWS_", "line")
	al := filepath.Join(dir, "allow.txt")
	os.WriteFile(al, []byte("GOOGLE_ only two fields\n"), 0o644)
	cfg.AllowlistPath = al
	if _, err := Check(cfg); err == nil {
		t.Fatal("a malformed allowlist entry passed silently")
	}
}
