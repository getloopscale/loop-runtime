// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"go/ast"
	"go/parser"
	"go/token"
	gotypes "go/types"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/server"
	"github.com/getloopscale/loopzero/api/internal/surfaces"
)

// TestControlSurfaces_DesligadasSaoObservaveisComRazao is B-115 slice 1: the
// platform's own control paths report whether they were registered.
//
// They were converted first because their absence is invisible from the product
// side. Nobody files a ticket when the ESO broker stops answering — the fleet
// just stops converging — and `secret_fetch` is measured OFF in dev-public right
// now, because the Secret it needs does not exist.
func TestControlSurfaces_DesligadasSaoObservaveisComRazao(t *testing.T) {
	// Rotas que cada superfície publica, para parear métrica com alcançabilidade.
	// Uma métrica que discordasse do roteador seria pior que a ausência dela.
	rotas := map[string]struct{ method, path string }{
		surfaces.Bootstrap:      {http.MethodPost, "/internal/bootstrap"},
		surfaces.ArgoCallback:   {http.MethodPost, "/internal/argocd-callback"},
		surfaces.SecretFetch:    {http.MethodPost, "/internal/v1/secret-fetch/tn-x/SECRET"},
		surfaces.AdminEvict:     {http.MethodPost, "/api/v1/admin/tenants/evict"},
		surfaces.AdminPin:       {http.MethodPost, "/api/v1/admin/tenants/pin"},
		surfaces.ReleaseInspect: {http.MethodGet, "/api/v1/admin/releases/r-1/inspect"},
	}

	reg := surfaces.NewRegistry()
	router := server.NewRouter(server.Config{Surfaces: reg})
	metrics.PublishSurfaces(reg.All())

	// As sete de topo. `ActiveTeam` fica de fora POR SER ANINHADA: a guarda dela
	// vive dentro do grupo autenticado, que num Config vazio está desligado, então
	// ela nem é avaliada. A série fica ausente em vez de zero — tolerável só
	// porque o zero da mãe (`auth_protected`) domina a pergunta. Ver o doc da
	// constante. Afirmar aqui que ela é gravada seria afirmar o contrário do que
	// o código faz.
	esperadas := []string{
		surfaces.Bootstrap, surfaces.ArgoCallback, surfaces.SecretFetch,
		surfaces.AdminEvict, surfaces.AdminPin, surfaces.ReleaseInspect,
		surfaces.AuthProtected,
	}

	if _, gravada := gravadasPorNome(reg)[surfaces.ActiveTeam]; gravada {
		t.Error("`active_team` foi gravada com o grupo autenticado desligado — " +
			"a semântica de aninhamento mudou e o doc da constante ficou falso")
	}

	gravadas := gravadasPorNome(reg)

	for _, name := range esperadas {
		s, ok := gravadas[name]
		if !ok {
			t.Errorf("superfície %q não foi gravada — o roteador decidiu e descartou a decisão", name)
			continue
		}
		if s.Enabled {
			t.Errorf("superfície %q reporta LIGADA num Config vazio", name)
			continue
		}
		if s.Reason == "" {
			t.Errorf("superfície %q está desligada sem razão — quem vê o zero não sabe o que provisionar", name)
		}
		if got := testutil.ToFloat64(metrics.SurfaceEnabled.WithLabelValues(name)); got != 0 {
			t.Errorf("métrica de %q = %v, esperado 0", name, got)
		}

		// A rota tem de estar mesmo ausente. Sem isto a métrica poderia mentir
		// nos dois sentidos: reportar desligada uma rota que responde, ou o
		// contrário.
		if r, temRota := rotas[name]; temRota {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(r.method, r.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("superfície %q reporta desligada mas %s %s devolveu %d, esperado 404",
					name, r.method, r.path, rec.Code)
			}
		}
	}
}

func gravadasPorNome(reg *surfaces.Registry) map[string]surfaces.Surface {
	out := map[string]surfaces.Surface{}
	for _, s := range reg.All() {
		out[s.Name] = s
	}
	return out
}

// TestSecretFetchSurface_LigadaQuandoOHandlerExiste proves the enabled half.
//
// `secret_fetch` is the representative because it is the one measured absent in
// dev-public (the `loopzero-broker-token` Secret does not exist), and because its
// dependency is a plain http.Handler — provable without stubbing five interfaces
// to assert the same property.
func TestSecretFetchSurface_LigadaQuandoOHandlerExiste(t *testing.T) {
	reg := surfaces.NewRegistry()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	router := server.NewRouter(server.Config{Surfaces: reg, Secrets: server.SecretsConfig{FetchHandler: handler}})
	metrics.PublishSurfaces(reg.All())

	if got := testutil.ToFloat64(metrics.SurfaceEnabled.WithLabelValues(surfaces.SecretFetch)); got != 1 {
		t.Errorf("métrica = %v com o handler presente, esperado 1", got)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/internal/v1/secret-fetch/tn-x/SECRET", nil))
	if rec.Code == http.StatusNotFound {
		t.Error("a rota devolveu 404 com o handler presente — a métrica estaria mentindo")
	}
}

// TestRouter_CadaSuperficieUsaUmNomeDistinto is structural, and it guards the one
// failure mode the behavioural tests above cannot see.
//
// `Registry.Record` refuses a duplicate name at RUNTIME, and `Recorded` swallows
// that error by design — so a copy-paste that reused a constant would produce a
// router that works, a series that silently reports one surface's state for two,
// and no test failure anywhere. Reading the source is the only place that catch
// can live.
func TestRouter_CadaSuperficieUsaUmNomeDistinto(t *testing.T) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "../../internal/server/router.go", nil, 0)
	if err != nil {
		t.Fatalf("parse router.go: %v", err)
	}

	vistos := map[string]int{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Recorded" && sel.Sel.Name != "Record") {
			return true
		}
		// O nome é o argumento imediatamente após o registro em `Recorded`, e o
		// primeiro em `Record`. Pegar por posição é frágil; procurar o argumento
		// que referencia o pacote `surfaces` é o que identifica a constante.
		for _, arg := range call.Args {
			src := gotypes.ExprString(arg)
			if len(src) > 9 && src[:9] == "surfaces." {
				vistos[src]++
			}
		}
		return true
	})

	if len(vistos) == 0 {
		t.Fatal("nenhuma constante de superfície encontrada em router.go — ou o roteador " +
			"mudou de forma, ou este teste parou de o reconhecer")
	}
	for nome, n := range vistos {
		// Duas ocorrências são a forma explícita if/else (uma por ramo). Três ou
		// mais significam reuso de constante entre superfícies distintas.
		if n > 2 {
			t.Errorf("a constante %s aparece %d vezes — duas superfícies compartilhando "+
				"nome tornam a série ambígua, e Recorded engole o erro que Record devolveria",
				nome, n)
		}
	}
}
