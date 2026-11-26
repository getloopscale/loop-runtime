// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"encoding/json"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// rollbackTargetFrom projects a persisted release row onto the delivery
// contract's own type.
//
// Counterpart of activationBuildFrom in the build context (B-079): the release
// context knows db.Release, deploycontracts does not. Nil in, nil out — the
// orchestrator has a nil-delivery fallback path and must reach it rather than
// panic on the way.
func rollbackTargetFrom(r *db.Release) *deploycontracts.RollbackTarget {
	if r == nil {
		return nil
	}
	// An app list that cannot be read leaves Apps empty, and the sync is then
	// confirmed by the Application's own health.
	apps, _ := workloadNames(r)
	return &deploycontracts.RollbackTarget{
		ID:        r.ID,
		GitCommit: r.GitCommit,
		Apps:      apps,
	}
}

// workloadNames lists the Kubernetes workloads of a release's apps as ArgoCD
// names them: `<project>-<app>`, the resource name the renderer emits, for
// every app that is not static. A deploy and a rollback are both confirmed by
// the health of these workloads.
func workloadNames(r *db.Release) ([]string, error) {
	if r.Apps == nil {
		return nil, nil
	}
	var apps []db.BuildAppSpec
	if err := json.Unmarshal(r.Apps, &apps); err != nil {
		return nil, fmt.Errorf("release %s: parse app specs: %w", r.ID, err)
	}
	var names []string
	for _, app := range apps {
		if app.Rendering == "static" {
			continue
		}
		names = append(names, fmt.Sprintf("%s-%s", r.ProjectID, app.Name))
	}
	return names, nil
}
