// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T1.3 (cutover-deep-review-hardening): reserved env key
// blocklist + schema patternProperties + maxProperties + maxLength tests.

package source

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func validServicesJSONWithEnv(env map[string]string) string {
	envFragments := make([]string, 0, len(env))
	for k, v := range env {
		envFragments = append(envFragments, fmt.Sprintf("%q:%q", k, v))
	}
	envBlock := ""
	if len(envFragments) > 0 {
		envBlock = fmt.Sprintf(`, "env": {%s}`, strings.Join(envFragments, ","))
	}
	return fmt.Sprintf(`{
		"version": 1,
		"services": [
			{
				"name": "web",
				"runtime": "node",
				"port": 8080,
				"proxy": "/api",
				"dev": "npm run dev",
				"start": "npm start",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false%s
			}
		]
	}`, envBlock)
}

// T1.3 — RED test #1: reserved key PATH MUST be rejected with E-010.
func TestValidateServicesJSON_ReservedEnv_PATH_Rejected(t *testing.T) {
	content := validServicesJSONWithEnv(map[string]string{"PATH": "/usr/bin"})
	_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err == nil {
		t.Fatal("expected validation error for reserved env key PATH, got nil")
	}
	if !strings.Contains(err.Error(), "E-010") {
		t.Errorf("expected E-010 marker in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "PATH") {
		t.Errorf("expected error to mention 'PATH', got: %v", err)
	}
}

// T1.3 — RED test #2: reserved prefix LD_LIBRARY_PATH rejected.
func TestValidateServicesJSON_ReservedEnv_LDLibraryPath_Rejected(t *testing.T) {
	content := validServicesJSONWithEnv(map[string]string{"LD_LIBRARY_PATH": "/lib"})
	_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err == nil {
		t.Fatal("expected validation error for LD_LIBRARY_PATH, got nil")
	}
}

// T1.3 — RED test #3: prefix KUBERNETES_SERVICE_HOST rejected.
func TestValidateServicesJSON_ReservedEnv_KubernetesPrefix_Rejected(t *testing.T) {
	content := validServicesJSONWithEnv(map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1"})
	_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err == nil {
		t.Fatal("expected validation error for KUBERNETES_SERVICE_HOST, got nil")
	}
}

// T1.3 — RED test #4: prefix LOOPZERO_AUDIT_KEY rejected (platform-reserved).
func TestValidateServicesJSON_ReservedEnv_LOOPZEROPrefix_Rejected(t *testing.T) {
	content := validServicesJSONWithEnv(map[string]string{"LOOPZERO_AUDIT_KEY": "x"})
	_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err == nil {
		t.Fatal("expected validation error for LOOPZERO_AUDIT_KEY, got nil")
	}
}

// T1.3 — RED test #5: app-scoped key DATABASE_URL passes through.
func TestValidateServicesJSON_AppScopedEnv_DATABASE_URL_OK(t *testing.T) {
	content := validServicesJSONWithEnv(map[string]string{
		"DATABASE_URL": "postgres://localhost/app",
		"API_KEY":      "secret",
		"NODE_ENV":     "production",
	})
	m, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err != nil {
		t.Fatalf("expected no error for app-scoped env, got: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manifest")
	}
}

// T1.3 — RED test #6: schema patternProperties rejects lowercase keys.
func TestValidateServicesJSON_EnvSchema_LowercaseKey_Rejected(t *testing.T) {
	content := validServicesJSONWithEnv(map[string]string{"database_url": "x"})
	_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err == nil {
		t.Fatal("expected validation error for lowercase env key, got nil")
	}
}

// T1.3 — RED test #7: maxLength on value (4097 chars rejected).
func TestValidateServicesJSON_EnvSchema_ValueTooLong_Rejected(t *testing.T) {
	huge := strings.Repeat("x", 4097)
	content := validServicesJSONWithEnv(map[string]string{"BIG": huge})
	_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err == nil {
		t.Fatal("expected validation error for >4096 char env value, got nil")
	}
}

// T1.3 — RED test #8: maxProperties 64 cap.
func TestValidateServicesJSON_EnvSchema_TooManyKeys_Rejected(t *testing.T) {
	env := make(map[string]string, 65)
	for i := 0; i < 65; i++ {
		env[fmt.Sprintf("VAR_%d", i)] = "v"
	}
	content := validServicesJSONWithEnv(env)
	_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err == nil {
		t.Fatal("expected validation error for >64 env keys, got nil")
	}
}

// T1.3 — RED test #9 (EC-8): warn-only mode accepts reserved key.
func TestValidateServicesJSON_WarnMode_PathAccepted(t *testing.T) {
	content := validServicesJSONWithEnv(map[string]string{"PATH": "/usr/bin"})
	_, err := ValidateServicesJSONNative(content, EnvBlocklistWarn)
	if err != nil {
		t.Fatalf("warn mode must accept reserved keys (with log), got error: %v", err)
	}
}

// T1.3 — RED test #10: empty LOOPZERO_ENV_BLOCKLIST_MODE defaults to enforce
// (fail-closed per CLAUDE.md §8).
func TestValidateServicesJSON_EmptyMode_DefaultsToEnforce(t *testing.T) {
	content := validServicesJSONWithEnv(map[string]string{"PATH": "/usr/bin"})
	_, err := ValidateServicesJSONNative(content, EnvBlocklistMode(""))
	if err == nil {
		t.Fatal("empty mode must default to enforce, expected error, got nil")
	}
}

// B-089 / ADR 2026-08-13-reserved-env-two-rules: this package's blocklist and
// the find_sec_004 blocklist are DIFFERENT RULES that happened to share a symbol
// name, and the collision produced a wrong conclusion — a backlog item filed
// claiming three copies of one rule.
//
// This package blocks keys that would COMPROMISE THE RUNTIME CONTAINER (linker
// hijack via LD_PRELOAD/DYLD_*, process hijack via PATH/SHELL). find_sec_004
// blocks keys that would SHADOW what the platform injects. Sharing a name makes
// a reader — and a parity gate — treat them as copies to reconcile.
func TestReservedEnv_SymbolNameDoesNotCollideWithFindSec004(t *testing.T) {
	// AST over the WHOLE package, not a substring over one file. A review proved
	// both evasions on the first version of this test: a plain `var (...)`
	// grouping restored the colliding symbol under a green suite, and a
	// declaration in any other file of the package was invisible. It is the same
	// evasion class already registered as B-090 for the parity gate — applying
	// the lesson here rather than repeating it.
	//
	// The DECLARATION is what collides, not a mention: the doc-comment names the
	// sibling rule on purpose, and forbidding that would remove the very sentence
	// that keeps the next reader from confusing them.
	// os.ReadDir + ParseFile instead of parser.ParseDir (SA1019, deprecated since Go
	// 1.25 for ignoring build tags). The alternative the warning suggests,
	// golang.org/x/tools/go/packages, would be a new dependency to scan a single
	// directory of this package's files — the stdlib covers it, so it does.
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*ast.File{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, perr := parser.ParseFile(fset, e.Name(), nil, 0)
		if perr != nil {
			t.Fatalf("parsing %s: %v", e.Name(), perr)
		}
		files[e.Name()] = f
	}
	var scanned int
	{
		for name, f := range files {
			scanned++
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, id := range vs.Names {
						if id.Name == "reservedEnvKeyPrefixes" {
							t.Errorf("%s declares `reservedEnvKeyPrefixes`, the same name find_sec_004 "+
								"uses in build_plan.go and loopzero-dockerfile-lint. They are different rules "+
								"blocking different threats; the shared name is what made them read as "+
								"diverging copies (B-089).", name)
						}
					}
				}
			}
		}
	}
	if scanned == 0 {
		t.Error("no file parsed; a check that inspected nothing verified nothing")
	}
}

// Non-regression: the threats this package actually blocks stay blocked, and the
// find_sec_004 prefixes it does NOT block stay unblocked — that gap is real,
// measured, and registered as its own item rather than closed by accident here.
// NOTE ON WHAT find_sec_004 ACTUALLY PROTECTS, measured 2026-08-13 by a review.
//
// Its API half (`build_plan.go`) iterates `unit.Env`, which traces back to
// `AppSpec.Env` — a field NO production site populates, and which the HTTP
// boundary type `routes.AppInput` does not even declare. So that loop never runs
// in production today. It does not weaken the two-rules thesis (it reinforces
// it: the two never met), but the first version of this file, of the ADR and of
// the gate line asserted live protection that cannot be there. Registered as its
// own item; the claims were corrected rather than left standing.
func TestReservedEnv_ThreatsBlockedAreUnchanged(t *testing.T) {
	for _, k := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "PATH", "LOOPZERO_X", "KUBERNETES_SERVICE_HOST"} {
		if !isReservedEnvKey(k) {
			t.Errorf("%q must stay blocked — it is a runtime-container compromise vector", k)
		}
	}
	for _, k := range []string{"AWS_ACCESS_KEY_ID", "DO_TOKEN", "GOOGLE_APPLICATION_CREDENTIALS"} {
		if isReservedEnvKey(k) {
			t.Errorf("%q became blocked here; this package does NOT cover find_sec_004 shadowing, "+
				"and closing that gap rejects payloads accepted today — it is a decision, not a "+
				"side effect (ADR 2026-08-13-reserved-env-two-rules)", k)
		}
	}
}
