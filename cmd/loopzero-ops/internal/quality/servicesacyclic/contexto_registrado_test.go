// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package servicesacyclic

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// O `allowed` e' deny-by-default: um subpacote AUSENTE dele reprova, forcando
// alguem a decidir conscientemente que contexto delimitado esta' criando. Um
// contexto registrado para codigo que NAO EXISTE gasta essa decisao adiantado —
// quem criar o diretorio depois comeca a importar sem que o gate pergunte nada.
//
// Medido em 2026-08-18 (B-059..B-065): `billing` estava registrado como folha
// pura e `api/internal/services/billing` era um diretorio VAZIO e nao rastreado
// pelo git — inexistente num clone novo. O codigo foi extraido para
// `../loopzero-cloud` no M4 e o registro ficou.
//
// A verificacao usa `git ls-files` e nao `os.Stat`: um diretorio vazio existe no
// disco de quem ja' rodou o build e nao existe num clone novo, entao o disco
// responderia coisas diferentes em maquinas diferentes. O indice do git e' o
// mesmo em toda parte.
func TestTodoContextoRegistradoTemCodigoRastreado(t *testing.T) {
	raiz := qtestutil.Root(t)
	if len(allowed) == 0 {
		t.Fatal("DAG congelado vazio: uma verificacao sobre nada passa por vacuidade")
	}
	for contexto := range allowed {
		dir := filepath.Join(servicesRoot, contexto)
		out, err := exec.Command("git", "-C", raiz, "ls-files", "--", dir).Output()
		if err != nil {
			t.Fatalf("git ls-files %s: %v", dir, err)
		}
		var arquivosGo int
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.HasSuffix(l, ".go") {
				arquivosGo++
			}
		}
		if arquivosGo == 0 {
			t.Errorf("o DAG congelado registra o contexto %q, e %s nao tem nenhum .go rastreado — "+
				"um contexto fantasma pre-aprova a decisao que este gate existe para forcar",
				contexto, dir)
		}
	}
}
