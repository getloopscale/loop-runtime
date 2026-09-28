// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"path/filepath"
	"regexp"
	"testing"
)

// loopzero-api refuses to boot on a request pool that can bypass RLS, so the
// bootstrap install must hand it the non-owner URLs generate-secrets.sh writes
// (#396): the request pool from `api-uri` (loopzero_api), the background pool from
// `worker-uri` (loopzero_worker). `uri` is the owner's and is for migrations.
func TestBootstrapLoopZeroAPIConnectsThroughTheRoleSplitKeys(t *testing.T) {
	chart := publishedChart(t, "loopzero-api")
	rendered := renderChart(t, chart, "role-split", nil, filepath.Join(chart, "values-bootstrap.yaml"))

	for env, key := range map[string]string{"DATABASE_URL": "api-uri", "LOOPZERO_WORKER_DATABASE_URL": "worker-uri"} {
		// The chart quotes every value (`name: "DATABASE_URL"`), so quotes are optional here.
		ref := regexp.MustCompile(`(?s)- name: "?` + env + `"?\s+valueFrom:\s+secretKeyRef:\s+name: "?loopzero-db-credentials"?\s+key: "?([a-z-]+)"?`)
		m := ref.FindStringSubmatch(rendered)
		if m == nil {
			t.Errorf("the bootstrap Deployment does not read %s from loopzero-db-credentials", env)
			continue
		}
		if m[1] != key {
			t.Errorf("%s reads key %q, want %q", env, m[1], key)
		}
	}
}
