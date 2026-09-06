// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package alertrunbooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rulesRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, DefaultRulesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

// cfg uses an empty exception list so tests are independent of the real default.
func cfg(root string) Config {
	return Config{Root: root, ExceptionList: []string{}}
}

func TestAlertWithRunbookURLPasses(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "groups:\n- rules:\n  - alert: GoodAlert\n    annotations:\n      runbook_url: https://docs/x\n",
	})
	if !Check(cfg(root)).Pass {
		t.Error("alert with runbook_url should pass")
	}
}

func TestAlertWithLegacyRunbookPasses(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: LegacyAlert\n    annotations:\n      runbook: see-wiki\n",
	})
	if !Check(cfg(root)).Pass {
		t.Error("legacy runbook: annotation should pass")
	}
}

func TestAlertMissingRunbookFails(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: NoRunbook\n    expr: up == 0\n",
	})
	o := Check(cfg(root))
	if o.Pass || len(o.Violations) != 1 {
		t.Errorf("expected 1 violation, got %v", o.Violations)
	}
	// O formato passou a carregar o diretorio em 2026-09-06 (B-067). Este teste fixava o
	// basename sozinho, e era exatamente essa forma que tornava dois sites espelhados
	// indistinguiveis no relatorio — ver TestViolacaoDistingueDiretoriosEspelhados.
	if o.Violations[0] != DefaultRulesDirs[0]+"/prometheusrule-x.yaml:NoRunbook" {
		t.Errorf("violation format wrong: %q", o.Violations[0])
	}
}

func TestExceptionListPasses(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: KnownGap\n    expr: up == 0\n",
	})
	o := Check(Config{Root: root, ExceptionList: []string{"KnownGap"}})
	if !o.Pass {
		t.Errorf("allowlisted alert should pass, got %v", o.Violations)
	}
}

func TestNonPrometheusRuleFileIgnored(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		// Filename lacks "prometheusrule" → not scanned.
		"servicemonitor.yaml": "spec:\n  - alert: Ignored\n    expr: up == 0\n",
	})
	if !Check(cfg(root)).Pass {
		t.Error("non-prometheusrule files must be ignored")
	}
}

func TestNonYAMLIgnored(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.txt": "spec:\n  - alert: Ignored\n    expr: up == 0\n",
	})
	if !Check(cfg(root)).Pass {
		t.Error(".txt files must be ignored")
	}
}

func TestMultipleAlertsMixed(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: Has\n    annotations:\n      runbook_url: u\n" +
			"  - alert: Missing1\n    expr: up == 0\n" +
			"  - alert: Missing2\n    expr: up == 1\n",
	})
	o := Check(cfg(root))
	if o.Pass || len(o.Violations) != 2 {
		t.Errorf("expected 2 violations (Missing1, Missing2), got %v", o.Violations)
	}
}

func TestRunbookBeyondLookaheadStillFails(t *testing.T) {
	// runbook_url placed >1500 chars into the block → outside the window → fail.
	pad := strings.Repeat("    # filler comment line\n", 80) // ~2000 chars
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: FarRunbook\n    expr: up == 0\n" + pad + "    runbook_url: late\n",
	})
	if Check(cfg(root)).Pass {
		t.Error("runbook beyond the 1500-char lookahead must still fail")
	}
}

// NENHUM diretorio de regras encontrado e' FALHA, e este teste afirmava o contrario ate'
// 2026-08-19 — chamava-se `TestMissingRulesDirTolerated` e exigia `Pass`.
//
// Tolerar era o "verde sobre nada": um gate que nao acha diretorio nenhum reporta
// exatamente o mesmo veredito de um que varreu tudo e nao achou defeito. E' a forma que o
// B-040 e o B-127 nomeiam, e ela estava aqui.
//
// A tolerancia por diretorio INDIVIDUAL continua — as arvores de teste tem um dos quatro —
// mas zero encontrados nao passa mais.
func TestNenhumDiretorioDeRegrasEFalhaEmVezDeVerde(t *testing.T) {
	root := t.TempDir()
	out := Check(cfg(root))
	if out.Pass {
		t.Error("varredura sem NENHUM diretorio de regras passou — verde sobre nada nao e' verde")
	}
	if out.DirsVarridos != 0 {
		t.Errorf("DirsVarridos=%d numa arvore vazia", out.DirsVarridos)
	}
}

// A contrapartida: UM diretorio presente entre os quatro basta, senao toda arvore de teste
// (que so' cria um) reprovaria por motivo errado.
func TestUmDiretorioPresenteEntreOsQuatroBasta(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: LoopZeroLockContention\n    expr: up == 0\n",
	})
	out := Check(Config{Root: root})
	if out.DirsVarridos != 1 {
		t.Errorf("queria 1 diretorio varrido, veio %d", out.DirsVarridos)
	}
	if !out.Pass {
		t.Errorf("um diretorio valido devia bastar: %+v", out.Violations)
	}
}

func TestDefaultExceptionListApplied(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		// A default-exception alert without runbook → passes via defaults.
		"prometheusrule-x.yaml": "spec:\n  - alert: LoopZeroLockContention\n    expr: up == 0\n",
	})
	if !Check(Config{Root: root}).Pass { // nil ExceptionList → defaults
		t.Error("default exception list should permit LoopZeroLockContention")
	}
}

// Dois diretorios varridos contem o MESMO nome de arquivo — `infra/helm/...` e o
// `charts/...` que o ArgoCD entrega sao copias espelhadas, e 9 pares assim existem na
// arvore real (medido 2026-09-06). Enquanto a violacao carregar so' o BASENAME, os dois
// sites produzem strings IDENTICAS: quem le' o relatorio nao sabe qual dos dois arquivos
// abrir, e consertar um deixa o outro reprovando com uma linha indistinguivel da que
// acabou de ser corrigida.
func TestViolacaoDistingueDiretoriosEspelhados(t *testing.T) {
	root := t.TempDir()
	corpo := "spec:\n  - alert: LoopZeroEspelhado\n    expr: up == 0\n"
	for _, d := range []string{DefaultRulesDirs[0], DefaultRulesDirs[1]} {
		dir := filepath.Join(root, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prometheusrule-x.yaml"), []byte(corpo), 0o644); err != nil {
			t.Fatalf("write %s: %v", d, err)
		}
	}
	out := Check(cfg(root))
	if len(out.Violations) != 2 {
		t.Fatalf("queria 2 sites em violacao, veio %d: %v", len(out.Violations), out.Violations)
	}
	if out.Violations[0] == out.Violations[1] {
		t.Errorf("os dois sites sao indistinguiveis: ambos %q", out.Violations[0])
	}
	for _, v := range out.Violations {
		if !strings.Contains(v, "/") {
			t.Errorf("violacao %q nao nomeia o diretorio", v)
		}
	}
}

// O relatorio conta SITES, e alertas distintos sao menos que sites sempre que um alerta
// vive nas duas copias. Dizer "N alertas sem runbook" quando N conta o mesmo alerta duas
// vezes e' o erro que o gate existe para nao cometer: numero inflado lido como divida.
func TestAlertasDistintosNaoContamOEspelhoDuasVezes(t *testing.T) {
	root := t.TempDir()
	corpo := "spec:\n  - alert: LoopZeroEspelhado\n    expr: up == 0\n"
	for _, d := range []string{DefaultRulesDirs[0], DefaultRulesDirs[1]} {
		dir := filepath.Join(root, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prometheusrule-x.yaml"), []byte(corpo), 0o644); err != nil {
			t.Fatalf("write %s: %v", d, err)
		}
	}
	out := Check(cfg(root))
	if got := out.AlertasDistintos(); got != 1 {
		t.Errorf("queria 1 alerta distinto entre 2 sites, veio %d", got)
	}
}
