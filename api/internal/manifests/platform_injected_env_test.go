// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// A plataforma injeta UMA env var no contentor do workload: `PORT`. O outro
// ponto que escreve `container.Env` copia o env do PROPRIO app — valores do
// usuario, nao da plataforma.
//
// Isto e' o que sustenta o rebaixamento do B-093. O validador do `services.json`
// nao bloqueia `AWS_`, `DO_` nem `GOOGLE_`, e o env do tenant CHEGA a este
// contentor — mas nao ha' sombreamento, porque a plataforma nao injeta nada com
// esses prefixos aqui. A conclusao depende inteiramente deste fato, e ate' agora
// ele vivia so' numa medicao datada.
//
// Se alguem acrescentar uma injecao de plataforma, este teste reprova e manda
// reabrir o B-093 — que e' exatamente a condicao que o item registra como "vira
// alta". Sem esta guarda, a mudanca passaria e a analise do item continuaria
// parecendo valida.
func TestPlataformaInjetaApenasPORTNoContainerDoWorkload(t *testing.T) {
	src, err := os.ReadFile("renderer_deployment.go")
	if err != nil {
		t.Fatalf("lendo o renderizador: %v", err)
	}

	// Cada ponto que acrescenta env ao contentor, com o nome que ele escreve.
	re := regexp.MustCompile(`container\.Env = append\(container\.Env, k8sEnvVar\{[^}]*Name:\s*([^,}]+)`)
	achados := re.FindAllStringSubmatch(string(src), -1)
	if len(achados) == 0 {
		t.Fatal("nenhum ponto de injecao encontrado: o regex deixou de casar e a guarda passou a vigiar nada")
	}

	var daPlataforma []string
	for _, m := range achados {
		nome := strings.TrimSpace(m[1])
		// `Name: k` copia a chave do env do proprio app — usuario, nao plataforma.
		if !strings.HasPrefix(nome, `"`) {
			continue
		}
		daPlataforma = append(daPlataforma, strings.Trim(nome, `"`))
	}

	if len(daPlataforma) != 1 || daPlataforma[0] != "PORT" {
		t.Errorf("a plataforma passou a injetar %v no contentor do workload; era apenas [PORT].\n\n"+
			"Reabra o B-093: o validador do services.json NAO bloqueia AWS_, DO_ nem GOOGLE_, e o env do "+
			"tenant chega a este mesmo contentor. Uma injecao de plataforma nova cria a possibilidade de "+
			"sombreamento que o rebaixamento daquele item declarou inexistente.", daPlataforma)
	}
}
