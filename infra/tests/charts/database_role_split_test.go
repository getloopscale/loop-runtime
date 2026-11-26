// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"path/filepath"
	"regexp"
	"testing"
)

// loopruntime-api refuses to boot on a request pool that can bypass RLS, so the
// bootstrap install must hand it the non-owner URLs generate-secrets.sh writes
// (#396): the request pool from `api-uri` (loopruntime_api), the background pool from
// `worker-uri` (loopruntime_worker). `uri` is the owner's and is for migrations.
func TestBootstrapLoopRuntimeAPIConnectsThroughTheRoleSplitKeys(t *testing.T) {
	chart := publishedChart(t, "loopruntime-api")
	rendered := renderChart(t, chart, "role-split", nil, filepath.Join(chart, "values-bootstrap.yaml"))

	for env, key := range map[string]string{"DATABASE_URL": "api-uri", "LOOPRUNTIME_WORKER_DATABASE_URL": "worker-uri"} {
		// The chart quotes every value (`name: "DATABASE_URL"`), so quotes are optional here.
		ref := regexp.MustCompile(`(?s)- name: "?` + env + `"?\s+valueFrom:\s+secretKeyRef:\s+name: "?loopruntime-db-credentials"?\s+key: "?([a-z-]+)"?`)
		m := ref.FindStringSubmatch(rendered)
		if m == nil {
			t.Errorf("the bootstrap Deployment does not read %s from loopruntime-db-credentials", env)
			continue
		}
		if m[1] != key {
			t.Errorf("%s reads key %q, want %q", env, m[1], key)
		}
	}
}

// loopruntime-cloud refuses to serve as the owner (#438), so its bootstrap install reads the
// non-owner `loopruntime_cloud` role's URL.
func TestBootstrapLoopRuntimeCloudConnectsAsTheCloudRole(t *testing.T) {
	chart := publishedChart(t, "loopruntime-cloud")
	rendered := renderChart(t, chart, "role-split", nil, filepath.Join(chart, "values-bootstrap.yaml"))
	ref := regexp.MustCompile(`(?s)- name: "?DATABASE_URL"?\s+valueFrom:\s+secretKeyRef:\s+name: "?loopruntime-db-credentials"?\s+key: "?([a-z-]+)"?`)
	m := ref.FindStringSubmatch(rendered)
	if m == nil {
		t.Fatal("the bootstrap loopruntime-cloud Deployment does not read DATABASE_URL from loopruntime-db-credentials")
	}
	if m[1] != "cloud-uri" {
		t.Errorf("loopruntime-cloud reads DATABASE_URL from key %q, want cloud-uri — the owner's uri makes it refuse to serve", m[1])
	}
}
