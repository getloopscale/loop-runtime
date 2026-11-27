// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package caddyfilenodockerip

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixture creates a temporary repo root with infra/local/Caddyfile
// containing the supplied body and returns the root path.
func writeFixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "infra", "local")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Caddyfile"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

const canonicalBody = `# Canonical dev-public edge Caddyfile fixture.

*.preview-dev.getloopscale.app {
  reverse_proxy https://127.0.0.1:8443 {
    transport http { tls_insecure_skip_verify }
  }
}

*.prod-dev.getloopscale.app {
  reverse_proxy https://127.0.0.1:8443 {
    transport http { tls_insecure_skip_verify }
  }
}
`

func TestCheck_CanonicalCaddyfilePasses(t *testing.T) {
	root := writeFixture(t, canonicalBody)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS, got err=%v violations=%v", err, outcome.Violations)
	}
	if len(outcome.Violations) != 0 {
		t.Fatalf("expected zero violations, got %v", outcome.Violations)
	}
}

func TestCheck_DockerNetworkIPHardcodedFails(t *testing.T) {
	body := `*.preview-dev.getloopscale.app {
  reverse_proxy https://172.18.0.10:443
}

*.prod-dev.getloopscale.app {
  reverse_proxy https://127.0.0.1:8443
}
`
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}

func TestCheck_DockerNetworkIPInCommentIsAllowed(t *testing.T) {
	body := `# The upstream MUST NOT be 172.18.0.10:443 — that IP drifts across docker restart.
*.preview-dev.getloopscale.app {
  reverse_proxy https://127.0.0.1:8443
}
*.prod-dev.getloopscale.app {
  reverse_proxy https://127.0.0.1:8443
}
`
	root := writeFixture(t, body)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("comment-only 172.18 should PASS, got err=%v violations=%v", err, outcome.Violations)
	}
}

func TestCheck_ProdDevBlockMissingCanonicalUpstreamFails(t *testing.T) {
	body := `*.preview-dev.getloopscale.app {
  reverse_proxy https://127.0.0.1:8443
}

*.prod-dev.getloopscale.app {
  reverse_proxy https://loop-runtime-cell-0.local:443
}
`
	root := writeFixture(t, body)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v (violations=%v)", err, outcome.Violations)
	}
}

func TestCheck_PreviewDevBlockMissingCanonicalUpstreamFails(t *testing.T) {
	body := `*.preview-dev.getloopscale.app {
  reverse_proxy https://loop-runtime-cell-0.local:443
}
*.prod-dev.getloopscale.app {
  reverse_proxy https://127.0.0.1:8443
}
`
	root := writeFixture(t, body)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v (violations=%v)", err, outcome.Violations)
	}
}

func TestCheck_CaddyfileMissingSurfacesError(t *testing.T) {
	root := t.TempDir()
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if err == nil {
		t.Fatal("expected error when Caddyfile missing, got nil")
	}
}

func TestCheck_RealRepoCaddyfilePasses(t *testing.T) {
	// Sanity: the real infra/local/Caddyfile currently in the repo must pass.
	// This test protects the invariant against silent regression when developers
	// edit the real file (parallel to the hermetic guard the bash script used to
	// enforce). It runs against the actual working tree, not a fixture.
	root := ".."
	// Walk up until we find a directory containing infra/local/Caddyfile.
	// The test binary runs from the package dir; go up until we hit repo root.
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(abs, "infra", "local", "Caddyfile")); statErr == nil {
			outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
			if checkErr != nil {
				t.Fatalf("real Caddyfile must satisfy invariant. err=%v violations=%v", checkErr, outcome.Violations)
			}
			return
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("could not locate infra/local/Caddyfile within 8 parent dirs; skipping real-repo assertion")
}

const cellEdgeBody = `(cell_edge) {
  forward_auth localhost:13001 {
    uri /internal/edge/route
  }
  map {header.X-Loop-Runtime-Cell} {cell_ingress} {
    cell-0 127.0.0.1:8443
    cell-1 127.0.0.1:8444
    default 127.0.0.1:9
  }
  reverse_proxy {cell_ingress} {
    transport http {
      tls
    }
  }
}

*.preview-dev.getloopscale.app {
  import cell_edge
}

*.prod-dev.getloopscale.app {
  import cell_edge
}
`

// The wildcards may route per cell through a snippet, as long as every cell maps to
// a stable host port — the property the canonical single upstream protected.
func TestCheck_CellEdgeSnippetOverLoopbackPasses(t *testing.T) {
	root := writeFixture(t, cellEdgeBody)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS, got err=%v violations=%v", err, outcome.Violations)
	}
}

// A cell mapped to a Docker network address is the #127 drift again, behind a map.
func TestCheck_CellEdgeMappingADockerAddressFails(t *testing.T) {
	body := strings.Replace(cellEdgeBody, "cell-1 127.0.0.1:8444", "cell-1 k3d-loop-runtime-cell-1-serverlb:443", 1)
	root := writeFixture(t, body)
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for a non-loopback cell, got %v", err)
	}
}

// Importing a snippet that proxies somewhere else does not satisfy the invariant.
func TestCheck_ImportOfASnippetWithoutACellMapFails(t *testing.T) {
	body := "(other) {\n  reverse_proxy localhost:5173\n}\n\n*.preview-dev.getloopscale.app {\n  import other\n}\n\n*.prod-dev.getloopscale.app {\n  import other\n}\n"
	root := writeFixture(t, body)
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}
