// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// NOTA DE ESCRITA, e ela e' evidencia do proprio B-160: este arquivo NAO pode ser
// criado por um heredoc de shell. A guarda varre o TEXTO do comando, e o comando que
// escreveria estas tabelas carrega os literais destrutivos — entao ela bloqueia a
// criacao do teste que a verifica. Uma guarda que impede escrever o proprio teste e' o
// mesmo defeito visto de outro angulo.

// rodarHook alimenta o hook com o JSON que o harness envia. 2 = bloqueado, 0 = permitido.
func rodarHook(t *testing.T, comando string) int {
	t.Helper()
	raiz := repoRootDir(t)
	entrada, err := json.Marshal(map[string]any{
		"tool_input": map[string]any{"command": comando},
	})
	if err != nil {
		t.Fatalf("montando json: %v", err)
	}
	cmd := exec.Command("bash", filepath.Join(raiz, ".claude", "hooks", "validate-command.sh"))
	cmd.Stdin = strings.NewReader(string(entrada))
	cmd.Dir = raiz
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		t.Fatalf("rodando o hook: %v", err)
	}
	return 0
}

// O falso positivo MEDIDO (B-160). As tres condicoes da guarda sao casadas
// INDEPENDENTEMENTE sobre a string inteira do bloco e depois combinadas com E: o `rm`
// vem do primeiro comando, o `-r` do `grep -rn`, e o "caminho perigoso" da expressao
// do `sed` — uma barra seguida de espacos. Nao existe `rm -r` aqui, nem caminho de
// raiz, e mesmo assim a mensagem acusa "'rm -r' on a system/home-root path".
func TestGuardaNaoBloqueiaRmSimplesQuandoOutroComandoTrazOMenosR(t *testing.T) {
	comando := "rm -f cmd/loopzero-ops/x_test.go\n" +
		"grep -rn foo cmd/loopzero-ops | sed 's/^/   /'"
	if got := rodarHook(t, comando); got == 2 {
		t.Error("bloqueou um `rm -f` de arquivo unico porque OUTRO comando do bloco tinha `-r` e um " +
			"TERCEIRO tinha uma barra. Guarda que erra em comando comum e' guarda que as pessoas " +
			"aprendem a rodear — foi exatamente o que aconteceu quando isto foi medido (B-160).")
	}
}

// Uma segunda forma da mesma familia, sem `sed`: o caminho perigoso vem de um `cd` que
// nao tem relacao nenhuma com o `rm`.
func TestGuardaNaoBloqueiaRmDeArquivoQuandoOCaminhoPerigosoVemDeOutroComando(t *testing.T) {
	comando := "ls /usr/lib > /dev/null\nrm -f BACKLOG.md.bak\ngrep -rn foo ."
	if got := rodarHook(t, comando); got == 2 {
		t.Error("o `rm -f` de um arquivo unico foi bloqueado porque um `ls` no mesmo bloco citava " +
			"/usr/lib e um `grep -rn` trazia o -r. Nenhum dos tres pedacos pertence ao mesmo comando.")
	}
}

// A contrapartida, e ela e' inegociavel: a guarda so' pode ficar mais PRECISA, nunca
// mais permissiva. Se uma linha desta tabela deixar de bloquear, o conserto do falso
// positivo abriu um buraco e nao vale.
func TestGuardaSegueBloqueandoAsFormasDestrutivas(t *testing.T) {
	for _, comando := range []string{
		"rm -rf /",
		"rm -rf $HOME",
		"rm -rf ~",
		"rm -rf ~/",
		"rm -rf /etc",
		"rm -rf /usr/lib",
		"rm -fr /",
		"rm -Rf /",
		"rm --recursive --force /",
		"rm -r -f /",
		"echo ola && rm -rf $HOME",
		"cd /tmp; rm -rf /home/paulo",
		"rm -rf /*",
	} {
		if got := rodarHook(t, comando); got != 2 {
			t.Errorf("%q NAO foi bloqueado (exit %d) — a guarda ficou mais permissiva", comando, got)
		}
	}
}

// A POPULACAO REAL (`rules/testing.md` § 4.2). Um mutante prova que a guarda dispara;
// so' isto diz o que ela faz com o que a arvore de fato escreve. Os apagamentos sao
// extraidos dos taskfiles, dos scripts de operacao e do helmfile — nenhum deles pode
// ser bloqueado, porque todos sao trabalho legitimo que alguem roda.
//
// O RESULTADO DA VARREDURA, e ele e' parte da regra mesmo sendo zero: medido em
// 2026-08-19, a guarda ANTERIOR ao conserto tambem nao bloqueava nenhuma das 43. Ou
// seja, esta varredura NAO encontrou defeito — ela e' catraca, nao achado. O defeito
// do B-160 vive em comando COMPOSTO ad-hoc (um apagamento simples, mais uma busca
// recursiva de outro comando, mais uma barra de um terceiro), que e' o que um agente
// ou uma pessoa digita num terminal e que nenhum arquivo versionado contem. Quem
// prova o conserto sao os dois testes de regressao acima, que falhavam antes dele.
// Dizer o contrario aqui faria esta varredura parecer ter pego algo.
func TestGuardaContraOsApagamentosQueAArvoreDeFatoUsa(t *testing.T) {
	raiz := repoRootDir(t)
	invocacao := regexp.MustCompile(`rm -[a-zA-Z]+ [^;|&#\n]*`)
	vistos := map[string]bool{}

	for _, sub := range []string{"taskfiles", filepath.Join("infra", "scripts"), filepath.Join("infra", "helmfile")} {
		err := filepath.WalkDir(filepath.Join(raiz, sub), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(filepath.Clean(p))
			if err != nil {
				return nil
			}
			for _, m := range invocacao.FindAllString(string(b), -1) {
				vistos[strings.TrimSpace(m)] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("varrendo %s: %v", sub, err)
		}
	}

	if len(vistos) == 0 {
		t.Fatal("nenhum apagamento encontrado na arvore — uma varredura que nao alcanca nada " +
			"reporta verde sobre nada")
	}
	// Piso MEDIDO em 2026-08-19: 43 invocacoes distintas, 13 delas recursivas. Um
	// numero menor significa que a extracao encolheu (pasta movida, forma nova) e o
	// verde passou a valer menos sem que nada avisasse.
	// Piso MEDIDO. Comecou em 43 e foi recongelado em **40** no mesmo dia, depois das
	// remocoes deliberadas do `B-073` (chart guarda-chuva) e do `B-157`
	// (`tools/loopzero-dockerfile-lint`) — as duas levaram consigo scripts que continham
	// apagamentos.
	//
	// A catraca fez o que devia: acusou o encolhimento em vez de deixa-lo passar. Baixar o
	// piso so' e' legitimo quando a causa e' conhecida e deliberada, e e' por isso que a
	// causa esta' escrita aqui e nao so' no commit.
	if len(vistos) < 40 {
		t.Errorf("so' %d invocacao(oes) alcancada(s); a arvore tinha 40 em 2026-08-19 "+
			"(recongelado apos as remocoes do B-073 e do B-157)", len(vistos))
	}

	var bloqueados []string
	for c := range vistos {
		if rodarHook(t, c) == 2 {
			bloqueados = append(bloqueados, c)
		}
	}
	sort.Strings(bloqueados)
	if len(bloqueados) > 0 {
		t.Errorf("a guarda bloqueia %d apagamento(s) que a propria arvore usa:\n  %s",
			len(bloqueados), strings.Join(bloqueados, "\n  "))
	}
}

// E o que a propria mensagem do hook declara permitido precisa passar, senao a guarda
// contradiz o texto que ela mesma imprime.
func TestGuardaPermiteOQueAPropriaMensagemDeclara(t *testing.T) {
	// O caminho profundo e' construido em tempo de execucao, e nao escrito. Cravar
	// `/home/<alguem>/...` num arquivo versionado e' vazamento de caminho de
	// workstation — o gate `publish-hygiene` reprova, e reprovou esta linha quando
	// eu a escrevi assim.
	profundoAbsoluto := filepath.Join(repoRootDir(t), "cmd", "loopzero-ops", "testdata", "tmp")

	for _, comando := range []string{
		"rm -rf /tmp/scratch-dir",
		"rm -rf cmd/loopzero-ops/testdata/tmp",
		"rm -f BACKLOG.md.bak",
		"rm -rf " + profundoAbsoluto,
	} {
		if got := rodarHook(t, comando); got == 2 {
			t.Errorf("%q foi bloqueado, mas a mensagem da guarda declara permitidos caminho "+
				"relativo ao projeto, subdiretorio profundo e /tmp/", comando)
		}
	}
}
