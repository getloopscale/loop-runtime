// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domains

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// DomainHealthStore abstracts persistence needed by the health checker.
type DomainHealthStore interface {
	// FindPendingForAutoVerify returns pending_verification domains owned by
	// tenants belonging to the given cell (roadmap 03). Pass domain.DefaultCellID
	// or "" for single-cell / backward-compatible behavior.
	FindPendingForAutoVerify(ctx context.Context, cellID string) ([]ports.CustomDomain, error)
	FindActiveForCNAMECheck(ctx context.Context, staleAfter time.Time) ([]ports.CustomDomain, error)
	FindStalePending(ctx context.Context, maxAge time.Duration) ([]ports.CustomDomain, error)
	FindStaleProvisioning(ctx context.Context, maxAge time.Duration) ([]ports.CustomDomain, error)
	FindActiveWithTLSExpiry(ctx context.Context, withinDays int) ([]ports.CustomDomain, error)
	UpdateVerified(ctx context.Context, domain string, verifiedAt time.Time) error
	UpdateCNAMECheck(ctx context.Context, domain string, checkTime time.Time, consecutiveFailures int) error
	UpdateFailure(ctx context.Context, domain, status, reason string) error
	TransitionStatus(ctx context.Context, domain, expectedStatus, newStatus string) error
	Delete(ctx context.Context, domain string) error
}

// DomainHealthChecker monitors custom domain health.
// Runs every 5 minutes. Responsibilities:
//   - Auto-verify: check pending domains for TXT record (convenience)
//   - Weekly CNAME re-check: detect dangling domains (MEET-033 D7, security)
//   - TLS expiry monitoring: warn on upcoming cert expiry
//   - Stale cleanup: remove domains pending > 7 days
//   - Cluster state convergence: detect missing IngressRoute (MEET-033 D4)
//
// MEET-2026-04-05-033: Background health checker.
type DomainHealthChecker struct {
	store       DomainHealthStore
	cnameTarget string // e.g., "cname.dev.loopzero.example"
	graceDays   int    // CNAME failure grace period (default: 14)
	staleDays   int    // pending cleanup threshold (default: 7)
	logger      *slog.Logger
	interval    time.Duration
	cellID      string // roadmap 03: cell scope for FindPendingForAutoVerify
}

// DomainHealthCheckerConfig holds configuration.
type DomainHealthCheckerConfig struct {
	Interval    time.Duration // Default: 5m
	CNAMETarget string        // e.g., "cname.dev.loopzero.example"
	GraceDays   int           // CNAME failure grace (default: 14, configurable via DOMAIN_CNAME_GRACE_DAYS)
	StaleDays   int           // Pending cleanup threshold (default: 7)
	// CellID scopes the checker to a single cell (roadmap 03). Empty means every
	// cell — how the API runs its loops (#450).
	CellID string
}

// DefaultDomainHealthCheckerConfig returns sensible defaults.
func DefaultDomainHealthCheckerConfig() DomainHealthCheckerConfig {
	return DomainHealthCheckerConfig{
		Interval:  5 * time.Minute,
		GraceDays: 14,
		StaleDays: 7,
	}
}

// NewDomainHealthChecker creates a new DomainHealthChecker.
func NewDomainHealthChecker(
	store DomainHealthStore,
	logger *slog.Logger,
	cfg DomainHealthCheckerConfig,
) *DomainHealthChecker {
	interval := cfg.Interval
	if interval == 0 {
		interval = 5 * time.Minute
	}
	graceDays := cfg.GraceDays
	if graceDays == 0 {
		graceDays = 14
	}
	staleDays := cfg.StaleDays
	if staleDays == 0 {
		staleDays = 7
	}
	// Empty means every cell (backgroundloops.Deps) — how the API runs its loops (#450).
	cellID := cfg.CellID
	return &DomainHealthChecker{
		store:       store,
		cnameTarget: cfg.CNAMETarget,
		graceDays:   graceDays,
		staleDays:   staleDays,
		logger:      logger,
		interval:    interval,
		cellID:      cellID,
	}
}

// Start runs the health checker loop until the context is cancelled.
func (h *DomainHealthChecker) Start(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	h.logger.Info("domain health checker started",
		"interval", h.interval,
		"cname_target", h.cnameTarget,
		"grace_days", h.graceDays,
		"stale_days", h.staleDays)

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("domain health checker stopped")
			return
		case <-ticker.C:
			h.runChecks(ctx)
		}
	}
}

func (h *DomainHealthChecker) runChecks(ctx context.Context) {
	h.autoVerifyPending(ctx)
	h.checkCNAME(ctx)
	h.cleanupStalePending(ctx)
	h.checkProvisioningTimeout(ctx) // F-006
	h.checkTLSExpiry(ctx)           // F-005
}

// autoVerifyPending checks pending domains for TXT record (convenience feature).
func (h *DomainHealthChecker) autoVerifyPending(ctx context.Context) {
	domains, err := h.store.FindPendingForAutoVerify(ctx, h.cellID)
	if err != nil {
		h.logger.Error("domain health: find pending failed", "error", err)
		return
	}

	for _, d := range domains {
		if ctx.Err() != nil {
			return
		}
		verified, err := verifyTXT(ctx, d.Domain, d.VerificationToken)
		if err != nil {
			// CODE-006 fix: log DNS errors instead of swallowing silently
			h.logger.Warn("domain health: auto-verify DNS lookup failed", "domain", d.Domain, "error", err)
			metrics.DomainHealthCheckerErrors.WithLabelValues("auto_verify").Inc()
			continue
		}
		if verified {
			if _, err := nextDomainStatus(d.Domain, d.Status, statemachine.DomainVerify); err != nil {
				h.logger.Warn("domain health: auto-verify skipped, status moved on", "error", err, "domain", d.Domain)
				continue
			}
			now := time.Now().UTC()
			if err := h.store.UpdateVerified(ctx, d.Domain, now); err != nil {
				h.logger.Error("domain health: auto-verify update failed", "error", err, "domain", d.Domain)
			} else {
				h.logger.Info("domain health: auto-verified", "domain", d.Domain)
			}
		}
	}
}

// checkCNAME performs weekly CNAME re-verification for active domains (MEET-033 D7).
// If CNAME is missing for 2 consecutive checks (14 days), domain degrades to failed.
func (h *DomainHealthChecker) checkCNAME(ctx context.Context) {
	if h.cnameTarget == "" {
		return // Skip if no CNAME target configured (local dev)
	}

	// Find active domains whose last_cname_check is older than 7 days
	staleAfter := time.Now().UTC().Add(-7 * 24 * time.Hour)
	domains, err := h.store.FindActiveForCNAMECheck(ctx, staleAfter)
	if err != nil {
		h.logger.Error("domain health: find active for CNAME check failed", "error", err)
		return
	}

	for _, d := range domains {
		if ctx.Err() != nil {
			return
		}

		// v2: skip CNAME check for CF-managed domains (CF zone handles DNS)
		if d.IsCFManaged() {
			continue
		}

		now := time.Now().UTC()
		ok := h.verifyCNAME(ctx, d.Domain)

		if ok {
			// CNAME valid — reset consecutive failures
			if err := h.store.UpdateCNAMECheck(ctx, d.Domain, now, 0); err != nil {
				h.logger.Error("domain health: CNAME check update failed", "error", err, "domain", d.Domain)
			}
			continue
		}

		// CNAME missing — increment consecutive failures
		// F-004: emit CNAME check failure metric
		metrics.DomainCNAMECheckFailures.WithLabelValues(d.TenantID).Inc()
		failures := d.CNAMEConsecutiveFailures + 1
		if err := h.store.UpdateCNAMECheck(ctx, d.Domain, now, failures); err != nil {
			h.logger.Error("domain health: CNAME check update failed", "error", err, "domain", d.Domain)
			continue
		}

		// At threshold (2 checks = ~14 days): degrade to failed
		maxFailures := h.graceDays / 7
		if maxFailures < 1 {
			maxFailures = 2
		}
		if failures >= maxFailures {
			// F-001 fix: transition to "removing" (not "failed") so that
			// DomainProvisioner.processRemoving() handles gitops cleanup.
			// This removes the IngressRoute + Certificate from the cluster,
			// preventing dangling domain takeover after CNAME loss.
			h.logger.Warn("domain health: CNAME lost — removing domain and cleaning up IngressRoute",
				"domain", d.Domain,
				"consecutive_failures", failures,
				"grace_days", h.graceDays)
			if _, err := transitionDomain(ctx, h.store, d.Domain, d.Status, statemachine.DomainRemove); err != nil {
				h.logger.Error("domain health: transition to removing failed", "error", err, "domain", d.Domain)
			}
		}
	}
}

// verifyCNAME checks if a domain's CNAME points to the expected target.
func (h *DomainHealthChecker) verifyCNAME(ctx context.Context, domain string) bool {
	cname, err := net.DefaultResolver.LookupCNAME(ctx, domain)
	if err != nil {
		return false
	}
	// CNAME returns with trailing dot (e.g., "cname.dev.loopzero.example.")
	cname = strings.TrimSuffix(cname, ".")
	// SEC-006 fix: require dot-boundary to prevent partial suffix matches
	// (e.g., "evil-cname.dev.loopzero.example" must NOT match "cname.dev.loopzero.example")
	return cname == h.cnameTarget
}

// cleanupStalePending removes domains pending verification for longer than staleDays.
func (h *DomainHealthChecker) cleanupStalePending(ctx context.Context) {
	maxAge := time.Duration(h.staleDays) * 24 * time.Hour
	domains, err := h.store.FindStalePending(ctx, maxAge)
	if err != nil {
		h.logger.Error("domain health: find stale pending failed", "error", err)
		return
	}

	for _, d := range domains {
		if ctx.Err() != nil {
			return
		}
		// FIND-005 fix: transition to "removing" instead of direct delete.
		// This preserves the state machine invariant and lets the provisioner's
		// processRemoving handle actual deletion (preventing race with concurrent verify).
		if _, err := transitionDomain(ctx, h.store, d.Domain, d.Status, statemachine.DomainRemove); err != nil {
			h.logger.Error("domain health: stale cleanup transition failed", "error", err, "domain", d.Domain)
		} else {
			h.logger.Info("domain health: stale pending domain marked for removal", "domain", d.Domain, "age_days", h.staleDays)
		}
	}
}

// checkProvisioningTimeout transitions domains stuck in "provisioning" > 1 hour to "failed".
// F-006: MEET-034 action #4 — prevents permanent stuck state.
func (h *DomainHealthChecker) checkProvisioningTimeout(ctx context.Context) {
	domains, err := h.store.FindStaleProvisioning(ctx, 1*time.Hour)
	if err != nil {
		h.logger.Error("domain health: find stale provisioning failed", "error", err)
		metrics.DomainHealthCheckerErrors.WithLabelValues("provisioning_timeout").Inc()
		return
	}

	for _, d := range domains {
		if ctx.Err() != nil {
			return
		}
		failed, err := nextDomainStatus(d.Domain, d.Status, statemachine.DomainProvisionTimeout)
		if err != nil {
			h.logger.Warn("domain health: provisioning timeout skipped, status moved on", "error", err, "domain", d.Domain)
			continue
		}
		if err := h.store.UpdateFailure(ctx, d.Domain, failed,
			"Provisioning timed out after 1 hour. Run `loopzero deploy` to retry."); err != nil {
			h.logger.Error("domain health: provisioning timeout transition failed", "error", err, "domain", d.Domain)
		} else {
			h.logger.Warn("domain health: domain provisioning timed out", "domain", d.Domain)
		}
	}
}

// checkTLSExpiry monitors TLS certificate expiry for active domains.
// F-005: emits DomainTLSExpiryDays gauge per domain for alerting.
func (h *DomainHealthChecker) checkTLSExpiry(ctx context.Context) {
	domains, err := h.store.FindActiveWithTLSExpiry(ctx, 30) // within 30 days
	if err != nil {
		h.logger.Error("domain health: find TLS expiry failed", "error", err)
		metrics.DomainHealthCheckerErrors.WithLabelValues("tls_expiry").Inc()
		return
	}

	now := time.Now().UTC()
	for _, d := range domains {
		if d.TLSExpiresAt == nil {
			continue
		}
		daysUntilExpiry := d.TLSExpiresAt.Sub(now).Hours() / 24
		metrics.DomainTLSExpiryDays.WithLabelValues(d.Domain).Set(daysUntilExpiry)

		if daysUntilExpiry < 7 {
			h.logger.Warn("domain health: TLS certificate expiring soon",
				"domain", d.Domain, "days_remaining", int(daysUntilExpiry))
		}
	}
}

// verifyTXT checks DNS TXT records.
// CODE-007 fix: distinguishes NXDOMAIN from transient DNS errors.
func verifyTXT(ctx context.Context, domain, expectedToken string) (bool, error) {
	txtName := "_loopzero-verify." + domain
	records, err := net.DefaultResolver.LookupTXT(ctx, txtName)
	if err != nil {
		// CODE-007 fix: NXDOMAIN is not an error — record just doesn't exist yet
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return false, nil
		}
		if strings.Contains(err.Error(), "no such host") {
			return false, nil
		}
		return false, err
	}
	for _, record := range records {
		if record == expectedToken {
			return true, nil
		}
	}
	return false, nil
}
