// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package openbaoinitauth

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// B-098: toda policy declarada precisa ser carregada por ao menos um role.
//
// Uma policy sem role nao concede nada — e essa e' justamente a razao de ninguem notar. O custo
// aparece quando alguem vai editar "a policy do loopzero-api" e encontra quatro nomes parecidos: foi
// assim que o B-072 aplicou um patch em `loopzero-api-transit-encrypt` em vez de `loopzero-api`, porque o
// padrao casou primeiro.
//
// E o erro simetrico e' pior. As duas orfas medidas em 2026-08-19 eram versoes ANTERIORES e MAIS
// LARGAS da policy corrente (`transit/encrypt/+` contra `transit/encrypt/loopzero-tenants-*`). Liga-las
// por engano nao concederia de menos: concederia demais.

var (
	reWritePolicy = regexp.MustCompile(`write_policy\s+"([^"]+)"`)
	reWriteRole   = regexp.MustCompile(`write_auth_role\s+((?:"[^"]*"\s*\\?\s*)+)`)
	reQuoted      = regexp.MustCompile(`"([^"]*)"`)
)

// posicaoPoliticasEmWriteAuthRole e' o indice do argumento `policies` na assinatura do helper
// renderizado: mount, role, bound_sas, bound_ns, policies, ttl[, bound_audiences].
const posicaoPoliticasEmWriteAuthRole = 4

// checarPoliciesOrfas devolve as falhas: policies escritas que nenhum role carrega.
//
// Ele tambem reprova quando NENHUMA policy e' encontrada — uma varredura que nao alcanca nada
// reporta verde sobre nada, e essa forma ja custou caro nesta arvore (`PASS — 0 routes`, B-040).
func checarPoliciesOrfas(renderizado string) []string {
	declaradas := map[string]bool{}
	for _, m := range reWritePolicy.FindAllStringSubmatch(renderizado, -1) {
		declaradas[m[1]] = true
	}
	if len(declaradas) == 0 {
		return []string{"nenhuma policy encontrada no chart renderizado — a varredura nao alcancou nada"}
	}

	ligadas := map[string]bool{}
	var malformados []string
	for _, m := range reWriteRole.FindAllStringSubmatch(renderizado, -1) {
		args := reQuoted.FindAllStringSubmatch(m[1], -1)
		if len(args) <= posicaoPoliticasEmWriteAuthRole {
			// Assinatura mudou. Reportar em vez de seguir lendo o argumento errado: um
			// parser que se desalinha em silencio marca policies vivas como orfas.
			malformados = append(malformados, fmt.Sprintf(
				"write_auth_role com %d argumento(s), esperado ao menos %d — a assinatura do helper mudou",
				len(args), posicaoPoliticasEmWriteAuthRole+1))
			continue
		}
		for _, p := range strings.Split(args[posicaoPoliticasEmWriteAuthRole][1], ",") {
			if p = strings.TrimSpace(p); p != "" {
				ligadas[p] = true
			}
		}
	}
	if len(ligadas) == 0 {
		malformados = append(malformados,
			"nenhum role carrega policy alguma — o parser nao alcancou os bindings")
	}

	var orfas []string
	for p := range declaradas {
		if !ligadas[p] {
			orfas = append(orfas, p)
		}
	}
	sort.Strings(orfas)

	falhas := malformados
	if len(orfas) > 0 {
		falhas = append(falhas, fmt.Sprintf(
			"policy declarada que NENHUM role carrega: %s — inerte hoje, mas um nome a mais na lista de "+
				"candidatos de quem for editar, e o B-072 ja aplicou um patch no candidato errado",
			strings.Join(orfas, ", ")))
	}
	return falhas
}
