// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package shellgen

import (
	"strings"
	"testing"
)

func TestScriptBuilder_BasicScript(t *testing.T) {
	script := New().
		SetE().
		Cd("/workspace").
		Run("npm", "ci").
		String()

	want := "set -euo pipefail\ncd '/workspace'\nnpm 'ci'"
	if script != want {
		t.Errorf("script mismatch:\ngot:  %q\nwant: %q", script, want)
	}
}

func TestScriptBuilder_EnvAndEcho(t *testing.T) {
	script := New().
		Env("NODE_ENV", "production").
		Echo("Building...").
		String()

	if !strings.Contains(script, "export NODE_ENV='production'") {
		t.Errorf("expected export line, got: %s", script)
	}
	if !strings.Contains(script, "echo 'Building...'") {
		t.Errorf("expected echo line, got: %s", script)
	}
}

func TestScriptBuilder_IfFileExists(t *testing.T) {
	script := New().
		SetE().
		IfFileExists("/workspace/package.json").
		Then(func(b *ScriptBuilder) {
			b.Run("npm", "ci")
		}).
		EndIf().
		String()

	if !strings.Contains(script, "if [ -f '/workspace/package.json' ]; then") {
		t.Errorf("expected if block, got: %s", script)
	}
	if !strings.Contains(script, "  npm 'ci'") {
		t.Errorf("expected indented npm ci, got: %s", script)
	}
	if !strings.Contains(script, "fi") {
		t.Errorf("expected fi, got: %s", script)
	}
}

func TestScriptBuilder_IfElse(t *testing.T) {
	script := New().
		IfFileExists("/workspace/yarn.lock").
		Then(func(b *ScriptBuilder) {
			b.Run("yarn", "install", "--frozen-lockfile")
		}).
		Else(func(b *ScriptBuilder) {
			b.Run("npm", "ci")
		}).
		EndIf().
		String()

	if !strings.Contains(script, "else") {
		t.Errorf("expected else branch, got: %s", script)
	}
}

func TestScriptBuilder_MkdirP(t *testing.T) {
	script := New().MkdirP("/var/log/app").String()
	if script != "mkdir -p '/var/log/app'" {
		t.Errorf("unexpected script: %s", script)
	}
}

func TestScriptBuilder_Comment(t *testing.T) {
	script := New().Comment("install dependencies").String()
	if script != "# install dependencies" {
		t.Errorf("unexpected script: %s", script)
	}
}

func TestScriptBuilder_Raw(t *testing.T) {
	script := New().Raw("while true; do sleep 1; done").String()
	if script != "while true; do sleep 1; done" {
		t.Errorf("unexpected script: %s", script)
	}
}

func TestScriptBuilder_Append(t *testing.T) {
	a := New().Echo("part 1")
	b := New().Echo("part 2")
	script := New().Append(a).Append(b).String()

	if !strings.Contains(script, "part 1") || !strings.Contains(script, "part 2") {
		t.Errorf("expected both parts, got: %s", script)
	}
}

func TestScriptBuilder_InjectionPrevention(t *testing.T) {
	// Malicious path with command injection attempt.
	script := New().
		SetE().
		Cd("$(whoami)").
		Run("ls", "'; rm -rf /").
		String()

	// The dangerous strings should be inside single quotes.
	if strings.Contains(script, "$(whoami)") && !strings.Contains(script, "'$(whoami)'") {
		t.Errorf("command substitution not quoted: %s", script)
	}
	if strings.Contains(script, "rm -rf") && !strings.Contains(script, "'") {
		t.Errorf("injection attempt not quoted: %s", script)
	}
}
