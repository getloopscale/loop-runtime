// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package alertrunbooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func escreverRegra(t *testing.T, raiz, nome, corpo string) {
	t.Helper()
	dir := filepath.Join(raiz, "infra", "helm", "monitoring", "manifests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, nome), []byte(corpo), 0o644); err != nil {
		t.Fatal(err)
	}
}

func regraCom(url string) string {
	return `spec:
  groups:
    - name: x
      rules:
        - alert: LoopZeroAlgoQuebrou
          expr: up == 0
          annotations:
            summary: "algo"
            runbook_url: "` + url + `"
`
}

// O gate media PRESENCA da anotacao, nao existencia do runbook. Medido em
// 2026-08-19: das 7 URLs na arvore, SEIS nao levavam a nada — e todas as sete
// contavam como satisfeitas. Um alerta que dispara as 3 da manha mandando o
// operador para um 404 e' pior que um alerta sem link, porque o link promete.
func TestAcusaCaminhoDoProprioRepoQueNaoExiste(t *testing.T) {
	raiz := t.TempDir()
	escreverRegra(t, raiz, "prometheusrule-teste.yaml", regraCom(
		"https://github.com/getloopscale/loopzero/blob/main/docs/runbooks/nao-existe.md"))

	out := Check(Config{Root: raiz})
	if len(out.LinksMortos) != 1 {
		t.Fatalf("queria 1 link morto, veio %d: %+v", len(out.LinksMortos), out.LinksMortos)
	}
	if !strings.Contains(out.LinksMortos[0].Motivo, "nao existe") {
		t.Errorf("o motivo nao diz que o arquivo nao existe: %+v", out.LinksMortos[0])
	}
	if out.Pass {
		t.Error("passou com um runbook apontando para arquivo inexistente no proprio repo")
	}
}

// A contrapartida: o caminho que EXISTE nao pode ser acusado, senao o gate vira
// ruido e as pessoas aprendem a contorna-lo.
func TestNaoAcusaCaminhoDoProprioRepoQueExiste(t *testing.T) {
	raiz := t.TempDir()
	if err := os.MkdirAll(filepath.Join(raiz, "docs", "runbooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raiz, "docs", "runbooks", "existe.md"), []byte("# ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	escreverRegra(t, raiz, "prometheusrule-teste.yaml", regraCom(
		"https://github.com/getloopscale/loopzero/blob/main/docs/runbooks/existe.md"))

	out := Check(Config{Root: raiz})
	if len(out.LinksMortos) != 0 {
		t.Errorf("acusou um runbook que existe em disco: %+v", out.LinksMortos)
	}
}

// A org divergente e' achado proprio, e hermetico: a identidade canonica deste
// repositorio esta' no caminho de modulo dos `go.mod` (`github.com/getloopscale/loopzero`).
// Medido: duas das URLs da arvore citam `getloopscale`, e a API autenticada confirma
// que os arquivos nao estao la'. Uma org errada nao e' detalhe de escrita — e' um
// link que nunca vai resolver para quem tem acesso a este repo.
func TestAcusaOrgDiferenteDaDoRepositorio(t *testing.T) {
	raiz := t.TempDir()
	escreverRegra(t, raiz, "prometheusrule-teste.yaml", regraCom(
		"https://github.com/outraorg/loopzero/blob/develop/docs/operations/x.md"))

	out := Check(Config{Root: raiz})
	if len(out.LinksMortos) != 1 {
		t.Fatalf("queria 1 link morto por org divergente, veio %d: %+v", len(out.LinksMortos), out.LinksMortos)
	}
	if !strings.Contains(out.LinksMortos[0].Motivo, "org") {
		t.Errorf("o motivo nao nomeia a org: %+v", out.LinksMortos[0])
	}
}

// O que o gate NAO pode verificar sem rede fica contado, nao escondido. Uma URL
// externa pode estar viva ou morta; afirmar qualquer um dos dois hermeticamente
// seria inventar. Contar e' honesto — e o numero e' o que faz alguem conferir.
func TestUrlExternaContaComoNaoVerificavelEmVezDeSilencio(t *testing.T) {
	raiz := t.TempDir()
	escreverRegra(t, raiz, "prometheusrule-teste.yaml", regraCom("https://docs.loopzero.example/runbooks/algo"))

	out := Check(Config{Root: raiz})
	if len(out.LinksMortos) != 0 {
		t.Errorf("acusou uma URL externa de morta sem poder saber: %+v", out.LinksMortos)
	}
	if out.LinksNaoVerificaveis != 1 {
		t.Errorf("queria 1 link nao-verificavel contado, veio %d", out.LinksNaoVerificaveis)
	}
}

// A populacao real (`rules/testing.md` § 4.2). Um mutante prova que o detector
// dispara; so' isto diz o que ele acha no que existe.
func TestPopulacaoRealDosLinksDeRunbook(t *testing.T) {
	out := Check(Config{Root: raizDoRepo(t)})
	total := len(out.LinksMortos) + out.LinksNaoVerificaveis
	if total == 0 {
		t.Fatal("nenhum link de runbook alcancado — verde sobre nada nao e' verde")
	}
	t.Logf("populacao: %d link(s) morto(s), %d nao-verificavel(is)",
		len(out.LinksMortos), out.LinksNaoVerificaveis)
	for _, d := range out.LinksMortos {
		t.Logf("  morto: %s [%s] %s — %s", d.Arquivo, d.Alerta, d.URL, d.Motivo)
	}
}

func raizDoRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "api", "go.mod")); err == nil {
			return dir
		}
		pai := filepath.Dir(dir)
		if pai == dir {
			break
		}
		dir = pai
	}
	t.Fatal("nao achei a raiz do repositorio")
	return ""
}
