// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"regexp"
	"strings"
	"testing"

	"github.com/loopzero/loopzero/api/internal/services"
)

// T1.1 (plan simplify-deploy-loopzerokit-only v2.0) — canonical Dockerfile generator.
// Replaces loopzero-packs (11-language heuristic) with two platform-controlled
// multi-stage Dockerfiles (node, python) for the fixed LoopZeroKit stack. The
// generator runs at API time WITHOUT the extracted source, so package-manager
// selection is deterministic build-time shell logic inside the Dockerfile.

func mustDockerfile(t *testing.T, runtime string, app services.AppSpec) string {
	t.Helper()
	df, err := canonicalDockerfile(runtime, app)
	if err != nil {
		t.Fatalf("canonicalDockerfile(%s): unexpected error: %v", runtime, err)
	}
	return df
}

func TestCanonicalDockerfile_NodeServer(t *testing.T) {
	df := mustDockerfile(t, "node", services.AppSpec{Name: "web", Type: "server", Port: 8080, Start: "node dist/main.js"})
	if !strings.HasPrefix(df, "# syntax=docker/dockerfile:1") {
		t.Error("missing syntax directive")
	}
	if !strings.Contains(df, "FROM "+imageNodeBase) {
		t.Errorf("node Dockerfile must FROM the digest-pinned node base; got:\n%s", df)
	}
	// C2 — frozen install branch present (pnpm/npm), no bare `npm install` fallback.
	if !strings.Contains(df, "--frozen-lockfile") || !strings.Contains(df, "npm ci") {
		t.Error("node recipe must use frozen installs (pnpm --frozen-lockfile / npm ci)")
	}
	// bare `npm install` (not `pnpm install`, not `npm ci`) is the C2 violation.
	if regexp.MustCompile(`(^|\s)npm install\b`).MatchString(df) {
		t.Error("C2 violated: bare `npm install` fallback present")
	}
	// EC-7 — server EXPOSEs its port.
	if !strings.Contains(df, "EXPOSE 8080") {
		t.Error("server must EXPOSE its port")
	}
	// CMD uses the declared start command.
	if !strings.Contains(df, "node dist/main.js") {
		t.Error("CMD must use the declared start command")
	}
}

func TestCanonicalDockerfile_NodeWorker_NoExpose(t *testing.T) {
	// EC-7 — worker carries a port in the manifest (schema-required) but the
	// Dockerfile must NOT EXPOSE it (workers receive no traffic).
	df := mustDockerfile(t, "node", services.AppSpec{Name: "jobs", Type: "worker", Port: 8001, Start: "node dist/worker.js"})
	if strings.Contains(df, "EXPOSE") {
		t.Errorf("worker Dockerfile must NOT contain EXPOSE; got:\n%s", df)
	}
	if !strings.Contains(df, "node dist/worker.js") {
		t.Error("worker CMD must use the declared start command")
	}
}

func TestCanonicalDockerfile_PythonServer(t *testing.T) {
	df := mustDockerfile(t, "python", services.AppSpec{Name: "api", Type: "server", Port: 8000, Start: "python -m app"})
	if !strings.Contains(df, "FROM "+imagePythonBase) {
		t.Errorf("python Dockerfile must FROM the digest-pinned python base; got:\n%s", df)
	}
	// C2 — frozen install (pip from requirements / poetry --no-root / uv sync --frozen).
	if !strings.Contains(df, "requirements.txt") || !strings.Contains(df, "--frozen") {
		t.Error("python recipe must use frozen installs (pip -r / poetry / uv --frozen)")
	}
	if !strings.Contains(df, "EXPOSE 8000") {
		t.Error("python server must EXPOSE its port")
	}
	if !strings.Contains(df, "python -m app") {
		t.Error("CMD must use the declared start command")
	}
}

func TestCanonicalDockerfile_UnsupportedRuntime_E011(t *testing.T) {
	_, err := canonicalDockerfile("go", services.AppSpec{Name: "x", Type: "server", Port: 8080, Start: "x"})
	if err == nil {
		t.Fatal("expected E-011 error for unsupported runtime, got nil")
	}
	if !strings.Contains(err.Error(), "E-011") {
		t.Errorf("error must be typed E-011, got: %v", err)
	}
}

func TestCanonicalDockerfile_MissingStart_Errors(t *testing.T) {
	// No silent default CMD — a missing declared start is a typed error.
	_, err := canonicalDockerfile("node", services.AppSpec{Name: "x", Type: "server", Port: 8080, Start: ""})
	if err == nil {
		t.Fatal("expected error for missing start command, got nil")
	}
}

func TestCanonicalBuild_InjectsServicesEnv(t *testing.T) {
	// C6/EC-6 — build-time env surfaced as ENV lines (rendered in Go, not via shell).
	df := mustDockerfile(t, "node", services.AppSpec{
		Name: "web", Type: "server", Port: 8080, Start: "node x.js",
		Env: map[string]string{"API_BASE": "https://api.example.com"},
	})
	if !strings.Contains(df, "ENV API_BASE=") || !strings.Contains(df, "https://api.example.com") {
		t.Errorf("services.json env must be injected as ENV; got:\n%s", df)
	}
}

func TestCanonicalBuild_EnvNotSecret(t *testing.T) {
	// C6 — secret-shaped env keys are rejected before reaching build-args/ENV
	// (build-args land in provenance + cache).
	_, err := canonicalDockerfile("node", services.AppSpec{
		Name: "web", Type: "server", Port: 8080, Start: "node x.js",
		Env: map[string]string{"DATABASE_PASSWORD": "hunter2"},
	})
	if err == nil {
		t.Fatal("expected error for secret-shaped env key, got nil")
	}
}

func TestCanonicalDockerfile_Deterministic(t *testing.T) {
	app := services.AppSpec{
		Name: "web", Type: "server", Port: 8080, Start: "node x.js",
		Env: map[string]string{"B": "2", "A": "1", "C": "3"},
	}
	first := mustDockerfile(t, "node", app)
	for i := 0; i < 20; i++ {
		if got := mustDockerfile(t, "node", app); got != first {
			t.Fatal("canonicalDockerfile must be deterministic (env ordering must be sorted)")
		}
	}
}

func TestBaseImages_AreDigestPinned(t *testing.T) {
	// C3 — base images MUST be @sha256: digest-pinned (reject tag-only).
	re := regexp.MustCompile(`@sha256:[a-f0-9]{64}$`)
	for name, img := range map[string]string{"imageNodeBase": imageNodeBase, "imagePythonBase": imagePythonBase} {
		if !re.MatchString(img) {
			t.Errorf("%s must be @sha256: digest-pinned, got %q", name, img)
		}
	}
}
