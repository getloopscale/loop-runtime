// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
)

// A change to loopzero-cloud's ConfigMap or Secret rolls its pods: the console reads its
// configuration as environment variables at start, so a pod that keeps running keeps
// the old values. On staging, LOOPZERO_ENV=prod and the OTLP endpoint stayed unapplied
// for hours after their deploys.
func TestLoopZeroCloudRollsItsPodsWhenItsConfigurationChanges(t *testing.T) {
	chart := uniqueChart(t, "loopzero-cloud")
	podAnnotations := func(set map[string]string) map[string]string {
		t.Helper()
		out := renderChart(t, chart, "loopzero-cloud", set)
		docs := chartrender.SplitYAMLByKind(out, "Deployment")
		if len(docs) != 1 {
			t.Fatalf("loopzero-cloud rendered %d Deployments, want 1", len(docs))
		}
		var deploy appsv1.Deployment
		if err := yaml.Unmarshal([]byte(docs[0]), &deploy); err != nil {
			t.Fatalf("decode the loopzero-cloud Deployment: %v", err)
		}
		return deploy.Spec.Template.Annotations
	}

	before := podAnnotations(map[string]string{"config.loopzeroEnv": "dev"})
	after := podAnnotations(map[string]string{"config.loopzeroEnv": "prod"})

	if before["checksum/config"] == "" {
		t.Fatalf("the pod template carries no checksum/config annotation: %v", before)
	}
	if before["checksum/config"] == after["checksum/config"] {
		t.Errorf("checksum/config did not change with config.loopzeroEnv: %q", before["checksum/config"])
	}
	if before["checksum/secret"] == "" {
		t.Errorf("the pod template carries no checksum/secret annotation: %v", before)
	}
}
