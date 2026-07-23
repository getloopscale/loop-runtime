// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/allowlistsunsets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/caddyfilenodockerip"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/canonicalfixturerepo"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/devpublicenv"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nocloud"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/webhookcanonicalpath"
)

// runCaddyfileNoDockerIP enforces that the canonical infra/local/Caddyfile does
// NOT hardcode Docker network IPs (172.18.x.x) in non-comment lines and that
// both wildcard blocks reverse_proxy to the stable https://127.0.0.1:8443.
// Migrated from infra/tests/check-caddyfile-no-docker-network-ip_test.sh
// (M-Migrate2 continuation 2026-07-07 — closes Red Team loopzero-cloud#127 guard).
func runCaddyfileNoDockerIP(args []string) int {
	fs := flag.NewFlagSet("caddyfile-no-docker-ip", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := caddyfilenodockerip.Check(caddyfilenodockerip.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "caddyfile-no-docker-ip: FAIL — %d violation(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("caddyfile-no-docker-ip: PASS — Caddyfile canonical (no 172.18.x.x, both wildcard blocks use https://127.0.0.1:8443)")
	return 0
}

// runCanonicalFixtureRepo enforces canonical getloopscale/loopzerokit-example fixture.
// Migrated from infra/tests/check-canonical-fixture-repo_test.sh (M-Migrate2 2026-07-07).
func runCanonicalFixtureRepo(args []string) int {
	fs := flag.NewFlagSet("canonical-fixture-repo", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := canonicalfixturerepo.Check(canonicalfixturerepo.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "canonical-fixture-repo: FAIL — %d legacy loopzero-repotest reference(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("canonical-fixture-repo: PASS — no legacy loopzero-repotest (canonical: getloopscale/loopzerokit-example)")
	return 0
}

// runCheckAllowlistSunsets is a hermetic contract test for the Python
// .claude/scripts/check-allowlist-sunsets.py gate (T5.2.a fixture verifier).
// Migrated from infra/tests/check-allowlist-sunsets_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runCheckAllowlistSunsets(args []string) int {
	fs := flag.NewFlagSet("check-allowlist-sunsets", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := allowlistsunsets.Check(allowlistsunsets.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-allowlist-sunsets: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("check-allowlist-sunsets: PASS — 7 fixture cases + live check GREEN (future/today/past/malformed/bad-date/missing/live)")
	return 0
}

// runDevPublicEnv enforces the canonical shape of
// infra/helmfile/environments/dev-public.yaml (dev URLs, cookieDomain,
// CORS + allowedReturnUrls, no localhost/legacy leaks, D5 OAuth reuse).
// Migrated from infra/tests/helm/dev-public-env_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runDevPublicEnv(args []string) int {
	fs := flag.NewFlagSet("dev-public-env", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := devpublicenv.Check(devpublicenv.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dev-public-env: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("dev-public-env: PASS — dev-public.yaml canonical shape preserved (dev URLs, cookieDomain, CORS, D5 OAuth reuse)")
	return 0
}

// runNoCloudDevDefault enforces T1.1 of plan m1-commit-canonical-30-fixes —
// no test/seed script may declare a default env-var value pointing at
// `cloud-dev.loopzero.example` (Caddy 301 redirect trap). Webhook handler is
// the single exception (EC-1 absorption).
// Migrated from infra/tests/check-no-cloud-dev-default_test.sh
// (M-Migrate2 2026-07-10 M16 T2.3 Wave 1).
func runNoCloudDevDefault(args []string) int {
	fs := flag.NewFlagSet("no-cloud-dev-default", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := nocloud.Check(nocloud.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-cloud-dev-default: FAIL — %d cloud-dev default(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		fmt.Fprintln(os.Stderr, "Fix: replace `${VAR:-https://cloud-dev.loopzero.example/...}` with the canonical hostname (app-dev/auth-dev/api-dev.loopzero.example per surface).")
		return 1
	}
	fmt.Println("no-cloud-dev-default: PASS — no cloud-dev.loopzero.example default env-vars in test/seed scripts (webhook handler exception allowed)")
	return 0
}

// runWebhookCanonicalPath enforces that no test/seed script references the
// wrong /engine/api/v1/webhooks/github URL (Caddy 301-redirect + 404 trap).
// The canonical is /api/v1/webhooks/github (handler-direct).
// Migrated from infra/tests/check-webhook-canonical-path_test.sh (M-Migrate2, 2026-07-07).
func runWebhookCanonicalPath(args []string) int {
	fs := flag.NewFlagSet("webhook-canonical-path", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := webhookcanonicalpath.Check(webhookcanonicalpath.Config{RepoRoot: *root})
	if err != nil {
		// Distinguish a genuine violation from a real I/O error (fail-closed):
		// reporting an I/O error as "detected in 0 place(s)" would mislead.
		if !errors.Is(err, webhookcanonicalpath.ErrViolations) {
			fmt.Fprintf(os.Stderr, "webhook-canonical-path: ERROR — could not complete scan: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "webhook-canonical-path: FAIL — /engine/api/v1/webhooks/github detected in %d place(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("webhook-canonical-path: PASS — no forbidden /engine/api/v1/webhooks/github references")
	return 0
}
