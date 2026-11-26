// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

func TestScriptInstallNode_PathWithSpaces_UsesQuoting(t *testing.T) {
	apps := []deploycontracts.AppSpec{{Name: "api", Path: "my app"}}
	script := scriptInstallNode(apps)

	// ShellQuote produces single-quoted path
	if !strings.Contains(script, "'/workspace/my app'") {
		t.Errorf("script should shell-quote path with spaces:\n%s", script)
	}
}

func TestScriptInstallNode_PathInjection_Prevented(t *testing.T) {
	apps := []deploycontracts.AppSpec{{Name: "api", Path: "$(whoami)"}}
	script := scriptInstallNode(apps)

	// ShellQuote wraps in single quotes — $() is literal inside single quotes
	if !strings.Contains(script, "'$(whoami)'") {
		t.Errorf("command substitution should be inside single quotes:\n%s", script)
	}
}

func TestScriptValidateNode_ContainsFileCheck(t *testing.T) {
	apps := []deploycontracts.AppSpec{{Name: "api", Path: "apps/web"}}
	script := scriptValidateNode(apps)

	if !strings.Contains(script, "package.json") {
		t.Error("validate node should check for package.json")
	}
	if !strings.Contains(script, "set -euo pipefail") {
		t.Error("validate node should start with set -euo pipefail")
	}
}

func TestScriptValidatePython_ChecksDepsFiles(t *testing.T) {
	apps := []deploycontracts.AppSpec{{Name: "ml", Path: "services/ml"}}
	script := scriptValidatePython(apps)

	if !strings.Contains(script, "requirements.txt") {
		t.Error("validate python should check requirements.txt")
	}
	if !strings.Contains(script, "pyproject.toml") {
		t.Error("validate python should check pyproject.toml")
	}
}

func TestScriptInstallPython_SetsPIPCACHE(t *testing.T) {
	apps := []deploycontracts.AppSpec{{Name: "ml", Path: "."}}
	script := scriptInstallPython(apps)

	if !strings.Contains(script, "PIP_CACHE_DIR") {
		t.Error("install python should set PIP_CACHE_DIR")
	}
}
