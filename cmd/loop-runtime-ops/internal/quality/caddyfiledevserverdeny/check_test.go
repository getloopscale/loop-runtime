// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package caddyfiledevserverdeny

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

// Fixtures follow the pattern from caddyfilecspgate: create a synthetic
// infra/local/Caddyfile under a temp dir and run Check.

const canonicalAppDevBlock = `
app-dev.getloopscale.com {
  header {
    X-Frame-Options "DENY"
    Content-Security-Policy "default-src 'self'"
  }

  @dev_server_only {
    path /src/* /@vite/* /@react-refresh /node_modules/* /vite.config.* /tsconfig.* /package.json /package-lock.json /.env /.env.*
  }
  respond @dev_server_only 404

  handle /sessions/* {
    reverse_proxy localhost:14433
  }

  handle {
    root * /some/dist
    file_server
  }
}
`

func writeCaddyfile(t *testing.T, dir, body string) {
	t.Helper()
	caddyDir := filepath.Join(dir, "infra", "local")
	if err := os.MkdirAll(caddyDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caddyDir, "Caddyfile"), []byte(body), 0o644); err != nil {
		t.Fatalf("write Caddyfile: %v", err)
	}
}

func TestCheck_HappyPath_PASS(t *testing.T) {
	tmp := t.TempDir()
	writeCaddyfile(t, tmp, canonicalAppDevBlock)

	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("expected PASS, got FAIL with violations: %v", out.Violations)
	}
	if out.Skipped {
		t.Fatalf("expected not skipped")
	}
}

func TestCheck_NoAppDevBlock_FAIL(t *testing.T) {
	tmp := t.TempDir()
	writeCaddyfile(t, tmp, "some-other-host.example {\n  respond 200\n}\n")

	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL when app-dev block absent")
	}
}

func TestCheck_MatcherMissing_FAIL(t *testing.T) {
	// loop-console#141 pre-fix state: no @dev_server_only matcher present.
	tmp := t.TempDir()
	writeCaddyfile(t, tmp, `
app-dev.getloopscale.com {
  handle {
    root * /some/dist
    file_server
  }
}
`)

	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL when @dev_server_only missing")
	}
	if !containsViolation(out.Violations, "assert_dev_server_only_matcher_defined") {
		t.Errorf("expected assert_dev_server_only_matcher_defined violation, got: %v", out.Violations)
	}
}

func TestCheck_MissingCriticalPath_FAIL(t *testing.T) {
	// The .env is arguably the highest-impact path for #141 — pin per-path.
	tmp := t.TempDir()
	writeCaddyfile(t, tmp, `
app-dev.getloopscale.com {
  @dev_server_only {
    path /src/* /@vite/* /@react-refresh /node_modules/* /vite.config.* /tsconfig.* /package.json /package-lock.json
  }
  respond @dev_server_only 404
  handle {
    root * /some/dist
    file_server
  }
}
`)

	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL when /.env missing from matcher")
	}
	found := false
	for _, v := range out.Violations {
		if strings.Contains(v, "/.env") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /.env violation, got: %v", out.Violations)
	}
}

func TestCheck_NoRespond404_FAIL(t *testing.T) {
	tmp := t.TempDir()
	writeCaddyfile(t, tmp, `
app-dev.getloopscale.com {
  @dev_server_only {
    path /src/* /@vite/* /@react-refresh /node_modules/* /vite.config.* /tsconfig.* /package.json /package-lock.json /.env /.env.*
  }
  handle {
    root * /some/dist
    file_server
  }
}
`)

	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL when respond @dev_server_only 404 missing")
	}
	if !containsViolation(out.Violations, "assert_dev_server_only_returns_404") {
		t.Errorf("expected assert_dev_server_only_returns_404, got: %v", out.Violations)
	}
}

func TestCheck_MatcherAfterCatchAll_FAIL(t *testing.T) {
	// If matcher declared AFTER catch-all `handle {}`, try_files can bypass
	// the deny with SPA-fallback to index.html.
	tmp := t.TempDir()
	writeCaddyfile(t, tmp, `
app-dev.getloopscale.com {
  handle {
    root * /some/dist
    try_files {path} /index.html
    file_server
  }
  @dev_server_only {
    path /src/* /@vite/* /@react-refresh /node_modules/* /vite.config.* /tsconfig.* /package.json /package-lock.json /.env /.env.*
  }
  respond @dev_server_only 404
}
`)

	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL when matcher declared after catch-all")
	}
	if !containsViolation(out.Violations, "assert_matcher_before_catchall") {
		t.Errorf("expected assert_matcher_before_catchall, got: %v", out.Violations)
	}
}

func TestCheck_MissingCaddyfile_SKIP(t *testing.T) {
	tmp := t.TempDir()
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Skipped {
		t.Fatal("expected Skipped=true when Caddyfile absent")
	}
	if !out.Pass {
		t.Fatal("expected Pass=true when Skipped (no-op)")
	}
}

func TestCheck_RealRepo_PASS(t *testing.T) {
	// Sanity: the real repo Caddyfile passes post-fix (M18/#141 remediation).
	repoRoot := qtestutil.Root(t)
	out, err := Check(Config{RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Errorf("real-repo Caddyfile should pass #141 gate; violations: %v", out.Violations)
	}
}

func containsViolation(vs []string, substr string) bool {
	for _, v := range vs {
		if strings.Contains(v, substr) {
			return true
		}
	}
	return false
}
