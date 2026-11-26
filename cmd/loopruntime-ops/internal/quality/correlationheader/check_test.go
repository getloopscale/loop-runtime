// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package correlationheader

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/scanresult"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestCheck_FlagsLegacyHeader: file using r.Header.Get("X-Correlation-ID")
// in an HTTP package → gate fails, error names the file + line + literal.
func TestCheck_FlagsLegacyHeader(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/release/releasehttp/promote.go",
		`package releasehttp

import "net/http"

func Handler(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-Correlation-ID")
	_ = id
}
`)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass() {
		t.Fatal("X-Correlation-ID in an HTTP package MUST fail the gate")
	}
	if len(out.Findings) != 1 {
		t.Fatalf("expected exactly 1 violation, got %d: %v", len(out.Findings), out.Findings)
	}
	v := out.Findings[0]
	if !strings.Contains(v, "promote.go") || !strings.Contains(v, `"X-Correlation-ID"`) {
		t.Errorf("violation must name file + literal; got %q", v)
	}
	if !strings.Contains(v, "correlation.Read") {
		t.Errorf("violation must suggest the canonical fix; got %q", v)
	}
}

// TestCheck_FlagsCanonicalHeader: even the "good" form is flagged
// because routes MUST go through the helper, not inline-read either
// header. This is the discipline that prevents future drift.
func TestCheck_FlagsCanonicalHeader(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/release/releasehttp/builds.go",
		`package releasehttp

import "net/http"

func Handler(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("Correlation-ID")
	_ = id
}
`)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass() {
		t.Fatal(`r.Header.Get("Correlation-ID") in an HTTP package MUST fail (use helper)`)
	}
	if !strings.Contains(out.Findings[0], `"Correlation-ID"`) {
		t.Errorf("violation must name the literal; got %v", out.Findings)
	}
}

// TestCheck_AllowsHelperReceiver: a route that calls
// correlation.Read(r) instead of r.Header.Get(...) passes cleanly.
func TestCheck_AllowsHelperReceiver(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/release/releasehttp/promote.go",
		`package releasehttp

import (
	"net/http"
	"github.com/getloopscale/loop-runtime/api/internal/correlation"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	id := correlation.Read(r)
	_ = id
}
`)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass() {
		t.Fatalf("correlation.Read(r) MUST pass; got %v", out.Findings)
	}
}

// TestCheck_IgnoresTestFiles: routes _test.go files may reference the
// literals (e.g., httptest request fixtures); not gate-flagged.
func TestCheck_IgnoresTestFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/release/releasehttp/promote.go", "package releasehttp\n")
	writeFile(t, root, "api/internal/release/releasehttp/promote_test.go",
		`package releasehttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPromote_Header(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Correlation-ID", "abc")
	got := r.Header.Get("X-Correlation-ID")
	if got != "abc" {
		t.Fatal("nope")
	}
}
`)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass() {
		t.Errorf("_test.go reference MUST NOT trip gate; got %v", out.Findings)
	}
}

// TestCheck_IgnoresOtherHeaders: r.Header.Get("X-Forwarded-For")
// (or any non-correlation header) is OUT OF gate scope.
func TestCheck_IgnoresOtherHeaders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/release/releasehttp/proxy.go",
		`package releasehttp

import "net/http"

func Handler(w http.ResponseWriter, r *http.Request) {
	ip := r.Header.Get("X-Forwarded-For")
	_ = ip
}
`)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass() {
		t.Errorf("X-Forwarded-For MUST be ignored; got %v", out.Findings)
	}
}

// TestCheck_IgnoresVariableArg: r.Header.Get(varName) is not flagged
// because the literal can't be statically resolved without type
// analysis. Documented limitation; rare in practice and routes/
// callers MUST use the helper anyway.
func TestCheck_IgnoresVariableArg(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/release/releasehttp/dynamic.go",
		`package releasehttp

import "net/http"

func Handler(w http.ResponseWriter, r *http.Request) {
	header := "X-Correlation-ID"
	id := r.Header.Get(header)
	_ = id
}
`)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass() {
		t.Errorf("variable arg MUST NOT trip gate; got %v", out.Findings)
	}
}

// TestCheck_IgnoresOutsideRoutes: the same literal in middleware/ or
// services/ is OUT OF scanDir scope. The middleware DOES read the
// header (that's its job — to inject the ID into context); routes only
// READ from context after middleware ran.
func TestCheck_IgnoresOutsideRoutes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/release/releasehttp/handler.go", "package releasehttp\n")
	writeFile(t, root, "api/internal/middleware/correlation.go",
		`package middleware

import "net/http"

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Correlation-ID")
		_ = id
		next.ServeHTTP(w, r)
	})
}
`)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass() {
		t.Errorf("middleware/ MUST be out of scope; got %v", out.Findings)
	}
}

// A root with no HTTP package is a misconfigured run, not a clean one.
func TestCheck_MissingHandlerTreeIsAnError(t *testing.T) {
	_, err := Check(Config{RepoRoot: t.TempDir()})
	if !errors.Is(err, scanresult.ErrNothingScanned) {
		t.Fatalf("want ErrNothingScanned, got %v", err)
	}
}

// A handler tree holding only tests scanned no production file.
func TestCheck_HandlerTreeWithOnlyTestsIsAnError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/release/releasehttp/a_test.go", "package releasehttp\n")
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, scanresult.ErrNothingScanned) {
		t.Fatalf("want ErrNothingScanned, got %v", err)
	}
}
