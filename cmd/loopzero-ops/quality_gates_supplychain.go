// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/argoappidentity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartdefaulttags"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/gomodauthorg"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/baseimagedigest"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/clusterimagepolicymode"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/deadlineratchet"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/dockerfilemirror"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/ldflagsymbols"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nopulumiinlinesecrets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/prodimagedigests"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/prodimagetags"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/trivyscan"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/trufflehogscan"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/valuesduplicatekeys"
)

// M9 Step 7: quality gate entry points for the "supplychain" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

// runDockerfileNoDockerHub is the Go home for the dockerfile-no-docker-hub gate
// (migrated from infra/tests/scripts/dockerfile-no-docker-hub_test.sh). Fails if
// any infra/images/*/Dockerfile FROM would resolve to Docker Hub.
func runDockerfileNoDockerHub(args []string) int {
	fs := flag.NewFlagSet("dockerfile-no-docker-hub", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := dockerfilemirror.Check(dockerfilemirror.Config{
		ImagesDir: filepath.Join(*root, "infra", "images"),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dockerfile-no-docker-hub: %v\n", err)
		return 1
	}
	fmt.Printf("%d/%d Dockerfile FROMs OK\n", outcome.OK, outcome.Total)
	if !outcome.Pass {
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "FAIL: %s\n", v)
		}
		return 1
	}
	fmt.Println("dockerfile-no-docker-hub: PASS — all FROMs use a mirror/private registry")
	return 0
}

// runNoPulumiInlineSecrets is the Go home for the no-pulumi-inline-secrets
// gate (T9.6 of plan 2026-06-02-openbao-adoption v1.3).
func runNoPulumiInlineSecrets(args []string) int {
	fs := flag.NewFlagSet("no-pulumi-inline-secrets", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	allowlist := fs.String("allowlist", "", "comma-separated allowlisted paths")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	cfg := nopulumiinlinesecrets.Config{RepoRoot: *root}
	if *allowlist != "" {
		for _, p := range strings.Split(*allowlist, ",") {
			cfg.AllowlistPaths = append(cfg.AllowlistPaths, strings.TrimSpace(p))
		}
	}
	outcome, err := nopulumiinlinesecrets.Check(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-pulumi-inline-secrets: %v\n", err)
		return 5
	}
	fmt.Fprint(os.Stderr, nopulumiinlinesecrets.FormatOutcome(outcome))
	if !outcome.Pass {
		return 1
	}
	return 0
}

// runLdflagSymbols refuses a `-X` injection that points at a symbol which does
// not exist.
//
// `-X` over a missing path or symbol is a no-op that exits 0 — the linker does
// not warn — so the build succeeds and the binary keeps its default. Measured
// (B-053): the api Dockerfile injected into the wrong org AND a package the
// decomposition had moved, and the engine answered `{"version":"dev"}` live.
//
// It RESOLVES rather than matching spelling: the import path against the go.mod
// files on disk, the symbol against the package AST. A grep for the wrong org
// would catch one typo; resolving catches the package move that a legitimate
// refactor introduces.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-16-ldflag-symbol-resolution
func runLdflagSymbols(args []string) int {
	fs := flag.NewFlagSet("ldflag-symbols", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	out, err := ldflagsymbols.Check(ldflagsymbols.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ldflag-symbols: %v\n", err)
		return 1
	}
	fmt.Println(out.Summary())
	if !out.Pass {
		for _, v := range out.Violations {
			fmt.Printf("  %s:%d — `-X %s.%s`: %s\n", v.File, v.Line, v.ImportPath, v.Symbol, v.Reason)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, ldflagsymbols.ActionableFix)
		return 1
	}
	return 0
}

// runProdImageDigests checks that published platform charts use
// image@sha256:<digest> instead of mutable :tag references.
// T2.3 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap. Default mode:
// WARN (violations reported, exit 0). Pass --enforce to fail on
// violations after 7-day soak validates zero false positives.
func runProdImageDigests(args []string) int {
	fs := flag.NewFlagSet("prod-image-digests", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	enforce := fs.Bool("enforce", false, "fail on violations (default: warn-only)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	// `--enforce` upgrades the policy to every kind. Without it the decided
	// default applies: literal `:tag` blocks, missing `image.digest` warns until
	// the image publisher emits the value. See kind.go.
	policy := prodimagedigests.DefaultEnforce()
	if *enforce {
		policy = prodimagedigests.EnforceAll()
	}
	outcome, err := prodimagedigests.Check(prodimagedigests.Config{Root: *root, Enforce: policy})
	if err != nil {
		fmt.Fprintf(os.Stderr, "prod-image-digests: %v\n", err)
		return 5
	}
	if len(outcome.Violations) > 0 {
		// Reported per kind, not under one banner. One number over both
		// populations is what kept three fixable findings invisible inside seven
		// that nobody could fix — the reader could not tell which half was which.
		if blocking := outcome.Enforced(); len(blocking) > 0 {
			fmt.Fprintf(os.Stderr, "prod-image-digests: FAIL — %d referência(s) de imagem mutável que bloqueiam:\n", len(blocking))
			for _, v := range blocking {
				fmt.Fprintf(os.Stderr, "  %s\n", v)
			}
		}
		if warned := outcome.Warned(); len(warned) > 0 {
			fmt.Fprintf(os.Stderr, "prod-image-digests: WARN — %d referência(s) reportada(s) sem bloquear (aguardam o publicador de imagens):\n", len(warned))
			for _, v := range warned {
				fmt.Fprintf(os.Stderr, "  %s\n", v)
			}
		}
		fmt.Fprintln(os.Stderr, "")
		// Two classes, two fixes. Before B-113 only the first existed, and the
		// message said "use the helper" — useless advice for a chart that ALREADY
		// uses the helper and declares no digest, which is the whole second class.
		fmt.Fprintln(os.Stderr, "Fix — literal in the template (`image: foo/bar:v1.2.3`):")
		fmt.Fprintln(os.Stderr, "     use `image: foo/bar@sha256:<digest>`, or delegate to")
		fmt.Fprintln(os.Stderr, "     `{{ include \"loopzero-common.image\" . }}`, which prefers a digest.")
		fmt.Fprintln(os.Stderr, "Fix — `image.digest missing` in values.yaml:")
		fmt.Fprintln(os.Stderr, "     the template is already right; the VALUE is missing. Declare `image.digest`")
		fmt.Fprintln(os.Stderr, "     (the image publisher produces it — a digest pinned by hand")
		fmt.Fprintln(os.Stderr, "     becomes a lie on the next publication).")
		fmt.Fprintln(os.Stderr, "See docs/architecture.md for context.")
		if !outcome.Pass {
			return 1
		}
	}
	if outcome.Pass {
		fmt.Printf("prod-image-digests: PASS (scanned %d files, %d warn-mode violation(s))\n", outcome.ScannedFiles, len(outcome.Violations))
	}
	return 0
}

// runTrivyScan wraps `trivy fs --severity CRITICAL,HIGH` as a quality gate.
// It fails closed: a missing scanner or a scan that could not complete exits 1
// with the reason, exactly like a finding — there is no path that exits 0
// without a completed scan.
func runTrivyScan(args []string) int {
	fs := flag.NewFlagSet("trivy-scan", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory to scan")
	severities := fs.String("severities", "CRITICAL,HIGH", "comma-separated severity filter")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := trivyscan.Check(context.Background(), trivyscan.Config{
		RepoRoot:   *root,
		Severities: *severities,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "trivy-scan: FAIL — %v\n", err)
		return 1
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "trivy-scan: FAIL\n%s\n", outcome.Reason)
		return 1
	}
	fmt.Printf("trivy-scan: PASS — %s\n", outcome.Reason)
	return 0
}

// runTrufflehogScan wraps `trufflehog git --only-verified --fail` as
// a quality gate. T4.7 of plan. Fails closed on a missing or failed scan.
func runTrufflehogScan(args []string) int {
	fs := flag.NewFlagSet("trufflehog-scan", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory to scan")
	baseline := fs.String("baseline", "", "adjudicated baseline path (default: <root>/"+trufflehogscan.BaselineRelPath+")")
	write := fs.Bool("write", false, "regenerate the adjudicated baseline (refuses while any candidate is live)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	if *write {
		n, err := trufflehogscan.Generate(context.Background(), trufflehogscan.Config{
			RepoRoot: *root, BaselinePath: *baseline,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "trufflehog-scan: %v\n", err)
			return 1
		}
		fmt.Printf("trufflehog-scan: baseline rewritten — %d candidate(s) adjudicated\n", n)
		return 0
	}

	out, err := trufflehogscan.Check(context.Background(), trufflehogscan.Config{
		RepoRoot: *root, BaselinePath: *baseline,
	})
	if err != nil {
		// NO branch returns 0 without a verdict. The three paths that used to end in
		// `exit 0` without knowing whether a secret was present — missing binary, run
		// error, and `--only-verified` with no network — fail here (B-143).
		fmt.Fprintf(os.Stderr, "trufflehog-scan: FAIL — %v\n", err)
		return 1
	}
	fmt.Printf("trufflehog-scan: PASS — %s\n", out.Reason)
	// Not a failure, and not always staleness: the baseline is generated with
	// --no-verification, a strict superset of verified mode (measured 67 ⊇ 65). An
	// entry with no matching finding may be a candidate verification discards — and
	// its being here is exactly why a network outage does not turn into a false red.
	for _, e := range out.StaleEntries {
		fmt.Printf("  in the baseline with no finding this pass (not a failure; the baseline is a superset): %s [%s] %s\n",
			e.File, e.Detector, e.ID)
	}
	return 0
}

func runProdImageTags(args []string) int {
	fs := flag.NewFlagSet("prod-image-tags", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	scanPaths := fs.Args() // optional positional scan-path overrides
	o, err := prodimagetags.Check(prodimagetags.Config{Root: *root, ScanPaths: scanPaths})
	if err != nil {
		fmt.Fprintf(os.Stderr, "prod-image-tags: %v\n", err)
		return 2
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "✗ VIOLATION: %d mutable image tag(s) in production-scoped manifests:\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Per ADR-0020 D1: production manifests MUST use immutable :sha7 tags.")
		fmt.Fprintln(os.Stderr, "To override an intentional case, add '# loopzero:mutable-tag-allowed' + ADR ref.")
		return 1
	}
	fmt.Printf("✓ Scanned %d file(s). No :prod or :latest tags in production manifests.\n", o.ScannedFiles)
	return 0
}

func runCosignCutover(args []string) int {
	return runRatchet("cosign-cutover", deadlineratchet.Spec{
		MarkerRelPath:    ".github/v1.0-ga-date.txt",
		ExceptionRelPath: ".github/cosign-cutover-exception.txt",
		WarnDay:          75,
		DeadlineDay:      90,
	}, args, ratchetLabels{
		dormant:   "pre-GA — marker absent. Check is dormant.",
		subject:   "cosign keyless cutover",
		exception: ".github/cosign-cutover-exception.txt",
		adr:       "2026-05-12-cosign-keyless-v11-cutover.md",
	})
}

func runClusterImagePolicyMode(args []string) int {
	fs := flag.NewFlagSet("clusterimagepolicy-mode", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := clusterimagepolicymode.Check(clusterimagepolicymode.Config{Root: *root})
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "FAIL — clusterImagePolicy.mode governance violation: %s\n", o.Reason)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "The deny-flip rejects unsigned images at admission. Either revert mode to")
		fmt.Fprintln(os.Stderr, "'warn', OR record the decision in docs/decisions.md referencing the cutover")
		fmt.Fprintln(os.Stderr, "(T8.6 / find_infra_008 / deny-flip / deny-mode / mitigation #4).")
		return 1
	}
	fmt.Printf("PASS — %s\n", o.Reason)
	return 0
}

func runBaseImageDigest(args []string) int {
	fs := flag.NewFlagSet("base-image-digest", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := baseimagedigest.Check(baseimagedigest.Config{Root: *root})
	if !o.Pass {
		for _, v := range o.Violations {
			fmt.Printf("BASE-IMAGE-DIGEST VIOLATION: %s\n", v)
		}
		fmt.Fprintf(os.Stderr, "\nFAIL — %d tag-only base image(s).\n", len(o.Violations))
		fmt.Fprintln(os.Stderr, "Fix: replace 'tag' with 'tag@sha256:<digest>' (Renovate pinDigests: true),")
		fmt.Fprintln(os.Stderr, "OR add '# pragma: allow-tag-pin' on the line above the FROM, OR refresh the")
		fmt.Fprintln(os.Stderr, "baseline in cmd/loopzero-ops/internal/quality/baseimagedigest if the bump is intentional + grandfathered.")
		return 1
	}
	fmt.Printf("PASS — %d FROM directive(s); every base image is digest-pinned, baselined, or pragma'd.\n", o.Scanned)
	return 0
}

// runChartDefaultTags is the #232 sibling gate: loopzero-owned Helm charts must not
// DEFAULT their image tag to a mutable ref. prod-image-tags scans the deployed
// values and cannot see chart defaults. Registered but NOT wired — RED today,
// fix lives in infra/helm/ (governance-gated).
func runChartDefaultTags(args []string) int {
	fs := flag.NewFlagSet("chart-default-tags", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	out, err := chartdefaulttags.Check(chartdefaulttags.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "chart-default-tags: %v\n", err)
		return 2
	}
	if out.Skipped {
		fmt.Printf("chart-default-tags: SKIP — %s\n", out.Reason)
		return 0
	}
	if out.Pass {
		fmt.Printf("chart-default-tags: PASS — %s\n", out.Reason)
		return 0
	}
	fmt.Fprintln(os.Stderr, "chart-default-tags: FAIL")
	for _, f := range out.Findings {
		fmt.Fprintf(os.Stderr, "  %s:%d — chart %s defaults to mutable tag %q\n", f.File, f.Line, f.Chart, f.Tag)
	}
	fmt.Fprintln(os.Stderr, "Fix: prefer `tag: \"\"` — an empty default fails loudly if nothing overrides it, where `latest` fails silently (#232).")
	return 1
}

// runValuesDuplicateKeys refuses a Helm values file that declares one mapping key
// twice: Helm keeps the last declaration silently, so the first block never
// renders while the file reads as configured.
func runValuesDuplicateKeys(args []string) int {
	fs := flag.NewFlagSet("values-duplicate-keys", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	out, err := valuesduplicatekeys.Check(valuesduplicatekeys.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "values-duplicate-keys: %v\n", err)
		return 2
	}
	if out.Skipped {
		fmt.Printf("values-duplicate-keys: SKIP — %s\n", out.Reason)
		return 0
	}
	if out.Pass {
		fmt.Printf("values-duplicate-keys: PASS — %s\n", out.Reason)
		return 0
	}
	fmt.Fprintln(os.Stderr, "values-duplicate-keys: FAIL")
	for _, f := range out.Findings {
		fmt.Fprintf(os.Stderr, "  %s:%d — key %q already declared at line %d; Helm keeps this one and drops the first\n",
			f.File, f.Line, f.Path, f.FirstLine)
	}
	fmt.Fprintln(os.Stderr, "Fix: merge the two blocks into one key.")
	return 1
}

// runGoModAuthOrg implements the `gomod-auth-org` gate.
//
// Keeps the org that build files AUTHENTICATE to in sync with the org the go.mod files
// REQUIRE. When they drift, git fetches the private module unauthenticated and GitHub answers
// "not found" — the same message as a repo that does not exist — so the build fails naming the
// wrong cause. This shipped twice in two days from one root cause (ci.yml, then api/Dockerfile).
func runGoModAuthOrg(args []string) int {
	fs := flag.NewFlagSet("gomod-auth-org", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	rep, err := gomodauthorg.Check(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gomod-auth-org: %v\n", err)
		return 5
	}
	if rep.OK() {
		fmt.Printf("gomod-auth-org: PASS — auth org matches the go.mod requirement (%v)\n",
			rep.RequiredOrgs)
		return 0
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(os.Stderr, "gomod-auth-org: FAIL %s — %s\n", f.File, f.Message)
	}
	return 1
}

// runArgoAppIdentity is the Go home for the argo-app-identity gate.
//
// Keeps the ArgoCD Application name derived in exactly one place —
// pkg/naming.ArgoAppName(tenant, project, env). Ten sites across two modules rebuilt it
// by hand as {tenant}-{env}, dropping the project, so two projects of one tenant in one
// environment collided on a single Application with conflicting destinations (#271).
//
// Two of the ten were inside pruneOrphanPreviewApps, which DELETES every Application
// outside the set it builds — and that delete cascades through the ArgoCD finalizer into
// the tenant's workload. Renaming the emit side without them would have made the operator
// destroy exactly what it had just created.
func runArgoAppIdentity(args []string) int {
	fs := flag.NewFlagSet("argo-app-identity", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	rep, err := argoappidentity.Check(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "argo-app-identity: %v\n", err)
		return 5
	}
	if rep.OK() {
		fmt.Printf("argo-app-identity: PASS — %d file(s) scanned; Application and Environment CR "+
			"identities derived only from naming.ArgoAppName / naming.EnvironmentCRName\n", rep.Scanned)
		return 0
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(os.Stderr, "argo-app-identity: FAIL %s\n", f.Message())
	}
	return 1
}
