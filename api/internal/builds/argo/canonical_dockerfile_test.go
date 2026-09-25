// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"regexp"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
)

// T1.1 (plan simplify-deploy-loopzerokit-only v2.0) — canonical Dockerfile generator.
// Replaces loopzero-packs (11-language heuristic) with two platform-controlled
// multi-stage Dockerfiles (node, python) for the fixed LoopZeroKit stack. The
// generator runs at API time WITHOUT the extracted source, so package-manager
// selection is deterministic build-time shell logic inside the Dockerfile.

func mustDockerfile(t *testing.T, runtime string, app deploycontracts.AppSpec) string {
	t.Helper()
	df, err := canonicalDockerfile(runtime, app)
	if err != nil {
		t.Fatalf("canonicalDockerfile(%s): unexpected error: %v", runtime, err)
	}
	return df
}

func TestCanonicalDockerfile_NodeServer(t *testing.T) {
	df := mustDockerfile(t, "node", deploycontracts.AppSpec{Name: "web", Type: "server", Port: 8080, Start: "node dist/main.js"})
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
	df := mustDockerfile(t, "node", deploycontracts.AppSpec{Name: "jobs", Type: "worker", Port: 8001, Start: "node dist/worker.js"})
	if strings.Contains(df, "EXPOSE") {
		t.Errorf("worker Dockerfile must NOT contain EXPOSE; got:\n%s", df)
	}
	if !strings.Contains(df, "node dist/worker.js") {
		t.Error("worker CMD must use the declared start command")
	}
}

func TestCanonicalDockerfile_PythonServer(t *testing.T) {
	df := mustDockerfile(t, "python", deploycontracts.AppSpec{Name: "api", Type: "server", Port: 8000, Start: "python -m app"})
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
	_, err := canonicalDockerfile("go", deploycontracts.AppSpec{Name: "x", Type: "server", Port: 8080, Start: "x"})
	if err == nil {
		t.Fatal("expected E-011 error for unsupported runtime, got nil")
	}
	if !strings.Contains(err.Error(), "E-011") {
		t.Errorf("error must be typed E-011, got: %v", err)
	}
}

func TestCanonicalDockerfile_MissingStart_Errors(t *testing.T) {
	// No silent default CMD — a missing declared start is a typed error.
	_, err := canonicalDockerfile("node", deploycontracts.AppSpec{Name: "x", Type: "server", Port: 8080, Start: ""})
	if err == nil {
		t.Fatal("expected error for missing start command, got nil")
	}
}

func TestCanonicalDockerfile_Deterministic(t *testing.T) {
	app := deploycontracts.AppSpec{
		Name: "web", Type: "server", Port: 8080, Start: "node x.js",
	}
	first := mustDockerfile(t, "node", app)
	for i := 0; i < 20; i++ {
		if got := mustDockerfile(t, "node", app); got != first {
			t.Fatal("canonicalDockerfile must be deterministic")
		}
	}
}

func TestCanonicalDockerfileStep_OverwritesAndWarns(t *testing.T) {
	// T2.3/D4/EC-2: the generate-dockerfile step writes the canonical Dockerfile
	// to the path BuildKit reads (dockerfilePathForApp), via a TRUNCATING redirect
	// (`cat > path`) so any user Dockerfile extracted at the collision path is
	// overwritten — fail-closed. It also WARNS when a user Dockerfile is present.
	c, err := canonicalDockerfileStep("node", []deploycontracts.AppSpec{{Name: "web", Type: "server", Port: 8080, Start: "node x.js"}}, 1)
	if err != nil {
		t.Fatalf("canonicalDockerfileStep: %v", err)
	}
	script := strings.Join(c.Args, "\n")
	// Truncating redirect to the canonical single-app path (= /workspace/Dockerfile,
	// the exact collision path a user bundle's /Dockerfile extracts to).
	if !strings.Contains(script, `cat > "/workspace/Dockerfile"`) {
		t.Errorf("must overwrite the canonical path via truncating redirect; script:\n%s", script)
	}
	if strings.Contains(script, `cat >> `) {
		t.Error("must NOT append (>>) — that would not overwrite a user Dockerfile")
	}
	// Warn so the user knows their Dockerfile had no effect.
	if !strings.Contains(script, "ignored user Dockerfile") {
		t.Errorf("must warn that a user Dockerfile is ignored; script:\n%s", script)
	}
}

// TestCanonicalDockerfileStep_WarnLoopCoversAllVectors locks D4 coverage for the
// file-shapes a user could smuggle a build recipe in (F-tests-1): the warn/find
// loop must match Dockerfile, Containerfile, and *.dockerfile, and recurse into
// subdirectories (monorepo apps) — not just the root. Given the generated step
// script, When inspected, Then it finds all three name patterns at -maxdepth 3.
func TestCanonicalDockerfileStep_WarnLoopCoversAllVectors(t *testing.T) {
	c, err := canonicalDockerfileStep("node", []deploycontracts.AppSpec{{Name: "web", Type: "server", Port: 8080, Start: "node x.js"}}, 1)
	if err != nil {
		t.Fatalf("canonicalDockerfileStep: %v", err)
	}
	script := strings.Join(c.Args, "\n")
	for _, pat := range []string{"-iname 'Dockerfile'", "-iname 'Containerfile'", "-iname '*.dockerfile'"} {
		if !strings.Contains(script, pat) {
			t.Errorf("warn/find loop must cover %q (a user could smuggle a recipe in that shape); script:\n%s", pat, script)
		}
	}
	if !strings.Contains(script, "-maxdepth 3") {
		t.Errorf("find must recurse into app subdirs (monorepo) — expected -maxdepth 3; script:\n%s", script)
	}
}

// TestCanonicalDockerfileStep_MultiApp_EachOverwritten locks that, for a monorepo
// with multiple apps, EVERY app's canonical Dockerfile is written via a truncating
// redirect to its distinct canonical path (F-tests-1 + F-dom-1 companion). No app
// is left for BuildKit to read a user file. Given two apps, When the step is built,
// Then both /workspace/Dockerfile.web and /workspace/Dockerfile.api are written.
func TestCanonicalDockerfileStep_MultiApp_EachOverwritten(t *testing.T) {
	apps := []deploycontracts.AppSpec{
		{Name: "web", Type: "server", Port: 8080, Start: "node web.js"},
		{Name: "api", Type: "server", Port: 8081, Start: "node api.js"},
	}
	c, err := canonicalDockerfileStep("node", apps, len(apps))
	if err != nil {
		t.Fatalf("canonicalDockerfileStep: %v", err)
	}
	script := strings.Join(c.Args, "\n")
	for _, p := range []string{`cat > "/workspace/Dockerfile.web"`, `cat > "/workspace/Dockerfile.api"`} {
		if !strings.Contains(script, p) {
			t.Errorf("each app must be overwritten at its canonical path; missing %q; script:\n%s", p, script)
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
