// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkTemplates(t *testing.T, root, chartPath string, files ...string) {
	t.Helper()
	dir := filepath.Join(root, chartPath, "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, f := range files {
		// O nome do arquivo E o kind na fixture: `ConfigMap.yaml` -> `kind: ConfigMap`.
		//
		// As fixtures escreviam `kind: X` em TODOS os arquivos, o que sob a
		// comparacao por recurso (B-110) nao e deriva nenhuma — elas diferiam por
		// NOME, que e exatamente o que o gate parou de medir. E derivar o kind
		// capitalizando a primeira letra me deu `Configmap` contra o `ConfigMap`
		// do registro: a mesma diferenca de grafia que este item existe para
		// deixar de tratar como fato.
		kind := strings.TrimSuffix(f, ".yaml")
		if err := os.WriteFile(filepath.Join(dir, f), []byte("kind: "+kind+"\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

// TestCheck_NoDrift_Pass — identical templates → no drift.
func TestCheck_NoDrift_Pass(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-foo", "Deployment.yaml", "Service.yaml")
	mkTemplates(t, root, "infra/helm/loopzero-foo", "Deployment.yaml", "Service.yaml")
	o, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (no drift), got %v", o.Drifts)
	}
}

// TestCheck_UnapprovedDrift_Fail — differing templates without ADR → FAIL.
func TestCheck_UnapprovedDrift_Fail(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-foo", "Deployment.yaml", "Service.yaml", "Secret.yaml")
	mkTemplates(t, root, "infra/helm/loopzero-foo", "Deployment.yaml")
	o, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (unapproved drift)")
	}
	if len(o.Drifts) != 1 || o.Drifts[0].Intentional {
		t.Errorf("expected 1 unintentional drift, got %v", o.Drifts)
	}
}

// TestCheck_ApprovedDrift_Pass — drift with approving ADR → PASS (intentional).
//
// Updated 2026-08-12 (slice-3 T1.4, ADR D2): the approval is now keyed by FILE,
// not by pair, so this test names files the registry actually approves. The old
// fixture used `rbac.yaml` / `monitoring.yaml` — names that never existed in
// either chart — and passed because the PAIR was blessed. That is the exact
// defect the change removes, and a fixture asserting it had to change with it.
func TestCheck_ApprovedDrift_Pass(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-api", "Deployment.yaml", "ConfigMap.yaml", "Secret.yaml")
	mkTemplates(t, root, "infra/helm/loopzero-api", "Deployment.yaml")
	o, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("esperava Pass (a deriva do loopzero-api esta aprovada por kind), veio %v", o.Drifts)
	}
	if len(o.Drifts) != 1 || !o.Drifts[0].Intentional || o.Drifts[0].ADR != "B-110" {
		t.Errorf("esperava deriva intencional assinada por B-110, veio %v", o.Drifts)
	}
}

// TestCheck_ConceptualPair — loopzero-admission-policies ↔ admission-policies.
func TestCheck_ConceptualPair(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-admission-policies", "ConfigMap.yaml", "Secret.yaml")
	mkTemplates(t, root, "infra/helm/admission-policies", "ConfigMap.yaml")
	o, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// drift exists, no ADR → fail, but the pair must be DETECTED (conceptual mapping)
	found := false
	for _, d := range o.Drifts {
		if d.Pair == "loopzero-admission-policies:admission-policies" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected conceptual pair detected, got %v", o.Drifts)
	}
}

// TestCheck_OnlyPublishedExists_NoCrash — bootstrap dir absent → no pair, no drift.
func TestCheck_OnlyPublishedExists_NoCrash(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-orphan", "a.yaml")
	// no infra/helm/loopzero-orphan
	o, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (no matching bootstrap → not a pair), got %v", o.Drifts)
	}
}

// T1.4 / B-046 RED: an ADR approving a PAIR approves every file that pair will
// ever drift on — including files nobody has seen. That is how two ExternalSecret
// templates and a ValidatingAdmissionPolicy left the published chart without
// anyone noticing: the pair was already blessed, so the gate stopped looking.
//
// The approval must name the FILE.
func TestApprovedPair_DoesNotBlessAnUnseenFile(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	touch := func(rel, kind string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("kind: "+kind+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("charts/loopzero-api/templates")
	mk("infra/helm/loopzero-api/templates")
	// Um recurso APROVADO ao lado de um que NENHUMA aprovacao cobre. O par
	// loopzero-api tem entradas no registro, e a pergunta e se ter entradas o
	// absolve do recurso seguinte — foi assim que uma ValidatingAdmissionPolicy
	// sumiu do chart publicado sem ninguem notar.
	touch("charts/loopzero-api/templates/a.yaml", "ConfigMap")
	touch("charts/loopzero-api/templates/b.yaml", "CronJob")

	out, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if out.Pass {
		t.Errorf("a drifted file with no per-file approval must FAIL the gate; blessing it because the PAIR was blessed is how the VAP went missing (drifts=%+v)", out.Drifts)
	}
}

// TestEveryApprovalIsMeasuredAssimetry replaces the pair of tests that guarded the
// old filename-keyed registry.
//
// Those asserted one of THREE measured classes — both charts render it (filename
// drift), neither renders it under default values (conditional), exactly one
// renders it (real asymmetry). Keying by KIND collapses the first two by
// construction: a kind only appears here when exactly one side installs it. So
// the contract that survives is narrower and stronger — every entry names an ADR
// and says what the two charts actually do differently.
func TestEveryApprovalIsMeasuredAssimetry(t *testing.T) {
	if len(approvedDriftKinds) == 0 {
		t.Fatal("nenhuma aprovação para checar — o teste passaria por vacuidade")
	}
	for key, a := range approvedDriftKinds {
		if a.ADR == "" {
			t.Errorf("%s não carrega ADR — uma aprovação que ninguém assinou", key)
		}
		if len(strings.TrimSpace(a.Reason)) < 30 {
			t.Errorf("%s: razão %q é curta demais para dizer o que foi observado sobre os dois charts",
				key, a.Reason)
		}
		// A chave tem de ser `publicado:bootstrap:KIND`, e o kind e' um nome de
		// recurso do Kubernetes — comeca em maiuscula. Uma chave com nome de
		// arquivo (`.yaml`) seria uma entrada da era anterior sobrevivendo a
		// migracao, e ela aprovaria algo que o gate nao mede mais.
		parts := strings.Split(key, ":")
		if len(parts) != 3 {
			t.Errorf("%s não tem a forma `publicado:bootstrap:KIND`", key)
			continue
		}
		kind := parts[2]
		if strings.Contains(kind, ".") || kind == "" || kind[0] < 'A' || kind[0] > 'Z' {
			t.Errorf("%s: %q não parece um kind do Kubernetes — uma entrada com nome de arquivo "+
				"seria resíduo da chave anterior, aprovando algo que o gate não mede mais", key, kind)
		}
	}
}

// fixtureRenderer devolve o conteudo dos arquivos de `templates/` como se fossem
// o manifesto renderizado.
//
// Os testes de Check montam fixtures em disco que NAO sao charts validos — nao
// tem Chart.yaml, nem values. O helmRenderer real falharia em todos, e a falha
// seria do fixture, nao do contrato sob teste. Injetar o duplo e o que permite a
// esses testes continuarem afirmando o que sempre afirmaram (deriva aprovada,
// deriva sem aprovacao, par conceitual) sobre uma base de comparacao nova.
type fixtureRenderer struct{}

// The `sets` are ignored: the fixture files on disk ARE the rendered output, so
// there is no template for a value to reach. Ignoring them in plain sight beats a
// second interface the fixture would not implement — a hermetic test of the
// COMPARISON has no values to vary, and the matrix is exercised by the real-tree
// test that renders real charts.
func (fixtureRenderer) Render(chartDir string, _ []string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(chartDir, "templates"))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		raw, rerr := os.ReadFile(filepath.Join(chartDir, "templates", e.Name()))
		if rerr != nil {
			return "", rerr
		}
		b.WriteString("---\n")
		b.Write(raw)
	}
	return b.String(), nil
}
