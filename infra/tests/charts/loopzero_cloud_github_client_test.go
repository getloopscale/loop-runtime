// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os"
	"regexp"
	"testing"
)

// The console sends the browser to GitHub's user authorization page with the App's
// OAuth client id. The id is public, so it is plain configuration of loopzero-cloud;
// the client secret stays with loopzero-api.

// githubClientIDEnv is the variable the per-environment values read the id from.
const githubClientIDEnv = "LOOPZERO_GITHUB_APP_CLIENT_ID"

func TestCloudChartLeavesTheGitHubClientIDEmptyByDefault(t *testing.T) {
	rendered := renderChart(t, cloudChartDir(t), "loopzero-cloud", nil)
	config := renderedObject(t, rendered, "ConfigMap", "loopzero-cloud")
	got, declared := dig(config, "data").(map[string]any)["GITHUB_APP_CLIENT_ID"]
	if !declared || got != "" {
		t.Errorf("GITHUB_APP_CLIENT_ID = %v (declared %v), want it declared and empty: the console keeps its connect flow off without it", got, declared)
	}
}

func TestCloudChartPassesTheGitHubClientIDItIsGiven(t *testing.T) {
	rendered := renderChart(t, cloudChartDir(t), "loopzero-cloud", map[string]string{"config.githubAppClientId": "Iv1.0123456789abcdef"})
	config := renderedObject(t, rendered, "ConfigMap", "loopzero-cloud")
	if got := dig(config, "data", "GITHUB_APP_CLIENT_ID"); got != "Iv1.0123456789abcdef" {
		t.Errorf("GITHUB_APP_CLIENT_ID = %v, want the value of config.githubAppClientId", got)
	}
}

// Each environment has its own GitHub App, so no values file names an id: every one
// takes it from the environment of the helmfile run.
func TestEnvironmentsTakeTheGitHubClientIDFromTheRun(t *testing.T) {
	for _, env := range []string{"staging", "prod", "dev-public"} {
		t.Run(env, func(t *testing.T) {
			outputs := []string{}
			if env == "staging" {
				outputs = stagingOutputs
			}
			values := helmfileValues(t, "core.helmfile.yaml.gotmpl", env, "loopzero-cloud", append(append([]string{}, outputs...), githubClientIDEnv+"=Iv1.fromtherun")...)
			if got := dig(values, "config", "githubAppClientId"); got != "Iv1.fromtherun" {
				t.Errorf("%s: config.githubAppClientId = %v, want the value of %s", env, got, githubClientIDEnv)
			}
		})
	}
}

func TestStagingCloudRendersTheGitHubClientIDOfTheRun(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopzero-cloud", "charts/loopzero-cloud",
		append(append([]string{}, stagingOutputs...), githubClientIDEnv+"=Iv1.stagingapp")...)
	config := renderedObject(t, rendered, "ConfigMap", "loopzero-cloud")
	if got := dig(config, "data", "GITHUB_APP_CLIENT_ID"); got != "Iv1.stagingapp" {
		t.Errorf("GITHUB_APP_CLIENT_ID = %v, want Iv1.stagingapp", got)
	}
	// The console builds the callback from its public URL: GitHub returns the browser
	// to <APP_PUBLIC_URL>/v1/integrations/github/callback, a path the app host routes
	// to loopzero-cloud.
	if got := dig(config, "data", "APP_PUBLIC_URL"); got != "https://app.staging.getloopscale.com" {
		t.Errorf("APP_PUBLIC_URL = %v, want https://app.staging.getloopscale.com", got)
	}
	if _, set := dig(config, "data").(map[string]any)["CLOUD_PUBLIC_URL"]; set {
		t.Error("CLOUD_PUBLIC_URL is set: the callback would leave the app host")
	}
}

// The host keeps the staging App's keys in github-app.json. staging:platform reads the
// public client id from it for the helmfile runs, and nothing else from that file.
func TestStagingPlatformReadsTheGitHubClientIDFromTheHostFile(t *testing.T) {
	taskfile, err := os.ReadFile("../../../taskfiles/staging.yml")
	if err != nil {
		t.Fatal(err)
	}
	read := regexp.MustCompile(githubClientIDEnv + `="?\$\(jq -r '([^']*)' /etc/loopzero-staging/github-app\.json`).FindSubmatch(taskfile)
	if read == nil {
		t.Fatalf("staging:platform does not set %s from /etc/loopzero-staging/github-app.json", githubClientIDEnv)
	}
	if string(read[1]) != `."client-id" // ""` {
		t.Errorf("staging:platform reads %s from the host file, want only the client id", read[1])
	}
	if !regexp.MustCompile(`(?m)^\s*export ` + githubClientIDEnv + `\b`).Match(taskfile) {
		t.Errorf("staging:platform does not export %s to the helmfile runs", githubClientIDEnv)
	}
}
