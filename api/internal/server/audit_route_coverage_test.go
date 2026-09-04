// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	mwaudit "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	policy "github.com/getloopscale/loopzero/api/internal/middleware/policy"
	ratelimit "github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
	"github.com/getloopscale/loopzero/api/internal/routes/argocd"
	"github.com/getloopscale/loopzero/api/internal/routes/audit"
	"github.com/getloopscale/loopzero/api/internal/routes/billing"
	"github.com/getloopscale/loopzero/api/internal/routes/builds"
	"github.com/getloopscale/loopzero/api/internal/routes/domains"
	"github.com/getloopscale/loopzero/api/internal/routes/environments"
	"github.com/getloopscale/loopzero/api/internal/routes/integrations"
	"github.com/getloopscale/loopzero/api/internal/routes/logs"
	"github.com/getloopscale/loopzero/api/internal/routes/promote"
	"github.com/getloopscale/loopzero/api/internal/routes/repositories"
	"github.com/getloopscale/loopzero/api/internal/routes/rollback"
	"github.com/getloopscale/loopzero/api/internal/routes/scale"
	"github.com/getloopscale/loopzero/api/internal/routes/status"
	"github.com/getloopscale/loopzero/api/internal/routes/teams"
	"github.com/getloopscale/loopzero/pkg/enforcement"
)

// B-059, terceira bala: "existe verificacao de que toda rota mutante continua gravando linha de
// auditoria com resource_id — hoje nenhum gate assere isso".
//
// A fatia anterior nao conseguiu escrever esta verificacao, e registrou por que: o caminho certo
// e' `chi.Walk` sobre o router REAL, mas `NewRouter(Config{})` monta um router vazio — o grupo
// auditado exige `TokenValidator` e `MemberStore` nao-nulos (`router.go:228`). A varredura andaria
// sobre zero rotas e passaria por vacuidade, que e' o defeito do B-040.
//
// O que faltava era o fixture, e ele sao duas interfaces de UM metodo cada. Com elas o grupo
// auditado monta, o `chi.Walk` enxerga as rotas, e a verificacao mede o que se propos a medir.
//
// Ela e' de CAIXA-PRETA de proposito: dispara a requisicao e le' o que o escritor de auditoria
// gravou. Chamar `deriveAction` direto testaria a funcao; isto testa o caminho — inclusive o
// middleware estar de fato montado sobre a rota.
func TestB059_TodaRotaMutanteGravaAcaoDeclarada(t *testing.T) {
	var gravadas []linhaDeAuditoria
	escritor := mwaudit.AuditWriter(func(tenantID, action, actor, resourceType, resourceID,
		correlationID string, metadata map[string]any) {
		gravadas = append(gravadas, linhaDeAuditoria{acao: action, tipo: resourceType, id: resourceID})
	})
	cfg := Config{
		TokenValidator:            validadorFixo{},
		MemberStore:               membroFixo{},
		AuditWriter:               escritor,
		BuildHandler:              &builds.BuildHandler{},
		BuildAttestationHandler:   &builds.BuildAttestationHandler{},
		BuildSBOMHandler:          &builds.BuildSBOMHandler{},
		ArgoCallbackHandler:       &argocd.ArgoCallbackHandler{},
		RateLimiter:               &ratelimit.RateLimiter{},
		IPRateLimit:               &ratelimit.IPRateLimiter{},
		VerifyRateLimiter:         &ratelimit.VerifyRateLimiter{},
		LogsHandler:               &logs.LogsHandler{},
		LogStreamHandler:          &logs.LogStreamHandler{},
		RollbackHandler:           &rollback.RollbackHandler{},
		PromoteHandler:            &promote.PromoteHandler{},
		StatusHandler:             &status.StatusHandler{},
		ScaleHandler:              &scale.ScaleHandler{},
		BuildListHandler:          &builds.BuildListHandler{},
		BuildStatsHandler:         &builds.BuildStatsHandler{},
		EnvironmentHandler:        &environments.EnvironmentHandler{},
		TeamHandler:               &teams.TeamHandler{},
		DomainHandler:             &domains.DomainHandler{},
		RepositoryHandler:         &repositories.RepositoryHandler{},
		IntegrationsGitHubHandler: &integrations.IntegrationsGitHubHandler{},
		AuditHandler:              &audit.AuditHandler{},
		BillingHandler:            &billing.BillingHandler{},
		PlanLimiter:               &enforcement.PlanLimiter{},
		CostEnforcer:              &policy.CostEnforcer{},
	}
	r := NewRouter(cfg)

	rotas := rotasMutantes(t, r)
	// O piso que impede o verde sobre nada. MEDIDO em 2026-08-19: 17 rotas mutantes.
	//
	// COBERTURA PARCIAL, e ela esta' declarada em vez de suposta. O repositorio tem ~32 rotas
	// mutantes; este fixture alcanca as que dependem de handlers de tipo PONTEIRO, que se
	// constroem com valor-zero. As demais dependem de campos de INTERFACE (ProjectDeleter,
	// ObjectUploader, IdempotencyStore e outros), cada um exigindo o seu proprio duble — fatia
	// propria. Enquanto isso a tabela declarada da fatia anterior cobre o restante.
	//
	// Dizer "toda rota mutante" sem esta nota seria a forma que este item existe para combater.
	if len(rotas) < 17 {
		t.Fatalf("a varredura achou %d rota(s) mutante(s) (%v); o fixture deve montar o grupo "+
			"auditado — uma varredura que nao alcanca nada reporta verde sobre nada (B-040)",
			len(rotas), rotas)
	}

	// Os handlers do fixture sao de valor-zero: registram rota e entrariam em panico se
	// executados. A requisicao passa entao pelo MIDDLEWARE DE AUDITORIA real — que e' quem
	// deriva a acao em producao — envolvendo um handler inerte. Continua sendo codigo de
	// producao a decidir; o que nao se exercita e' o corpo do handler, que nao participa da
	// derivacao.
	auditar := mwaudit.AuditLog(slog.New(slog.NewTextHandler(io.Discard, nil)), escritor)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	t.Logf("rotas mutantes alcancadas pelo fixture: %d", len(rotas))

	var semAcao []string
	var inspecionadas int
	for _, rt := range rotas {
		gravadas = nil
		req := httptest.NewRequest(rt.metodo, concretizar(rt.padrao), nil)
		auditar.ServeHTTP(httptest.NewRecorder(), comUsuario(req))

		inspecionadas += len(gravadas)
		for _, g := range gravadas {
			if strings.HasPrefix(g.acao, "unknown.") {
				semAcao = append(semAcao, rt.metodo+" "+rt.padrao+" -> "+g.acao)
			}
		}
	}
	// Sem isto, zero acusacoes seria indistinguivel de zero inspecoes — e foi exatamente esse o
	// estado da primeira versao deste teste, pega pelo mutante abaixo.
	if inspecionadas < len(rotas) {
		t.Fatalf("%d linha(s) de auditoria inspecionada(s) para %d rota(s): o middleware nao gravou "+
			"para todas, entao o verde nao cobre o que aparenta", inspecionadas, len(rotas))
	}
	sort.Strings(semAcao)
	if len(semAcao) > 0 {
		t.Errorf("%d rota(s) mutante(s) gravam acao de fallback. O fallback devolve o ultimo "+
			"segmento do caminho CONCRETO, entao a acao carrega o id e a cardinalidade e' ilimitada: "+
			"nenhuma consulta consegue filtrar essas operacoes.\n  %s",
			len(semAcao), strings.Join(semAcao, "\n  "))
	}
}

// comUsuario injeta o UserContext que a autenticacao poe em producao. Sem ele o middleware de
// auditoria nao grava linha nenhuma (`audit.go`: escreve so' quando `tenantID != ""`), e o teste
// passaria sem inspecionar coisa alguma — foi o mutante abaixo que pegou isso.
func comUsuario(req *http.Request) *http.Request {
	uc := &auth.UserContext{UserID: "u-fixo", Email: "u@example.test", TenantID: "tn-fixo"}
	return req.WithContext(context.WithValue(req.Context(), auth.UserContextKeyPublic(), uc))
}

type linhaDeAuditoria struct{ acao, tipo, id string }

type rota struct{ metodo, padrao string }

// rotasMutantes percorre o router REAL. `chi.Walk` e' recurso nativo do roteador ja instalado
// (escada da parsimonia, degrau 3) — nao ha lista mantida a mao que possa envelhecer.
func rotasMutantes(t *testing.T, r chi.Router) []rota {
	t.Helper()
	var out []rota
	err := chi.Walk(r, func(metodo, padrao string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch metodo {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			out = append(out, rota{metodo: metodo, padrao: padrao})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	return out
}

// concretizar troca cada `{param}` por um valor fixo. O valor e' irrelevante para a acao — e essa
// e' justamente a propriedade sob teste: a acao gravada nao pode variar com o id.
func concretizar(padrao string) string {
	var b strings.Builder
	for _, seg := range strings.Split(padrao, "/") {
		if seg == "" {
			continue
		}
		b.WriteString("/")
		if strings.HasPrefix(seg, "{") {
			b.WriteString("fixo")
		} else {
			b.WriteString(strings.TrimSuffix(seg, "/*"))
		}
	}
	if b.Len() == 0 {
		return "/"
	}
	return b.String()
}

type validadorFixo struct{}

func (validadorFixo) ValidateToken(_ context.Context, _ string) (*auth.TokenValidationResult, error) {
	return &auth.TokenValidationResult{UserID: "u-fixo", Email: "u@example.test"}, nil
}

type membroFixo struct{}

func (membroFixo) GetMemberships(_ context.Context, _ string) ([]auth.TenantMembership, error) {
	return []auth.TenantMembership{{TenantID: "tn-fixo"}}, nil
}

// O mutante: sem ele, um PASS acima seria indistinguivel de um detector que nunca dispara.
// Um caminho que nenhuma regra mapeia TEM de ser acusado — e a acusacao carrega o ultimo
// segmento do caminho concreto, que e' o dano: a acao varia com o id.
func TestB059_ODetectorAcusaCaminhoNaoMapeado(t *testing.T) {
	var gravadas []linhaDeAuditoria
	escritor := mwaudit.AuditWriter(func(_, action, _, _, _, _ string, _ map[string]any) {
		gravadas = append(gravadas, linhaDeAuditoria{acao: action})
	})
	auditar := mwaudit.AuditLog(slog.New(slog.NewTextHandler(io.Discard, nil)), escritor)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	auditar.ServeHTTP(httptest.NewRecorder(),
		comUsuario(httptest.NewRequest(http.MethodPost, "/api/v1/coisa-que-ninguem-mapeou/abc123", nil)))

	if len(gravadas) == 0 {
		t.Fatal("nenhuma linha de auditoria gravada para uma mutacao — o middleware nao esta' medindo nada")
	}
	if !strings.HasPrefix(gravadas[0].acao, "unknown.") {
		t.Errorf("acao = %q; um caminho nao mapeado tem de cair no fallback, senao o teste "+
			"principal nunca teria como acusar coisa alguma", gravadas[0].acao)
	}
	if !strings.Contains(gravadas[0].acao, "abc123") {
		t.Errorf("acao = %q; o fallback devolve o ultimo segmento do caminho CONCRETO — e' por "+
			"isso que a cardinalidade fica ilimitada e nenhuma consulta consegue agrupar", gravadas[0].acao)
	}
}
