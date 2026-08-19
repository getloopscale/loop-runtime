// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A ServiceAccount que o chart PUBLICADO renderiza precisa ser a mesma que as politicas
// de admissao isentam — e precisa ser assim para QUALQUER nome de release.
//
// # B-096
//
// Ate' 2026-08-19, `charts/loopzero-api/values.yaml` deixava `serviceAccount.name: ""`, entao
// o nome derivava do release: `helm template t charts/loopzero-api` rendia `t-loopzero-api`. As
// VAPs isentam `system:serviceaccount:loopzero-system:loopzero-api`. Um deploy pelo chart
// publicado subia o loopzero-api com identidade que as proprias cercas da plataforma tratam
// como estranha, e a falha aparecia como `Forbidden` na reconciliacao — nao como erro de
// instalacao.
//
// O teste roda com DOIS nomes de release de proposito. Com um so', ele passaria tanto com
// o nome fixo quanto com um `values.yaml` que casualmente derivasse o nome certo daquele
// release — e a segunda forma nao e' garantia nenhuma.
func TestServiceAccountDoChartPublicadoBateComAIsencaoDasVAPs(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm nao esta' no PATH")
	}
	raiz := repoRoot(t)

	isentas := serviceAccountsIsentasPelasVAPs(t, raiz)
	if len(isentas) == 0 {
		t.Fatal("nenhuma ServiceAccount isenta encontrada nas VAPs — uma varredura que nao alcanca " +
			"nada reporta verde sobre nada, e este teste inteiro dependeria dela")
	}

	for _, release := range []string{"loopzero", "um-release-qualquer"} {
		out, err := exec.Command("helm", "template", release,
			filepath.Join(raiz, "charts", "loopzero-api")).CombinedOutput()
		if err != nil {
			t.Fatalf("helm template %s falhou: %v\n%s", release, err, truncate(string(out), 800))
		}
		sa := primeiroValor(string(out), "serviceAccountName:")
		if sa == "" {
			t.Fatalf("release %q: o render nao declara serviceAccountName", release)
		}
		if !isentas[sa] {
			t.Errorf("release %q: o chart publicado renderiza a ServiceAccount %q, e as VAPs isentam %v.\n"+
				"Um deploy por este chart subiria o loopzero-api com identidade que as proprias cercas da "+
				"plataforma tratam como estranha — a falha aparece como Forbidden na reconciliacao, nao "+
				"como erro de instalacao (B-096).", release, sa, chaves(isentas))
		}
	}
}

// serviceAccountsIsentasPelasVAPs le' as isencoes declaradas nos templates das politicas.
// Le' o FONTE em vez de renderizar: a lista e' literal nos arquivos, e renderizar exigiria
// os values do overlay — mais peças para a mesma resposta.
func serviceAccountsIsentasPelasVAPs(t *testing.T, raiz string) map[string]bool {
	t.Helper()
	dir := filepath.Join(raiz, "infra", "helm", "admission-policies", "templates")
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("lendo %s: %v", dir, err)
	}
	isentas := map[string]bool{}
	for _, e := range entradas {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "vap-") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Clean(filepath.Join(dir, e.Name())))
		if rerr != nil {
			continue
		}
		for _, linha := range strings.Split(string(b), "\n") {
			const marca = "system:serviceaccount:loopzero-system:"
			i := strings.Index(linha, marca)
			if i < 0 {
				continue
			}
			resto := linha[i+len(marca):]
			fim := strings.IndexAny(resto, "'\",: \t")
			if fim < 0 {
				fim = len(resto)
			}
			if nome := resto[:fim]; nome != "" {
				isentas[nome] = true
			}
		}
	}
	return isentas
}

func primeiroValor(rendered, chave string) string {
	for _, linha := range strings.Split(rendered, "\n") {
		if i := strings.Index(linha, chave); i >= 0 {
			return strings.TrimSpace(linha[i+len(chave):])
		}
	}
	return ""
}

func chaves(m map[string]bool) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	return k
}
