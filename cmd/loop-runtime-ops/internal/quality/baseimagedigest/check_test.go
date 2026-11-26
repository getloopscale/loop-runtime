// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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
	write(t, root, "api/Dockerfile", "FROM k3d-loop-runtime-registry:5000/loop-runtime-api:dev\n")
	if !Check(cfg(root, "api/Dockerfile", []string{})).Pass {
		t.Error("k3d-loop-runtime-registry refs should be allowed")
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

// An individually absent Dockerfile is tolerated as long as something else was
// read — the tolerance exists for optional images, not for a broken scan.
func TestMissingDockerfileTolerated(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile", "FROM golang:1.25"+digest+" AS builder\n")
	o := Check(Config{
		Root:               root,
		Dockerfiles:        []string{"api/Dockerfile", "operators/Dockerfile.gone"},
		ExceptionsBaseline: []string{},
	})
	if !o.Pass {
		t.Errorf("an absent Dockerfile alongside a readable one should be tolerated, got %v", o.Violations)
	}
	if o.FilesRead != 1 {
		t.Errorf("expected 1 file read, got %d", o.FilesRead)
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

// Nil lists fall back to the defaults — DefaultDockerfiles picks api/Dockerfile
// up, and the now-empty DefaultExceptionsBaseline grandfathers nothing, so a
// tag-only FROM fails instead of being waved through. This assertion is the
// inverse of what it used to be, and deliberately so: the baseline was emptied
// on 2026-08-06 once every production base image was digest-pinned.
func TestDefaultsApplied(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile", "FROM alpine:3.22\n")
	o := Check(Config{Root: root}) // nil lists → DefaultDockerfiles + empty baseline
	if o.Pass {
		t.Error("a tag-only FROM must fail under the defaults — the baseline no longer grandfathers")
	}
	if len(o.Violations) != 1 || o.Violations[0] != "api/Dockerfile: FROM alpine:3.22" {
		t.Errorf("expected the alpine:3.22 violation, got %v", o.Violations)
	}
}

// The exception mechanism itself still works when a caller supplies one — what
// changed is that the shipped default list is empty, not the feature.
func TestExplicitBaselineStillGrandfathers(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/Dockerfile", "FROM alpine:3.22\n")
	o := Check(cfg(root, "api/Dockerfile", []string{"api/Dockerfile:FROM alpine:3.22"}))
	if !o.Pass {
		t.Errorf("an explicit baseline entry must still except the line, got %v", o.Violations)
	}
}

// TestExceptionsBaselineStaysEmpty locks the decision taken on 2026-08-06: the
// grandfather list was emptied because every production FROM is digest-pinned.
// A non-empty baseline means some image is un-pinned again and the exception is
// carrying it — which is how this gate went red and got quarantined last time.
func TestExceptionsBaselineStaysEmpty(t *testing.T) {
	if len(DefaultExceptionsBaseline) != 0 {
		t.Errorf("the exceptions baseline must stay empty; got %d entr(y|ies): %v\n"+
			"    Pin the base image to its multi-arch index digest instead of grandfathering it.",
			len(DefaultExceptionsBaseline), DefaultExceptionsBaseline)
	}
}

// TestDefaultDockerfilesAllExist guards the #255 shape: a path that left the
// repo is read as "missing Dockerfile tolerated" and scans to a vacuous pass,
// so the list silently over-reports what the gate covers.
func TestDefaultDockerfilesAllExist(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	for _, df := range DefaultDockerfiles {
		if _, err := os.Stat(filepath.Join(root, df)); err != nil {
			t.Errorf("DefaultDockerfiles lists %q, which does not exist: %v\n"+
				"    Remove it — an absent path is scanned to a vacuous pass.", df, err)
		}
	}
}

// Negative case, and the sharpest one: scanning zero FROM directives is NOT a
// pass. Run from the wrong working directory, every configured Dockerfile misses
// the tolerant os.ReadFile branch and the gate reported
// "PASS — 0 FROM directive(s)" — a green verdict asserting nothing, which is
// precisely the failure mode issue #255 exists to remove.
func TestZeroScannedIsNotAPass(t *testing.T) {
	root := t.TempDir() // no Dockerfiles at all
	o := Check(Config{Root: root})
	if o.Pass {
		t.Error("zero scanned FROM directives must fail closed, not pass vacuously")
	}
	if o.Scanned != 0 {
		t.Errorf("expected 0 scanned, got %d", o.Scanned)
	}
}
