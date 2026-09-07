// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domains

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/getloopscale/loopzero/api/internal/routes/httpio"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

// createWithCFZone implements the v2 domain creation flow:
// 1. Create CF zone for the domain
// 2. Scan existing DNS records (email preservation)
// 3. Add CNAME @ → cname.dev.loopzero.example (proxied, CF flattening)
// 4. Return nameservers for user to configure at registrar
//
// Plan 2026-06-15-code-review-49-findings-remediation T5.4 (R2-F3 HIGH):
// CC 16 → ≤ 12 via 2 helper extracts (removeConflictingApexRecords +
// dedupAndListZoneRecords). Behavior preserved verbatim.
func (h *DomainHandler) createWithCFZone(w http.ResponseWriter, r *http.Request, ctx context.Context, uc *auth.UserContext, req domainCreateRequest) {
	// Step 1: Create CF zone
	zone, err := h.cfZone.CreateZone(ctx, req.Domain)
	if err != nil {
		slog.ErrorContext(ctx, "cf zone create failed", "domain", req.Domain, "error", err)
		httpio.WriteJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Failed to configure domain. Please try again.",
			"code":  "CF_ZONE_ERROR",
		})
		return
	}

	// Step 2: Scan existing DNS records (email, DKIM, etc.)
	if scanErr := h.cfZone.ScanDNSRecords(ctx, zone.ZoneID); scanErr != nil {
		slog.WarnContext(ctx, "cf dns scan failed (non-fatal)", "domain", req.Domain, "error", scanErr)
	}

	// Step 3: Remove conflicting apex records (A/AAAA/CNAME at root)
	h.removeConflictingApexRecords(ctx, zone.ZoneID, req.Domain)

	// Step 4: Add routing CNAME (proxied → CF edge → tunnel → Traefik)
	proxied := true
	_, cnameErr := h.cfZone.CreateDNSRecord(ctx, zone.ZoneID, CFDNSRecord{
		Type:    "CNAME",
		Name:    req.Domain,
		Content: h.cnameTarget,
		TTL:     1, // auto (required when proxied)
		Proxied: &proxied,
	})
	if cnameErr != nil {
		slog.ErrorContext(ctx, "cf cname create failed", "domain", req.Domain, "error", cnameErr)
		// Rollback: delete the zone
		_ = h.cfZone.DeleteZone(ctx, zone.ZoneID)
		httpio.WriteJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Failed to configure domain routing. Please try again.",
			"code":  "CF_ZONE_ERROR",
		})
		return
	}

	// Step 5: List + deduplicate imported records
	dnsRecords, recordCount := h.dedupAndListZoneRecords(ctx, zone.ZoneID, req.Domain)

	// Step 6: Save to DB
	zoneID := zone.ZoneID
	domain := &CustomDomain{
		ID:                 ports.GenerateDomainID(),
		TenantID:           uc.TenantID,
		ProjectName:        req.Project,
		AppName:            req.App,
		Environment:        req.Environment,
		Domain:             req.Domain,
		Status:             "pending_ns",
		VerificationToken:  "", // not needed for v2
		CFZoneID:           &zoneID,
		CFZoneStatus:       "pending",
		CFNameservers:      zone.Nameservers,
		DNSRecordsImported: recordCount > 1, // >1 because CNAME always exists
	}

	if err := h.store.CreateWithCFZone(ctx, domain); err != nil {
		if errors.Is(err, ports.ErrDomainExists) {
			// Rollback zone
			_ = h.cfZone.DeleteZone(ctx, zone.ZoneID)
			httpio.WriteJSON(w, http.StatusConflict, map[string]string{
				"error": "domain already registered",
				"code":  "DOMAIN_ALREADY_EXISTS",
			})
			return
		}
		slog.ErrorContext(ctx, "domain create failed", "error", err)
		_ = h.cfZone.DeleteZone(ctx, zone.ZoneID)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to register domain"})
		return
	}

	metrics.CustomDomainsTotal.WithLabelValues(uc.TenantID, "pending_ns").Inc()

	httpio.WriteJSON(w, http.StatusCreated, domainCreateV2Response{
		OK:                 true,
		ID:                 domain.ID,
		Domain:             domain.Domain,
		Project:            domain.ProjectName,
		App:                domain.AppName,
		Environment:        domain.Environment,
		Status:             domain.Status,
		Nameservers:        zone.Nameservers,
		DNSRecordsImported: recordCount,
		DNSRecords:         dnsRecords,
		ActionRequired:     "Change your nameservers at your DNS provider to activate this domain.",
	})
}

// removeConflictingApexRecords deletes A/AAAA/CNAME records at the apex
// of the zone before our routing CNAME is created. The CF DNS scan may
// import existing A records from registrar parking — those conflict
// with the canonical CNAME @ → cname.dev.loopzero.example required for CF
// proxy + Traefik routing.
func (h *DomainHandler) removeConflictingApexRecords(ctx context.Context, zoneID, domain string) {
	scannedRecords, listErr := h.cfZone.ListDNSRecords(ctx, zoneID)
	if listErr != nil {
		return
	}
	for _, rec := range scannedRecords {
		if (rec.Type == "A" || rec.Type == "AAAA" || rec.Type == "CNAME") &&
			(rec.Name == domain || rec.Name == "@") {
			slog.InfoContext(ctx, "cf removing conflicting apex record",
				"domain", domain, "type", rec.Type, "content", rec.Content)
			_ = h.cfZone.DeleteDNSRecord(ctx, zoneID, rec.ID)
		}
	}
}

// dedupAndListZoneRecords lists the zone's DNS records + removes
// duplicates that CF scan sometimes imports with different quoting.
// Returns the deduplicated record list + count.
func (h *DomainHandler) dedupAndListZoneRecords(ctx context.Context, zoneID, domain string) ([]CFDNSRecord, int) {
	records, listErr := h.cfZone.ListDNSRecords(ctx, zoneID)
	if listErr != nil {
		return nil, 0
	}
	var deduped []CFDNSRecord
	seen := make(map[string]bool)
	for _, rec := range records {
		content := strings.Trim(rec.Content, "\"")
		key := rec.Type + ":" + rec.Name + ":" + content
		if seen[key] {
			slog.InfoContext(ctx, "cf removing duplicate record",
				"domain", domain, "type", rec.Type, "name", rec.Name)
			_ = h.cfZone.DeleteDNSRecord(ctx, zoneID, rec.ID)
			continue
		}
		seen[key] = true
		deduped = append(deduped, rec)
	}
	return deduped, len(deduped)
}
