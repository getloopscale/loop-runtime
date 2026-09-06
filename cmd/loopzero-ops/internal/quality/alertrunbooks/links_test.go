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

// ORG diferente NAO e' link morto — e este teste substitui um que afirmava o contrario.
//
// A versao de algumas horas atras tratava qualquer org != `getloopscale` como morta "por
// construcao". Medido depois, com credencial que enxerga: `getloopscale/loopzero` e
// `getloopscale/loopzero` sao **o mesmo repositorio**, o primeiro redirecionando para o segundo,
// e os arquivos resolvem nos dois. A regra teria produzido falso positivo assim que o
// escopo do gate crescesse (B-164).
//
// Como o erro entrou: extrai URLs truncadas na saida, INVENTEI os nomes de arquivo que
// elas pareciam ter, sondei ESSES nomes pela API e recebi Not Found. Os nomes reais eram
// outros e resolvem. Sondar o que se supoe em vez do que o arquivo diz e' fabricacao de
// evidencia com um passo a mais.
//
// A regra que sobrevive a medicao e' por REPOSITORIO: se a URL aponta para este repo
// (`loopzero`), o caminho e' conferido em disco, seja qual for a org; se aponta para outro
// repo, o caminho nao existe nesta arvore por definicao e afirmar que nao existe NA DE LA'
// exigiria rede — entao e' contado como nao-verificavel, nao acusado.
func TestOrgDiferenteNoMesmoRepoAindaEConferidaEmDisco(t *testing.T) {
	raiz := t.TempDir()
	if err := os.MkdirAll(filepath.Join(raiz, "docs", "runbooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raiz, "docs", "runbooks", "existe.md"), []byte("# ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	escreverRegra(t, raiz, "prometheusrule-teste.yaml", regraCom(
		"https://github.com/getloopscale/loopzero/blob/develop/docs/runbooks/existe.md"))

	out := Check(Config{Root: raiz})
	if len(out.LinksMortos) != 0 {
		t.Errorf("acusou como morto um link cujo arquivo EXISTE, so' porque a org do caminho difere. "+
			"`getloopscale/loopzero` redireciona para `getloopscale/loopzero` — e' o mesmo repositorio: %+v", out.LinksMortos)
	}
	if out.LinksNaoVerificaveis != 0 {
		t.Errorf("contou como nao-verificavel um link deste mesmo repo, que da' para conferir em disco")
	}
}

// Repositorio diferente e' NAO-VERIFICAVEL, nao morto: o caminho nao existe nesta arvore
// por definicao, e dizer que nao existe na arvore DELE exigiria rede.
func TestOutroRepositorioContaComoNaoVerificavel(t *testing.T) {
	raiz := t.TempDir()
	escreverRegra(t, raiz, "prometheusrule-teste.yaml", regraCom(
		"https://github.com/getloopscale/outro-repo/blob/main/docs/x.md"))

	out := Check(Config{Root: raiz})
	if len(out.LinksMortos) != 0 {
		t.Errorf("acusou de morto um link para OUTRO repositorio, sem ter como saber: %+v", out.LinksMortos)
	}
	if out.LinksNaoVerificaveis != 1 {
		t.Errorf("queria 1 nao-verificavel, veio %d", out.LinksNaoVerificaveis)
	}
}

// A travessia nao pode alcancar arquivo fora da raiz. O mutante e' um arquivo REAL
// plantado FORA — sem o tratamento, `Join(root, "../alvo.md")` resolveria nele e o
// gate diria que o runbook existe. Com o tratamento, o `..` e' resolvido contra a
// raiz e vira um caminho relativo comum que nao existe.
//
// Este teste substitui uma checagem de contencao que eu escrevi e removi: depois do
// `Clean`, ela nao tinha como disparar. Guarda inalcancavel e' codigo morto que se
// le' como protecao.
func TestTravessiaNaoAlcancaArquivoForaDaRaiz(t *testing.T) {
	base := t.TempDir()
	raiz := filepath.Join(base, "repo")
	if err := os.MkdirAll(raiz, 0o755); err != nil {
		t.Fatal(err)
	}
	// O alvo da travessia, existente e fora da raiz.
	if err := os.WriteFile(filepath.Join(base, "alvo.md"), []byte("# fora\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	escreverRegra(t, raiz, "prometheusrule-teste.yaml", regraCom(
		"https://github.com/getloopscale/loopzero/blob/main/../alvo.md"))

	out := Check(Config{Root: raiz})
	if len(out.LinksMortos) != 1 {
		t.Fatalf("a travessia alcancou o arquivo de fora e o link passou por vivo: %+v", out.LinksMortos)
	}
	if !strings.Contains(out.LinksMortos[0].Motivo, "nao existe") {
		t.Errorf("motivo inesperado: %+v", out.LinksMortos[0])
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
	// Conta os CONFERIDOS, nao os problematicos. A primeira versao somava mortos +
	// nao-verificaveis e tratava zero como "nao alcancou nada" — mas zero e' TAMBEM o
	// resultado de todos os links estarem bons, que e' o estado atual da arvore. Confundir
	// "nao achei nada" com "nao ha' nada errado" e' a forma exata que este gate combate, e
	// ela estava no teste que devia prova-lo.
	if out.LinksConferidos == 0 {
		t.Fatal("nenhum link de runbook alcancado — verde sobre nada nao e' verde")
	}
	if out.DirsVarridos == 0 {
		t.Fatal("nenhum diretorio de regras varrido")
	}
	t.Logf("populacao: %d diretorio(s), %d link(s) conferido(s), %d morto(s), %d nao-verificavel(is)",
		out.DirsVarridos, out.LinksConferidos, len(out.LinksMortos), out.LinksNaoVerificaveis)
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
