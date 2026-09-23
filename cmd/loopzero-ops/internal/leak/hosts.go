// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import (
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/exposure"
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
func Hosts(inventory string) ([]string, error) {
	decls, err := loadInventory(inventory)
	if err != nil {
		return nil, err
	}
	var hosts []string
	seen := map[string]bool{}
	for _, d := range decls {
		if !seen[d.Site] {
			seen[d.Site] = true
			hosts = append(hosts, d.Site)
		}
	}
	return hosts, nil
}

// loadInventory reads the exposure inventory through the parser the
// proxy-exposure gate uses, and refuses a malformed row: a row the probe cannot
// read is a declared route it would silently not probe.
func loadInventory(path string) ([]exposure.Declaration, error) {
	decls, malformed, err := exposure.Load(path)
	if err != nil {
		return nil, err
	}
	if len(malformed) > 0 {
		return nil, fmt.Errorf("exposure inventory %s has malformed rows: %s", path, strings.Join(malformed, "; "))
	}
	return decls, nil
}
