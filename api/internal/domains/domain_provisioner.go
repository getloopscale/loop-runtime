// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domains

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/manifests"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/pkg/statemachine"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// DomainProvisionerStore abstracts the domain persistence needed by the provisioner.
type DomainProvisionerStore interface {
	FindAndClaimVerified(ctx context.Context) ([]ports.CustomDomain, error)
	FindRemoving(ctx context.Context) ([]ports.CustomDomain, error)
	ListActiveByApp(ctx context.Context, tenantID, project, app, env string) ([]ports.CustomDomain, error)
	TransitionStatus(ctx context.Context, domain, expectedStatus, newStatus string) error
	UpdateCFZoneStatus(ctx context.Context, domain, cfZoneStatus string) error
	UpdateFailure(ctx context.Context, domain, status, reason string) error
	Delete(ctx context.Context, domain string) error

	// v2: CF Zone Management
	FindPendingNS(ctx context.Context) ([]ports.CustomDomain, error)
	FindNSActive(ctx context.Context) ([]ports.CustomDomain, error)
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
	gitops         deploycontracts.GitOpsPusher
	locker         deploycontracts.EnvironmentLocker
	logger         *slog.Logger
	baseDomain     string
	registryURL    string
	clusterIssuer  string // B14 (MEET-2026-04-23-058): required for manifest rendering
	prometheusAddr string // argo-rollouts-progressive-delivery-plan T1.2 (D6)
	progressive    bool   // Config.ProgressiveDelivery (D8)
	leanResources  bool   // Config.LeanResources (T-08)
	interval       time.Duration
	cfZone         CFZoneStatusChecker // nil = v1 only, non-nil = v2 CF Zone polling
	savedScaling   SavedScaling        // nil = render the build's scaling
}

// SavedScaling overlays the saved scaling configuration of an environment on
// the apps a build declared (scaling.StoredScaling).
type SavedScaling interface {
	Apply(ctx context.Context, tenantID, project, env string, apps []db.BuildAppSpec) ([]db.BuildAppSpec, error)
}

// WithSavedScaling makes every render carry the saved scaling configuration.
// The provisioner renders the running release again to add or remove a host;
// with the build's apps alone it would write the build's replica count to the
// GitOps repository and undo a scale made since the deploy.
func (p *DomainProvisioner) WithSavedScaling(s SavedScaling) *DomainProvisioner {
	p.savedScaling = s
	return p
}

// withSavedScaling returns apps with the saved scaling configuration applied.
func (p *DomainProvisioner) withSavedScaling(ctx context.Context, d ports.CustomDomain, apps []db.BuildAppSpec) ([]db.BuildAppSpec, error) {
	if p.savedScaling == nil {
		return apps, nil
	}
	scaled, err := p.savedScaling.Apply(ctx, d.TenantID, d.ProjectName, d.Environment, apps)
	if err != nil {
		return nil, fmt.Errorf("saved scaling lookup failed: %w", err)
	}
	return scaled, nil
}

// DomainProvisionerConfig holds configuration for the domain provisioner.
type DomainProvisionerConfig struct {
	Interval          time.Duration
	BaseDomain        string
	RegistryURL       string
	ClusterIssuer     string // B14: ClusterIssuer for Certificate CRs
	PrometheusAddress string // argo-rollouts-progressive-delivery-plan T1.2 (D6)
	// ProgressiveDelivery and LeanResources are copied into every RenderInput
	// the provisioner builds (Config.ProgressiveDelivery / Config.LeanResources).
	ProgressiveDelivery bool
	LeanResources       bool
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
	gitops deploycontracts.GitOpsPusher,
	locker deploycontracts.EnvironmentLocker,
	logger *slog.Logger,
	cfg DomainProvisionerConfig,
) *DomainProvisioner {
	interval := cfg.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}
	return &DomainProvisioner{
		store:          store,
		releaseStore:   releaseStore,
		buildStore:     buildStore,
		gitops:         gitops,
		locker:         locker,
		logger:         logger,
		baseDomain:     cfg.BaseDomain,
		registryURL:    cfg.RegistryURL,
		clusterIssuer:  cfg.ClusterIssuer,     // B14
		prometheusAddr: cfg.PrometheusAddress, // argo-rollouts-progressive-delivery-plan T1.2
		progressive:    cfg.ProgressiveDelivery,
		leanResources:  cfg.LeanResources,
		interval:       interval,
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
			p.processVerified(ctx)  // v1: TXT-verified → provisioning
			p.processPendingNS(ctx) // v2: check CF zone activation
			p.processNSActive(ctx)  // v2: ns_active → provisioning (same as verified)
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
			failed, err := nextDomainStatus(d.Domain, d.Status, statemachine.DomainNSTimeout)
			if err != nil {
				log.Warn("domain provisioner: NS timeout skipped, status moved on", "error", err)
				continue
			}
			if err := p.store.UpdateFailure(ctx, d.Domain, failed, "Nameserver propagation timed out after 48 hours. Ensure nameservers are set correctly at your registrar."); err != nil {
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
			if _, err := transitionDomain(ctx, p.store, d.Domain, string(statemachine.DomainPendingNS), statemachine.DomainNSActivated); err != nil {
				log.Error("domain provisioner: ns_active transition failed", "error", err)
				continue
			}
			if err := p.store.UpdateCFZoneStatus(ctx, d.Domain, "active"); err != nil {
				log.Error("domain provisioner: cf zone status update failed", "error", err)
			}
			metrics.CustomDomainsTotal.WithLabelValues(d.TenantID, "ns_active").Inc()
		} else {
			// Trigger activation check to speed up detection. Fire-and-forget by
			// design — no control flow depends on it and the next tick re-reads
			// the zone status regardless — but a failure is still reported,
			// matching the sibling CF status call above.
			if err := p.cfZone.TriggerActivationCheck(ctx, *d.CFZoneID); err != nil {
				log.Warn("domain provisioner: CF zone activation check trigger failed", "error", err)
			}
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
		next, err := transitionDomain(ctx, p.store, d.Domain, string(statemachine.DomainNSActive), statemachine.DomainProvision)
		if err != nil {
			p.logger.Warn("domain provisioner: ns_active→provisioning failed", "domain", d.Domain, "error", err)
			continue
		}

		d.Status = next
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
func (p *DomainProvisioner) provisionDomain(ctx context.Context, d ports.CustomDomain) {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.ports.provision",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", d.TenantID),
			attribute.String("loopzero.domain", d.Domain),
		))
	defer span.End()

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
	var pushOpts deploycontracts.GitOpsPushOpts

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
		if apps, err = p.withSavedScaling(ctx, d, apps); err != nil {
			return err
		}

		// CHG-2026-04-29-DOGFOOD iter 8 — the manifest builder now requires a
		// per-app digest (the supply-chain VAP rejects tag refs). In the domain
		// provisioner the only available source is release.ImageDigest, which
		// phaseACreateRelease populates for Apps[0] only (per ADR-0014).
		// Multi-app custom domains are not emitted here — fail explicitly
		// instead of producing a manifest with the wrong image.
		appDigests := make(map[string]string, len(apps))
		if release.ImageDigest != nil && *release.ImageDigest != "" && len(apps) > 0 {
			appDigests[apps[0].Name] = *release.ImageDigest
		}

		// F-010: shared manifest building function
		manifestApps, err := deploycontracts.BuildManifestApps(ctx, deploycontracts.BuildManifestAppsInput{
			RegistryURL: p.registryURL,
			TenantID:    d.TenantID,
			ProjectID:   d.ProjectName,
			Environment: d.Environment,
			CommitSHA:   build.CommitSHA,
			Apps:        apps,
			AppDigests:  appDigests,
			DomainStore: p.store,
		})
		if err != nil {
			return err
		}

		// Plan v1.2 T3.1 — propagate Render error inside the lock so the
		// caller can fail the domain provisioning gracefully.
		renderedYAML, renderErr := manifests.Render(manifests.RenderInput{
			TenantID:          d.TenantID,
			ProjectID:         d.ProjectName,
			Environment:       d.Environment,
			BuildID:           release.BuildID,
			ReleaseID:         release.ID,
			BaseDomain:        p.baseDomain,
			ClusterIssuerName: p.clusterIssuer, // B14
			Apps:              manifestApps,
			// argo-rollouts-progressive-delivery-plan T1.2.
			PrometheusAddress:   p.prometheusAddr,
			ProgressiveDelivery: p.progressive,
			LeanResources:       p.leanResources,
		})
		if renderErr != nil {
			return fmt.Errorf("manifests.Render: %w", renderErr)
		}
		rendered = renderedYAML

		pushOpts = deploycontracts.GitOpsPushOpts{
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
		metrics.DomainProvisioningDuration.WithLabelValues("failure").Observe(time.Since(start).Seconds())
		failed, smErr := nextDomainStatus(d.Domain, string(statemachine.DomainProvisioning), statemachine.DomainProvisionFail)
		if smErr != nil {
			log.Error("domain provisioner: failure transition not in the domain machine", "error", smErr)
			return
		}
		if err2 := p.store.UpdateFailure(ctx, d.Domain, failed, fmt.Sprintf("Provisioning failed: %v", lockErr)); err2 != nil {
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
		metrics.DomainProvisioningDuration.WithLabelValues("failure").Observe(time.Since(start).Seconds())
		return // stay in provisioning — F-006 timeout handles permanent failures
	}

	// Phase C: CAS transition provisioning → active (HIGH-05 fix).
	// If a concurrent Delete already moved to "removing", this CAS fails harmlessly.
	if _, err := transitionDomain(ctx, p.store, d.Domain, string(statemachine.DomainProvisioning), statemachine.DomainActivate); err != nil {
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
func (p *DomainProvisioner) removeDomain(ctx context.Context, d ports.CustomDomain) {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.ports.remove",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", d.TenantID),
			attribute.String("loopzero.domain", d.Domain),
		))
	defer span.End()

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

	// Phase A: Lock → fetch + render manifests WITHOUT removed ports.
	var pushOpts deploycontracts.GitOpsPushOpts

	lockErr := p.locker.WithLock(ctx, d.TenantID, d.ProjectName, d.Environment, func(tx db.DBTX) error {
		build, err := p.buildStore.GetByID(ctx, release.BuildID)
		if err != nil || build == nil {
			return fmt.Errorf("build lookup failed: %w", err)
		}

		apps, err := p.buildStore.GetBuildApps(ctx, release.BuildID)
		if err != nil {
			return fmt.Errorf("build apps lookup failed: %w", err)
		}
		if apps, err = p.withSavedScaling(ctx, d, apps); err != nil {
			return err
		}

		// CHG-2026-04-29-DOGFOOD iter 8 — the manifest builder now requires a
		// per-app digest (the supply-chain VAP rejects tag refs). The remove
		// flow has the same restriction as provisionDomain: the digest comes
		// from release.ImageDigest and covers Apps[0] only (per ADR-0014).
		appDigests := make(map[string]string, len(apps))
		if release.ImageDigest != nil && *release.ImageDigest != "" && len(apps) > 0 {
			appDigests[apps[0].Name] = *release.ImageDigest
		}

		// F-010: shared manifest building — excludes the domain being removed
		manifestApps, err := deploycontracts.BuildManifestApps(ctx, deploycontracts.BuildManifestAppsInput{
			RegistryURL:   p.registryURL,
			TenantID:      d.TenantID,
			ProjectID:     d.ProjectName,
			Environment:   d.Environment,
			CommitSHA:     build.CommitSHA,
			Apps:          apps,
			AppDigests:    appDigests,
			DomainStore:   p.store,
			ExcludeDomain: d.Domain,
		})
		if err != nil {
			return err
		}

		// Plan v1.2 T3.1 — propagate Render error from the domain-removal path.
		rendered, renderErr := manifests.Render(manifests.RenderInput{
			TenantID:          d.TenantID,
			ProjectID:         d.ProjectName,
			Environment:       d.Environment,
			BuildID:           release.BuildID,
			ReleaseID:         release.ID,
			BaseDomain:        p.baseDomain,
			ClusterIssuerName: p.clusterIssuer, // B14
			Apps:              manifestApps,
			// argo-rollouts-progressive-delivery-plan T1.2.
			PrometheusAddress:   p.prometheusAddr,
			ProgressiveDelivery: p.progressive,
			LeanResources:       p.leanResources,
		})
		if renderErr != nil {
			return fmt.Errorf("manifests.Render (domain remove): %w", renderErr)
		}

		pushOpts = deploycontracts.GitOpsPushOpts{
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
