// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domainshttp

import (
	"context"
	"errors"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/ports"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

// createWithCFZone implements the v2 domain creation flow:
// 1. Create CF zone for the domain
// 2. Scan existing DNS records (email preservation)
// 3. Add CNAME @ → cname.dev.getloopscale.com (proxied, CF flattening)
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
		h.rollBackZone(ctx, zone.ZoneID, req.Domain)
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
		Status:             string(statemachine.DPhPendingNS),
		VerificationToken:  "", // not needed for v2
		CFZoneID:           &zoneID,
		CFZoneStatus:       "pending",
		CFNameservers:      zone.Nameservers,
		DNSRecordsImported: recordCount > 1, // >1 because CNAME always exists
	}

	if err := h.store.CreateWithCFZone(ctx, domain); err != nil {
		if errors.Is(err, ports.ErrDomainExists) {
			h.rollBackZone(ctx, zone.ZoneID, req.Domain)
			httpio.WriteJSON(w, http.StatusConflict, map[string]string{
				"error": "domain already registered",
				"code":  "DOMAIN_ALREADY_EXISTS",
			})
			return
		}
		slog.ErrorContext(ctx, "domain create failed", "error", err)
		h.rollBackZone(ctx, zone.ZoneID, req.Domain)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to register domain"})
		return
	}

	metrics.CustomDomainsTotal.WithLabelValues(uc.TenantID, string(statemachine.DPhPendingNS)).Inc()

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
// with the canonical CNAME @ → cname.dev.getloopscale.com required for CF
// proxy + Traefik routing.
func (h *DomainHandler) removeConflictingApexRecords(ctx context.Context, zoneID, domain string) {
	scannedRecords, listErr := h.cfZone.ListDNSRecords(ctx, zoneID)
	if listErr != nil {
		// The routing record is created next; a conflicting apex record left
		// here makes that create fail, and the request then rolls back.
		slog.WarnContext(ctx, "cf apex record listing failed; conflicting records not removed",
			"domain", domain, "zone_id", zoneID, "error", listErr)
		return
	}
	for _, rec := range scannedRecords {
		if (rec.Type == "A" || rec.Type == "AAAA" || rec.Type == "CNAME") &&
			(rec.Name == domain || rec.Name == "@") {
			slog.InfoContext(ctx, "cf removing conflicting apex record",
				"domain", domain, "type", rec.Type, "content", rec.Content)
			h.deleteRecordCompensating(ctx, zoneID, domain, rec)
		}
	}
}

// dedupAndListZoneRecords lists the zone's DNS records + removes
// duplicates that CF scan sometimes imports with different quoting.
// Returns the deduplicated record list + count.
func (h *DomainHandler) dedupAndListZoneRecords(ctx context.Context, zoneID, domain string) ([]CFDNSRecord, int) {
	records, listErr := h.cfZone.ListDNSRecords(ctx, zoneID)
	if listErr != nil {
		slog.WarnContext(ctx, "cf zone record listing failed; imported records not reported",
			"domain", domain, "zone_id", zoneID, "error", listErr)
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
			h.deleteRecordCompensating(ctx, zoneID, domain, rec)
			continue
		}
		seen[key] = true
		deduped = append(deduped, rec)
	}
	return deduped, len(deduped)
}

// rollBackZone deletes a zone this request created and will not use. When the
// delete fails the zone is orphaned — no domain row points at it — so the
// failure is logged with the zone id an operator needs to remove it, and
// counted so it can be alerted on.
func (h *DomainHandler) rollBackZone(ctx context.Context, zoneID, domain string) {
	if err := h.cfZone.DeleteZone(ctx, zoneID); err != nil {
		metrics.DomainZoneCleanupFailuresTotal.WithLabelValues("delete_zone").Inc()
		slog.ErrorContext(ctx, "cf zone rollback failed; zone orphaned",
			"domain", domain, "zone_id", zoneID, "error", err)
	}
}

// deleteRecordCompensating removes a record the setup does not want (an apex
// conflict or a duplicate). A failure leaves the record in the zone; it is
// logged and counted rather than failing the setup.
func (h *DomainHandler) deleteRecordCompensating(ctx context.Context, zoneID, domain string, rec CFDNSRecord) {
	if err := h.cfZone.DeleteDNSRecord(ctx, zoneID, rec.ID); err != nil {
		metrics.DomainZoneCleanupFailuresTotal.WithLabelValues("delete_record").Inc()
		slog.WarnContext(ctx, "cf record delete failed; record left in zone",
			"domain", domain, "zone_id", zoneID, "record_id", rec.ID, "type", rec.Type, "error", err)
	}
}
