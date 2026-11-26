// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The API publishes the host to cell routes of tenant apps to the CloudFront
// KeyValueStore that infra-live's edge/app-routes unit creates (#68). Its ARN carries a
// random id, so it travels like the other infra-live outputs: `task staging:outputs`
// exports it, the staging overlay of loopruntime-api reads it, and the chart hands it to the
// API as LOOPRUNTIME_EDGE_KVS_ARN.

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{repoRoot()}, parts...)...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	return string(data)
}

func TestStagingOutputsExportTheEdgeRouteStore(t *testing.T) {
	task := readRepoFile(t, "taskfiles", "staging.yml")
	re := regexp.MustCompile(`printf 'export LOOPRUNTIME_EDGE_KVS_ARN="%s"\\n' "\$\(out edge/app-routes arn\)"`)
	if !re.MatchString(task) {
		t.Error("staging:outputs does not export LOOPRUNTIME_EDGE_KVS_ARN from the edge/app-routes arn output")
	}
}

func TestTheStagingAPIGetsTheEdgeRouteStore(t *testing.T) {
	values := readRepoFile(t, "infra", "helmfile", "values", "staging", "loopruntime-api.yaml.gotmpl")
	if !regexp.MustCompile(`(?m)^\s+edgeKvsArn: \{\{ requiredEnv "LOOPRUNTIME_EDGE_KVS_ARN" \| quote \}\}$`).MatchString(values) {
		t.Error("the staging overlay of loopruntime-api does not set config.edgeKvsArn from LOOPRUNTIME_EDGE_KVS_ARN")
	}
	configmap := readRepoFile(t, "charts", "loopruntime-api", "templates", "configmap.yaml")
	if !regexp.MustCompile(`(?m)^\s+LOOPRUNTIME_EDGE_KVS_ARN: \{\{ \.Values\.config\.edgeKvsArn \| quote \}\}$`).MatchString(configmap) {
		t.Error("the loopruntime-api chart does not pass config.edgeKvsArn as LOOPRUNTIME_EDGE_KVS_ARN")
	}
}
