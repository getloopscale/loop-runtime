// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/manifests"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/pkg/naming"
)

// DomainProvisionerStore abstracts the domain persistence needed by the provisioner.
type DomainProvisionerStore interface {
	FindAndClaimVerified(ctx context.Context) ([]routes.CustomDomain, error)
	FindRemoving(ctx context.Context) ([]routes.CustomDomain, error)
	ListActiveByApp(ctx context.Context, tenantID, project, app, env string) ([]routes.CustomDomain, error)
	UpdateStatus(ctx context.Context, domain, status string) error
	UpdateFailure(ctx context.Context, domain, status, reason string) error
	Delete(ctx context.Context, domain string) error
}

// DomainReleaseReader fetches the active release for an environment.
type DomainReleaseReader interface {
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*db.Release, error)
}

// DomainBuildReader fetches build data for manifest rendering.
type DomainBuildReader interface {
	GetByID(ctx context.Context, buildID string) (*db.Build, error)
	GetBuildApps(ctx context.Context, buildID string) ([]db.BuildAppSpec, error)
}

// DomainProvisioner picks up verified domains and provisions them via manifest re-render + gitops push.
// Also handles domain removal (removing → gitops cleanup → row deletion).
// Runs every 30 seconds.
// MEET-2026-04-05-033: Background service — verify endpoint is DNS-only, provisioning is async.
// MEET-2026-04-06-034: Full implementation with manifest rendering and gitops push.
type DomainProvisioner struct {
	store        DomainProvisionerStore
	releaseStore DomainReleaseReader
	buildStore   DomainBuildReader
	gitops       GitOpsPusher
	locker       EnvironmentLocker
	logger       *slog.Logger
	baseDomain   string
	registryURL  string
	interval     time.Duration
}

// DomainProvisionerConfig holds configuration for the domain provisioner.
type DomainProvisionerConfig struct {
	Interval    time.Duration
	BaseDomain  string
	RegistryURL string
}

// DefaultDomainProvisionerConfig returns sensible defaults.
func DefaultDomainProvisionerConfig() DomainProvisionerConfig {
	return DomainProvisionerConfig{
		Interval: 30 * time.Second,
	}
}

// NewDomainProvisioner creates a new DomainProvisioner.
func NewDomainProvisioner(
	store DomainProvisionerStore,
	releaseStore DomainReleaseReader,
	buildStore DomainBuildReader,
	gitops GitOpsPusher,
	locker EnvironmentLocker,
	logger *slog.Logger,
	cfg DomainProvisionerConfig,
) *DomainProvisioner {
	interval := cfg.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}
	return &DomainProvisioner{
		store:        store,
		releaseStore: releaseStore,
		buildStore:   buildStore,
		gitops:       gitops,
		locker:       locker,
		logger:       logger,
		baseDomain:   cfg.BaseDomain,
		registryURL:  cfg.RegistryURL,
		interval:     interval,
	}
}

// Start runs the provisioner loop until the context is cancelled.
func (p *DomainProvisioner) Start(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.logger.Info("domain provisioner started", "interval", p.interval)

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("domain provisioner stopped")
			return
		case <-ticker.C:
			p.processVerified(ctx)
			p.processRemoving(ctx)
		}
	}
}

// processVerified atomically claims verified→provisioning and provisions them.
func (p *DomainProvisioner) processVerified(ctx context.Context) {
	domains, err := p.store.FindAndClaimVerified(ctx)
	if err != nil {
		p.logger.Error("domain provisioner: claim verified failed", "error", err)
		return
	}

	for _, d := range domains {
		if ctx.Err() != nil {
			return
		}
		p.provisionDomain(ctx, d)
	}
}

// provisionDomain renders manifests with the custom domain and pushes to gitops.
// MEET-034: Pre-check → Lock (render) → Unlock → Push → Transition to active.
func (p *DomainProvisioner) provisionDomain(ctx context.Context, d routes.CustomDomain) {
	log := p.logger.With("domain", d.Domain, "tenant", d.TenantID, "project", d.ProjectName, "app", d.AppName, "env", d.Environment)
	start := time.Now()

	// Pre-check: is there an active release? (no lock needed for read)
	release, err := p.releaseStore.GetActiveByEnv(ctx, d.TenantID, d.ProjectName, d.Environment)
	if err != nil {
		log.Error("domain provisioner: release lookup failed", "error", err)
		return // retry next tick
	}
	if release == nil {
		// No active release — domain cannot be provisioned yet.
		// The next `loopzero deploy` will include it via Block 0 fix.
		log.Info("domain provisioner: no active release, skipping (will be included on next deploy)")
		return
	}

	// Get build record for CommitSHA (needed for image tag computation)
	build, err := p.buildStore.GetByID(ctx, release.BuildID)
	if err != nil || build == nil {
		log.Error("domain provisioner: build lookup failed", "error", err, "build_id", release.BuildID)
		p.store.UpdateFailure(ctx, d.Domain, "failed", "Failed to read build record")
		return
	}

	// Get build apps for manifest rendering
	apps, err := p.buildStore.GetBuildApps(ctx, release.BuildID)
	if err != nil {
		log.Error("domain provisioner: build apps lookup failed", "error", err, "build_id", release.BuildID)
		p.store.UpdateFailure(ctx, d.Domain, "failed", "Failed to read build configuration")
		return
	}

	// Build manifest input with custom domains for ALL apps
	var manifestApps []manifests.AppManifestInput
	for _, app := range apps {
		imageRef := naming.ImageTag(p.registryURL, d.TenantID, d.ProjectName, app.Name, build.CommitSHA)

		// Query custom domains for this app
		var customDomains []manifests.CustomDomainInput
		activeDomains, err := p.store.ListActiveByApp(ctx, d.TenantID, d.ProjectName, app.Name, d.Environment)
		if err != nil {
			log.Warn("domain provisioner: custom domain query failed for app", "app", app.Name, "error", err)
		} else {
			for _, cd := range activeDomains {
				customDomains = append(customDomains, manifests.CustomDomainInput{Domain: cd.Domain, ID: cd.ID})
			}
		}

		manifestApps = append(manifestApps, manifests.AppManifestInput{
			Name:          app.Name,
			Type:          app.Type,
			Rendering:     app.Rendering,
			Port:          app.Port,
			ImageRef:      imageRef,
			Secrets:       app.Secrets,
			CustomDomains: customDomains,
		})
	}

	// Render full manifests
	rendered := manifests.Render(manifests.RenderInput{
		TenantID:    d.TenantID,
		ProjectID:   d.ProjectName,
		Environment: d.Environment,
		BuildID:     release.BuildID,
		ReleaseID:   release.ID,
		BaseDomain:  p.baseDomain,
		Apps:        manifestApps,
	})

	// Push to gitops (OUTSIDE lock — consistent with deploy pattern, MEET-034 Q1)
	pushCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	_, err = p.gitops.Push(pushCtx, GitOpsPushOpts{
		TenantID:      d.TenantID,
		ProjectID:     d.ProjectName,
		Environment:   d.Environment,
		Manifests:     rendered,
		BuildID:       release.BuildID,
		ReleaseID:     release.ID,
		CorrelationID: d.ID,
	})
	if err != nil {
		log.Error("domain provisioner: gitops push failed", "error", err)
		p.store.UpdateFailure(ctx, d.Domain, "failed", fmt.Sprintf("Deploy failed: %v", err))
		return
	}

	// Option C (MEET-034 Q3): set active after gitops push. TLS tracked separately.
	if err := p.store.UpdateStatus(ctx, d.Domain, "active"); err != nil {
		log.Error("domain provisioner: status update to active failed", "error", err)
		return
	}

	log.Info("domain provisioner: domain active",
		"duration", time.Since(start).Seconds())
}

// processRemoving picks up domains in "removing" status and cleans them up.
func (p *DomainProvisioner) processRemoving(ctx context.Context) {
	domains, err := p.store.FindRemoving(ctx)
	if err != nil {
		p.logger.Error("domain provisioner: find removing failed", "error", err)
		return
	}

	for _, d := range domains {
		if ctx.Err() != nil {
			return
		}
		p.removeDomain(ctx, d)
	}
}

// removeDomain re-renders manifests WITHOUT the removed domain, pushes to gitops,
// then deletes the DB row. MEET-034 Block 3.
func (p *DomainProvisioner) removeDomain(ctx context.Context, d routes.CustomDomain) {
	log := p.logger.With("domain", d.Domain, "tenant", d.TenantID, "project", d.ProjectName, "env", d.Environment)

	// Pre-check: is there an active release?
	release, err := p.releaseStore.GetActiveByEnv(ctx, d.TenantID, d.ProjectName, d.Environment)
	if err != nil {
		log.Error("domain provisioner: release lookup failed on remove", "error", err)
		return // retry next tick
	}
	if release == nil {
		// No active release — no gitops resources to clean up. Just delete the row.
		log.Info("domain provisioner: no active release on remove, deleting row directly")
		if err := p.store.Delete(ctx, d.Domain); err != nil {
			log.Error("domain provisioner: delete failed", "error", err)
		}
		return
	}

	// Get build record for CommitSHA
	build, err := p.buildStore.GetByID(ctx, release.BuildID)
	if err != nil || build == nil {
		log.Error("domain provisioner: build lookup failed on remove", "error", err)
		return
	}

	// Get build apps for manifest re-rendering
	apps, err := p.buildStore.GetBuildApps(ctx, release.BuildID)
	if err != nil {
		log.Error("domain provisioner: build apps lookup failed on remove", "error", err)
		return
	}

	// Build manifest input — custom domains for all apps EXCEPT the one being removed
	var manifestApps []manifests.AppManifestInput
	for _, app := range apps {
		imageRef := naming.ImageTag(p.registryURL, d.TenantID, d.ProjectName, app.Name, build.CommitSHA)

		var customDomains []manifests.CustomDomainInput
		activeDomains, err := p.store.ListActiveByApp(ctx, d.TenantID, d.ProjectName, app.Name, d.Environment)
		if err != nil {
			log.Warn("domain provisioner: custom domain query failed for app on remove", "app", app.Name, "error", err)
		} else {
			for _, cd := range activeDomains {
				// Exclude the domain being removed
				if cd.Domain == d.Domain {
					continue
				}
				customDomains = append(customDomains, manifests.CustomDomainInput{Domain: cd.Domain, ID: cd.ID})
			}
		}

		manifestApps = append(manifestApps, manifests.AppManifestInput{
			Name:          app.Name,
			Type:          app.Type,
			Rendering:     app.Rendering,
			Port:          app.Port,
			ImageRef:      imageRef,
			Secrets:       app.Secrets,
			CustomDomains: customDomains,
		})
	}

	// Re-render WITHOUT the removed domain
	rendered := manifests.Render(manifests.RenderInput{
		TenantID:    d.TenantID,
		ProjectID:   d.ProjectName,
		Environment: d.Environment,
		BuildID:     release.BuildID,
		ReleaseID:   release.ID,
		BaseDomain:  p.baseDomain,
		Apps:        manifestApps,
	})

	// Push to gitops — removes IngressRoute + Certificate from repo
	pushCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	_, err = p.gitops.Push(pushCtx, GitOpsPushOpts{
		TenantID:      d.TenantID,
		ProjectID:     d.ProjectName,
		Environment:   d.Environment,
		Manifests:     rendered,
		BuildID:       release.BuildID,
		ReleaseID:     release.ID,
		CorrelationID: d.ID,
	})
	if err != nil {
		log.Error("domain provisioner: gitops push failed on remove", "error", err)
		return // leave in "removing", retry next tick
	}

	// Gitops push succeeded — safe to delete the DB row
	if err := p.store.Delete(ctx, d.Domain); err != nil {
		log.Error("domain provisioner: delete after gitops push failed", "error", err)
		return
	}

	log.Info("domain provisioner: domain removed and cleaned up from cluster")
}
