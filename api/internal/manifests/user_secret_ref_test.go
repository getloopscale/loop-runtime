// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"testing"

	"github.com/getloopscale/loopzero/pkg/naming"
)

// secretRefOf returns the container's envFrom entry for the Secret named name.
func secretRefOf(c k8sContainer, name string) (k8sSecretRef, bool) {
	for _, src := range c.EnvFrom {
		if src.SecretRef.Name == name {
			return src.SecretRef, true
		}
	}
	return k8sSecretRef{}, false
}

// Secret sync writes the Secret named by naming.UserSecretName; the container
// must mount exactly that name, or the app starts without its secrets. A secret the
// app declares is required: the pod does not start without it.
func TestAppContainerMountsTheUserSecretSecretSyncWrites(t *testing.T) {
	// Arrange
	ri := baseInput()
	app := AppManifestInput{Name: "api", Type: ServiceTypeServer, Port: 8080, ImageRef: "reg/acme:1", Secrets: []string{"DB_URL"}}

	// Act
	c := buildAppContainer(scopeFor(t, ri), app, 8080)

	// Assert
	want := naming.UserSecretName(ri.TenantID, ri.ProjectID, ri.Environment, app.Name)
	ref, ok := secretRefOf(c, want)
	if !ok {
		t.Fatalf("envFrom = %+v, want a secretRef %q", c.EnvFrom, want)
	}
	if ref.Optional != nil && *ref.Optional {
		t.Errorf("the Secret of an app that declares secrets is optional; want it required")
	}
}

// A secret set with `loopscale secret set` reaches the app even when the app's
// configuration declares none: secret sync writes the app's Secret (and the
// environment-wide one for a secret set without an app), and the container mounts
// both, optionally, so an app with no secret still starts.
func TestAppContainerMountsUndeclaredUserSecretsOptionally(t *testing.T) {
	// Arrange
	ri := baseInput()
	app := AppManifestInput{Name: "api", Type: ServiceTypeServer, Port: 8080, ImageRef: "reg/acme:1"}

	// Act
	c := buildAppContainer(scopeFor(t, ri), app, 8080)

	// Assert
	envWide := naming.UserSecretName(ri.TenantID, ri.ProjectID, ri.Environment, "")
	appWide := naming.UserSecretName(ri.TenantID, ri.ProjectID, ri.Environment, app.Name)
	if len(c.EnvFrom) != 2 || c.EnvFrom[0].SecretRef.Name != envWide || c.EnvFrom[1].SecretRef.Name != appWide {
		t.Fatalf("envFrom = %+v, want the environment's Secret %q then the app's %q, so the app's value wins", c.EnvFrom, envWide, appWide)
	}
	for _, src := range c.EnvFrom {
		if src.SecretRef.Optional == nil || !*src.SecretRef.Optional {
			t.Errorf("secretRef %q is required; an app without secrets would not start", src.SecretRef.Name)
		}
	}
}
