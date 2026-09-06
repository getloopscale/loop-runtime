// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domainsinkpurity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// prefixoDoModulo e' o unico prefixo que os caminhos vigiados usam. Derivar o
// diretorio do caminho de import so' e' valido enquanto isto for verdade, e a
// primeira assercao do teste e' justamente que continua sendo.
const prefixoDoModulo = "github.com/getloopscale/loopzero/"

// Este gate vigia por CAMINHO DE IMPORT. Um caminho que deixe de existir — um
// pacote movido, renomeado ou deletado — nao produz erro: a chave simplesmente
// para de casar, o pacote sai da vigilancia e o gate segue verde sobre menos do
// que promete.
//
// Nao e' hipotese. O doc-comment do proprio mapa registra que aconteceu: "a
// primeira implementacao nao vigiava `manifests` de jeito nenhum — tres
// referencias publicas cruzaram com o gate verde". O que faltava era esta
// verificacao, e sem ela o proximo movimento de pacote repete o episodio.
func TestCaminhosVigiadosExistemNaArvore(t *testing.T) {
	raiz := repoRoot(t)
	if len(watchedImportPaths) == 0 {
		t.Fatal("mapa de vigilancia vazio: um gate que vigia nada passa por vacuidade")
	}
	for caminho := range watchedImportPaths {
		if !strings.HasPrefix(caminho, prefixoDoModulo) {
			t.Errorf("%q nao comeca com %q — a traducao para diretorio deixou de valer",
				caminho, prefixoDoModulo)
			continue
		}
		dir := filepath.Join(raiz, strings.TrimPrefix(caminho, prefixoDoModulo))
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Errorf("o gate vigia %q, mas %s nao existe — o pacote saiu da vigilancia "+
				"em silencio e o verde deixou de significar o que promete", caminho, dir)
		}
	}
}

// O diretorio do sink tem o mesmo modo de falha, e e' pior: sem ele o gate nao
// tem o que ler.
func TestDiretorioDoSinkExiste(t *testing.T) {
	dir := filepath.Join(repoRoot(t), sinkDir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("sinkDir aponta para %s, que nao existe", dir)
	}
}
