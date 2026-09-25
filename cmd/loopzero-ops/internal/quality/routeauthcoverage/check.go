// Package routeauthcoverage enforces R2-F8 (HIGH) from the 49-findings code review:
// every user-facing API route in api/internal/server/router.go MUST be registered
// INSIDE the middleware.Auth group OR appear in the explicit public-allowlist.
//
// New routes added without auth + without allowlist entry fail this gate.
//
// Algorithm, over the go/parser AST of router.go:
//  1. Locate the body of the `Group(func(r chi.Router){…})` that references
//     middleware.Auth, by AST position.
//  2. Locate the body of registerProtectedRoutes (also protected).
//  3. Enumerate every chi registration call (Get, Post, …, Handle, Method), on any
//     receiver chain, so `r.With(mw).Post(…)` counts like `r.Post(…)`.
//  4. For each: PASS if its line is inside either body OR the route matches an
//     allowlist prefix. FAIL otherwise.
//
// It then refuses an enumeration below the declared floor, and scans the rest of
// the api tree for route registrations outside router.go that are not declared.
// Hermetic: it parses source and runs nothing.
package routeauthcoverage

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const routerPath = "api/internal/server/router.go"

// allowlist mirrors the bash guard's ALLOWLIST array (order preserved).
// Prefix-match against the route string.
var allowlist = []string{
	"/api/v1/health",
	"/api/v1/health/deep",
	"/api/v1/version",
	// Contrato da frota de status. PUBLICO pelo mesmo motivo que o
	// /api/v1/version logo acima — serve o MESMO metadado de build, que nao e'
	// segredo — e por um motivo proprio: o coletor do painel /status sonda o alvo
	// sem sessao alguma. Atras do Auth, o painel leria 401 como "servico
	// indisponivel", que e' precisamente a mentira que ele existe para evitar.
	// Nao expoe nada alem de {version, built_at}.
	"/v1/version",
	"/internal/bootstrap",
	"/internal/argocd-callback",
	"/internal/v1/secret-fetch",
	"/api/v1/webhooks/github",
	"/api/v1/admin/tenants/evict",
	"/api/v1/admin/tenants/pin",
	"/api/v1/admin/releases",
}

// Config controls the check.
type Config struct {
	RepoRoot string
	// OffRouterMounts declara arquivos de produção que registram rota fora do
	// router.go, mapeando caminho → RAZÃO. Nil usa DefaultOffRouterMounts.
	//
	// O gate não decide se essas rotas estão protegidas — ele recusa não vê-las.
	// Um mapa vazio (não-nil) significa "nenhuma declaração", que é o que os
	// testes de recusa exercitam.
	OffRouterMounts map[string]string
	// MinRoutes é a contagem MÍNIMA de rotas enumeradas. Zero usa DefaultMinRoutes.
	//
	// Mínima e não exata de propósito: um número exato viraria manutenção a cada
	// rota nova, e a primeira pessoa a tropeçar nele o desligaria.
	MinRoutes int
}

// DefaultOffRouterMounts é a lista real, e hoje tem exatamente um item.
//
// Medida em 2026-08-17 varrendo `api/**/*.go` de produção: um único arquivo registra
// rota fora do router.go.
var DefaultOffRouterMounts = map[string]string{
	"api/internal/routes/timeline/deploy_timeline_register.go": "ponto de montagem canônico do timeline, chamado de router.go:550 — de DENTRO da faixa protegida. " +
		"O helper existe para que a fiação tenha teste (`deploy_timeline_register_test.go`), e a rota tem 401 próprio quando não há tenant. " +
		"Medido no B-123: o gate reportava 42 rotas sobre um sistema com pelo menos 43, e esta era a que faltava.",
}

// DefaultMinRoutes é o piso. O roteador real enumera ~71; 55 dá folga para remoção
// legítima de família (o B-080 está movendo rotas) e ainda derruba uma queda pela
// metade, que é o modo de falha realista.
const DefaultMinRoutes = 55

// Outcome is the summary.
type Outcome struct {
	AuthStart, AuthEnd int
	ProtStart, ProtEnd int
	PassCount          int
	Violations         []string
	// OffRouterUndeclared lista registros fora do router.go sem declaração.
	OffRouterUndeclared []string
}

// ErrViolations means at least one route is unprotected and un-allowlisted.
var ErrViolations = errors.New("at least one router.go route lacks middleware.Auth protection and lacks an allowlist entry")

// ErrRouterNotFound is emitted when api/internal/server/router.go is missing.
var ErrRouterNotFound = errors.New("router.go not found")

// ErrNoRoutesEnumerated means the walker found the structural markers but no
// route registrations at all. A coverage gate that examined nothing has not
// passed — it has abstained, and reporting that as PASS is how the auth
// invariant over the whole public surface would disappear in silence during the
// routes split (B-040, B-041).
var ErrNoRoutesEnumerated = errors.New("router.go parsed and the Auth group located, but ZERO route registrations were enumerated — the gate examined nothing, so it cannot vouch for anything")

// ErrAuthGroupMissing is emitted when the Auth chi.Router group cannot be
// located — that itself is a systemic regression the gate refuses to gloss over.
var ErrAuthGroupMissing = errors.New("could not locate the middleware.Auth chi.Router group in router.go")

// Check runs the router.go coverage audit.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	full := filepath.Join(root, routerPath)
	if _, statErr := os.Stat(full); statErr != nil {
		return Outcome{}, ErrRouterNotFound
	}
	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, full, nil, parser.SkipObjectResolution)
	if perr != nil {
		// Os DOIS erros são embrulhados: `errors.Is` continua reconhecendo o
		// sentinela, e quem lê o log recebe a posição exata do parse — que é o
		// diagnóstico inteiro quando um gate diz "não entendi o arquivo".
		return Outcome{}, fmt.Errorf("%w: %w", ErrRouterUnparseable, perr)
	}

	authStart, authEnd, protStart, protEnd := authRanges(fset, file)
	if authStart == 0 {
		return Outcome{}, ErrAuthGroupMissing
	}

	pass := 0
	var violations []string
	for _, site := range enumerateFile(fset, file, routerPath) {
		route := site.Pattern
		lineNo := site.Line

		// Inside Auth group
		if lineNo >= authStart && lineNo <= authEnd {
			pass++
			continue
		}
		// Inside registerProtectedRoutes
		if protStart > 0 && lineNo >= protStart && lineNo <= protEnd {
			pass++
			continue
		}
		// Allowlist prefix match
		if inAllowlist(route) {
			pass++
			continue
		}
		violations = append(violations, fmt.Sprintf("router.go:%d  %s  (NOT in Auth group, NOT in allowlist)", lineNo, route))
	}

	outcome := Outcome{
		AuthStart:  authStart,
		AuthEnd:    authEnd,
		ProtStart:  protStart,
		ProtEnd:    protEnd,
		PassCount:  pass,
		Violations: violations,
	}
	if len(violations) > 0 {
		return outcome, ErrViolations
	}
	// The enumeration floor (B-040). Order matters and is load-bearing: this
	// runs AFTER ErrViolations, and it counts routes ENUMERATED — approved plus
	// violating — not routes approved.
	//
	// Written over PassCount alone it would be wrong in the worst possible
	// direction. PassCount counts only APPROVED routes, so it is also zero when
	// EVERY route violates; the gate would then answer "enumerated nothing" — an
	// instrumentation error — for the most serious finding this repository can
	// produce, a public surface with no authentication. See EC-1 of
	// .claude/knowledge-base/reviews/slice-1-tripwires-edge-cases-2026-08-11.md.
	enumerated := outcome.PassCount + len(violations)
	if enumerated == 0 {
		return outcome, ErrNoRoutesEnumerated
	}

	// O piso por CONTAGEM MÍNIMA. `== 0` recusava só o colapso total; uma queda de
	// 71 para 35 passava, e é essa a queda que uma mudança de forma de registro
	// produz. Roda depois de ErrViolations pela mesma razão que o zero rodava:
	// enumerar pouco é erro de instrumentação, e um achado de segurança real tem
	// precedência sobre ele.
	floor := cfg.MinRoutes
	if floor == 0 {
		floor = DefaultMinRoutes
	}
	if enumerated < floor {
		return outcome, fmt.Errorf("%w: %d enumeradas, piso %d", ErrEnumerationBelowFloor, enumerated, floor)
	}

	// A varredura da árvore. Roda por último: uma rota desprotegida no router.go é
	// um achado maior que um ponto de montagem não declarado.
	declared := cfg.OffRouterMounts
	if declared == nil {
		declared = DefaultOffRouterMounts
	}
	undeclared, serr := scanOffRouter(root, declared)
	if serr != nil {
		return outcome, fmt.Errorf("varredura da árvore: %w", serr)
	}
	if len(undeclared) > 0 {
		outcome.OffRouterUndeclared = undeclared
		return outcome, ErrUndeclaredOffRouterMount
	}
	return outcome, nil
}

// inAllowlist returns true when the route is exactly an allowed prefix or a
// path under an allowed prefix.
func inAllowlist(route string) bool {
	for _, a := range allowlist {
		if route == a || strings.HasPrefix(route, a+"/") {
			return true
		}
	}
	return false
}
