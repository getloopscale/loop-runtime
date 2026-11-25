// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

// The dashboard SPA reads its runtime config from window.__THEO_CONFIG__
// (getloopscale/console dashboard/src/lib/http/config.ts), with the keys API_URL,
// AUTH_URL, ACCOUNTS_URL and OAUTH_URL. The chart wrote window.__LOOPZERO_CONFIG__
// with other keys, so nothing it set reached the SPA, and the CLI's device flow was
// refused on staging: Hydra's public origin was not among the origins the SPA may
// redirect to (getloopscale/console#531).

import (
	"strings"
	"testing"
)

func dashboardConfigJS(t *testing.T, values string) string {
	t.Helper()
	out := renderChart(t, uniqueChart(t, "loopzero-dashboard"), "loopzero-dashboard", nil, writeValues(t, values))
	for _, d := range parseDocs(t, out) {
		if d.obj.Kind == "ConfigMap" && strings.Contains(d.raw, "__config.js") {
			return d.raw
		}
	}
	t.Fatalf("no ConfigMap carries __config.js:\n%s", out)
	return ""
}

func TestDashboardConfigIsTheObjectTheSPAReads(t *testing.T) {
	js := dashboardConfigJS(t, `
image:
  tag: "test"
config:
  viteApiUrl: "https://api.example.test"
  viteAuthUrl: "https://kratos.example.test"
  viteAccountsUrl: "https://accounts.example.test"
  viteOauthUrl: "https://auth.example.test"
`)
	if !strings.Contains(js, "window.__THEO_CONFIG__ = {") {
		t.Fatalf("__config.js does not set window.__THEO_CONFIG__:\n%s", js)
	}
	for _, want := range []string{
		`API_URL: "https://api.example.test"`,
		`AUTH_URL: "https://kratos.example.test"`,
		`ACCOUNTS_URL: "https://accounts.example.test"`,
		`OAUTH_URL: "https://auth.example.test"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("__config.js lacks %s:\n%s", want, js)
		}
	}
	if strings.Contains(js, "__LOOPZERO_CONFIG__") {
		t.Errorf("__config.js still writes __LOOPZERO_CONFIG__, which the SPA never reads")
	}
}

func TestStagingDashboardRedirectsToHydraPublicOrigin(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopzero-dashboard", "charts/loopzero-dashboard", stagingOutputs...)
	if !strings.Contains(rendered, `OAUTH_URL: "https://auth.staging.getloopscale.com"`) {
		t.Errorf("the staging dashboard does not get Hydra's public origin as OAUTH_URL")
	}
}
