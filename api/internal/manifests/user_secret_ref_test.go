// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"testing"

	"github.com/getloopscale/loopzero/pkg/naming"
)

// Secret sync writes the Secret named by naming.UserSecretName; the container
// must mount exactly that name, or the app starts without its secrets.
func TestAppContainerMountsTheUserSecretSecretSyncWrites(t *testing.T) {
	// Arrange
	ri := baseInput()
	app := AppManifestInput{Name: "api", Type: ServiceTypeServer, Port: 8080, ImageRef: "reg/acme:1", Secrets: []string{"DB_URL"}}

	// Act
	c := buildAppContainer(scopeFor(t, ri), app, 8080)

	// Assert
	want := naming.UserSecretName(ri.TenantID, ri.ProjectID, ri.Environment, app.Name)
	if len(c.EnvFrom) != 1 || c.EnvFrom[0].SecretRef.Name != want {
		t.Fatalf("envFrom = %+v, want one secretRef %q", c.EnvFrom, want)
	}
}
