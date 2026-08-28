// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package charts reune os testes de render dos charts e os classificadores que eles usam.
//
// O comentario de pacote esta AQUI, e nao num `_test.go`, porque este e' o primeiro arquivo
// nao-de-teste do pacote — enquanto ele so' tinha testes, o ST1000 nao disparava.
package charts

import "strings"

// ClassificarFalhaDeRender diz se a saida de um `helmfile`/`helm template` que falhou
// indica REDE, e devolve o texto a acrescentar na mensagem do teste.
//
// # Por que isto existe (B-156)
//
// `delivery-gate.sh` roda `task quality:all` antes de TODO push e nao tem bypass. Um
// render que contata repositorio remoto transforma indisponibilidade de rede em entrega
// bloqueada — e a mensagem que o operador recebe e' `helmfile template failed: exit
// status 1` seguida de centenas de linhas. Medido em 2026-08-19: uma dessas falhas levou
// 123,89s e barrou um commit que so' tocava `CHANGELOG.md` e `BACKLOG.md`.
//
// O caso do `loopzero-cloud` foi resolvido na raiz — ele renderiza um chart LOCAL, entao
// `--skip-deps` elimina a rede (medido: 17 chamadas de repo e 10s viraram zero e 0s).
// O do `openbao` NAO pode: o chart vem de repositorio remoto, e com `--skip-deps` o
// render falha com `repo openbao not found`. Para esse, o que resta e' o segundo ramo da
// DoD — que a saida diga QUAL das duas coisas aconteceu.
//
// # O que ele deliberadamente NAO classifica como rede
//
// `repo <nome> not found` fica de fora de proposito. Isso e' repositorio nao declarado —
// configuracao, nao conectividade — e foi exatamente o que apareceu quando eu apliquei
// `--skip-deps` no lugar errado. Classifica-lo como rede mandaria a proxima pessoa
// procurar defeito na conexao por causa de um erro de arquivo.
func ClassificarFalhaDeRender(saida string) (rede bool, explicacao string) {
	assinaturas := []string{
		"dial tcp",
		"i/o timeout",
		"no such host",
		"connection refused",
		"TLS handshake timeout",
		"context deadline exceeded",
		"temporary failure in name resolution",
		"could not download",
		"failed to fetch",
	}
	baixo := strings.ToLower(saida)
	for _, a := range assinaturas {
		if strings.Contains(baixo, strings.ToLower(a)) {
			return true, "A FALHA E' DE REDE, nao de conteudo do chart: a saida contem " +
				"`" + a + "`. Este render contata repositorio remoto, entao indisponibilidade de " +
				"rede reprova o portao de entrega sem que nada no repositorio esteja errado " +
				"(B-156). Rode de novo com rede, ou pule com LOOPZERO_TEST_SKIP_HELMFILE=1."
		}
	}
	return false, "A saida nao carrega assinatura de rede — trate como falha de CONTEUDO " +
		"e leia o erro do helm acima."
}
