// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"

	"github.com/loopzero/loopzero/api/internal/services"
)

func TestScriptInstallNode_PathWithSpaces_UsesQuoting(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "my app"}}
	script := scriptInstallNode(apps)

	// ShellQuote produces single-quoted path
	if !strings.Contains(script, "'/workspace/my app'") {
		t.Errorf("script should shell-quote path with spaces:\n%s", script)
	}
}

func TestScriptInstallNode_PathInjection_Prevented(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "$(whoami)"}}
	script := scriptInstallNode(apps)

	// ShellQuote wraps in single quotes — $() is literal inside single quotes
	if !strings.Contains(script, "'$(whoami)'") {
		t.Errorf("command substitution should be inside single quotes:\n%s", script)
	}
}

func TestScriptBuildGo_PathQuoting(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "services/api"}}
	script := scriptBuildGo(apps)

	if !strings.Contains(script, "'/workspace/services/api'") {
		t.Errorf("script should contain shell-quoted path:\n%s", script)
	}
}

// CODE-005 extended: scriptBuildGo must set GOCACHE to a writable path because
// Argo containers run as uid 1000 and cannot write to $HOME/.cache/go-build
// which resolves to /.cache/go-build when HOME is unset.
func TestScriptBuildGo_SetsGOCACHE(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "."}}
	script := scriptBuildGo(apps)

	if !strings.Contains(script, "export GOCACHE='/workspace/.cache/go-build'") {
		t.Errorf("scriptBuildGo must export GOCACHE=/workspace/.cache/go-build:\n%s", script)
	}
	if !strings.Contains(script, "mkdir -p '/workspace/.cache/go-build'") {
		t.Errorf("scriptBuildGo must mkdir -p /workspace/.cache/go-build:\n%s", script)
	}
}

func TestScriptValidateNode_ContainsFileCheck(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "apps/web"}}
	script := scriptValidateNode(apps)

	if !strings.Contains(script, "package.json") {
		t.Error("validate node should check for package.json")
	}
	if !strings.Contains(script, "set -euo pipefail") {
		t.Error("validate node should start with set -euo pipefail")
	}
}

func TestScriptValidateGo_ChecksGoModAndGoSum(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "."}}
	script := scriptValidateGo(apps)

	if !strings.Contains(script, "go.mod") {
		t.Error("validate go should check go.mod")
	}
	if !strings.Contains(script, "go.sum") {
		t.Error("validate go should check go.sum")
	}
}

func TestScriptValidatePython_ChecksDepsFiles(t *testing.T) {
	apps := []services.AppSpec{{Name: "ml", Path: "services/ml"}}
	script := scriptValidatePython(apps)

	if !strings.Contains(script, "requirements.txt") {
		t.Error("validate python should check requirements.txt")
	}
	if !strings.Contains(script, "pyproject.toml") {
		t.Error("validate python should check pyproject.toml")
	}
}

func TestScriptValidateCustom_ChecksDockerfile(t *testing.T) {
	script := scriptValidateCustom()

	if !strings.Contains(script, "Dockerfile") {
		t.Error("validate custom should check for Dockerfile")
	}
}

func TestScriptInstallGo_SetsGOCACHE(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "."}}
	script := scriptInstallGo(apps)

	if !strings.Contains(script, "GOCACHE") {
		t.Error("install go should set GOCACHE")
	}
}

func TestScriptInstallPython_SetsPIPCACHE(t *testing.T) {
	apps := []services.AppSpec{{Name: "ml", Path: "."}}
	script := scriptInstallPython(apps)

	if !strings.Contains(script, "PIP_CACHE_DIR") {
		t.Error("install python should set PIP_CACHE_DIR")
	}
}

func TestScriptValidateFileAny_MultipleFiles(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "."}}
	script := scriptValidateFileAny(apps, "Gemfile", "Rakefile")

	if !strings.Contains(script, "Gemfile") || !strings.Contains(script, "Rakefile") {
		t.Errorf("should check both files:\n%s", script)
	}
}

func TestScriptInstallRuby_ChecksGemfile(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "."}}
	script := scriptInstallRuby(apps)

	if !strings.Contains(script, "Gemfile") {
		t.Error("install ruby should check Gemfile")
	}
	if !strings.Contains(script, "bundle install") {
		t.Error("install ruby should run bundle install")
	}
}
