// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"reflect"
	"testing"
)

func stampedApp() AppManifestInput {
	return AppManifestInput{Name: "api", Type: ServiceTypeServer, Port: 8080, ImageRef: "reg/acme@sha256:abc"}
}

// A redeploy of the same commit delivers the same image digest. The workload
// controller starts new pods only when the pod template changes, and user
// secrets are read through envFrom at process start: without something in the
// template that differs per build, a redeploy released nothing and a changed
// secret never reached the app.
func TestRolloutPodTemplateChangesWithEveryBuild(t *testing.T) {
	// Arrange
	first, second := baseInput(), baseInput()
	first.BuildID, second.BuildID = "build-1", "build-2"

	// Act
	a := buildRollout(scopeFor(t, first), stampedApp(), 8080)
	b := buildRollout(scopeFor(t, second), stampedApp(), 8080)

	// Assert
	if reflect.DeepEqual(a.Spec.Template, b.Spec.Template) {
		t.Fatal("two builds of the same image render the same pod template: the second one rolls no pod")
	}
	if got := b.Spec.Template.Metadata.Annotations[podBuildIDAnnotation]; got != "build-2" {
		t.Errorf("pod template annotation %s = %q, want build-2", podBuildIDAnnotation, got)
	}
}

func TestDeploymentPodTemplateChangesWithEveryBuild(t *testing.T) {
	// Arrange
	first, second := baseInput(), baseInput()
	first.BuildID, second.BuildID = "build-1", "build-2"

	// Act
	a := buildDeployment(scopeFor(t, first), stampedApp(), 8080)
	b := buildDeployment(scopeFor(t, second), stampedApp(), 8080)

	// Assert
	if reflect.DeepEqual(a.Spec.Template, b.Spec.Template) {
		t.Fatal("two builds of the same image render the same pod template: the second one rolls no pod")
	}
}

// The manifests of a release are rendered again without a deploy, when a custom
// domain is added or the app is scaled. Those renders carry the release id,
// which the build's own render does not have yet, and they must not restart
// the app.
func TestPodTemplateIsTheSameForEveryRenderOfOneBuild(t *testing.T) {
	// Arrange
	atBuild, later := baseInput(), baseInput()
	atBuild.BuildID, later.BuildID = "build-1", "build-1"
	atBuild.ReleaseID, later.ReleaseID = "", "rel-1"

	// Act
	a := buildRollout(scopeFor(t, atBuild), stampedApp(), 8080)
	b := buildRollout(scopeFor(t, later), stampedApp(), 8080)

	// Assert
	if !reflect.DeepEqual(a.Spec.Template, b.Spec.Template) {
		t.Errorf("the pod template differs between two renders of build-1: a domain or scale change would restart the app\nfirst:  %+v\nsecond: %+v",
			a.Spec.Template.Metadata, b.Spec.Template.Metadata)
	}
}
