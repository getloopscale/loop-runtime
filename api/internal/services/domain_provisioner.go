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
	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// DomainProvisionerStore abstracts the domain persistence needed by the provisioner.
type DomainProvisionerStore interface {
	FindAndClaimVerified(ctx context.Context) ([]routes.CustomDomain, error)
	FindRemoving(ctx context.Context) ([]routes.CustomDomain, error)
	ListActiveByApp(ctx context.Context, tenantID, project, app, env string) ([]routes.CustomDomain, error)
	UpdateStatus(ctx context.Context, domain, status string) error
	TransitionStatus(ctx context.Context, domain, expectedStatus, newStatus string) error
	UpdateCFZoneStatus(ctx context.Context, domain, cfZoneStatus string) error
	UpdateFailure(ctx context.Context, domain, status, reason string) error
	Delete(ctx context.Context, domain string) error

	// v2: CF Zone Management
	FindPendingNS(ctx context.Context) ([]routes.CustomDomain, error)
	FindNSActive(ctx context.Context) ([]routes.CustomDomain, error)
}

// CFZoneStatusChecker checks CF zone activation status.
type CFZoneStatusChecker interface {
	GetZoneStatus(ctx context.Context, zoneID string) (string, error)
	TriggerActivationCheck(ctx context.Context, zoneID string) error
	DeleteZone(ctx context.Context, zoneID string) error
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
	store          DomainProvisionerStore
	releaseStore   DomainReleaseReader
	buildStore     DomainBuildReader
	gitops         GitOpsPusher
	locker         EnvironmentLocker
	logger         *slog.Logger
	baseDomain     string
	registryURL    string
	clusterIssuer  string // B14 (MEET-2026-04-23-058): required for manifest rendering
	interval       time.Duration
	cfZone         CFZoneStatusChecker // nil = v1 only, non-nil = v2 CF Zone polling
}

// DomainProvisionerConfig holds configuration for the domain provisioner.
type DomainProvisionerConfig struct {
	Interval      time.Duration
	BaseDomain    string
	RegistryURL   string
	ClusterIssuer string // B14: ClusterIssuer for Certificate CRs
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
		store:         store,
		releaseStore:  releaseStore,
		buildStore:    buildStore,
		gitops:        gitops,
		locker:        locker,
		logger:        logger,
		baseDomain:    cfg.BaseDomain,
		registryURL:   cfg.RegistryURL,
		clusterIssuer: cfg.ClusterIssuer, // B14
		interval:      interval,
	}
}

// WithCFZone enables CF Zone activation polling (v2).
func (p *DomainProvisioner) WithCFZone(cfZone CFZoneStatusChecker) *DomainProvisioner {
	p.cfZone = cfZone
	return p
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
			p.processVerified(ctx)   // v1: TXT-verified → provisioning
			p.processPendingNS(ctx)  // v2: check CF zone activation
			p.processNSActive(ctx)   // v2: ns_active → provisioning (same as verified)
			p.processRemoving(ctx)
		}
	}
}

// ProcessOnce runs a single provisioning + removal cycle. Exported for testing (T-01 fix).
func (p *DomainProvisioner) ProcessOnce(ctx context.Context) {
	p.processVerified(ctx)
	p.processPendingNS(ctx)
	p.processNSActive(ctx)
	p.processRemoving(ctx)
}

// --- v2: CF Zone activation polling ---

// processPendingNS checks CF zones waiting for nameserver propagation.
// For each domain in pending_ns: query CF API for zone status.
// If active → transition to ns_active. If > 48h → fail.
func (p *DomainProvisioner) processPendingNS(ctx context.Context) {
	if p.cfZone == nil {
		return // v1 mode — no CF zones to check
	}

	domains, err := p.store.FindPendingNS(ctx)
	if err != nil {
		p.logger.Error("domain provisioner: find pending_ns failed", "error", err)
		return
	}

	for _, d := range domains {
		if ctx.Err() != nil {
			return
		}
		if d.CFZoneID == nil || *d.CFZoneID == "" {
			continue
		}

		log := p.logger.With("domain", d.Domain, "zone_id", *d.CFZoneID)

		// Check timeout: 48h for NS propagation
		if time.Since(d.CreatedAt) > 48*time.Hour {
			log.Warn("domain provisioner: NS propagation timeout (48h)")
			if err := p.store.UpdateFailure(ctx, d.Domain, "failed", "Nameserver propagation timed out after 48 hours. Ensure nameservers are set correctly at your registrar."); err != nil {
				log.Error("domain provisioner: failed to record NS timeout", "error", err)
			}
			continue
		}

		// Query CF for zone status
		status, err := p.cfZone.GetZoneStatus(ctx, *d.CFZoneID)
		if err != nil {
			log.Warn("domain provisioner: CF zone status check failed", "error", err)
			continue
		}

		if status == "active" {
			log.Info("domain provisioner: CF zone active, transitioning to ns_active")
			if err := p.store.TransitionStatus(ctx, d.Domain, "pending_ns", "ns_active"); err != nil {
				log.Error("domain provisioner: ns_active transition failed", "error", err)
				continue
			}
			if err := p.store.UpdateCFZoneStatus(ctx, d.Domain, "active"); err != nil {
				log.Error("domain provisioner: cf zone status update failed", "error", err)
			}
			metrics.CustomDomainsTotal.WithLabelValues(d.TenantID, "ns_active").Inc()
		} else {
			// Trigger activation check to speed up detection
			_ = p.cfZone.TriggerActivationCheck(ctx, *d.CFZoneID)
		}
	}
}

// processNSActive handles CF-managed domains where NS is active but not yet provisioned.
// Same logic as processVerified — claims ns_active→provisioning, renders manifests, pushes gitops.
func (p *DomainProvisioner) processNSActive(ctx context.Context) {
	if p.cfZone == nil {
		return
	}

	domains, err := p.store.FindNSActive(ctx)
	if err != nil {
		p.logger.Error("domain provisioner: find ns_active failed", "error", err)
		return
	}

	for _, d := range domains {
		if ctx.Err() != nil {
			return
		}

		// Check if there's an active release (same as v1 flow)
		preRelease, err := p.releaseStore.GetActiveByEnv(ctx, d.TenantID, d.ProjectName, d.Environment)
		if err != nil {
			p.logger.Error("domain provisioner: release lookup failed", "error", err, "domain", d.Domain)
			continue
		}
		if preRelease == nil {
			p.logger.Info("domain provisioner: no active release for ns_active domain, skipping (will provision on next deploy)",
				"domain", d.Domain, "tenant", d.TenantID, "project", d.ProjectName, "env", d.Environment)
			continue
		}

		// Transition ns_active → provisioning, then use the shared provisioning flow
		if err := p.store.TransitionStatus(ctx, d.Domain, "ns_active", "provisioning"); err != nil {
			p.logger.Warn("domain provisioner: ns_active→provisioning failed", "domain", d.Domain, "error", err)
			continue
		}

		d.Status = "provisioning"
		p.provisionDomain(ctx, d)
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
// F-002 fix: Lock (fetch + render) → Unlock → Push → Update status.
// Pattern matches deploy flow in ContainerDeliveryActivator.
func (p *DomainProvisioner) provisionDomain(ctx context.Context, d routes.CustomDomain) {
	log := p.logger.With("domain", d.Domain, "tenant", d.TenantID, "project", d.ProjectName, "app", d.AppName, "env", d.Environment)
	start := time.Now()

	// Pre-check: is there an active release? (quick read, no lock)
	preRelease, err := p.releaseStore.GetActiveByEnv(ctx, d.TenantID, d.ProjectName, d.Environment)
	if err != nil {
		log.Error("domain provisioner: release lookup failed", "error", err)
		return
	}
	if preRelease == nil {
		log.Info("domain provisioner: no active release, skipping (will be included on next deploy)")
		return
	}

	// Phase A: Lock → re-read release (HIGH-04: release may have been superseded) + render.
	var rendered string
	var pushOpts GitOpsPushOpts

	lockErr := p.locker.WithLock(ctx, d.TenantID, d.ProjectName, d.Environment, func(tx db.DBTX) error {
		// HIGH-04 fix: re-read release inside lock to prevent using a superseded release
		release, err := p.releaseStore.GetActiveByEnv(ctx, d.TenantID, d.ProjectName, d.Environment)
		if err != nil || release == nil {
			return fmt.Errorf("no active release inside lock")
		}

		build, err := p.buildStore.GetByID(ctx, release.BuildID)
		if err != nil || build == nil {
			return fmt.Errorf("build lookup failed (build_id=%s): %w", release.BuildID, err)
		}

		apps, err := p.buildStore.GetBuildApps(ctx, release.BuildID)
		if err != nil {
			return fmt.Errorf("build apps lookup failed: %w", err)
		}

		// F-010: shared manifest building function
		manifestApps, err := BuildManifestApps(ctx, BuildManifestAppsInput{
			RegistryURL: p.registryURL,
			TenantID:    d.TenantID,
			ProjectID:   d.ProjectName,
			Environment: d.Environment,
			CommitSHA:   build.CommitSHA,
			Apps:        apps,
			DomainStore: p.store,
		})
		if err != nil {
			return err
		}

		rendered = manifests.Render(manifests.RenderInput{
			TenantID:          d.TenantID,
			ProjectID:         d.ProjectName,
			Environment:       d.Environment,
			BuildID:           release.BuildID,
			ReleaseID:         release.ID,
			BaseDomain:        p.baseDomain,
			ClusterIssuerName: p.clusterIssuer, // B14
			Apps:              manifestApps,
		})

		pushOpts = GitOpsPushOpts{
			TenantID:      d.TenantID,
			ProjectID:     d.ProjectName,
			Environment:   d.Environment,
			Manifests:     rendered,
			BuildID:       release.BuildID,
			ReleaseID:     release.ID,
			CorrelationID: d.ID,
		}

		return nil
	})

	if lockErr != nil {
		log.Error("domain provisioner: lock/render failed", "error", lockErr)
		if err2 := p.store.UpdateFailure(ctx, d.Domain, "failed", fmt.Sprintf("Provisioning failed: %v", lockErr)); err2 != nil {
			log.Error("domain provisioner: failed to record failure", "error", err2)
		}
		return
	}

	// Phase B: Push to gitops (OUTSIDE lock — avoid holding lock during network I/O).
	// F-007 fix: on gitops error, stay in provisioning (retry next tick or next deploy).
	pushCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if _, err := p.gitops.Push(pushCtx, pushOpts); err != nil {
		log.Warn("domain provisioner: gitops push failed, will retry", "error", err)
		return // stay in provisioning — F-006 timeout handles permanent failures
	}

	// Phase C: CAS transition provisioning → active (HIGH-05 fix).
	// If a concurrent Delete already moved to "removing", this CAS fails harmlessly.
	if err := p.store.TransitionStatus(ctx, d.Domain, "provisioning", "active"); err != nil {
		log.Warn("domain provisioner: CAS active transition failed (domain may have been removed)", "error", err)
		return
	}

	// F-004: emit provisioning duration metric
	elapsed := time.Since(start).Seconds()
	metrics.DomainProvisioningDuration.WithLabelValues("success").Observe(elapsed)
	log.Info("domain provisioner: domain active", "duration", elapsed)
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
// then deletes the DB row. F-002 fix: uses withEnvironmentLock for render phase.
func (p *DomainProvisioner) removeDomain(ctx context.Context, d routes.CustomDomain) {
	log := p.logger.With("domain", d.Domain, "tenant", d.TenantID, "project", d.ProjectName, "env", d.Environment)

	// Pre-check: is there an active release?
	release, err := p.releaseStore.GetActiveByEnv(ctx, d.TenantID, d.ProjectName, d.Environment)
	if err != nil {
		log.Error("domain provisioner: release lookup failed on remove", "error", err)
		return
	}
	if release == nil {
		log.Info("domain provisioner: no active release on remove, deleting row directly")
		if err := p.store.Delete(ctx, d.Domain); err != nil {
			log.Error("domain provisioner: delete failed", "error", err)
		}
		return
	}

	// Phase A: Lock → fetch + render manifests WITHOUT removed domain.
	var pushOpts GitOpsPushOpts

	lockErr := p.locker.WithLock(ctx, d.TenantID, d.ProjectName, d.Environment, func(tx db.DBTX) error {
		build, err := p.buildStore.GetByID(ctx, release.BuildID)
		if err != nil || build == nil {
			return fmt.Errorf("build lookup failed: %w", err)
		}

		apps, err := p.buildStore.GetBuildApps(ctx, release.BuildID)
		if err != nil {
			return fmt.Errorf("build apps lookup failed: %w", err)
		}

		// F-010: shared manifest building — excludes the domain being removed
		manifestApps, err := BuildManifestApps(ctx, BuildManifestAppsInput{
			RegistryURL:   p.registryURL,
			TenantID:      d.TenantID,
			ProjectID:     d.ProjectName,
			Environment:   d.Environment,
			CommitSHA:     build.CommitSHA,
			Apps:          apps,
			DomainStore:   p.store,
			ExcludeDomain: d.Domain,
		})
		if err != nil {
			return err
		}

		rendered := manifests.Render(manifests.RenderInput{
			TenantID:          d.TenantID,
			ProjectID:         d.ProjectName,
			Environment:       d.Environment,
			BuildID:           release.BuildID,
			ReleaseID:         release.ID,
			BaseDomain:        p.baseDomain,
			ClusterIssuerName: p.clusterIssuer, // B14
			Apps:              manifestApps,
		})

		pushOpts = GitOpsPushOpts{
			TenantID:      d.TenantID,
			ProjectID:     d.ProjectName,
			Environment:   d.Environment,
			Manifests:     rendered,
			BuildID:       release.BuildID,
			ReleaseID:     release.ID,
			CorrelationID: d.ID,
		}
		return nil
	})

	if lockErr != nil {
		log.Error("domain provisioner: lock/render failed on remove", "error", lockErr)
		return // stay in removing, retry next tick
	}

	// Phase B: Push to gitops (outside lock).
	pushCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if _, err := p.gitops.Push(pushCtx, pushOpts); err != nil {
		log.Warn("domain provisioner: gitops push failed on remove, will retry", "error", err)
		return // stay in removing, retry next tick
	}

	// v2: Delete CF zone if this was a CF-managed domain
	if d.IsCFManaged() && p.cfZone != nil {
		if err := p.cfZone.DeleteZone(ctx, *d.CFZoneID); err != nil {
			log.Warn("domain provisioner: CF zone deletion failed (non-fatal, zone may be manually cleaned)", "error", err)
		} else {
			log.Info("domain provisioner: CF zone deleted")
		}
	}

	// Gitops push succeeded — safe to delete the DB row
	if err := p.store.Delete(ctx, d.Domain); err != nil {
		log.Error("domain provisioner: delete after gitops push failed", "error", err)
		return
	}

	log.Info("domain provisioner: domain removed and cleaned up from cluster")
}
