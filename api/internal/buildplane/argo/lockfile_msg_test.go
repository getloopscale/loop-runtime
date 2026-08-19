// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"
)

// A instalacao de dependencias roda na RAIZ do workspace (`COPY . .` copia o
// bundle inteiro), enquanto o app pode declarar `path: api`. Quando o lockfile
// esta' no subdiretorio, o usuario le' "no lockfile found" com o arquivo commitado
// no proprio repositorio, e a mensagem nao dizia ONDE tinha procurado (B-155).
//
// Medido ao vivo em 2026-08-18: `api/requirements.txt` existia e o build morreu
// dizendo que nao havia lockfile.
func TestMensagemDeLockfileNomeiaOndeProcurou(t *testing.T) {
	for nome, script := range map[string]string{"node": nodeInstall, "python": pythonInstall} {
		if !strings.Contains(script, "$(pwd)") {
			t.Errorf("%s: a mensagem nao nomeia o diretorio onde procurou:\n%s", nome, script)
		}
		if !strings.Contains(script, "repository root") {
			t.Errorf("%s: a mensagem nao diz de onde as dependencias sao instaladas:\n%s", nome, script)
		}
		if !strings.Contains(script, "Commit one of these files at the root") {
			t.Errorf("%s: a mensagem nao diz o que fazer:\n%s", nome, script)
		}
	}
}

// A lista de arquivos procurados continua na mensagem: sem ela, saber o
// diretorio nao ajuda — o usuario precisa dos dois.
func TestMensagemDeLockfileSegueListandoOsArquivos(t *testing.T) {
	if !strings.Contains(pythonInstall, "requirements.txt") || !strings.Contains(pythonInstall, "poetry.lock") {
		t.Errorf("a lista de arquivos sumiu da mensagem python:\n%s", pythonInstall)
	}
	if !strings.Contains(nodeInstall, "pnpm-lock.yaml") || !strings.Contains(nodeInstall, "package-lock.json") {
		t.Errorf("a lista de arquivos sumiu da mensagem node:\n%s", nodeInstall)
	}
}
