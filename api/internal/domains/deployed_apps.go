// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domains

import (
	"context"
	"fmt"
)

// DeployedAppsReader answers which apps the running release of an environment
// has. found is false when the environment has no running release.
type DeployedAppsReader interface {
	DeployedApps(ctx context.Context, tenantID, project, env string) (apps []string, found bool, err error)
}

// ReleaseDeployedApps reads the apps of the running release from the release
// and build stores, the same ones the provisioner renders from.
type ReleaseDeployedApps struct {
	Releases DomainReleaseReader
	Builds   DomainBuildReader
}

// DeployedApps implements DeployedAppsReader.
func (r ReleaseDeployedApps) DeployedApps(ctx context.Context, tenantID, project, env string) ([]string, bool, error) {
	release, err := r.Releases.GetActiveByEnv(ctx, tenantID, project, env)
	if err != nil {
		return nil, false, fmt.Errorf("deployed apps: active release of %s/%s: %w", project, env, err)
	}
	if release == nil {
		return nil, false, nil
	}
	specs, err := r.Builds.GetBuildApps(ctx, release.BuildID)
	if err != nil {
		return nil, false, fmt.Errorf("deployed apps: apps of build %s: %w", release.BuildID, err)
	}
	apps := make([]string, 0, len(specs))
	for _, spec := range specs {
		apps = append(apps, spec.Name)
	}
	return apps, true, nil
}

// appNotDeployedReason is the failure reason of a domain whose app is not part
// of the environment's running release.
func appNotDeployedReason(app, env string) string {
	return fmt.Sprintf("The app %q is not part of the running release of %s, so nothing answers this domain. Remove the domain and add it to an app that is deployed.", app, env)
}
