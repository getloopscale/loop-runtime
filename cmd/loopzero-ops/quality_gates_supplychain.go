// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/baseimagedigest"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/clusterimagepolicymode"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/deadlineratchet"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/dockerfilemirror"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nopulumiinlinesecrets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/prodimagedigests"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/prodimagetags"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/trivyscan"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/trufflehogscan"
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

	outcome, err := prodimagedigests.Check(prodimagedigests.Config{Root: *root, EnforceMode: *enforce})
	if err != nil {
		fmt.Fprintf(os.Stderr, "prod-image-digests: %v\n", err)
		return 5
	}
	if len(outcome.Violations) > 0 {
		mode := "WARN"
		if *enforce {
			mode = "FAIL"
		}
		fmt.Fprintf(os.Stderr, "prod-image-digests: %s — %d mutable :tag reference(s) in platform charts:\n", mode, len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Fix: replace `image: foo/bar:v1.2.3` with `image: foo/bar@sha256:<digest>`")
		fmt.Fprintln(os.Stderr, "     OR use `{{ include \"loopzero-common.image\" . }}` helper which prefers digest.")
		fmt.Fprintln(os.Stderr, "See docs/architecture/infra/diff/cosign-digest-enforcement.md for context.")
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
// T4.7 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap. Skip-on-missing —
// returns exit 0 with Skipped reason when trivy binary absent (local-dev
// fallback). PR-time enforcement happens in chosen CI platform (T0.2).
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
		fmt.Fprintf(os.Stderr, "trivy-scan: %v\n", err)
		return 5
	}
	if outcome.Skipped {
		fmt.Printf("trivy-scan: SKIP — %s\n", outcome.Reason)
		return 0
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "trivy-scan: FAIL\n%s\n", outcome.Reason)
		return 1
	}
	fmt.Printf("trivy-scan: PASS — %s\n", outcome.Reason)
	return 0
}

// runTrufflehogScan wraps `trufflehog git --only-verified --fail` as
// a quality gate. T4.7 of plan. Same skip-on-missing pattern as trivy.
func runTrufflehogScan(args []string) int {
	fs := flag.NewFlagSet("trufflehog-scan", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory to scan")
	verified := fs.Bool("only-verified", true, "use --only-verified (network call to validate secrets)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := trufflehogscan.Check(context.Background(), trufflehogscan.Config{
		RepoRoot:     *root,
		OnlyVerified: *verified,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "trufflehog-scan: %v\n", err)
		return 5
	}
	if outcome.Skipped {
		fmt.Printf("trufflehog-scan: SKIP — %s\n", outcome.Reason)
		return 0
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "trufflehog-scan: FAIL\n%s\n", outcome.Reason)
		return 1
	}
	fmt.Printf("trufflehog-scan: PASS — %s\n", outcome.Reason)
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
		fmt.Fprintln(os.Stderr, "'warn', OR add an ADR in .claude/knowledge-base/decisions/ (or a MEET record)")
		fmt.Fprintln(os.Stderr, "referencing the cutover (T8.6 / find_infra_008 / deny-mode / mitigation #4).")
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
