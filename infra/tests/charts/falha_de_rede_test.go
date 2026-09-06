// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestClassificarFalhaDeRenderSeparaRedeDeConteudo(t *testing.T) {
	casos := []struct {
		nome       string
		saida      string
		esperaRede bool
	}{
		{"timeout de dns", "Error: failed to download: no such host", true},
		{"conexao recusada", "dial tcp 1.2.3.4:443: connection refused", true},
		{"handshake", "Get \"https://charts.example\": TLS handshake timeout", true},
		// O caso que me pegou ao aplicar --skip-deps no lugar errado: e' configuracao,
		// nao conectividade. Classifica-lo como rede mandaria a proxima pessoa procurar
		// defeito na conexao por causa de um erro de arquivo.
		{"repo nao declarado NAO e rede", "Error: repo openbao not found", false},
		{"erro de template", "Error: template: loopzero-cloud/templates/svc.yaml:12: nil pointer", false},
		{"schema", "values don't meet the specifications of the schema(s)", false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rede, exp := ClassificarFalhaDeRender(c.saida)
			if rede != c.esperaRede {
				t.Errorf("classificou rede=%v, queria %v — saida: %q", rede, c.esperaRede, c.saida)
			}
			if exp == "" {
				t.Error("a explicacao veio vazia; a mensagem e' o produto desta funcao")
			}
			if c.esperaRede && !strings.Contains(exp, "REDE") {
				t.Errorf("a explicacao de rede nao diz que e' de rede: %q", exp)
			}
		})
	}
}

// A populacao real (`rules/testing.md` § 4.2): quantos testes deste portao executam
// `helmfile`, e quantos deles ja' nao contatam a rede.
//
// Medido em 2026-08-19: das 8 mencoes a "helmfile" nos testes, apenas DUAS sao execucao
// (`loopzero_cloud_edge_nodeport_test.go` e `vault_host_published_reach_test.go`); as outras
// sao caminhos de arquivo ou literais dentro de asserçoes sobre scripts. Das duas, a do
// `loopzero-cloud` renderiza chart LOCAL e passou a usar `--skip-deps` (0s, zero repos); a do
// `openbao` renderiza chart REMOTO e nao pode.
//
// Este teste existe para que a proxima execucao acrescentada seja uma decisao consciente:
// ele falha se aparecer uma terceira sem `--skip-deps` e sem porta de saida declarada.
func TestExecucoesDeHelmfileNoPortaoSaoAsMedidas(t *testing.T) {
	execs := listarExecucoesDeHelmfile(t)
	if len(execs) == 0 {
		t.Fatal("nenhuma execucao de helmfile encontrada — uma varredura que nao alcanca nada " +
			"reporta verde sobre nada")
	}
	if len(execs) > 2 {
		t.Errorf("apareceram %d execucoes de helmfile nos testes do portao; eram 2 em 2026-08-19 (%v). "+
			"Cada execucao nova transforma indisponibilidade de rede em entrega bloqueada — use "+
			"`--skip-deps` se o chart for local, ou declare a porta de saida como o teste do openbao "+
			"faz (B-156).", len(execs), execs)
	}
}

// listarExecucoesDeHelmfile varre os testes do portao e devolve os arquivos que EXECUTAM
// `helmfile` — distinguindo execucao de mencao. A distincao importa: das 8 ocorrencias do
// nome, 6 sao caminho de arquivo ou literal dentro de uma asserção sobre um script.
func listarExecucoesDeHelmfile(t *testing.T) []string {
	t.Helper()
	raiz := repoRoot(t)
	base := filepath.Join(raiz, "infra", "tests")
	var achados []string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return err
		}
		// A varredura casa consigo mesma: este arquivo carrega os dois literais que ela
		// procura, dentro da propria logica de deteccao. Nao e' execucao — e' o detector.
		// Excluir por nome e' preferivel a afinar o padrao, porque qualquer padrao mais
		// esperto erraria noutro lugar; e uma execucao real de helmfile ACRESCENTADA a
		// este arquivo seria bizarra o bastante para ser vista na revisao.
		if strings.HasSuffix(p, "falha_de_rede_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(filepath.Clean(p))
		if rerr != nil {
			return nil
		}
		s := string(b)
		// Execucao = `exec.Command(` seguido, na mesma expressao, do literal "helmfile".
		for _, pedaco := range strings.Split(s, "exec.Command(")[1:] {
			fim := strings.Index(pedaco, ")")
			if fim < 0 {
				fim = len(pedaco)
			}
			if strings.Contains(pedaco[:fim], `"helmfile"`) {
				rel, _ := filepath.Rel(raiz, p)
				achados = append(achados, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("varrendo %s: %v", base, err)
	}
	sort.Strings(achados)
	return achados
}
