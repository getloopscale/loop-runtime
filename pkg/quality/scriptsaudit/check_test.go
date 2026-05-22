// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scriptsaudit

import (
	"bytes"
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// fixtureRepo materializa um repo sintético em t.TempDir com a estrutura
// mínima necessária para exercitar ScanRepo + DetectCallers + ProposeDisposition.
// Retorna o root path; cleanup é automático via t.TempDir().
func fixtureRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for relPath, content := range files {
		full := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return root
}

// TestScanRepo_FindsAllShellScripts — baseline assertion: ScanRepo retorna
// todos os .sh do repo, com Path + LOC + LastMod populados.
func TestScanRepo_FindsAllShellScripts(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/a.sh":           "#!/bin/bash\necho a\n",
		"infra/scripts/b.sh":           "#!/bin/bash\necho b\necho line3\n",
		"infra/tests/scripts/c_test.sh": "#!/bin/bash\necho c\n",
		"README.md":                    "# not a shell script",
	})

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	if len(scripts) != 3 {
		t.Errorf("expected 3 scripts, got %d", len(scripts))
	}
	for _, s := range scripts {
		if !strings.HasSuffix(s.Path, ".sh") {
			t.Errorf("Path must end in .sh, got %s", s.Path)
		}
		if s.LOC <= 0 {
			t.Errorf("LOC must be > 0, got %d for %s", s.LOC, s.Path)
		}
		if s.LastMod.IsZero() {
			t.Errorf("LastMod must be set for %s", s.Path)
		}
	}
}

// TestScanRepo_ExcludesNodeModules — vendor/, node_modules/, .git/ devem ser
// excluídos da varredura.
func TestScanRepo_ExcludesNodeModules(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/real.sh":        "#!/bin/bash\necho real\n",
		"node_modules/foo/bin/foo.sh":  "#!/bin/bash\necho dep\n",
		"vendor/x/script.sh":           "#!/bin/bash\necho vendor\n",
		".git/hooks/pre-commit.sample": "#!/bin/sh\necho git\n",
	})

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	if len(scripts) != 1 {
		t.Errorf("expected 1 script (excluding node_modules/vendor/.git), got %d", len(scripts))
		for _, s := range scripts {
			t.Logf("  found: %s", s.Path)
		}
	}
}

// TestScanRepo_HandlesSymlinks_NoCycle — EC-07: symlinks são deduplicados
// (script real é listado UMA vez) e não causam ciclo.
func TestScanRepo_HandlesSymlinks_NoCycle(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/real.sh": "#!/bin/bash\necho real\n",
	})
	// Criar symlink apontando para real.sh
	linkPath := filepath.Join(root, "infra/scripts/alias.sh")
	if err := os.Symlink(filepath.Join(root, "infra/scripts/real.sh"), linkPath); err != nil {
		t.Skipf("symlink not supported on this filesystem: %v", err)
	}

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	// O contrato: lista UMA vez (não duas). Aceitamos seja o real ou o alias
	// como path canônico — mas NUNCA ambos. Implementação resolve via EvalSymlinks.
	if len(scripts) > 1 {
		t.Errorf("expected 1 unique script after symlink dedup, got %d", len(scripts))
		for _, s := range scripts {
			t.Logf("  found: %s", s.Path)
		}
	}
	if len(scripts) == 0 {
		t.Errorf("expected at least 1 script, got 0")
	}
}

// TestDetectCallers_FindsTaskfileRefs — script invocado por Taskfile.yml ou
// taskfiles/*.yml deve aparecer com o caller correspondente.
func TestDetectCallers_FindsTaskfileRefs(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/target.sh": "#!/bin/bash\necho target\n",
		"Taskfile.yml": `
version: '3'
tasks:
  build:
    cmds:
      - bash infra/scripts/target.sh
`,
	})

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var target *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "target.sh") {
			target = &scripts[i]
			break
		}
	}
	if target == nil {
		t.Fatal("target.sh not found")
	}
	if len(target.Callers) == 0 {
		t.Errorf("expected caller for target.sh, got 0 callers")
	}
	foundTaskfile := false
	for _, c := range target.Callers {
		if strings.Contains(c, "Taskfile.yml") {
			foundTaskfile = true
		}
	}
	if !foundTaskfile {
		t.Errorf("expected Taskfile.yml as caller, got %v", target.Callers)
	}
}

// TestDetectCallers_FindsCIYamlRefs — script referenciado em
// .github/workflows/*.yml deve aparecer com o caller correspondente.
func TestDetectCallers_FindsCIYamlRefs(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/ci-tool.sh": "#!/bin/bash\necho ci\n",
		".github/workflows/test.yml": `
jobs:
  test:
    steps:
      - run: bash infra/scripts/ci-tool.sh
`,
	})

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var target *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "ci-tool.sh") {
			target = &scripts[i]
			break
		}
	}
	if target == nil {
		t.Fatal("ci-tool.sh not found")
	}
	foundCI := false
	for _, c := range target.Callers {
		if strings.Contains(c, "test.yml") {
			foundCI = true
		}
	}
	if !foundCI {
		t.Errorf("expected workflows/test.yml as caller, got %v", target.Callers)
	}
}

// TestDetectCallers_FindsCrossScriptRefs — script A.sh que invoca B.sh
// deve criar caller A em B.
func TestDetectCallers_FindsCrossScriptRefs(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/parent.sh": "#!/bin/bash\nbash infra/scripts/child.sh\n",
		"infra/scripts/child.sh":  "#!/bin/bash\necho child\n",
	})

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var child *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "child.sh") {
			child = &scripts[i]
			break
		}
	}
	if child == nil {
		t.Fatal("child.sh not found")
	}
	foundParent := false
	for _, c := range child.Callers {
		if strings.Contains(c, "parent.sh") {
			foundParent = true
		}
	}
	if !foundParent {
		t.Errorf("expected parent.sh as caller of child.sh, got %v", child.Callers)
	}
}

// TestProposeDisposition_DeadCode — script com 0 callers + last-mod > 90d
// é classificado como DELETE-DEAD.
func TestProposeDisposition_DeadCode(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	s := Script{
		Path:    "infra/scripts/old-dead.sh",
		LOC:     50,
		LastMod: old,
		Callers: nil,
	}
	disp := ProposeDisposition(s)
	if disp != "DELETE-DEAD" {
		t.Errorf("expected DELETE-DEAD, got %s", disp)
	}
}

// TestProposeDisposition_HermeticTest — X_test.sh é classificado como AUTO-DIE
// (morre com o pai X.sh).
func TestProposeDisposition_HermeticTest(t *testing.T) {
	s := Script{
		Path:    "infra/tests/scripts/foo_test.sh",
		LOC:     30,
		LastMod: time.Now(),
	}
	disp := ProposeDisposition(s)
	if disp != "AUTO-DIE" {
		t.Errorf("expected AUTO-DIE for hermetic test pattern, got %s", disp)
	}
}

// TestProposeDisposition_BareHelmWrapper — script com conteúdo `helm upgrade`
// é classificado como DELEGATE-HELMFILE (per D2).
func TestProposeDisposition_BareHelmWrapper(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "install.sh")
	body := "#!/bin/bash\nhelm upgrade --install foo charts/foo --namespace foo\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{
		Path:    path,
		LOC:     2,
		LastMod: time.Now(),
	}
	disp := ProposeDisposition(s)
	if disp != "DELEGATE-HELMFILE" {
		t.Errorf("expected DELEGATE-HELMFILE, got %s", disp)
	}
}

// TestProposeDisposition_PureLogicGate — script pattern `check-*.sh` em
// infra/scripts/ é candidato a MIGRATE-GO-PUREFN.
func TestProposeDisposition_PureLogicGate(t *testing.T) {
	s := Script{
		Path:    "infra/scripts/check-prod-image-tags.sh",
		LOC:     92,
		LastMod: time.Now(),
		Callers: []string{".github/workflows/ci.yml"},
	}
	disp := ProposeDisposition(s)
	if disp != "MIGRATE-GO-PUREFN" {
		t.Errorf("expected MIGRATE-GO-PUREFN, got %s", disp)
	}
}

// TestProposeDisposition_CheckScriptThatShellsOut_NotPureFn — T0.1
// false-positive-awareness: a `check-*.sh`/`validate-*.sh` that invokes an
// external CLI (kubectl/helm/cosign/doctl/pulumi/argocd) is NOT pure-logic;
// migrating it to Go would be a forbidden shell-wrap (parent D5). It must NOT
// be classified MIGRATE-GO-PUREFN.
func TestProposeDisposition_CheckScriptThatShellsOut_NotPureFn(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "check-prod-vap-readiness.sh")
	body := "#!/bin/bash\nkubectl get validatingadmissionpolicy\ncosign verify $IMG\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{Path: path, LOC: 3, LastMod: time.Now(), Callers: []string{".github/workflows/x.yml"}}
	if disp := ProposeDisposition(s); disp == "MIGRATE-GO-PUREFN" {
		t.Errorf("check-* that shells out must NOT be MIGRATE-GO-PUREFN, got %s", disp)
	}
}

// TestProposeDisposition_PureCheckScript_StaysPureFn — converse: a check-*.sh
// with NO external CLI invocation remains a MIGRATE-GO-PUREFN candidate.
func TestProposeDisposition_PureCheckScript_StaysPureFn(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "check-prod-image-tags.sh")
	body := "#!/bin/bash\ngrep -r ':latest' manifests/ && exit 1\nexit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{Path: path, LOC: 3, LastMod: time.Now(), Callers: []string{".github/workflows/x.yml"}}
	if disp := ProposeDisposition(s); disp != "MIGRATE-GO-PUREFN" {
		t.Errorf("pure check-* must stay MIGRATE-GO-PUREFN, got %s", disp)
	}
}

// TestProposeDisposition_GeneratorThatShellsOut_NotPureGenerator — T0.1
// false-positive-awareness: a `generate-*.sh` that injects secrets via kubectl
// is a bootstrap injector, NOT a pure file generator. It belongs to the
// helmfile-delegation slice, not a hermetic go:generate migration.
func TestProposeDisposition_GeneratorThatShellsOut_NotPureGenerator(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "generate-secrets.sh")
	body := "#!/bin/bash\nPW=$(openssl rand -hex 16)\nkubectl create secret generic foo --from-literal=pw=$PW\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{Path: path, LOC: 3, LastMod: time.Now(), Callers: []string{"Taskfile.yml"}}
	if disp := ProposeDisposition(s); disp == "MIGRATE-GENERATOR" {
		t.Errorf("generate-* that shells out must NOT be MIGRATE-GENERATOR, got %s", disp)
	}
}

// TestProposeDisposition_PureGenerator_StaysGenerator — converse: a generate-*
// with no external CLI (writes files only) remains MIGRATE-GENERATOR.
func TestProposeDisposition_PureGenerator_StaysGenerator(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gen-state-machine-dots.sh")
	body := "#!/bin/bash\nfor m in build release; do echo \"digraph $m {}\" > docs/$m.dot; done\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{Path: path, LOC: 2, LastMod: time.Now(), Callers: []string{"Taskfile.yml"}}
	if disp := ProposeDisposition(s); disp != "MIGRATE-GENERATOR" {
		t.Errorf("pure generate-* must stay MIGRATE-GENERATOR, got %s", disp)
	}
}

// TestProposeDisposition_ScriptWithDynamicCaller_NotMarkedDead — EC-03:
// Quando script é referenciado via interpolação dinâmica (`bash "$VAR"`),
// caller_count fica em zero (regex não pega), mas a heurística NÃO deve marcar
// como DELETE-DEAD para esses casos. Esta validação acontece em T1.1 via grep
// dinâmico; aqui validamos que CallerCount > 0 derruba DELETE-DEAD.
func TestProposeDisposition_ScriptWithDynamicCaller_NotMarkedDead(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	s := Script{
		Path:    "infra/scripts/lib/dispatcher.sh",
		LOC:     50,
		LastMod: old,
		Callers: []string{"infra/scripts/main.sh"}, // tem caller documentado
	}
	disp := ProposeDisposition(s)
	if disp == "DELETE-DEAD" {
		t.Errorf("script with caller should not be DELETE-DEAD, got %s", disp)
	}
}

// TestEmitCSV_StableSchema — CSV emite header e linhas com o schema canônico:
// path, loc, last_mod_utc, caller_count, disposition, callers
func TestEmitCSV_StableSchema(t *testing.T) {
	scripts := []Script{
		{
			Path:        "a.sh",
			LOC:         10,
			LastMod:     time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
			Callers:     []string{"Taskfile.yml"},
			Disposition: "KEEP-OPERATOR",
		},
	}
	var buf bytes.Buffer
	if err := EmitCSV(scripts, &buf); err != nil {
		t.Fatalf("EmitCSV: %v", err)
	}
	r := csv.NewReader(&buf)
	header, err := r.Read()
	if err != nil {
		t.Fatalf("read header: %v", err)
	}
	expected := []string{"path", "loc", "last_mod_utc", "caller_count", "disposition", "callers"}
	if len(header) != len(expected) {
		t.Errorf("header columns mismatch: got %v, want %v", header, expected)
	}
	for i, col := range expected {
		if i >= len(header) || header[i] != col {
			t.Errorf("header[%d]: got %q, want %q", i, header[i], col)
		}
	}
	// Read the data row
	row, err := r.Read()
	if err == io.EOF {
		t.Fatal("expected 1 data row, got EOF")
	}
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row[0] != "a.sh" {
		t.Errorf("path: got %q, want a.sh", row[0])
	}
	if row[1] != "10" {
		t.Errorf("loc: got %q, want 10", row[1])
	}
	if row[3] != "1" {
		t.Errorf("caller_count: got %q, want 1", row[3])
	}
	if row[4] != "KEEP-OPERATOR" {
		t.Errorf("disposition: got %q, want KEEP-OPERATOR", row[4])
	}
}

// TestEmitCSV_DeterministicOrder — output ordenado por path (estável em diff).
func TestEmitCSV_DeterministicOrder(t *testing.T) {
	scripts := []Script{
		{Path: "zeta.sh", LOC: 1, LastMod: time.Now(), Disposition: "X"},
		{Path: "alpha.sh", LOC: 1, LastMod: time.Now(), Disposition: "Y"},
		{Path: "mid.sh", LOC: 1, LastMod: time.Now(), Disposition: "Z"},
	}
	var buf bytes.Buffer
	if err := EmitCSV(scripts, &buf); err != nil {
		t.Fatalf("EmitCSV: %v", err)
	}
	r := csv.NewReader(&buf)
	if _, err := r.Read(); err != nil {
		t.Fatalf("read header: %v", err)
	}
	var paths []string
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		paths = append(paths, row[0])
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("paths not sorted: %v", paths)
	}
	want := []string{"alpha.sh", "mid.sh", "zeta.sh"}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths[%d]: got %q, want %q", i, paths[i], want[i])
		}
	}
}

// === Additional coverage tests (push to >=90%) ===

// TestProposeDisposition_DelegateArgoCD — script with kubectl apply -f argocd
func TestProposeDisposition_DelegateArgoCD(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tenant-sync.sh")
	body := "#!/bin/bash\nkubectl apply -f infra/argocd/tenants/\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{Path: path, LOC: 2, LastMod: time.Now()}
	if disp := ProposeDisposition(s); disp != "DELEGATE-ARGOCD" {
		t.Errorf("expected DELEGATE-ARGOCD, got %s", disp)
	}
}

// TestProposeDisposition_MigrateGoSDK — pulumi-*.sh
func TestProposeDisposition_MigrateGoSDK(t *testing.T) {
	s := Script{Path: "infra/scripts/pulumi-archive.sh", LOC: 50, LastMod: time.Now()}
	if disp := ProposeDisposition(s); disp != "MIGRATE-GO-SDK" {
		t.Errorf("expected MIGRATE-GO-SDK, got %s", disp)
	}
}

// TestProposeDisposition_Generator — gen-*.sh
func TestProposeDisposition_Generator(t *testing.T) {
	s := Script{Path: "infra/scripts/gen-state-machine-dots.sh", LOC: 30, LastMod: time.Now()}
	if disp := ProposeDisposition(s); disp != "MIGRATE-GENERATOR" {
		t.Errorf("expected MIGRATE-GENERATOR for gen-*, got %s", disp)
	}
	s2 := Script{Path: "infra/scripts/generate-secrets.sh", LOC: 20, LastMod: time.Now()}
	if disp := ProposeDisposition(s2); disp != "MIGRATE-GENERATOR" {
		t.Errorf("expected MIGRATE-GENERATOR for generate-*, got %s", disp)
	}
}

// TestProposeDisposition_MigrateGinkgo — test orchestrators
func TestProposeDisposition_MigrateGinkgo(t *testing.T) {
	s := Script{Path: "infra/tests/parity/validate-matrix.sh", LOC: 80, LastMod: time.Now()}
	if disp := ProposeDisposition(s); disp != "MIGRATE-GINKGO" {
		t.Errorf("expected MIGRATE-GINKGO for parity test, got %s", disp)
	}
	s2 := Script{Path: "infra/tests/dogfood/journey-01.sh", LOC: 60, LastMod: time.Now()}
	if disp := ProposeDisposition(s2); disp != "MIGRATE-GINKGO" {
		t.Errorf("expected MIGRATE-GINKGO for dogfood, got %s", disp)
	}
}

// TestProposeDisposition_KeepOperatorBreakGlass — break-glass small script
func TestProposeDisposition_KeepOperatorBreakGlass(t *testing.T) {
	s := Script{Path: "infra/scripts/break-glass.sh", LOC: 50, LastMod: time.Now()}
	if disp := ProposeDisposition(s); disp != "KEEP-OPERATOR" {
		t.Errorf("expected KEEP-OPERATOR for break-glass, got %s", disp)
	}
}

// TestProposeDisposition_ReviewNeededDefault — script not matching any heuristic
func TestProposeDisposition_ReviewNeededDefault(t *testing.T) {
	s := Script{Path: "infra/scripts/random-utility.sh", LOC: 200, LastMod: time.Now(), Callers: []string{"X"}}
	if disp := ProposeDisposition(s); disp != "REVIEW-NEEDED" {
		t.Errorf("expected REVIEW-NEEDED default, got %s", disp)
	}
}

// TestIsolatedStemPresent — direct test of stem-only matching with various prefixes
func TestIsolatedStemPresent(t *testing.T) {
	cases := []struct {
		text string
		stem string
		want bool
	}{
		{"bash /foo/scripts/install.sh", "install", true},  // preceded by /
		{"  install runs here", "install", true},            // preceded by space
		{"\"install\" is the name", "install", true},        // preceded by "
		{"thisinstall is embedded", "install", false},       // no isolated prefix (prev is word char)
		{"abc", "ab", false},                                // stem too short (<4)
		{"foo_install_bar", "install", false},               // prev=_ is word-char → no isolated prefix
	}
	for _, c := range cases {
		got := isolatedStemPresent(c.text, c.stem)
		if got != c.want {
			t.Errorf("isolatedStemPresent(%q, %q) = %v, want %v", c.text, c.stem, got, c.want)
		}
	}
}

// TestCountLines_EmptyFile — empty file returns 0 lines
func TestCountLines_EmptyFile(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "empty.sh")
	if err := os.WriteFile(tmp, []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	n, err := countLines(tmp)
	if err != nil {
		t.Fatalf("countLines: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 lines, got %d", n)
	}
}

// TestCountLines_NoTrailingNewline — last line counted even without final \n
func TestCountLines_NoTrailingNewline(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "noeol.sh")
	if err := os.WriteFile(tmp, []byte("line1\nline2"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	n, err := countLines(tmp)
	if err != nil {
		t.Fatalf("countLines: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 lines (last without \\n), got %d", n)
	}
}

// === Phase 0 T0.1: false-positive-aware reference detection ===

// TestDetectsDynamicSourcedLib — EC false-positive class #1: a lib sourced via
// `source "$DIR/cell2-gate-${g}.sh"` must be credited a caller even though its
// literal basename never appears in the parent. Without this, the 13 cell2-gate
// libs false-positive to DELETE-DEAD.
func TestDetectsDynamicSourcedLib(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/tests/parity/lib/cell2-gate-c5.sh": "#!/bin/bash\necho gate5\n",
		"infra/tests/parity/cell2-activation-gates.sh": `#!/bin/bash
LIB_DIR="$(dirname "$0")/lib"
for g in c1 c5 c12; do
  source "$LIB_DIR/cell2-gate-${g}.sh"
done
`,
	})
	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var lib *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "cell2-gate-c5.sh") {
			lib = &scripts[i]
			break
		}
	}
	if lib == nil {
		t.Fatal("cell2-gate-c5.sh not found")
	}
	if len(lib.Callers) == 0 {
		t.Errorf("dynamically-sourced lib must have a caller, got 0 (false-positive DELETE-DEAD risk)")
	}
}

// TestDetectsGlobRunnerTestScripts — EC false-positive class #2: a test script
// run by a CI glob-runner (`for f in infra/tests/scripts/drill-*_test.sh`) must
// be credited a caller even though only the glob (not the literal name) appears.
func TestDetectsGlobRunnerTestScripts(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/tests/scripts/drill-mint-jwt_test.sh": "#!/bin/bash\necho t\n",
		".github/workflows/drill-hermetic-tests.yml": `
jobs:
  test:
    steps:
      - run: |
          for f in infra/tests/scripts/drill-*_test.sh; do bash "$f"; done
`,
	})
	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var ts *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "drill-mint-jwt_test.sh") {
			ts = &scripts[i]
			break
		}
	}
	if ts == nil {
		t.Fatal("drill-mint-jwt_test.sh not found")
	}
	if len(ts.Callers) == 0 {
		t.Errorf("glob-runner test script must have a caller, got 0")
	}
}

// TestDetectsDockerfileEntrypoint — EC false-positive class #3: a container
// entrypoint referenced by Dockerfile ENTRYPOINT must be credited a caller.
func TestDetectsDockerfileEntrypoint(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"cloud/dashboard/docker-entrypoint.sh": "#!/bin/sh\nexec node server.js\n",
		"cloud/dashboard/Dockerfile": `FROM node:20
COPY docker-entrypoint.sh /usr/local/bin/
ENTRYPOINT ["docker-entrypoint.sh"]
`,
	})
	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var ep *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "docker-entrypoint.sh") {
			ep = &scripts[i]
			break
		}
	}
	if ep == nil {
		t.Fatal("docker-entrypoint.sh not found")
	}
	foundDockerfile := false
	for _, c := range ep.Callers {
		if strings.Contains(c, "Dockerfile") {
			foundDockerfile = true
		}
	}
	if !foundDockerfile {
		t.Errorf("Dockerfile ENTRYPOINT must be a caller, got %v", ep.Callers)
	}
}

// TestDetectsHookRegistration — EC false-positive class #4: a Claude harness
// hook (.claude/hooks/*.sh) is registered with the harness, not invoked by
// literal basename; it must NOT be classified DELETE-DEAD even if old + 0-caller.
func TestDetectsHookRegistration(t *testing.T) {
	old := time.Now().Add(-200 * 24 * time.Hour)
	s := Script{
		Path:    ".claude/hooks/task-completed.sh",
		LOC:     100,
		LastMod: old,
		Callers: nil,
	}
	if disp := ProposeDisposition(s); disp == "DELETE-DEAD" {
		t.Errorf("harness hook must not be DELETE-DEAD, got %s", disp)
	}
}

// TestIsCallerSearchFile — exercises all branches
func TestIsCallerSearchFile(t *testing.T) {
	tests := map[string]bool{
		"Taskfile.yml":  true,
		"workflow.yml":  true,
		"config.yaml":   true,
		"helper.sh":     true,
		"chart.gotmpl":  true,
		"Tiltfile":      true,
		"Tiltfile.local": true,
		"README.md":     false,
		"main.go":       false,
	}
	for name, want := range tests {
		got := isCallerSearchFile(name)
		if got != want {
			t.Errorf("isCallerSearchFile(%q) = %v, want %v", name, got, want)
		}
	}
}
