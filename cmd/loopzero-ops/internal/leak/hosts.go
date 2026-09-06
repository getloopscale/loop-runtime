// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import (
	"fmt"
	"os"
	"strings"
)

// Hosts devolve, em ordem de declaracao e sem repetir, todo host que o inventario de
// exposicao declara.
//
// # O defeito, medido
//
// B-025, 2026-08-18: `app.loopzero.example`, `cloud.loopzero.example` e `auth.loopzero.example` — os
// nomes SEM o sufixo `-dev` — tem bloco proprio no Caddyfile e cinco linhas no inventario,
// e nenhum deles jamais foi sondado. `Targets` filtra por um unico host, e `leak live`
// passa `app-dev`/`auth-dev` por padrao: os tres eram inalcancaveis por construcao.
//
// E eles nao apontam para o vazio, como o item que os registrou supunha. Medido ao vivo:
//
//	cloud.loopzero.example/v1/billing      -> 401   (loopzero-cloud vivo, negando credencial)
//	auth.loopzero.example/health/ready     -> 200   (Kratos vivo)
//	app.loopzero.example/                  -> 502   (Vite morto — mas o bloco continua la)
//
// Sao hostnames de producao servindo instancias de desenvolvimento, e a unica coisa que os
// media era ninguem.
//
// # Por que listar do inventario, e nao os tres nomes
//
// Porque o inventario JA e a declaracao do que a borda expoe — `proxy-exposure.txt` existe
// exatamente para isso, e o gate hermetico de mesmo nome obriga que toda rota da borda
// esteja declarada nele. Fixar tres nomes aqui deixaria o quarto host futuro invisivel pelo
// mesmo motivo que estes tres ficaram. Lendo a declaracao, o que for declarado e medido.
func Hosts(inventário string) ([]string, error) {
	// #nosec G304 -- o caminho vem da flag `--inventory` do operador, cujo padrao e o
	// inventario versionado do proprio repositorio. E o mesmo modelo de confianca do
	// `Targets` ao lado; nao ha entrada de usuario final neste caminho.
	b, err := os.ReadFile(inventário)
	if err != nil {
		return nil, fmt.Errorf("ler inventário de exposição %s: %w", inventário, err)
	}
	var hosts []string
	visto := map[string]bool{}
	for _, linha := range strings.Split(string(b), "\n") {
		linha = strings.TrimSpace(linha)
		if linha == "" || strings.HasPrefix(linha, "#") {
			continue
		}
		campos := strings.Fields(linha)
		if len(campos) == 0 {
			continue
		}
		h := campos[0]
		// A primeira coluna e o site; uma linha de tabela markdown ou um separador nao e.
		if !strings.Contains(h, ".") || strings.HasPrefix(h, "|") {
			continue
		}
		if !visto[h] {
			visto[h] = true
			hosts = append(hosts, h)
		}
	}
	return hosts, nil
}
