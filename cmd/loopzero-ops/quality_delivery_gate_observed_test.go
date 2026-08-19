// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A cobertura do caminho de push era conferida LENDO a saida de `task --dry`, e
// `--dry` SUPER-REPORTA: um `cmds:` em bloco (`- |`) e' impresso verbatim, sem
// avaliar o `if`/`exit` de dentro. Prova viva ja' na arvore — `quality.yml`
// aborta com `exit 1` quando falta o `helmfile`, e o `--dry` imprime o comando
// seguinte assim mesmo.
//
// O buraco nao e' o bypass deliberado (a metade comportamental do teste irmao
// fecha esse): e' o acidente futuro. Um alvo com a forma
// `- |` / `[ -n "$SKIP" ] && exit 0` / `cd cmd/loopzero-ops && go test ./...`
// satisfaria a leitura do `--dry` e nao rodaria nada (B-102).
//
// Aqui a cobertura e' conferida por EXECUCAO OBSERVADA: o alvo roda de verdade,
// com um substituto de `go` no PATH que grava diretorio e argv. Se o controle de
// fluxo pular a chamada, ela nao aparece no registro — que e' exatamente a
// diferenca entre o que o alvo IMPRIME e o que ele EXECUTA.
func TestPernaHermeticaRodaDeFatoASuiteDesteModulo(t *testing.T) {
	if _, err := exec.LookPath("task"); err != nil {
		// FALHA, nao skip: um skip apaga a guarda em silencio justamente na
		// maquina onde alguem estaria improvisando. O caminho que roda este
		// teste passa por `task`, entao na maquina que importa ele existe.
		t.Fatal("go-task nao esta' no PATH, e e' pre-requisito declarado do repositorio")
	}
	raiz := repoRootDir(t)

	bin := t.TempDir()
	registro := filepath.Join(bin, "registro")
	falso := "#!/bin/sh\nprintf '%s\\t%s\\n' \"$(pwd)\" \"$*\" >> " + registro + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(falso), 0o755); err != nil {
		t.Fatalf("escrevendo o substituto de go: %v", err)
	}

	cmd := exec.Command("task", "quality:hermetic")
	cmd.Dir = raiz
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rodando quality:hermetic com o substituto: %v\n%s", err, out)
	}

	b, err := os.ReadFile(registro)
	if err != nil {
		t.Fatalf("o alvo nao invocou `go` nenhuma vez — nada foi observado: %v", err)
	}
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(linhas) == 0 || linhas[0] == "" {
		t.Fatal("registro vazio: uma verificacao sobre nenhuma execucao passa por vacuidade")
	}

	// A suite INTEIRA deste modulo, rodada a partir DESTE diretorio. `./...` e
	// nao `./internal/quality/...`: o pacote `main` daqui e' onde vivem as
	// guardas do proprio caminho de entrega, e uma varredura que o exclua deixa
	// de cobrir justamente elas.
	var achou bool
	for _, l := range linhas {
		partes := strings.SplitN(l, "\t", 2)
		if len(partes) != 2 {
			continue
		}
		dir, args := partes[0], partes[1]
		if strings.HasSuffix(dir, filepath.Join("cmd", "loopzero-ops")) && strings.HasPrefix(args, "test ./...") {
			achou = true
			break
		}
	}
	if !achou {
		t.Errorf("a perna hermetica nao EXECUTOU `go test ./...` a partir de cmd/loopzero-ops.\n\n"+
			"Observado:\n%s\n\nSe o alvo passou a esconder a chamada atras de um `- |` com "+
			"`exit` no meio, ela continua aparecendo no `task --dry` e nao roda — que e' o "+
			"defeito que este teste existe para pegar (B-102).", strings.Join(linhas, "\n"))
	}
}
