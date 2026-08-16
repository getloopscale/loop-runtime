// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package billingsecrets reprova montagens de segredo de cobrança cuja opcionalidade é um
// literal em vez de um valor.
//
// MEDIDO em 2026-08-11 no app-dev: o pod do painel rodou 20h `Running` sem
// `loopzero-stripe-webhook` e sem `loopzero-stripe-api-keys`, porque ambos são montados com
// `optional: true`. Não houve evento, aviso ou condição — a ausência da credencial que
// autoriza cobrar era, para o Kubernetes, uma configuração perfeitamente normal.
//
// O gate não exige que os segredos sejam sempre obrigatórios. Exige que a decisão seja
// EXPRESSÁVEL: um literal `true` é o mesmo em todo ambiente, então o hermético e o público
// necessariamente compartilham a escolha mais permissiva. Um valor pode divergir, e é essa
// possibilidade de divergir que o gate protege.
package billingsecrets

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// billingSecretNames são os segredos cuja ausência mata a cobrança.
//
// Lista fechada de propósito: um segredo novo de cobrança precisa ser acrescentado aqui
// conscientemente, e é essa revisão que o gate compra. Uma heurística por prefixo
// ("qualquer coisa com stripe") acertaria mais casos e não deixaria ninguém decidir nada.
var billingSecretNames = []string{
	"loopzero-stripe-webhook",
	"loopzero-stripe-api-keys",
}

// chartTemplates são os dois irmãos que precisam andar juntos.
//
// `CLAUDE.md § Helm chart dual system`: `loopzero-cloud` existe em `infra/helm/` (instalado
// pelo Helmfile) e em `charts/` (publicado por OCI). Corrigir um só publica o defeito.
var chartTemplates = []string{
	"infra/helm/loopzero-cloud/templates/deployment.yaml",
	"charts/loopzero-cloud/templates/deployment.yaml",
}

// Config é a entrada do gate.
type Config struct {
	RepoRoot string
}

// Finding é uma montagem cuja opcionalidade não pode variar por ambiente.
type Finding struct {
	File string
	Line int
	Why  string
}

// Outcome é o resultado do gate.
type Outcome struct {
	Findings []Finding
	// Scanned lista os arquivos efetivamente lidos, para que um gate que não
	// encontrou nada por não ter lido nada não se pareça com um gate que passou.
	Scanned []string
}

// Check varre os dois charts e reprova opcionalidade literal nos segredos de cobrança.
//
// Trabalha sobre o TEXTO do template, e não sobre YAML parseado, por uma razão concreta:
// o arquivo contém sintaxe de template Go (`{{ ... }}`), que nenhum parser YAML aceita.
// Renderizar exigiria o Helm instalado, transformando um verificador de lógica pura numa
// casca em volta de um binário externo — o que `CLAUDE.md § D5` recusa.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	for _, rel := range chartTemplates {
		caminho := filepath.Join(cfg.RepoRoot, rel)
		f, err := os.Open(caminho) // #nosec G304 -- caminho derivado de lista fixa
		if err != nil {
			if os.IsNotExist(err) {
				// Um repositório sem o chart não é uma violação. Inventar um achado
				// aqui faria o gate falhar em qualquer checkout parcial.
				continue
			}
			return out, fmt.Errorf("abrir %s: %w", rel, err)
		}
		achados, err := scanTemplate(f, rel)
		_ = f.Close()
		if err != nil {
			return out, err
		}
		out.Scanned = append(out.Scanned, rel)
		out.Findings = append(out.Findings, achados...)
	}
	sort.Slice(out.Findings, func(i, j int) bool {
		if out.Findings[i].File != out.Findings[j].File {
			return out.Findings[i].File < out.Findings[j].File
		}
		return out.Findings[i].Line < out.Findings[j].Line
	})
	return out, nil
}

// scanTemplate percorre o arquivo procurando `optional:` literal dentro do bloco de um
// segredo de cobrança.
//
// O estado que carrega é deliberadamente curto: o nome do segredo visto por último. Um
// `optional:` só é julgado enquanto esse nome for de cobrança, e o estado é descartado ao
// encontrar o próximo `name:` — o que evita atribuir a um segredo a opcionalidade do
// vizinho quando a indentação muda.
func scanTemplate(r *os.File, rel string) ([]Finding, error) {
	var achados []Finding
	sc := bufio.NewScanner(r)
	atual := ""
	n := 0
	for sc.Scan() {
		n++
		linha := strings.TrimSpace(sc.Text())

		if nome, ok := strings.CutPrefix(linha, "name:"); ok {
			atual = ""
			nome = strings.Trim(strings.TrimSpace(nome), `"'`)
			for _, s := range billingSecretNames {
				if nome == s {
					atual = s
				}
			}
			continue
		}

		valor, ok := strings.CutPrefix(linha, "optional:")
		if !ok || atual == "" {
			continue
		}
		valor = strings.TrimSpace(valor)
		if strings.Contains(valor, "{{") {
			// Decidido por valor: pode ser opcional no hermético e obrigatório no
			// público. É exatamente o que se quer.
			continue
		}
		achados = append(achados, Finding{
			File: rel,
			Line: n,
			Why: fmt.Sprintf("`optional: %s` é literal no segredo %s — um literal vale para todo "+
				"ambiente, então o público herda a escolha mais permissiva do hermético e sobe "+
				"sem a credencial que autoriza cobrar. Use um valor (`{{ not .Values.billing.required }}`) "+
				"para que os dois possam divergir", valor, atual),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("ler %s: %w", rel, err)
	}
	return achados, nil
}
