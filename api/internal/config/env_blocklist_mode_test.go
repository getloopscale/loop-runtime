// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"strings"
	"testing"
)

// The reserved-env-key blocklist stops a tenant hijacking its process
// (LD_PRELOAD, PATH...). Warn mode lets those keys through, so production
// must refuse to start with it.
func TestValidateRejectsTheEnvBlocklistWarnModeInProduction(t *testing.T) {
	t.Setenv("LOOPZERO_ENV", "production")
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "warn")

	err := Load().Validate()

	if err == nil || !strings.Contains(err.Error(), "LOOPZERO_ENV_BLOCKLIST_MODE") {
		t.Fatalf("Validate() = %v, want a refusal naming LOOPZERO_ENV_BLOCKLIST_MODE", err)
	}
}

func TestValidateRejectsAnUnknownEnvBlocklistMode(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "off")

	err := Load().Validate()

	if err == nil || !strings.Contains(err.Error(), "LOOPZERO_ENV_BLOCKLIST_MODE") {
		t.Fatalf("Validate() = %v, want a refusal naming LOOPZERO_ENV_BLOCKLIST_MODE", err)
	}
}

func TestLoadCarriesTheGitHubAPIBaseOverride(t *testing.T) {
	t.Setenv("GITHUB_API_BASE_URL", "https://github.acme.corp/api/v3")

	if got := Load().GitHubAPIBaseURL; got != "https://github.acme.corp/api/v3" {
		t.Errorf("GitHubAPIBaseURL = %q", got)
	}
}

// The GitHub web root is where the App's user authorization lives. A GitHub
// Enterprise Server install, and a test, point it elsewhere.
func TestLoad_GitHubWebBaseURLDefaultsToGitHubAndFollowsTheEnvironment(t *testing.T) {
	t.Setenv("GITHUB_WEB_BASE_URL", "")
	if got := Load().GitHubWebBaseURL; got != "https://github.com" {
		t.Errorf("GitHubWebBaseURL = %q, want https://github.com by default", got)
	}
	t.Setenv("GITHUB_WEB_BASE_URL", "https://github.acme.corp")
	if got := Load().GitHubWebBaseURL; got != "https://github.acme.corp" {
		t.Errorf("GitHubWebBaseURL = %q, want the configured root", got)
	}
}
