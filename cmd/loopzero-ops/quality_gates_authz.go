// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/agplisolation"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/anonymousauth"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/billinglayers"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/billingsecrets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/caddyhostparity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/clusterrolenosecretsstar"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/dashboardrootparity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/deadlineratchet"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/lintcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/moduleselfcontained"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nolegacysatoken"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/oidccredadminloopzero"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/orykratoslifespan"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/routeauthcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/runbookparity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scheduledimage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/securitycontext"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/tenantpredicate"
)

// M9 Step 7: quality gate entry points for the "authz" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

// runClusterRoleNoSecretsStar is the Go home for the
// cluster-role-no-secrets-star gate (T5.15 of plan 2026-06-02-openbao-adoption
// v1.3). Walks templates/clusterrole.yaml in charts/ + infra/helm/ and FAILs
// if any unconditional secrets/configmaps cluster-wide grant is found.
func runClusterRoleNoSecretsStar(args []string) int {
	fs := flag.NewFlagSet("cluster-role-no-secrets-star", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	allowlist := fs.String("allowlist", "", "comma-separated extra allowlisted chart paths (relative to root)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	cfg := clusterrolenosecretsstar.Config{
		RepoRoot: *root,
	}
	if *allowlist != "" {
		for _, p := range strings.Split(*allowlist, ",") {
			cfg.AllowlistPaths = append(cfg.AllowlistPaths, strings.TrimSpace(p))
		}
	}

	outcome, err := clusterrolenosecretsstar.Check(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cluster-role-no-secrets-star: %v\n", err)
		return 5
	}
	fmt.Fprint(os.Stderr, clusterrolenosecretsstar.FormatOutcome(outcome))
	if !outcome.Pass {
		return 1
	}
	return 0
}

// runNoLegacySATokenSecret is the Go home for the no-legacy-sa-token-secret
// gate (T7.6 of plan 2026-06-02-openbao-adoption v1.3).
func runNoLegacySATokenSecret(args []string) int {
	fs := flag.NewFlagSet("no-legacy-sa-token-secret", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	allowlist := fs.String("allowlist", "", "comma-separated allowlisted paths")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	cfg := nolegacysatoken.Config{RepoRoot: *root}
	if *allowlist != "" {
		for _, p := range strings.Split(*allowlist, ",") {
			cfg.AllowlistPaths = append(cfg.AllowlistPaths, strings.TrimSpace(p))
		}
	}
	outcome, err := nolegacysatoken.Check(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-legacy-sa-token-secret: %v\n", err)
		return 5
	}
	fmt.Fprint(os.Stderr, nolegacysatoken.FormatOutcome(outcome))
	if !outcome.Pass {
		return 1
	}
	return 0
}

// runAnonymousAuth blocks `--kube-apiserver-arg=anonymous-auth=true` (the
// substring `anonymous-auth=true`) anywhere outside `infra/k3d/`. T1.5 / CR-3
// of MEET-2026-05-28-001 — see ADR
// 2026-05-28-T1.4-anonymous-auth-local-only.md for why the flag is
// LOCAL-only and why drift into a prod chart would broaden the apiserver
// attack surface.
func runAnonymousAuth(args []string) int {
	fs := flag.NewFlagSet("anonymous-auth", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := anonymousauth.Check(anonymousauth.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "anonymous-auth: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "anonymous-auth: FAIL — %d %q occurrence(s) outside infra/k3d/:\n", len(outcome.Violations), anonymousauth.ForbiddenLiteral)
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Fix: remove the flag, or move the file under infra/k3d/. Production DOKS uses authenticated OIDC discovery via Pulumi sigstore.ts — see ADR 2026-05-28-T1.4-anonymous-auth-local-only.")
		return 1
	}
	fmt.Printf("anonymous-auth: PASS — no %q outside infra/k3d/\n", anonymousauth.ForbiddenLiteral)
	return 0
}

func runServiceAuthRotation(args []string) int {
	return runRatchet("service-auth-rotation", deadlineratchet.Spec{
		MarkerRelPath:    ".github/service-auth-ed25519-rotation-date.txt",
		ExceptionRelPath: ".github/service-auth-rotation-exception.txt",
		WarnDay:          60,
		DeadlineDay:      90,
	}, args, ratchetLabels{
		dormant:   "no rotation marker — check is dormant. First rotation should create it.",
		subject:   "service-auth Ed25519 rotation",
		exception: ".github/service-auth-rotation-exception.txt",
		adr:       "2026-05-13-service-auth-ed25519-adoption.md",
	})
}

func runSecurityContext(args []string) int {
	fs := flag.NewFlagSet("security-context", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	gate := fs.Bool("gate", false, "gate mode: exit 1 if any NON-CONFORMANT chart lacks an ADR exception")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := securitycontext.Check(securitycontext.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "security-context: %v\n", err)
		return 2
	}

	if *gate {
		if o.Pass {
			fmt.Println("✅ securityContext audit GREEN — 0 NON-CONFORMANT charts without ADR exception.")
			return 0
		}
		fmt.Fprintf(os.Stderr, "❌ securityContext audit FAILED — %d NON-CONFORMANT chart(s) without ADR:\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
			fmt.Fprintf(os.Stderr, "    Action: fix securityContext in infra/helm/%s/, OR create infra/helm/%s/ADR-securitycontext-exception.md\n", v, v)
		}
		return 1
	}

	// report mode — markdown matrix to stdout.
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	fmt.Println("# securityContext Audit — infra/helm/")
	fmt.Println()
	fmt.Printf("**Charts inspected:** %d\n\n", len(o.Charts))
	fmt.Println("| Chart | runAsNonRoot | readOnlyRootFS | drop:[ALL] | allowPrivilegeEscalation:false | Status | ADR Exception |")
	fmt.Println("|---|---|---|---|---|---|---|")
	var conformant, partial, nonconf, na int
	for _, c := range o.Charts {
		if !c.PodBearing {
			fmt.Printf("| %s | n/a | n/a | n/a | n/a | N/A (no pods) | — |\n", c.Name)
			na++
			continue
		}
		adr := "—"
		if c.HasADR {
			adr = "✅ exists"
		} else if c.Status == "NON-CONFORMANT" {
			adr = "❌ missing"
		}
		fmt.Printf("| %s | %s | %s | %s | %s | %s | %s |\n",
			c.Name, yn(c.RunAsNonRoot), yn(c.ReadOnlyRootFS), yn(c.DropAll), yn(c.NoPrivEsc), c.Status, adr)
		switch c.Status {
		case "CONFORMANT":
			conformant++
		case "PARTIAL":
			partial++
		case "NON-CONFORMANT":
			nonconf++
		}
	}
	fmt.Printf("\n## Summary\n\n- CONFORMANT: %d/%d\n- PARTIAL: %d/%d\n- NON-CONFORMANT: %d/%d\n- N/A (no pods): %d/%d\n- NON-CONFORMANT without ADR: %d\n",
		conformant, len(o.Charts), partial, len(o.Charts), nonconf, len(o.Charts), na, len(o.Charts), len(o.Violations))
	return 0
}

func runAGPLIsolation(args []string) int {
	fs := flag.NewFlagSet("agpl-isolation", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := agplisolation.Check(agplisolation.Config{Root: *root})
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "FAIL — %d AGPL violation(s) outside the ADR-0048 allow-list:\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  AGPL VIOLATION: %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "AGPL-3.0 has copyleft network-distribution requirements. Either re-implement")
		fmt.Fprintln(os.Stderr, "with an Apache-2.0 alternative, OR add the import to DefaultAllowlist in")
		fmt.Fprintln(os.Stderr, "cmd/loopzero-ops/internal/quality/agplisolation with a NEW ADR + legal sign-off.")
		return 1
	}
	fmt.Println("PASS — no AGPL imports outside the ADR-0048 allow-list.")
	return 0
}

// runTenantPredicate enforces that every multi-tenant query carries the
// `tenant_id = $N` predicate (or marks itself exempt). T0.5 of plan
// 2026-05-29-sqlc-migration v1.2. Per platform-security-engineer condition
// 1 (VETO-backed) + M-1 + M-2.
func runTenantPredicate(args []string) int {
	fs := flag.NewFlagSet("tenant-predicate", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := tenantpredicate.Check(tenantpredicate.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "tenant-predicate: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "tenant-predicate: FAIL — %d violation(s) (scanned %d .sql files):\n",
			len(outcome.Violations), outcome.Scanned)
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  [%s] %s @ %s:%s — %s\n", v.Reason, v.Query, v.File, v.Table, v.Detail)
		}
		fmt.Fprintf(os.Stderr, "\nFix: %s\n", tenantpredicate.ActionableFix())
		return 1
	}
	fmt.Printf("tenant-predicate: PASS — %d .sql files scanned; every multi-tenant query carries tenant_id\n", outcome.Scanned)
	return 0
}

// runOIDCCredAdminLoopzero enforces the canonical admin@loopzero.example / loopzero-dev-password
// credential — rejects fabricated paulo@loopzero.test / LoopZeroDev2026!.
// Migrated from infra/tests/check-oidc-cred-admin-loopzero_test.sh (M-Migrate2 2026-07-07).
func runOIDCCredAdminLoopzero(args []string) int {
	fs := flag.NewFlagSet("oidc-cred-admin-loopzero", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := oidccredadminloopzero.Check(oidccredadminloopzero.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "oidc-cred-admin-loopzero: FAIL — %d fabricated credential reference(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("oidc-cred-admin-loopzero: PASS — no fabricated paulo@loopzero.test/LoopZeroDev2026! (canonical: admin@loopzero.example)")
	return 0
}

// runOryKratosLifespan enforces canonical self-service flow lifespans in
// infra/helm/ory-kratos/values.yaml.gotmpl (login=10m, registration=10m,
// recovery=15m — defense pair with edge rate_limit for Red Team #89).
// Migrated from infra/tests/helm/ory-kratos-lifespan_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runOryKratosLifespan(args []string) int {
	fs := flag.NewFlagSet("ory-kratos-lifespan", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := orykratoslifespan.Check(orykratoslifespan.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ory-kratos-lifespan: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("ory-kratos-lifespan: PASS — login/registration=10m, recovery=15m (defense pair with edge rate_limit for RedTeam #89)")
	return 0
}

// runRouteAuthCoverage enforces R2-F8 (HIGH): every user-facing router.go route
// MUST be inside middleware.Auth group OR registerProtectedRoutes OR appear in
// the explicit public-allowlist.
// Migrated from infra/tests/check-route-auth-coverage_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runRouteAuthCoverage(args []string) int {
	fs := flag.NewFlagSet("route-auth-coverage", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := routeauthcoverage.Check(routeauthcoverage.Config{RepoRoot: *root})
	if errors.Is(err, routeauthcoverage.ErrNoRoutesEnumerated) {
		// Named apart from the generic failure so the operator reads the CAUSE.
		// "0 routes" is the one failure whose fix is never in router.go: either
		// the registrations moved (B-041) and the walker must follow them, or
		// the receiver was renamed and the regexp in check.go:84 no longer
		// matches. Printing only the error would send the reader hunting for an
		// unprotected route that does not exist.
		fmt.Fprintf(os.Stderr, "route-auth-coverage: FAIL — %v\n", err)
		fmt.Fprintf(os.Stderr, "  Auth group spans lines %d..%d, registerProtectedRoutes %d..%d — both located.\n",
			outcome.AuthStart, outcome.AuthEnd, outcome.ProtStart, outcome.ProtEnd)
		fmt.Fprintln(os.Stderr, "  Fix: point the walker at wherever the r.{Get,Post,...} calls now live, or restore them to router.go.")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "route-auth-coverage: FAIL — %v\n", err)
		if len(outcome.Violations) > 0 {
			fmt.Fprintf(os.Stderr, "  Auth group spans lines %d..%d\n", outcome.AuthStart, outcome.AuthEnd)
			fmt.Fprintf(os.Stderr, "  registerProtectedRoutes spans lines %d..%d\n", outcome.ProtStart, outcome.ProtEnd)
			for _, v := range outcome.Violations {
				fmt.Fprintf(os.Stderr, "  %s\n", v)
			}
			fmt.Fprintln(os.Stderr, "Fix: move the route inside middleware.Auth group OR add explicit allowlist entry with rationale.")
		}
		return 1
	}
	fmt.Printf("route-auth-coverage: PASS — %d routes either in Auth group or allowlisted (auth spans %d..%d, protected %d..%d)\n",
		outcome.PassCount, outcome.AuthStart, outcome.AuthEnd, outcome.ProtStart, outcome.ProtEnd)
	return 0
}

// runBillingSecrets reprova opcionalidade LITERAL nos segredos de cobrança.
//
// Medido em 2026-08-11 no app-dev: o pod do painel rodou 20h `Running` sem
// `loopzero-stripe-webhook` e sem `loopzero-stripe-api-keys`, sem emitir um único evento —
// porque `optional: true` é, para o Kubernetes, uma configuração perfeitamente normal.
//
// O gate não exige que os segredos sejam sempre obrigatórios: o hermético monta sem
// credencial de propósito. Exige que a decisão seja um VALOR, porque um literal vale
// igualmente para todo ambiente, e o público acaba herdando a escolha mais permissiva.
func runBillingSecrets(args []string) int {
	fs := flag.NewFlagSet("billing-secrets", flag.ContinueOnError)
	root := fs.String("repo-root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	outcome, err := billingsecrets.Check(billingsecrets.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "billing-secrets: %v\n", err)
		return 5
	}
	if len(outcome.Findings) > 0 {
		fmt.Fprintf(os.Stderr, "billing-secrets: FAIL — %d montagem(ns) de segredo de cobrança com opcionalidade literal:\n",
			len(outcome.Findings))
		for _, f := range outcome.Findings {
			fmt.Fprintf(os.Stderr, "  %s:%d — %s\n", f.File, f.Line, f.Why)
		}
		return 2
	}
	// Dizer QUANTOS arquivos foram lidos importa: um gate que não achou nada por não ter
	// lido nada se pareceria, no log, com um gate que passou.
	fmt.Printf("billing-secrets: PASS — %d chart(s) verificados, opcionalidade decidida por valor\n",
		len(outcome.Scanned))
	return 0
}

// runBillingLayers verifica que a CADEIA de cobrança continua enumerável, com dono.
//
// O irmão `billing-secrets` cobre UMA camada. Este cobre a lista das cinco — e é a lista
// que o B-038 mede: "a quinta camada nasce sem a verificação que as outras quatro tinham".
func runModuleSelfContained(args []string) int {
	fs := flag.NewFlagSet("module-selfcontained", flag.ContinueOnError)
	root := fs.String("repo-root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	out, err := moduleselfcontained.Check(moduleselfcontained.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "module-selfcontained: FAIL — %v\n", err)
		for _, f := range out.Failed {
			fmt.Fprintf(os.Stderr, "  %s:\n%s\n", f.Module, f.Err)
		}
		return 2
	}
	fmt.Printf("module-selfcontained: PASS — %s\n", out.Reason)
	return 0
}

func runRunbookParity(args []string) int {
	fs := flag.NewFlagSet("runbook-parity", flag.ContinueOnError)
	root := fs.String("repo-root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	out, err := runbookparity.Check(runbookparity.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "runbook-parity: FAIL — %v\n", err)
		for _, f := range out.Findings {
			fmt.Fprintf(os.Stderr, "  %s:%d nomeia %q; runbook prescreve %q\n", f.File, f.Line, f.Named, f.Prescribed)
		}
		return 2
	}
	fmt.Printf("runbook-parity: PASS — %s\n", out.Reason)
	return 0
}

func runDashboardRootParity(args []string) int {
	fs := flag.NewFlagSet("dashboard-root-parity", flag.ContinueOnError)
	root := fs.String("repo-root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	out, err := dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard-root-parity: FAIL — %v\n", err)
		return 2
	}
	fmt.Printf("dashboard-root-parity: PASS — a borda serve %s e o runbook o nomeia\n", out.Served)
	return 0
}

func runCaddyHostParity(args []string) int {
	fs := flag.NewFlagSet("caddy-host-parity", flag.ContinueOnError)
	root := fs.String("repo-root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	out, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "caddy-host-parity: FAIL — %v\n", err)
		for _, f := range out.Findings {
			fmt.Fprintf(os.Stderr, "  %s sem bloco de site; sem certificado o handshake cai antes de qualquer HTTP\n", f.Host)
		}
		return 2
	}
	fmt.Printf("caddy-host-parity: PASS — %d hostnames exigidos, %d servidos\n", out.Scanned, out.Served)
	return 0
}

func runLintCoverage(args []string) int {
	fs := flag.NewFlagSet("lint-coverage", flag.ContinueOnError)
	root := fs.String("repo-root", ".", "repository root")
	write := fs.Bool("write", false, "recongela a linha de base com o medido")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	if *write {
		out, merr := lintcoverage.Measure(lintcoverage.Config{RepoRoot: *root})
		// Recongelar usa o MEDIDO, mesmo quando houve regressão — é um ato consciente,
		// e o diff do arquivo é onde ele fica visível.
		if merr != nil || (len(out.Lint) == 0 && len(out.Gofmt) == 0) {
			fmt.Fprintf(os.Stderr, "lint-coverage: nada medido, nada a congelar: %v\n", merr)
			return 5
		}
		p := filepath.Join(*root, lintcoverage.BaselineRelPath)
		if werr := lintcoverage.WriteBaseline(p, out.Lint, out.Gofmt); werr != nil {
			fmt.Fprintf(os.Stderr, "lint-coverage: %v\n", werr)
			return 5
		}
		fmt.Printf("lint-coverage: linha de base regravada — %s\n", out.Reason)
		return 0
	}

	out, err := lintcoverage.Check(lintcoverage.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "lint-coverage: FAIL — %v\n", err)
		for _, r := range out.Regressions {
			fmt.Fprintf(os.Stderr, "  regressão: %s [%s] %d → %d\n", r.Module, r.Dimension, r.Was, r.Now)
		}
		return 2
	}
	fmt.Printf("lint-coverage: PASS — %s\n", out.Reason)
	for _, i := range out.Improvements {
		fmt.Printf("  melhorou: %s %d → %d (recongele com `--write`)\n", i.Module, i.Was, i.Now)
	}
	return 0
}

func runScheduledImage(args []string) int {
	fs := flag.NewFlagSet("scheduled-image", flag.ContinueOnError)
	root := fs.String("repo-root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	out, err := scheduledimage.Check(scheduledimage.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "scheduled-image: FAIL — %v\n", err)
		for _, f := range out.Findings {
			fmt.Fprintf(os.Stderr, "  %s [%s] %s\n", f.File, f.Chart, f.Image)
		}
		return 2
	}
	fmt.Printf("scheduled-image: PASS — %s\n", out.Reason)
	return 0
}

func runBillingLayers(args []string) int {
	fs := flag.NewFlagSet("billing-layers", flag.ContinueOnError)
	root := fs.String("repo-root", ".", "repository root")
	sibling := fs.String("sibling-root", "", "raiz de ../loopzero-cloud (padrão: irmão do repo-root)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	if *sibling == "" {
		*sibling = filepath.Join(*root, "..", "loopzero-cloud")
	}

	out, err := billinglayers.Check(billinglayers.Config{RepoRoot: *root, SiblingRoot: *sibling})
	if err != nil {
		// Não existe ramo que devolva 0 sem veredito: uma guarda que não resolve, uma
		// dívida fechada sem cobertura, ou uma camada nova não declarada reprovam aqui.
		fmt.Fprintf(os.Stderr, "billing-layers: FAIL — %v\n", err)
		return 2
	}
	fmt.Printf("billing-layers: PASS — %s\n", out.Reason)
	return 0
}
