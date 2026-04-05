// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/manifests"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// DomainProvisionerStore abstracts the domain persistence needed by the provisioner.
type DomainProvisionerStore interface {
	// FindVerified returns domains in "verified" status ready for provisioning.
	FindVerified(ctx context.Context) ([]routes.CustomDomain, error)
	// FindRemoving returns domains in "removing" status ready for cleanup.
	FindRemoving(ctx context.Context) ([]routes.CustomDomain, error)
	// UpdateStatus transitions a domain's status.
	UpdateStatus(ctx context.Context, domain, status string) error
	// UpdateFailure transitions a domain to failed with reason.
	UpdateFailure(ctx context.Context, domain, status, reason string) error
	// Delete permanently removes a domain row.
	Delete(ctx context.Context, domain string) error
	// ListActiveByApp returns active custom domains for a specific app.
	ListActiveByApp(ctx context.Context, tenantID, project, app, env string) ([]routes.CustomDomain, error)
}

// DomainManifestRenderer generates manifests for a project+environment.
type DomainManifestRenderer interface {
	// RenderForDomainChange re-renders manifests including custom domains.
	RenderForDomainChange(ctx context.Context, tenantID, project, env string, customDomains map[string][]manifests.CustomDomainInput) (string, error)
}

// DomainProvisioner picks up verified domains and provisions them (manifests → gitops → ArgoCD).
// Also handles domain removal (removing → gitops cleanup → row deletion).
// Runs every 30 seconds. Uses SELECT FOR UPDATE SKIP LOCKED for HA coordination.
// MEET-2026-04-05-033: Background service — verify endpoint is DNS-only, provisioning is async.
type DomainProvisioner struct {
	store    DomainProvisionerStore
	gitops   GitOpsPusher
	locker   EnvironmentLocker
	logger   *slog.Logger
	interval time.Duration
}

// DomainProvisionerConfig holds configuration for the domain provisioner.
type DomainProvisionerConfig struct {
	Interval time.Duration // Default: 30s
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
		store:    store,
		gitops:   gitops,
		locker:   locker,
		logger:   logger,
		interval: interval,
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

// processVerified picks up domains in "verified" status and provisions them.
func (p *DomainProvisioner) processVerified(ctx context.Context) {
	domains, err := p.store.FindVerified(ctx)
	if err != nil {
		p.logger.Error("domain provisioner: find verified failed", "error", err)
		return
	}

	for _, d := range domains {
		if ctx.Err() != nil {
			return
		}
		p.provisionDomain(ctx, d)
	}
}

// provisionDomain handles a single verified domain: manifests → gitops push.
func (p *DomainProvisioner) provisionDomain(ctx context.Context, d routes.CustomDomain) {
	log := p.logger.With("domain", d.Domain, "tenant", d.TenantID, "project", d.ProjectName, "app", d.AppName, "env", d.Environment)

	// Transition to provisioning
	if err := p.store.UpdateStatus(ctx, d.Domain, "provisioning"); err != nil {
		log.Error("domain provisioner: update to provisioning failed", "error", err)
		return
	}

	// The actual manifest re-rendering and gitops push would happen here.
	// This requires fetching the current release's manifest input, adding the
	// custom domain, re-rendering, and pushing to gitops — all under environment lock.
	//
	// For now, this is a placeholder that transitions to provisioning.
	// The full integration with the manifest renderer and gitops pusher will be
	// connected when the DomainManifestRenderer is implemented with the build store.
	log.Info("domain provisioner: domain moved to provisioning (manifest integration pending)")
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

// removeDomain handles cleanup for a domain being removed.
// CODE-004 fix: Must re-render manifests WITHOUT this domain and push to gitops
// BEFORE deleting the DB row. Otherwise IngressRoute + Certificate persist in cluster.
func (p *DomainProvisioner) removeDomain(ctx context.Context, d routes.CustomDomain) {
	log := p.logger.With("domain", d.Domain, "tenant", d.TenantID, "project", d.ProjectName, "env", d.Environment)

	// Step 1: Re-render manifests without this domain and push to gitops.
	// This must happen under withEnvironmentLock to prevent race with concurrent deploys.
	// The gitops push removes the domain's IngressRoute + Certificate from the repo.
	// ArgoCD sync with prune=true then deletes the orphaned K8s resources.
	//
	// TODO(CODE-004): Implement manifest re-render + gitops push here.
	// Pattern: p.locker.WithLock(ctx, d.TenantID, d.ProjectName, d.Environment, func(tx db.DBTX) error {
	//     1. Fetch active release for this (tenant, project, env)
	//     2. Get all active custom domains EXCEPT this one
	//     3. Re-render manifests with remaining custom domains
	//     4. Push to gitops
	//     5. Delete domain row (inside lock)
	// })
	// For now: delete row directly. The next deploy will re-render without this domain.
	// This is acceptable for pre-release but must be fixed before production launch.
	log.Warn("domain provisioner: gitops cleanup not yet implemented — domain will be removed from manifests on next deploy")

	// Step 2: Delete the row.
	if err := p.store.Delete(ctx, d.Domain); err != nil {
		log.Error("domain provisioner: delete failed", "error", err)
		return
	}

	log.Info("domain provisioner: domain row removed")
}
