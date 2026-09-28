// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cleanroom

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"sigs.k8s.io/yaml"
)

// The dev loop ran loopzero-api under a ClusterRole of its own, in
// infra/tilt/api-deployment.yaml, that had drifted from the chart's: it could create
// and update a Tenant but not patch one, so the admin cell pin failed with
// `tenants.loopzero.io is forbidden ... cannot patch` on the dev loop while production,
// installed from the chart, allowed it (#414). Local equals production: the dev loop
// takes loopzero-api's RBAC from the chart, and keeps no copy.
func TestTheDevLoopTakesTheAPIRBACFromTheChart(t *testing.T) {
	tiltfile, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "Tiltfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range []string{"templates/clusterrole.yaml", "templates/clusterrolebinding.yaml"} {
		if !regexp.MustCompile(`helm template loopzero-api charts/loopzero-api [^"]*-s ` + regexp.QuoteMeta(tpl)).Match(tiltfile) {
			t.Errorf("the Tiltfile's loopzero-api render does not include %s", tpl)
		}
	}

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "tilt", "api-deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range regexp.MustCompile(`(?m)^---\s*$`).Split(string(raw), -1) {
		var obj struct {
			Kind string `json:"kind"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatalf("parse api-deployment.yaml: %v", err)
		}
		if obj.Kind == "ClusterRole" || obj.Kind == "ClusterRoleBinding" {
			t.Errorf("infra/tilt/api-deployment.yaml still defines a %s for loopzero-api; it must come from the chart", obj.Kind)
		}
	}
}

// Cluster-scoped objects carry no namespace; only the namespaced ones in the render
// are placed in loopzero-system.
func TestTheDevLoopNamespacesOnlyNamespacedAPIObjects(t *testing.T) {
	tiltfile, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "Tiltfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`if _obj\["kind"\] in \(\s*"ConfigMap",\s*"Secret"\s*\):\s*\n\s*_obj\["metadata"\]\["namespace"\] = "loopzero-system"`).Match(tiltfile) {
		t.Error("the Tiltfile must set loopzero-system only on the render's namespaced objects (ConfigMap, Secret)")
	}
}
