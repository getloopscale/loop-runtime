// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package caddyhostparity_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/caddyhostparity"
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
		"    app-dev.getloopscale.com cloud-dev.getloopscale.com",
		"app-dev.getloopscale.com {\n  reverse_proxy localhost:3000\n}\n")

	out, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}))
	if !errors.Is(err, caddyhostparity.ErrUnserved) {
		t.Fatalf("expected ErrUnserved, got %v", err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Host != "cloud-dev.getloopscale.com" {
		t.Fatalf("expected cloud-dev as the only finding, got %+v", out.Findings)
	}
}

// Every demanded host declared: the configuration the verifier can actually pass.
func TestFullyServedPasses(t *testing.T) {
	root := tree(t,
		"    app-dev.getloopscale.com cloud-dev.getloopscale.com",
		"app-dev.getloopscale.com {\n}\ncloud-dev.getloopscale.com {\n}\n")

	out, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected a pass, got %v", err)
	}
	if out.Scanned != 2 {
		t.Fatalf("expected 2 scanned, got %d", out.Scanned)
	}
}

// A wildcard block serves every host under its suffix — refusing one would be a
// false positive on a configuration that works.
func TestWildcardBlockServesItsSuffix(t *testing.T) {
	root := tree(t,
		"    pr-7.preview-dev.getloopscale.app",
		"*.preview-dev.getloopscale.app {\n}\n")

	if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); err != nil {
		t.Fatalf("a wildcard block should serve its suffix, got %v", err)
	}
}

// A hostname mentioned only inside a CSP string is NOT served — that is precisely how
// cloud-dev looked in the real Caddyfile while the handshake failed.
func TestMentionInsideDirectiveIsNotAServedHost(t *testing.T) {
	root := tree(t,
		"    cloud-dev.getloopscale.com",
		"app-dev.getloopscale.com {\n  header Content-Security-Policy \"connect-src https://cloud-dev.getloopscale.com\"\n}\n")

	if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); !errors.Is(err, caddyhostparity.ErrUnserved) {
		t.Fatalf("a mention inside a CSP is not a site block; expected ErrUnserved, got %v", err)
	}
}

// Zero demanded hosts means the parse found nothing, not that nothing is wrong.
func TestEmptyDemandIsRefusedNotPassed(t *testing.T) {
	root := tree(t, "", "app-dev.getloopscale.com {\n}\n")

	if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); !errors.Is(err, caddyhostparity.ErrNothingScanned) {
		t.Fatalf("expected ErrNothingScanned, got %v", err)
	}
}

// Absent verifier: nothing prescribes a host list, so there is no parity to report.
func TestMissingVerifierIsRefused(t *testing.T) {
	root := t.TempDir()
	if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); !errors.Is(err, caddyhostparity.ErrVerifierMissing) {
		t.Fatalf("expected ErrVerifierMissing, got %v", err)
	}
}

// The population (rules/testing.md § 4.2): a synthetic mutant proves the detector SEES
// the shape; only this says how many times it already happened in the real tree.
func TestRealTreeIsMeasured(t *testing.T) {
	root := "../../../../.."
	out, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}))
	if errors.Is(err, caddyhostparity.ErrVerifierMissing) || errors.Is(err, caddyhostparity.ErrNothingScanned) {
		t.Fatalf("the scan did not reach the real tree: %v", err)
	}
	if out.Scanned == 0 {
		t.Fatal("zero hostnames scanned — green over nothing")
	}
	t.Logf("real population: %d hostnames demanded, %d served, %d without a site block",
		out.Scanned, out.Served, len(out.Findings))
	for _, f := range out.Findings {
		t.Logf("  no TLS terminator: %s", f.Host)
	}
}

// A `)` inside the array body truncated the lazy match and the rest of the list vanished
// with no signal — a PASS over hostnames never compared. Reproduced by review 2026-08-18.
func TestParenInsideArrayDoesNotTruncateTheList(t *testing.T) {
	root := tree(t,
		"    # note $(hostname -f) about the list\n    app-dev.getloopscale.com cloud.getloopscale.com",
		"app-dev.getloopscale.com {\n}\n")

	out, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}))
	if out.Scanned != 2 {
		t.Fatalf("a parenthesis in the body must not swallow hostnames: expected 2 scanned, got %d", out.Scanned)
	}
	if !errors.Is(err, caddyhostparity.ErrUnserved) {
		t.Fatalf("cloud.getloopscale.com has no block; expected ErrUnserved, got %v", err)
	}
}

// An earlier `hosts=(` elsewhere in the script won the match, so the real list was never
// read and the finding named a hostname nobody demands.
func TestEarlierArrayDoesNotWin(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "infra/scripts/local/caddy-up-dev-public.sh")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/usr/bin/env bash\nlegacy_hosts=(old.example.com)\nverify_certs() {\n  local hosts=(\n    app-dev.getloopscale.com\n  )\n}\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := filepath.Join(root, "infra/local/Caddyfile")
	if err := os.MkdirAll(filepath.Dir(c), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("app-dev.getloopscale.com {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("the real list serves the only demanded host; got %v", err)
	}
	if out.Scanned != 1 {
		t.Fatalf("expected to read the verify list, got %d scanned", out.Scanned)
	}
}

// The comma list is the canonical multi-host form in Caddy; refusing it would fail a
// configuration that works, which is how this gate's own ancestor became permanently red.
func TestCommaSeparatedSiteAddressesAreServed(t *testing.T) {
	for _, caddy := range []string{
		"app-dev.getloopscale.com, cloud.getloopscale.com {\n}\n",
		"app-dev.getloopscale.com,cloud.getloopscale.com {\n}\n",
		"https://app-dev.getloopscale.com, cloud.getloopscale.com:8443 {\n}\n",
		"App-Dev.GetLoopScale.com, CLOUD.getloopscale.com {\n}\n",
	} {
		root := tree(t, "    app-dev.getloopscale.com cloud.getloopscale.com", caddy)
		if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); err != nil {
			t.Fatalf("a form Caddy serves was refused:\n%s\n  -> %v", caddy, err)
		}
	}
}

// A catch-all address serves every hostname; treating it as unserved is a false positive.
func TestCatchAllServesEverything(t *testing.T) {
	for _, addr := range []string{"*", ":443", ":80"} {
		root := tree(t, "    anything.getloopscale.com", addr+" {\n}\n")
		if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); err != nil {
			t.Fatalf("catch-all %q should serve everything, got %v", addr, err)
		}
	}
}

// An `import` pulls site blocks this gate cannot see. Reporting parity over a partial
// view is the green-over-nothing this package exists to refuse.
func TestImportMakesParityUnverifiable(t *testing.T) {
	root := tree(t, "    app-dev.getloopscale.com", "import sites/*.caddyfile\n")
	if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); !errors.Is(err, caddyhostparity.ErrPartialView) {
		t.Fatalf("expected ErrPartialView, got %v", err)
	}
}

// The column anchor is what separates a served site from a mention inside a directive.
// The earlier fixture passed because the hostname was followed by a quote, not because
// the anchor existed — it had no coverage at all.
func TestColumnAnchorIsWhatDiscardsIndentedMentions(t *testing.T) {
	root := tree(t, "    cloud.getloopscale.com",
		"app-dev.getloopscale.com {\n  respond \"see cloud.getloopscale.com {\" 200\n}\n")

	if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); !errors.Is(err, caddyhostparity.ErrUnserved) {
		t.Fatalf("an indented mention is not a site block; expected ErrUnserved, got %v", err)
	}
}

// The wildcard matches exactly one label: neither a deeper host nor the apex.
func TestWildcardDoesNotServeDeeperHostsNorApex(t *testing.T) {
	for _, host := range []string{"a.b.preview-dev.getloopscale.app", "preview-dev.getloopscale.app"} {
		root := tree(t, "    "+host, "*.preview-dev.getloopscale.app {\n}\n")
		if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); !errors.Is(err, caddyhostparity.ErrUnserved) {
			t.Fatalf("the wildcard should not serve %q, got %v", host, err)
		}
	}
}

// A snippet defined in the same file adds no site blocks, so importing it leaves the
// view complete: parity is checked, and a host nobody serves still fails.
func TestImportOfALocalSnippetKeepsParityVerifiable(t *testing.T) {
	caddy := "(edge) {\n  reverse_proxy localhost:1\n}\n\napp-dev.getloopscale.com {\n  import edge\n}\n"
	root := tree(t, "    app-dev.getloopscale.com", caddy)
	if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); err != nil {
		t.Fatalf("a local snippet import made parity unverifiable: %v", err)
	}
	root = tree(t, "    app-dev.getloopscale.com\n    cloud.getloopscale.com", caddy)
	if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); !errors.Is(err, caddyhostparity.ErrUnserved) {
		t.Fatalf("an unserved host passed next to a snippet import: %v", err)
	}
}

// An import naming no snippet in this file is a file import, and the view is partial.
func TestImportOfAnUndefinedNameIsStillPartial(t *testing.T) {
	root := tree(t, "    app-dev.getloopscale.com", "app-dev.getloopscale.com {\n  import edge\n}\n")
	if _, err := violationOf(caddyhostparity.Check(caddyhostparity.Config{RepoRoot: root})); !errors.Is(err, caddyhostparity.ErrPartialView) {
		t.Fatalf("expected ErrPartialView, got %v", err)
	}
}
