// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"testing"
)

// The platform injects exactly ONE environment variable into the workload
// container: `PORT`. Every other env entry is copied from the app's own
// configuration — user values, not platform values.
//
// This is what supports the B-093 downgrade. The services.json validator does
// not block `AWS_`, `DO_` or `GOOGLE_`, and the tenant's env DOES reach this
// container — but nothing is shadowed, because the platform injects nothing
// with those prefixes here.
//
// Asserted on the shared container builder, which both the Rollout and the
// legacy Deployment use. If someone adds a platform injection this test fails
// and tells them to reopen B-093, the condition that item records as "becomes
// high".
func TestPlatformInjectsOnlyPortIntoTheWorkloadContainer(t *testing.T) {
	// Arrange: an app that declares no env of its own, so every variable left
	// in the container came from the platform.
	ri := RenderInput{TenantID: "tn-x", ProjectID: "proj", Environment: "production"}
	app := AppManifestInput{Name: "api", Port: 8080, Secrets: []string{"DB_URL"}}

	// Act
	c := buildAppContainer(ri, app, app.Port)

	// Assert
	var injected []string
	for _, e := range c.Env {
		injected = append(injected, e.Name)
	}
	if len(injected) != 1 || injected[0] != "PORT" {
		t.Errorf("the platform now injects %v into the workload container; it was only [PORT].\n\n"+
			"Reopen B-093: the services.json validator does NOT block AWS_, DO_ or GOOGLE_, and the "+
			"tenant's env reaches this same container. A new platform injection creates the shadowing "+
			"that item's downgrade declared impossible.", injected)
	}
}
