// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package caddyhostparity_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/caddyhostparity"
)

// tree writes a minimal repo: the verifier's host list and the Caddyfile.
func tree(t *testing.T, verifyHosts, caddy string) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("infra/scripts/local/caddy-up-dev-public.sh", `#!/usr/bin/env bash
verify_certs() {
  local hosts=(
`+verifyHosts+`
  )
  for h in "${hosts[@]}"; do :; done
}
`)
	mk("infra/local/Caddyfile", caddy)
	return root
}

// A host the verifier demands and the Caddyfile never declares is the B-150 shape:
// no site block means no certificate, and the handshake dies before any HTTP status.
func TestUnservedHostIsRefused(t *testing.T) {
	root := tree(t,
		"    app-dev.loopzero.example cloud-dev.loopzero.example",
		"app-dev.loopzero.example {\n  reverse_proxy localhost:3000\n}\n")

	out, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})
	if !errors.Is(err, caddyhostparity.ErrUnserved) {
		t.Fatalf("esperava ErrUnserved, veio %v", err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Host != "cloud-dev.loopzero.example" {
		t.Fatalf("esperava cloud-dev como único achado, veio %+v", out.Findings)
	}
}

// Every demanded host declared: the configuration the verifier can actually pass.
func TestFullyServedPasses(t *testing.T) {
	root := tree(t,
		"    app-dev.loopzero.example cloud-dev.loopzero.example",
		"app-dev.loopzero.example {\n}\ncloud-dev.loopzero.example {\n}\n")

	out, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("esperava passar, veio %v", err)
	}
	if out.Scanned != 2 {
		t.Fatalf("esperava 2 varridos, veio %d", out.Scanned)
	}
}

// A wildcard block serves every host under its suffix — refusing one would be a
// false positive on a configuration that works.
func TestWildcardBlockServesItsSuffix(t *testing.T) {
	root := tree(t,
		"    pr-7.preview-dev.loopzero.example",
		"*.preview-dev.loopzero.example {\n}\n")

	if _, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}); err != nil {
		t.Fatalf("bloco curinga deveria servir o sufixo, veio %v", err)
	}
}

// A hostname mentioned only inside a CSP string is NOT served — that is precisely how
// cloud-dev looked in the real Caddyfile while the handshake failed.
func TestMentionInsideDirectiveIsNotAServedHost(t *testing.T) {
	root := tree(t,
		"    cloud-dev.loopzero.example",
		"app-dev.loopzero.example {\n  header Content-Security-Policy \"connect-src https://cloud-dev.loopzero.example\"\n}\n")

	if _, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}); !errors.Is(err, caddyhostparity.ErrUnserved) {
		t.Fatalf("citação em CSP não é bloco de site; esperava ErrUnserved, veio %v", err)
	}
}

// Zero demanded hosts means the parse found nothing, not that nothing is wrong.
func TestEmptyDemandIsRefusedNotPassed(t *testing.T) {
	root := tree(t, "", "app-dev.loopzero.example {\n}\n")

	if _, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}); !errors.Is(err, caddyhostparity.ErrNothingScanned) {
		t.Fatalf("esperava ErrNothingScanned, veio %v", err)
	}
}

// Absent verifier: nothing prescribes a host list, so there is no parity to report.
func TestMissingVerifierIsRefused(t *testing.T) {
	root := t.TempDir()
	if _, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}); !errors.Is(err, caddyhostparity.ErrVerifierMissing) {
		t.Fatalf("esperava ErrVerifierMissing, veio %v", err)
	}
}

// The population (rules/testing.md § 4.2): a synthetic mutant proves the detector SEES
// the shape; only this says how many times it already happened in the real tree.
func TestRealTreeIsMeasured(t *testing.T) {
	root := "../../../../.."
	out, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})
	if errors.Is(err, caddyhostparity.ErrVerifierMissing) || errors.Is(err, caddyhostparity.ErrNothingScanned) {
		t.Fatalf("a varredura não alcançou a árvore real: %v", err)
	}
	if out.Scanned == 0 {
		t.Fatal("zero hostnames varridos — verde sobre nada")
	}
	t.Logf("população real: %d hostnames exigidos, %d sem bloco de site", out.Scanned, len(out.Findings))
	for _, f := range out.Findings {
		t.Logf("  sem terminador TLS: %s", f.Host)
	}
}

// The ratchet: a host already known unserved is frozen with the item that owns it, so the
// gate is runnable today; a NEW one is refused. Freezing without an owner would be hiding.
func TestBaselineFreezesKnownAndRefusesNew(t *testing.T) {
	root := tree(t,
		"    app-dev.loopzero.example cloud-dev.loopzero.example novo.loopzero.example",
		"app-dev.loopzero.example {\n}\n")
	base := filepath.Join(root, "infra/quality/caddy-unserved-baseline.txt")
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte("# congelado\ncloud-dev.loopzero.example B-151\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})
	if !errors.Is(err, caddyhostparity.ErrUnserved) {
		t.Fatalf("um hostname NOVO sem bloco deve reprovar, veio %v", err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Host != "novo.loopzero.example" {
		t.Fatalf("só o novo deve reprovar; o congelado tem dono. veio %+v", out.Findings)
	}
	if out.Frozen != 1 {
		t.Fatalf("esperava 1 congelado contabilizado, veio %d", out.Frozen)
	}
}

// A frozen entry without an owning item is hiding, not tracking.
func TestBaselineEntryWithoutOwnerIsRefused(t *testing.T) {
	root := tree(t, "    cloud-dev.loopzero.example", "app-dev.loopzero.example {\n}\n")
	base := filepath.Join(root, "infra/quality/caddy-unserved-baseline.txt")
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte("cloud-dev.loopzero.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}); !errors.Is(err, caddyhostparity.ErrUnownedFreeze) {
		t.Fatalf("esperava ErrUnownedFreeze, veio %v", err)
	}
}

// A frozen host that has since been served is stale: the freeze outlived its reason.
func TestStaleFreezeIsRefused(t *testing.T) {
	root := tree(t, "    app-dev.loopzero.example", "app-dev.loopzero.example {\n}\n")
	base := filepath.Join(root, "infra/quality/caddy-unserved-baseline.txt")
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte("app-dev.loopzero.example B-151\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}); !errors.Is(err, caddyhostparity.ErrStaleFreeze) {
		t.Fatalf("esperava ErrStaleFreeze, veio %v", err)
	}
}
