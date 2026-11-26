// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scaling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/manifests"
	"github.com/getloopscale/loop-runtime/pkg/naming"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// ErrDeployInFlight is returned when the environment has a deploy that is not
// verified yet, so its running release is not what the GitOps repository holds.
var ErrDeployInFlight = errors.New("scaling: a deploy of the environment is in flight")

// ScalerReleaseReader reads the releases of an environment.
type ScalerReleaseReader interface {
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*db.Release, error)
	ListByEnv(ctx context.Context, tenantID, projectID, env string, limit int) ([]db.Release, error)
}

// ScalerBuildReader reads the build a release delivered.
type ScalerBuildReader interface {
	GetByID(ctx context.Context, buildID string) (*db.Build, error)
	GetBuildApps(ctx context.Context, buildID string) ([]db.BuildAppSpec, error)
}

// SyncTrigger starts a sync of an ArgoCD Application.
type SyncTrigger interface {
	TriggerSync(ctx context.Context, appName string) error
}

// RenderSettings is the platform configuration every render of an
// environment's manifests uses.
type RenderSettings struct {
	RegistryURL         string
	BaseDomain          string
	ClusterIssuer       string
	PrometheusAddress   string
	ProgressiveDelivery bool
	LeanResources       bool
}

// ReleaseScaler delivers the stored scaling config of an environment to its
// running workloads.
type ReleaseScaler struct {
	Releases ScalerReleaseReader
	Builds   ScalerBuildReader
	Configs  ScalingConfigReader
	Domains  deploycontracts.CustomDomainQuerier // optional
	Locker   deploycontracts.EnvironmentLocker
	GitOps   deploycontracts.GitOpsPusher
	Sync     SyncTrigger
	Render   RenderSettings
}

// gitOpsPushTimeout bounds the push, which runs outside the environment lock.
const gitOpsPushTimeout = 60 * time.Second

// inFlightLookback is how many of the environment's newest releases are checked
// for a deploy or rollback that has not finished.
const inFlightLookback = 5

// Deliver makes the environment's running workloads match its stored scaling
// config, and reports whether there was a running release to deliver to.
//
// It uses the path a deploy uses to change a workload on a cell: the manifests
// of the running release are rendered again with the stored config, pushed to
// the tenant's GitOps repository and synced by ArgoCD. The render keeps the
// build, so the pod template is the deployed one and only the replica count
// (or the autoscaler) changes. ArgoCD ignores spec.replicas when it compares,
// because an autoscaler may own it, but a sync applies the manifests as they
// are, so the sync is what carries the new count.
//
// Without a running release there is nothing to change and the first deploy
// reads the config (ScalingResolver). While a deploy or rollback is in flight
// the repository already holds manifests the running release did not render:
// Deliver returns ErrDeployInFlight and pushes nothing.
func (s *ReleaseScaler) Deliver(ctx context.Context, tenantID, project, env string) (bool, error) {
	return s.deliver(ctx, tenantID, project, env, false)
}

// DeliverSaved is Deliver for a caller that has just put other manifests in
// the GitOps repository, as a rollback does with the ones of an older release:
// they carry the replica count of their day, and this puts the saved config
// back. With no config saved for any app of the running release it pushes
// nothing and reports false, because those manifests already hold.
func (s *ReleaseScaler) DeliverSaved(ctx context.Context, tenantID, project, env string) (bool, error) {
	return s.deliver(ctx, tenantID, project, env, true)
}

func (s *ReleaseScaler) deliver(ctx context.Context, tenantID, project, env string, onlyIfSaved bool) (bool, error) {
	var push *deploycontracts.GitOpsPushOpts
	err := s.Locker.WithLock(ctx, tenantID, project, env, func(db.DBTX) error {
		opts, saved, renderErr := s.renderRunningRelease(ctx, tenantID, project, env)
		if renderErr == nil && (saved || !onlyIfSaved) {
			push = opts
		}
		return renderErr
	})
	if err != nil {
		return false, err
	}
	if push == nil {
		return false, nil
	}

	// Outside the lock, as the deploy does: it is network I/O.
	pushCtx, cancel := context.WithTimeout(ctx, gitOpsPushTimeout)
	defer cancel()
	if _, err := s.GitOps.Push(pushCtx, *push); err != nil {
		return false, fmt.Errorf("scaling: push manifests of %s/%s: %w", project, env, err)
	}

	appName, err := naming.ArgoAppName(tenantID, project, env)
	if err != nil {
		return false, fmt.Errorf("scaling: argocd application name: %w", err)
	}
	// The Application has no automated sync: without this the push changes
	// nothing on the cell.
	if err := s.Sync.TriggerSync(ctx, appName); err != nil {
		return false, fmt.Errorf("scaling: sync %s: %w", appName, err)
	}
	return true, nil
}

// renderRunningRelease renders the manifests of the environment's running
// release with the stored scaling config, and reports whether any of its apps
// has one. It returns nil when nothing runs.
func (s *ReleaseScaler) renderRunningRelease(ctx context.Context, tenantID, project, env string) (*deploycontracts.GitOpsPushOpts, bool, error) {
	release, err := s.Releases.GetActiveByEnv(ctx, tenantID, project, env)
	if err != nil {
		return nil, false, fmt.Errorf("scaling: read running release of %s/%s: %w", project, env, err)
	}
	if release == nil {
		return nil, false, nil
	}
	if err := s.refuseDeployInFlight(ctx, release); err != nil {
		return nil, false, err
	}

	build, err := s.Builds.GetByID(ctx, release.BuildID)
	if err != nil {
		return nil, false, fmt.Errorf("scaling: read build %s: %w", release.BuildID, err)
	}
	if build == nil {
		return nil, false, fmt.Errorf("scaling: build %s of release %s not found", release.BuildID, release.ID)
	}
	apps, err := s.Builds.GetBuildApps(ctx, release.BuildID)
	if err != nil {
		return nil, false, fmt.Errorf("scaling: read apps of build %s: %w", release.BuildID, err)
	}
	stored, anySaved := StoredScaling{Configs: s.Configs}, false
	for i := range apps {
		var saved bool
		if apps[i], saved, err = stored.ApplyToApp(ctx, tenantID, project, env, apps[i]); err != nil {
			return nil, false, err
		}
		anySaved = anySaved || saved
	}
	digests, err := releaseImageDigests(release, apps)
	if err != nil {
		return nil, false, err
	}

	manifestApps, err := deploycontracts.BuildManifestApps(ctx, deploycontracts.BuildManifestAppsInput{
		RegistryURL: s.Render.RegistryURL,
		TenantID:    tenantID,
		ProjectID:   project,
		Environment: env,
		CommitSHA:   build.CommitSHA,
		Apps:        apps,
		AppDigests:  digests,
		DomainStore: s.Domains,
	})
	if err != nil {
		return nil, false, fmt.Errorf("scaling: %w", err)
	}
	rendered, err := manifests.Render(manifests.RenderInput{
		TenantID:            tenantID,
		ProjectID:           project,
		Environment:         env,
		BuildID:             release.BuildID,
		ReleaseID:           release.ID,
		BaseDomain:          s.Render.BaseDomain,
		ClusterIssuerName:   s.Render.ClusterIssuer,
		Apps:                manifestApps,
		PrometheusAddress:   s.Render.PrometheusAddress,
		ProgressiveDelivery: s.Render.ProgressiveDelivery,
		LeanResources:       s.Render.LeanResources,
	})
	if err != nil {
		return nil, false, fmt.Errorf("scaling: render manifests of release %s: %w", release.ID, err)
	}
	return &deploycontracts.GitOpsPushOpts{
		TenantID:      tenantID,
		ProjectID:     project,
		Environment:   env,
		Manifests:     rendered,
		BuildID:       release.BuildID,
		ReleaseID:     release.ID,
		CorrelationID: release.CorrelationID,
	}, anySaved, nil
}

// refuseDeployInFlight returns ErrDeployInFlight when a release other than the
// running one is being delivered or rolled back.
func (s *ReleaseScaler) refuseDeployInFlight(ctx context.Context, running *db.Release) error {
	latest, err := s.Releases.ListByEnv(ctx, running.TenantID, running.ProjectID, running.Environment, inFlightLookback)
	if err != nil {
		return fmt.Errorf("scaling: list releases of %s/%s: %w", running.ProjectID, running.Environment, err)
	}
	for _, r := range latest {
		if r.ID == running.ID {
			continue
		}
		switch statemachine.State(r.Status) {
		case statemachine.ReleaseReady, statemachine.ReleasePending, statemachine.ReleaseRollingBack:
			return fmt.Errorf("%w: release %s is %s", ErrDeployInFlight, r.ID, r.Status)
		}
	}
	return nil
}

// releaseImageDigests maps each app to the image digest the release delivered.
// The release records one per app; a release written before it did carries only
// the digest of its first app.
func releaseImageDigests(release *db.Release, apps []db.BuildAppSpec) (map[string]string, error) {
	digests := make(map[string]string, len(apps))
	if len(release.Apps) > 0 {
		var delivered []deploycontracts.AppRelease
		if err := json.Unmarshal(release.Apps, &delivered); err != nil {
			return nil, fmt.Errorf("scaling: parse apps of release %s: %w", release.ID, err)
		}
		for _, app := range delivered {
			if app.ImageDigestSHA256 != "" {
				digests[app.Name] = app.ImageDigestSHA256
			}
		}
	}
	if len(apps) > 0 && digests[apps[0].Name] == "" && release.ImageDigest != nil {
		digests[apps[0].Name] = *release.ImageDigest
	}
	return digests, nil
}
