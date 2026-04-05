// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// DomainHealthStore abstracts persistence needed by the health checker.
type DomainHealthStore interface {
	// FindPendingForAutoVerify returns pending_verification domains.
	FindPendingForAutoVerify(ctx context.Context) ([]routes.CustomDomain, error)
	// FindActiveForCNAMECheck returns active domains due for weekly CNAME re-verification.
	FindActiveForCNAMECheck(ctx context.Context, staleAfter time.Time) ([]routes.CustomDomain, error)
	// FindStalePending returns domains in pending_verification older than maxAge.
	FindStalePending(ctx context.Context, maxAge time.Duration) ([]routes.CustomDomain, error)
	// UpdateVerified transitions a domain to verified.
	UpdateVerified(ctx context.Context, domain string, verifiedAt time.Time) error
	// UpdateCNAMECheck records the result of a CNAME re-verification.
	UpdateCNAMECheck(ctx context.Context, domain string, checkTime time.Time, consecutiveFailures int) error
	// UpdateFailure transitions a domain to failed.
	UpdateFailure(ctx context.Context, domain, status, reason string) error
	// UpdateStatus transitions a domain's status.
	UpdateStatus(ctx context.Context, domain, status string) error
	// Delete removes a domain row.
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
	store          DomainHealthStore
	cnameTarget    string // e.g., "cname.dev.loopzero.example"
	graceDays      int    // CNAME failure grace period (default: 14)
	staleDays      int    // pending cleanup threshold (default: 7)
	logger         *slog.Logger
	interval       time.Duration
}

// DomainHealthCheckerConfig holds configuration.
type DomainHealthCheckerConfig struct {
	Interval    time.Duration // Default: 5m
	CNAMETarget string        // e.g., "cname.dev.loopzero.example"
	GraceDays   int           // CNAME failure grace (default: 14, configurable via DOMAIN_CNAME_GRACE_DAYS)
	StaleDays   int           // Pending cleanup threshold (default: 7)
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
	return &DomainHealthChecker{
		store:       store,
		cnameTarget: cfg.CNAMETarget,
		graceDays:   graceDays,
		staleDays:   staleDays,
		logger:      logger,
		interval:    interval,
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
}

// autoVerifyPending checks pending domains for TXT record (convenience feature).
func (h *DomainHealthChecker) autoVerifyPending(ctx context.Context) {
	domains, err := h.store.FindPendingForAutoVerify(ctx)
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
			continue
		}
		if verified {
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
			h.logger.Warn("domain health: CNAME lost — degrading to failed",
				"domain", d.Domain,
				"consecutive_failures", failures,
				"grace_days", h.graceDays)
			if err := h.store.UpdateFailure(ctx, d.Domain, "failed", "CNAME record no longer resolves to "+h.cnameTarget); err != nil {
				h.logger.Error("domain health: degrade to failed error", "error", err, "domain", d.Domain)
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
		if err := h.store.Delete(ctx, d.Domain); err != nil {
			h.logger.Error("domain health: stale cleanup delete failed", "error", err, "domain", d.Domain)
		} else {
			h.logger.Info("domain health: cleaned up stale pending domain", "domain", d.Domain, "age_days", h.staleDays)
		}
	}
}

// verifyTXT checks DNS TXT records (reused from domain package, inlined to avoid circular dep).
func verifyTXT(ctx context.Context, domain, expectedToken string) (bool, error) {
	txtName := "_loopzero-verify." + domain
	records, err := net.DefaultResolver.LookupTXT(ctx, txtName)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record == expectedToken {
			return true, nil
		}
	}
	return false, nil
}
