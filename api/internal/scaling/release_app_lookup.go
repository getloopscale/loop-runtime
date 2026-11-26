// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scaling

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// ActiveReleaseReader is the one release-store read ReleaseAppLookup makes.
type ActiveReleaseReader interface {
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*db.Release, error)
}

// ReleaseAppLookup lists the apps of a project's active release in an
// environment, so the scale endpoint can refuse an app the project does not have.
type ReleaseAppLookup struct {
	Releases ActiveReleaseReader
}

// DeployedAppNames returns the app names the active release declares, in its
// order, or nil when the environment has no active release. projectName is the
// project's name, which releases store as project_id.
func (l ReleaseAppLookup) DeployedAppNames(ctx context.Context, tenantID, projectName, env string) ([]string, error) {
	release, err := l.Releases.GetActiveByEnv(ctx, tenantID, projectName, env)
	if err != nil {
		return nil, fmt.Errorf("read active release of %s/%s: %w", projectName, env, err)
	}
	if release == nil || len(release.Apps) == 0 {
		return nil, nil
	}
	var apps []db.BuildAppSpec
	if err := json.Unmarshal(release.Apps, &apps); err != nil {
		return nil, fmt.Errorf("parse apps of release %s: %w", release.ID, err)
	}
	names := make([]string, 0, len(apps))
	for _, app := range apps {
		names = append(names, app.Name)
	}
	return names, nil
}
