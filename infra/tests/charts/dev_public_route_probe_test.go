// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import "testing"

// dev-public's route hostnames are the internal SNI *.loopruntime.local, which the edge
// rewrites the public host to and which resolves nowhere; the app-operator probes the
// public URL instead (#447). The domain it probes under is the one loopruntime-api announces
// to users as the app's URL — two values for one fact would drift.
func TestDevPublicAppOperatorProbesThePublicDomainTheAPIAnnounces(t *testing.T) {
	op := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev-public", "loopruntime-app-operator", "charts/loopruntime-app-operator", helmfileCoreEnv...)
	got, _ := configMapData(t, op, "loopruntime-app-operator")["ROUTE_PROBE_PUBLIC_DOMAIN"].(string)

	api := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev-public", "loopruntime-api", "charts/loopruntime-api", helmfileCoreEnv...)
	announced, _ := configMapData(t, api, "loopruntime-api")["LOOPRUNTIME_PUBLIC_DOMAIN"].(string)

	if announced == "" {
		t.Fatal("loopruntime-api renders no LOOPRUNTIME_PUBLIC_DOMAIN — this test compares against it")
	}
	if got != announced {
		t.Errorf("app-operator ROUTE_PROBE_PUBLIC_DOMAIN = %q, loopruntime-api LOOPRUNTIME_PUBLIC_DOMAIN = %q — the probe must ask for the URL users are given", got, announced)
	}
}
