// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"reflect"
	"testing"
)

// The legacy Deployment (ProgressiveDelivery=false) is the incident-rollback
// path. It must carry the same pod as the Rollout, so a fix made once in the
// shared builders reaches both.

func legacyInput() (RenderInput, AppManifestInput) {
	ri := RenderInput{TenantID: "tn-x", ProjectID: "proj", Environment: "production", BuildID: "b1", ReleaseID: "r1"}
	app := AppManifestInput{
		Name: "api", Type: "server", Port: 8080, ImageRef: "example/api@sha256:abc",
		Env: map[string]string{"B": "2", "A": "1"}, Secrets: []string{"DB_URL"},
	}
	return ri, app
}

func TestLegacyDeploymentPodHasThePreStopDrain(t *testing.T) {
	// Arrange
	ri, app := legacyInput()

	// Act
	d := buildDeployment(ri, app, app.Port)

	// Assert (#61): the endpoint-removal race is identical on the legacy path.
	c := d.Spec.Template.Spec.Containers[0]
	if c.Lifecycle == nil || c.Lifecycle.PreStop == nil || c.Lifecycle.PreStop.Sleep == nil {
		t.Fatal("legacy Deployment container has no preStop sleep — the #61 fix did not reach it")
	}
}

func TestLegacyDeploymentPodIsTheSharedPodSpec(t *testing.T) {
	ri, app := legacyInput()

	d := buildDeployment(ri, app, app.Port)

	want := buildAppPodSpec(buildAppContainer(ri, app, app.Port))
	if !reflect.DeepEqual(d.Spec.Template.Spec, want) {
		t.Errorf("legacy Deployment pod spec diverges from the shared builder:\n got  %+v\n want %+v",
			d.Spec.Template.Spec, want)
	}
	if d.Spec.Replicas != replicasForApp(app) {
		t.Errorf("replicas = %d, want replicasForApp = %d", d.Spec.Replicas, replicasForApp(app))
	}
}
