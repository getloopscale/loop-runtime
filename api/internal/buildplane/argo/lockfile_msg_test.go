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

// O ramo de lockfile ausente precisa REPROVAR, e nao so' avisar. Sem isto, a instalacao
// seguiria sem dependencias e a falha apareceria depois, noutro passo, com causa aparente
// errada — a mesma familia do B-153, onde um `exit 1` existia e era engolido pelo `tee`.
func TestLockfileAusenteReprovaEmVezDeSoAvisar(t *testing.T) {
	for nome, script := range map[string]string{"node": nodeInstall, "python": pythonInstall} {
		if !strings.Contains(script, "exit 1") {
			t.Errorf("%s: o ramo de lockfile ausente nao reprova; um build sem dependencias falharia "+
				"depois, noutro passo, com causa aparente errada:\n%s", nome, script)
		}
		if !strings.Contains(script, ">&2") {
			t.Errorf("%s: a mensagem nao vai para stderr:\n%s", nome, script)
		}
	}
}

// A DECISAO que o B-155 pedia registrar, fixada aqui em vez de ficar implicita no codigo:
// **lockfile por app nao e' suportado**. A instalacao roda no WORKDIR da imagem, que e' a
// raiz do repositorio; quem e' monorepo-aware e' a etapa de BUILD, nao a de instalacao.
//
// Se algum dia a instalacao passar a receber o diretorio do app, a decisao mudou — e este
// teste obriga a revisitar o item, em vez de deixar a mensagem que promete
// "installed from the repository root" virar mentira em silencio.
func TestApenasOBuildEMonorepoAwareNaoAInstalacao(t *testing.T) {
	const appDir = "servicos/api"
	if build := nodeBuild(appDir); !strings.Contains(build, appDir) {
		t.Errorf("a etapa de build deixou de ser monorepo-aware; nao cita %q:\n%s", appDir, build)
	}
	for nome, script := range map[string]string{"node": nodeInstall, "python": pythonInstall} {
		if strings.Contains(script, appDir) {
			t.Errorf("%s: a instalacao passou a citar o diretorio do app. Se for deliberado, a mensagem "+
				"que diz \"installed from the repository root\" virou mentira — reveja o B-155", nome)
		}
	}
}
