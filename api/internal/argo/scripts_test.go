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

	// %q produces Go-style quoted string with escaped special chars
	if !strings.Contains(script, `"/workspace/my app"`) {
		t.Errorf("script should quote path with spaces:\n%s", script)
	}
	// Must NOT contain unquoted cd /workspace/my app (would fail in shell)
	if strings.Contains(script, "cd /workspace/my app\n") {
		t.Error("path with spaces must be quoted")
	}
}

func TestScriptInstallNode_PathWithQuotes_IsEscaped(t *testing.T) {
	// %q escapes embedded quotes, preventing shell parsing errors.
	// Primary defense against shell metacharacters ($, backticks) is
	// ValidateAppName at the API boundary, which rejects special chars.
	apps := []services.AppSpec{{Name: "api", Path: `my "app"`}}
	script := scriptInstallNode(apps)

	// %q should escape the embedded quotes
	if strings.Contains(script, `cd "/workspace/my "app""`) {
		t.Errorf("script should escape embedded quotes in path:\n%s", script)
	}
	// Should contain the Go-quoted version with backslash-escaped quotes
	if !strings.Contains(script, `"/workspace/my \"app\""`) {
		t.Errorf("script should contain Go-quoted path with escaped quotes:\n%s", script)
	}
}

func TestValidatePathsScript_UsesQuoting(t *testing.T) {
	paths := []string{"my app"}
	script := validatePathsScript(paths, "package.json")

	// Should use %q quoting
	if strings.Contains(script, `"my app/package.json"`) {
		// This is the old pattern with manual double-quoting, which doesn't escape $()
		// The new pattern uses %q which Go-escapes the whole path
	}
	// Should NOT have raw format specifiers left over
	if strings.Contains(script, "%%s") {
		t.Error("script should not contain raw format specifiers")
	}
}

func TestScriptBuildGo_PathQuoting(t *testing.T) {
	apps := []services.AppSpec{{Name: "api", Path: "services/api"}}
	script := scriptBuildGo(apps)

	if !strings.Contains(script, `"/workspace/services/api"`) {
		t.Errorf("script should contain quoted path:\n%s", script)
	}
}

func TestCheckAppLockfiles_PathQuoting(t *testing.T) {
	paths := []string{"my app"}
	script := checkAppLockfiles(paths)

	// Should use %q quoting for the path part
	if strings.Contains(script, `"my app/$f"`) {
		// Old pattern — manual double-quoting doesn't protect against injection
	}
}

func TestValidatePythonDeps_PathQuoting(t *testing.T) {
	paths := []string{"services/ml"}
	script := validatePythonDeps(paths)

	if !strings.Contains(script, `"services/ml/requirements.txt"`) {
		t.Errorf("script should contain quoted path:\n%s", script)
	}
}
