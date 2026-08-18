// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `k3d:secrets:create` nao pode reapontar o kubectl do operador em silencio.
//
// B-024, bala 3 do DoD. O `k3d cluster create` troca o `current-context` por padrao, e esse
// efeito colateral era a origem do defeito: os seeds herdam o contexto ativo, entao qualquer
// seed rodado depois deste task operava o cluster de secrets achando que era o central.
//
// A escada de parcimonia parou na rung 3, recurso nativo: `--kubeconfig-switch-context=false`
// existe no k3d v5.7.4. Nao ha logica de restauracao a escrever, e
// `--kubeconfig-update-default` continua no padrao `true`, entao a entrada do contexto segue
// gravada e todo `--context k3d-loopzero-secrets` do proprio task continua funcionando.
//
// Muda-se APENAS este task. Os outros quatro criam clusters dentro de `task setup`, e ali
// a troca de contexto e o comportamento que se espera de quem acabou de montar o ambiente.
//
// Essa distincao e JULGAMENTO, nao medicao — uma versao anterior deste comentario dizia
// "esta medida, nao suposta", e a revisao chamou: nada mediu expectativa de operador. O que
// esta medido e que sao seis `k3d cluster create` nos taskfiles e que nenhum devolve o
// contexto. A protecao estrutural contra a heranca nao mora aqui: mora em `CORE_CONTEXT`,
// que fixa o cluster na raiz de composicao dos seeds.
func TestSecretsCreate_DoesNotHijackTheOperatorsContext(t *testing.T) {
	body := readTaskfile(t, "k3d.yml")

	cmds := comandosDoTask(body, "k3d:secrets:create:")
	if strings.TrimSpace(cmds) == "" {
		t.Fatal("task k3d:secrets:create nao encontrado — o nome mudou?")
	}
	linhaCreate := ""
	for _, l := range strings.Split(cmds, "\n") {
		if strings.Contains(l, "k3d cluster create") {
			linhaCreate = l
			break
		}
	}
	if linhaCreate == "" {
		t.Fatal("o task nao cria mais cluster — este teste esta medindo outra coisa")
	}
	// A flag tem de estar na PROPRIA linha do comando. No bloco inteiro, um comentario
	// falando dela satisfaria a assercao.
	if !strings.Contains(linhaCreate, "--kubeconfig-switch-context=false") {
		t.Error("k3d:secrets:create troca o contexto ativo do operador em silencio; " +
			"declare --kubeconfig-switch-context=false")
	}
}

// A entrada do contexto PRECISA continuar sendo gravada, senao o proprio task quebra: todos
// os comandos dele usam `--context k3d-loopzero-secrets`.
func TestSecretsCreate_StillWritesTheContextEntry(t *testing.T) {
	cmds := comandosDoTask(readTaskfile(t, "k3d.yml"), "k3d:secrets:create:")

	if strings.Contains(cmds, "--kubeconfig-update-default=false") {
		t.Error("desligar update-default apaga a entrada do contexto e quebra os " +
			"`--context k3d-loopzero-secrets` do proprio task")
	}
	if !strings.Contains(cmds, "--context k3d-loopzero-secrets") {
		t.Error("o task deixou de fixar o contexto nos proprios comandos")
	}
}

func readTaskfile(t *testing.T, nome string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(filepath.Join("..", "..", "..", "..", "taskfiles", nome)))
	if err != nil {
		t.Fatalf("ler taskfiles/%s: %v", nome, err)
	}
	return string(b)
}

// comandosDoTask recorta a secao do task e devolve SO as linhas de comando.
//
// Descartar comentario nao e detalhe: a primeira versao desta guarda casava o bloco
// inteiro, e o comentario que eu mesmo adicionei no mesmo commit continha as duas strings
// procuradas. Dois mutantes passaram — flag removida do comando, e a linha do comando
// APAGADA por inteiro. A guarda escrita para impedir isso era satisfeita pelo texto que
// falava sobre isso. Mesma armadilha que o gate runbook-parity ja pagou nesta base.
func comandosDoTask(body, header string) string {
	var b strings.Builder
	for _, linha := range strings.Split(secaoDoTask(body, header), "\n") {
		if strings.HasPrefix(strings.TrimSpace(linha), "#") {
			continue
		}
		b.WriteString(linha)
		b.WriteString("\n")
	}
	return b.String()
}

// secaoDoTask recorta do cabecalho do task ate o proximo task no mesmo nivel de indentacao.
func secaoDoTask(body, header string) string {
	i := strings.Index(body, header)
	if i < 0 {
		return ""
	}
	rest := body[i+len(header):]
	// O proximo task comeca com dois espacos e um nome terminado em dois-pontos.
	for _, linha := range []string{"\n  k3d:", "\n  clusters:", "\n  registry:"} {
		if j := strings.Index(rest, linha); j >= 0 {
			rest = rest[:j]
		}
	}
	return rest
}
