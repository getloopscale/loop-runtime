// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package baseimagedigest

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// cfg points the gate at a single Dockerfile with an empty baseline.
func cfg(root, df string, baseline []string) Config {
	return Config{Root: root, Dockerfiles: []string{df}, ExceptionsBaseline: baseline}
}

const digest = "@sha256:" + "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

func TestDigestPinnedPasses(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile", "FROM golang:1.25"+digest+" AS builder\n")
	if !Check(cfg(root, "api/Dockerfile", []string{})).Pass {
		t.Error("digest-pinned FROM should pass")
	}
}

func TestTagOnlyFails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile", "FROM golang:1.25-alpine AS builder\n")
	o := Check(cfg(root, "api/Dockerfile", []string{}))
	if o.Pass || len(o.Violations) != 1 {
		t.Errorf("expected 1 violation, got %v", o.Violations)
	}
	if o.Violations[0] != "api/Dockerfile: FROM golang:1.25-alpine AS builder" {
		t.Errorf("violation format wrong: %q", o.Violations[0])
	}
}

func TestK3dRegistryAllowed(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile", "FROM k3d-loopzero-registry:5000/loopzero-api:dev\n")
	if !Check(cfg(root, "api/Dockerfile", []string{})).Pass {
		t.Error("k3d-loopzero-registry refs should be allowed")
	}
}

func TestScratchAllowed(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile", "FROM scratch\n")
	if !Check(cfg(root, "api/Dockerfile", []string{})).Pass {
		t.Error("FROM scratch should be allowed")
	}
}

func TestPragmaAllows(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile", "# pragma: allow-tag-pin\nFROM golang:1.25-alpine AS builder\n")
	if !Check(cfg(root, "api/Dockerfile", []string{})).Pass {
		t.Error("pragma above FROM should allow tag-only")
	}
}

func TestPragmaTooFarAway(t *testing.T) {
	root := t.TempDir()
	// pragma 3 lines above the FROM → outside the 2-line lookback → still fails.
	write(t, root, "api/Dockerfile", "# pragma: allow-tag-pin\nx\ny\nFROM golang:1.25-alpine\n")
	if Check(cfg(root, "api/Dockerfile", []string{})).Pass {
		t.Error("pragma beyond 2-line lookback must not apply")
	}
}

func TestBaselineExceptionAllows(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile", "FROM golang:1.25-alpine AS builder\n")
	baseline := []string{"api/Dockerfile:FROM golang:1.25-alpine AS builder"}
	if !Check(cfg(root, "api/Dockerfile", baseline)).Pass {
		t.Error("baselined tag-only FROM should pass")
	}
}

func TestInlineCommentStripped(t *testing.T) {
	root := t.TempDir()
	// digest with a trailing comment → comment stripped, still recognized as pinned.
	write(t, root, "api/Dockerfile", "FROM golang:1.25"+digest+" AS builder  # renovate\n")
	if !Check(cfg(root, "api/Dockerfile", []string{})).Pass {
		t.Error("trailing comment must not break digest recognition")
	}
}

func TestNonFromLinesIgnored(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile", "RUN echo FROM golang:1.25-alpine\nWORKDIR /app\n")
	o := Check(cfg(root, "api/Dockerfile", []string{}))
	if !o.Pass || o.Scanned != 0 {
		t.Errorf("only `FROM ` directives at line start count, got scanned=%d", o.Scanned)
	}
}

func TestMissingDockerfileTolerated(t *testing.T) {
	root := t.TempDir() // api/Dockerfile absent
	if !Check(cfg(root, "api/Dockerfile", []string{})).Pass {
		t.Error("missing Dockerfile should be tolerated")
	}
}

func TestMultipleFromMixed(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile",
		"FROM golang:1.25"+digest+" AS builder\nFROM alpine:3.22\nFROM scratch\n")
	o := Check(cfg(root, "api/Dockerfile", []string{}))
	if o.Pass || len(o.Violations) != 1 { // only alpine:3.22 is tag-only
		t.Errorf("expected 1 violation (alpine:3.22), got %v", o.Violations)
	}
	if o.Scanned != 3 {
		t.Errorf("expected 3 FROM directives scanned, got %d", o.Scanned)
	}
}

func TestDefaultsApplied(t *testing.T) {
	root := t.TempDir()
	// A default-baseline entry (alpine:3.22 in api/Dockerfile) → passes via defaults.
	write(t, root, "api/Dockerfile", "FROM alpine:3.22\n")
	if !Check(Config{Root: root}).Pass { // nil lists → defaults (DefaultDockerfiles + baseline)
		t.Error("default baseline should grandfather api/Dockerfile alpine:3.22")
	}
}
