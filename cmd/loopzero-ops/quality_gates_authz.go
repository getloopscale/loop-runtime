// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/agplisolation"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/anonymousauth"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/clusterrolenosecretsstar"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/deadlineratchet"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nolegacysatoken"
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
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

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
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
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
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

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
		fmt.Fprintln(os.Stderr, "pkg/quality/agplisolation with a NEW ADR + legal sign-off.")
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
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

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
